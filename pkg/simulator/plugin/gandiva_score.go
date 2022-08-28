package plugin

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
	"github.com/alibaba/open-simulator/pkg/utils"
)

type GandivaScorePlugin struct {
	handle framework.Handle
}

var _ framework.ScorePlugin = &GandivaScorePlugin{}

func NewGandivaScorePlugin(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &GandivaScorePlugin{
		handle: handle,
	}, nil
}

func (plugin *GandivaScorePlugin) Name() string {
	return simontype.GandivaScorePluginName
}

func (plugin *GandivaScorePlugin) Score(_ context.Context, _ *framework.CycleState, p *v1.Pod, nodeName string) (int64, *framework.Status) {
	nodeResPtr, podResPtr := utils.GetNodeResourceAndPodResourceViaHandle(p, nodeName, plugin.handle)
	if nodeResPtr == nil || podResPtr == nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get nodeRes or podRes"))
	}

	// 100: Allocated machine with sufficient GPUs and same affinity
	podGpuAffinity := gpushareutils.GetGpuAffinityFromPodAnnotation(p)
	if nodeResPtr.GpuAffinity[podGpuAffinity] > 0 {
		return framework.MaxNodeScore, framework.NewStatus(framework.Success)
	}
	// 50: Idle machine with no affinity
	if len(nodeResPtr.GpuAffinity) == 0 {
		return framework.MaxNodeScore / 2, framework.NewStatus(framework.Success)
	}
	// 0: Allocated machine with sufficient GPUs and different affinity
	return framework.MinNodeScore, framework.NewStatus(framework.Success)
}

func (plugin *GandivaScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}
