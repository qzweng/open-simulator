package plugin

import (
	"context"
	"fmt"
	"math"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

type BestFitScorePlugin struct {
	handle framework.Handle
}

var _ framework.ScorePlugin = &BestFitScorePlugin{}

func NewBestFitScorePlugin(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &BestFitScorePlugin{
		handle: handle,
	}, nil
}

func (bfs *BestFitScorePlugin) Name() string {
	return simontype.BestFitScorePluginName
}

func (bfs *BestFitScorePlugin) Score(ctx context.Context, state *framework.CycleState,
	p *corev1.Pod, nodeName string) (int64, *framework.Status) {

	node, err := bfs.handle.ClientSet().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return framework.MinNodeScore,
			framework.NewStatus(framework.Error, fmt.Sprintf("failed to get node(%s): %s\n", nodeName, err.Error()))
	}

	nodeResPtr := utils.GetNodeResourceViaHandle(bfs.handle, node)
	if nodeResPtr == nil {
		return framework.MinNodeScore,
			framework.NewStatus(framework.Error, fmt.Sprintf("failed to get nodeRes(%s)\n", nodeName))
	}
	nodeRes := *nodeResPtr
	podRes := utils.GetPodResource(p)

	score := getBestFitScore(nodeRes, podRes)
	if score == -1 {
		return framework.MinNodeScore, framework.NewStatus(framework.Error,
			fmt.Sprintf("the score between node(%s) and pod(%s) is negative, should not happen\n", nodeName, utils.GeneratePodKey(p)))
	}
	return -score, framework.NewStatus(framework.Success)
}

func (bfs *BestFitScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return bfs
}

func (bfs *BestFitScorePlugin) NormalizeScore(ctx context.Context, state *framework.CycleState,
	p *corev1.Pod, scores framework.NodeScoreList) *framework.Status {

	// find highest and lowest scores
	var highest int64 = -math.MaxInt64
	var lowest int64 = math.MaxInt64
	for _, nodeScore := range scores {
		if nodeScore.Score > highest {
			highest = nodeScore.Score
		}
		if nodeScore.Score < lowest {
			lowest = nodeScore.Score
		}
	}
	log.Tracef("[BestFitScore] [Normalized] highest: %d, lowest: %d\n", highest, lowest)

	// transform the highest to the lowest score range to fit the framework's min to max node score range
	oldRange := highest - lowest
	newRange := framework.MaxNodeScore - framework.MinNodeScore
	for i, nodeScore := range scores {
		if oldRange == 0 {
			scores[i].Score = framework.MinNodeScore
		} else {
			scores[i].Score = ((nodeScore.Score - lowest) * newRange / oldRange) + framework.MinNodeScore
		}
		log.Tracef("[BestFitScore] [Normalized] Node %s, Score: %d\n", scores[i].Name, scores[i].Score)
	}
	return framework.NewStatus(framework.Success)
}

// BestFit assigns a score Σ_{i} weights_{i} (free_{i} - request_{i}),
// where i corresponds to one kind of resource, lower is better
func getBestFitScore(nodeRes simontype.NodeResource, podRes simontype.PodResource) int64 {
	freeVec := nodeRes.ToResourceVec()
	reqVec := podRes.ToResourceVec()
	weights := []float64{1, 100} // cpu, gpu memory
	if len(freeVec) != len(weights) || len(reqVec) != len(weights) {
		log.Errorf("length not equal, freeVec(%v), reqVec(%v), weights(%v)\n", freeVec, reqVec, weights)
		return -1
	}

	var score float64 = 0
	for i := 0; i < len(freeVec); i++ {
		if freeVec[i] < reqVec[i] {
			log.Errorf("free resource not enough, freeVec(%v), reqVec(%v), weights(%v)\n", freeVec, reqVec, weights)
			return -1
		}
		score += (freeVec[i] - reqVec[i]) * weights[i]
	}
	log.Debugf("[BestFitScore] score(%.4f), freeVec(%v), reqVec(%v), weights(%v)\n",
		score, freeVec, reqVec, weights)
	return int64(score)
}
