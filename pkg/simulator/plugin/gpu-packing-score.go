package plugin

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/utils/integer"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
	"github.com/alibaba/open-simulator/pkg/utils"
)

type GpuPackingScorePlugin struct {
	handle framework.Handle
}

var _ framework.ScorePlugin = &GpuPackingScorePlugin{}

func NewGpuPackingScorePlugin(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	gpuPackingScorePlugin := &GpuPackingScorePlugin{
		handle: handle,
	}
	return gpuPackingScorePlugin, nil
}

func (plugin *GpuPackingScorePlugin) Name() string {
	return simontype.GpuPackingScorePluginName
}

func (plugin *GpuPackingScorePlugin) Score(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
	podGpuMilli := gpushareutils.GetGpuMilliFromPodAnnotation(pod)
	if podGpuMilli <= 0 {
		return framework.MinNodeScore, framework.NewStatus(framework.Success)
	}

	node, err := plugin.handle.ClientSet().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get node(%s): %s\n", nodeName, err.Error()))
	}

	nodeResPtr := utils.GetNodeResourceViaHandle(plugin.handle, node)
	if nodeResPtr == nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get nodeRes(%s)\n", nodeName))
	}
	nodeRes := *nodeResPtr

	podRes := utils.GetPodResource(pod)
	_, err = nodeRes.Sub(podRes)
	if err != nil {
		log.Errorf("failed to schedule pod(%s) on node(%s)\n", utils.GeneratePodKey(pod), node.Name)
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to schedule pod(%s) on node(%s) because of insufficient resources\n", utils.GeneratePodKey(pod), nodeName))
	}

	score, err := getPackingScore(podRes, nodeRes)
	if err != nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to schedule pod(%s) on node(%s)", utils.GeneratePodKey(pod), nodeName))
	}

	return score, framework.NewStatus(framework.Success)
}

func (plugin *GpuPackingScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// score rule, from high to low:
//     case-1. use shared GPUs: return maxNodeScore - freeGPUMemRatioOnUsedGpu/10, capped in the range [maxNodeScore/2, maxNodeScore]
//     case-2. use free GPUs on a used node: return maxNodeScore/2 - fullyFreeGpuNumToUse, capped in the range [maxNodeScore/3, maxNodeScore/2]
//     case-3. use free GPUs on a free node: return maxNodeScore/3 - freeGpuNum, capped in the range [minNodeScore, maxNodeScore/3]
func getPackingScore(podRes simontype.PodResource, nodeRes simontype.NodeResource) (int64, error) {
	fullyFreeGpuNum := nodeRes.GetFullyFreeGpuNum()

	// case-3: all gpus on the node are free
	if fullyFreeGpuNum == nodeRes.GpuNumber {
		score := framework.MaxNodeScore/3 - int64(fullyFreeGpuNum)
		cappedScore := integer.Int64Max(score, int64(fullyFreeGpuNum))
		return cappedScore, nil
	}

	sortedIndex := nodeRes.SortedMilliGpuLeftIndexList(true) // minimum GPU left first
	var gpuReq = podRes.GpuNumber
	var fullyFreeGpuNumToUse = 0
	var gpuToUse []int
	for _, idx := range sortedIndex {
		if gpuReq == 0 {
			break
		}
		gpuMilliLeft := nodeRes.MilliGpuLeftList[idx]
		if podRes.MilliGpu <= gpuMilliLeft {
			gpuReq--
			gpuToUse = append(gpuToUse, idx)
			if gpuMilliLeft == gpushareutils.MILLI {
				fullyFreeGpuNumToUse++
			}
		}
	}
	if gpuReq != 0 {
		return framework.MinNodeScore, fmt.Errorf("failed to allocate gpu resource to pod(%s) on node(%s)\n",
			podRes.Repr(), nodeRes.Repr())
	}
	// case-2: have to use fully-free gpus
	if fullyFreeGpuNumToUse > 0 {
		score := framework.MaxNodeScore/2 - int64(fullyFreeGpuNumToUse)
		cappedScore := integer.Int64Max(score, framework.MaxNodeScore/3)
		return cappedScore, nil
	}

	// case-1: use shared gpus
	var freeGpuRatioOnUsedGpu int64 = 0
	for _, gpu := range gpuToUse {
		freeGpuRatioOnUsedGpu += nodeRes.MilliGpuLeftList[gpu] * 100 / gpushareutils.MILLI
	}
	score := framework.MaxNodeScore - freeGpuRatioOnUsedGpu/10
	cappedScore := integer.Int64Max(score, framework.MaxNodeScore/2)
	return cappedScore, nil
}
