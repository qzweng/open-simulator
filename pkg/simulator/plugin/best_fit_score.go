package plugin

import (
	"context"
	"fmt"

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

func (plugin *BestFitScorePlugin) Name() string {
	return simontype.BestFitScorePluginName
}

func (plugin *BestFitScorePlugin) Score(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
	// < common procedure that prepares node, podRes, nodeRes>
	node, err := plugin.handle.ClientSet().CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get node %s: %s\n", nodeName, err.Error()))
	}

	nodeResPtr := utils.GetNodeResourceViaHandle(plugin.handle, node)
	if nodeResPtr == nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get nodeRes(%s)\n", nodeName))
	}
	nodeRes := *nodeResPtr

	podRes := utils.GetPodResource(pod)
	if !utils.IsNodeAccessibleToPod(nodeRes, podRes) {
		log.Errorf("Node (%s) %s does not match GPU type request of pod %s. Should be filtered by GpuSharePlugin", nodeName, nodeRes.Repr(), podRes.Repr())
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("Node (%s) %s does not match GPU type request of pod %s\n", nodeName, nodeRes.Repr(), podRes.Repr()))
	}
	// </common procedure that prepares node, podRes, nodeRes>

	score := getBestFitScore(nodeRes, podRes)
	if score == -1 {
		return framework.MinNodeScore, framework.NewStatus(framework.Error,
			fmt.Sprintf("the score between node(%s) and pod(%s) is negative, should not happen\n", nodeName, utils.GeneratePodKey(pod)))
	}
	return -score, framework.NewStatus(framework.Success)
}

func (plugin *BestFitScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return plugin
}

func (plugin *BestFitScorePlugin) NormalizeScore(ctx context.Context, state *framework.CycleState, p *corev1.Pod, scores framework.NodeScoreList) *framework.Status {
	return NormalizeScore(scores)
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
