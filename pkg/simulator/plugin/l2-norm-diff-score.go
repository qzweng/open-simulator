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

type L2NormDiffScorePlugin struct {
	handle framework.Handle
}

var _ framework.ScorePlugin = &L2NormDiffScorePlugin{}

func NewL2NormDiffScorePlugin(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &L2NormDiffScorePlugin{
		handle: handle,
	}, nil
}

func (lsp *L2NormDiffScorePlugin) Name() string {
	return simontype.L2NormDiffScorePluginName
}

func (lsp *L2NormDiffScorePlugin) Score(ctx context.Context, state *framework.CycleState,
	p *corev1.Pod, nodeName string) (int64, *framework.Status) {

	node, err := lsp.handle.ClientSet().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error,
			fmt.Sprintf("failed to get node(%s): %v", nodeName, err))
	}

	nodeResPtr := utils.GetNodeResourceViaHandle(lsp.handle, node)
	if nodeResPtr == nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error,
			fmt.Sprintf("failed to get nodeRes(%s)\n", nodeName))
	}
	nodeRes := *nodeResPtr
	podRes := utils.GetPodResource(p)

	nodeCap := utils.GetNodeAllocatableCpuGpu(node)
	nodeVec := utils.NormalizeVector(nodeRes.ToResourceVec(), nodeCap)
	podVec := utils.NormalizeVector(podRes.ToResourceVec(), nodeCap)

	score := utils.CalculateL2NormDiff(nodeVec, podVec)
	log.Tracef("L2 Norm Diff score between nodeRes(%s) and podRes(%s) with nodeCap(%v): %.4f\n",
		nodeRes.Repr(), podRes.Repr(), nodeCap, score)
	if score == -1 {
		return framework.MinNodeScore, framework.NewStatus(framework.Success)
	}
	score /= float64(len(podVec)) // normalize score to [0, 1]
	return int64(float64(framework.MaxNodeScore) * score), framework.NewStatus(framework.Success)
}

func (lsp *L2NormDiffScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}