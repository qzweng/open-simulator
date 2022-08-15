package plugin

import (
	"fmt"
	"math"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

// PreScoreFragGpuRatio return the fragGpuRatio (0.0-1.0) of the current cluster,
//	 i.e., how many of the idle GPUs are fragment.
//   Its output should be cached to plugin.fragGpuRatio to avoid re-computation
func PreScoreFragGpuRatio(nodes []*corev1.Node, handle framework.Handle, typicalPods simontype.TargetPodList) (float64, *framework.Status) {
	data := make([]float64, len(utils.FragRatioDataMap))
	clusterFragAmount := utils.NewFragAmount("cluster", data)

	for _, node := range nodes {
		nodeResPtr := utils.GetNodeResourceViaHandle(handle, node)
		nodeFragAmount := utils.NodeGpuFragAmount(*nodeResPtr, typicalPods)
		if err := clusterFragAmount.Add(nodeFragAmount); err != nil {
			return 0.0, framework.NewStatus(framework.Error, fmt.Sprintf("[ClusterAnalysis] %s\n", err.Error()))
		}
	}

	var idleGpuMilli float64
	for _, v := range utils.FragRatioDataMap { // 7 entries
		idleGpuMilli += clusterFragAmount.Data[v]
	}

	val := clusterFragAmount.FragAmountSumExceptQ3()
	fragGpuRatio := val / idleGpuMilli
	log.Infof("PreScore: frag_gpu_ratio=%5.2f%%\n", fragGpuRatio*100)
	return fragGpuRatio, framework.NewStatus(framework.Success)
}

// NormalizeScore in Score Extension.
//   Reused by all plugins whose scores might go beyond 0(framework.MinNodeScore)--100(framework.MaxNodeScore)
func NormalizeScore(scores framework.NodeScoreList) *framework.Status {
	// Find highest and lowest scores.
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
	log.Tracef("[Normalized] highest: %d, lowest: %d\n", highest, lowest)

	// Transform the highest to the lowest score range to fit the framework's min to max node score range.
	oldRange := highest - lowest
	newRange := framework.MaxNodeScore - framework.MinNodeScore
	for i, nodeScore := range scores {
		if oldRange == 0 {
			scores[i].Score = framework.MinNodeScore
		} else {
			scores[i].Score = ((nodeScore.Score - lowest) * newRange / oldRange) + framework.MinNodeScore
		}
		log.Tracef("[Normalized] Node %s, Score: %d\n", scores[i].Name, scores[i].Score)
	}
	return framework.NewStatus(framework.Success)
}
