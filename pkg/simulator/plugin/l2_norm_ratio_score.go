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

type L2NormRatioScorePlugin struct {
	handle framework.Handle
}

var _ framework.ScorePlugin = &L2NormRatioScorePlugin{}

func NewL2NormRatioScorePlugin(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &L2NormRatioScorePlugin{
		handle: handle,
	}, nil
}

func (plugin *L2NormRatioScorePlugin) Name() string {
	return simontype.L2NormRatioScorePluginName
}

func (plugin *L2NormRatioScorePlugin) Score(ctx context.Context, state *framework.CycleState,
	p *corev1.Pod, nodeName string) (int64, *framework.Status) {

	node, err := plugin.handle.ClientSet().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error,
			fmt.Sprintf("failed to get node(%s): %v", nodeName, err))
	}

	nodeResPtr := utils.GetNodeResourceViaHandle(plugin.handle, node)
	if nodeResPtr == nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error,
			fmt.Sprintf("failed to get nodeRes(%s)\n", nodeName))
	}
	nodeRes := *nodeResPtr
	podRes := utils.GetPodResource(p)

	nodeCap := utils.GetNodeAllocatableCpuGpu(node)
	nodeVec := utils.NormalizeVector(nodeRes.ToResourceVec(), nodeCap)
	podVec := utils.NormalizeVector(podRes.ToResourceVec(), nodeCap)

	score := utils.CalculateL2NormRatio(podVec, nodeVec) // order matters: podVec / nodeVec < 1. The larger the norm ratio, the higher the score
	log.Tracef("L2 Norm Diff score between nodeRes(%s) and podRes(%s) with nodeCap(%v): %.4f\n",
		nodeRes.Repr(), podRes.Repr(), nodeCap, score)
	if score == -1 {
		return framework.MinNodeScore, framework.NewStatus(framework.Success)
	}
	score /= float64(len(podVec)) // normalize score to [0, 1]
	return int64(float64(framework.MaxNodeScore) * score), framework.NewStatus(framework.Success)
}

func (plugin *L2NormRatioScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}
