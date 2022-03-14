package plugin

import (
	"context"
	"fmt"
	"sort"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	externalclientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/utils/integer"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/pkg/utils"
	"github.com/alibaba/open-simulator/pkg/utils"
)

type GpuPackingScorePlugin struct {
	fakeclient externalclientset.Interface
}

var _ framework.ScorePlugin = &GpuPackingScorePlugin{}

func NewGpuPackingScorePlugin(fakeclient externalclientset.Interface, configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
	gpuPackingScorePlugin := &GpuPackingScorePlugin{
		fakeclient: fakeclient,
	}
	return gpuPackingScorePlugin, nil
}

func (plugin *GpuPackingScorePlugin) Name() string {
	return simontype.GpuPackingScorePluginName
}

func (plugin *GpuPackingScorePlugin) Score(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
	podGpuMem := gpushareutils.GetGpuMemoryFromPodAnnotation(pod)
	if podGpuMem <= 0 {
		return framework.MinNodeScore, framework.NewStatus(framework.Success)
	}

	node, err := plugin.fakeclient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get node(%s): %s\n", nodeName, err.Error()))
	}

	nodeRes, err := GetNodeResourceViaClient(plugin.fakeclient, ctx, node)
	if err != nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get nodeRes(%s): %s\n", nodeName, err.Error()))
	}

	podRes := utils.GetTargetPodResource(pod)
	_, err = nodeRes.Sub(podRes)
	if err != nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Unschedulable, fmt.Sprintf("failed to schedule pod(%s) on node(%s) because of insufficient resources\n", utils.GeneratePodKey(pod), nodeName))
	}

	score := getPackingScore(podRes, nodeRes)

	return score, framework.NewStatus(framework.Success)
}

func (plugin *GpuPackingScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// score rule, from high to low:
//     case-1. use shared GPUs: return maxNodeScore - freeGPUMemRatioOnUsedGpu/10, capped in the range [maxNodeScore/2, maxNodeScore]
//     case-2. use free GPUs on a used node: return maxNodeScore/2 - fullyFreeGpuNumToUse, capped in the range [maxNodeScore/3, maxNodeScore/2]
//     case-3. use free GPUs on a free node: return maxNodeScore/3 - freeGpuNum, capped in the range [minNodeScore, maxNodeScore/3]
func getPackingScore(podRes simontype.TargetPodResource, nodeRes simontype.TargetNodeResource) int64 {
	var fullyFreeGpuNum = 0
	for _, gpuMemLeft := range nodeRes.GpuMemLeftList {
		if gpuMemLeft == nodeRes.GpuMemTotal {
			fullyFreeGpuNum++
		}
	}

	// case-3: all gpus on the node are free
	if fullyFreeGpuNum == nodeRes.GpuNumber {
		score := framework.MaxNodeScore/3 - int64(fullyFreeGpuNum)
		cappedScore := integer.Int64Max(score, int64(fullyFreeGpuNum))
		return cappedScore
	}

	sort.SliceStable(nodeRes.GpuMemLeftList, func(i, j int) bool {
		return nodeRes.GpuMemLeftList[i] < nodeRes.GpuMemLeftList[j]
	})
	var gpuReq = podRes.GpuNumber
	var fullyFreeGpuNumToUse = 0
	var gpuToUse []int
	for idx, gpuMemLeft := range nodeRes.GpuMemLeftList {
		if gpuReq == 0 {
			break
		}
		if podRes.GpuMemory <= gpuMemLeft {
			gpuReq--
			gpuToUse = append(gpuToUse, idx)
			if gpuMemLeft == nodeRes.GpuMemTotal {
				fullyFreeGpuNumToUse++
			}
		}
	}
	if gpuReq != 0 {
		log.Errorf("failed to allocate gpu resource to pod(%s) on node(%s)\n",
			utils.GeneratePodKeyByName(podRes.Namespace, podRes.Name), nodeRes.NodeName)
		return framework.MinNodeScore
	}
	// case-2: have to use fully-free gpus
	if fullyFreeGpuNumToUse > 0 {
		score := framework.MaxNodeScore/2 - int64(fullyFreeGpuNumToUse)
		cappedScore := integer.Int64Max(score, framework.MaxNodeScore/3)
		return cappedScore
	}

	// case-1: use shared gpus
	var freeGpuMemRatioOnUsedGpu int64 = 0
	for _, gpu := range gpuToUse {
		freeGpuMemRatioOnUsedGpu += nodeRes.GpuMemLeftList[gpu] * 100 / nodeRes.GpuMemTotal
	}
	score := framework.MaxNodeScore - freeGpuMemRatioOnUsedGpu/10
	cappedScore := integer.Int64Max(score, framework.MaxNodeScore/2)
	return cappedScore
}
