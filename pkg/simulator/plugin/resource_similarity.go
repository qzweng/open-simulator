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

type ResourceSimilarityPlugin struct {
	handle framework.Handle
}

var _ framework.ScorePlugin = &GpuPackingScorePlugin{}

func NewResourceSimilarityPlugin(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &ResourceSimilarityPlugin{
		handle: handle,
	}, nil
}

func (rsp *ResourceSimilarityPlugin) Name() string {
	return simontype.ResourceSimilarityPluginName
}

func (rsp *ResourceSimilarityPlugin) Score(ctx context.Context, state *framework.CycleState,
	p *corev1.Pod, nodeName string) (int64, *framework.Status) {

	node, err := rsp.handle.ClientSet().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error,
			fmt.Sprintf("failed to get node(%s): %v", nodeName, err))
	}

	nodeResPtr := utils.GetNodeResourceViaHandle(rsp.handle, node)
	if nodeResPtr == nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error,
			fmt.Sprintf("failed to get nodeRes(%s)\n", nodeName))
	}
	nodeRes := *nodeResPtr
	podRes := utils.GetPodResource(p)

	score := utils.GetResourceSimilarity(nodeRes, podRes)
	log.Tracef("resource similarity score between nodeRes(%s) and podRes(%s): %.4f\n",
		nodeRes.Repr(), podRes.Repr(), score)
	if score == -1 {
		// score should be in the range of [0,1]
		return framework.MinNodeScore, framework.NewStatus(framework.Success)
	}
	return int64(float64(framework.MaxNodeScore) * score), framework.NewStatus(framework.Success)
}

func (rsp *ResourceSimilarityPlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}
