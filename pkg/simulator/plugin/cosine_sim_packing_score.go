package plugin

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

type CosineSimPackingPlugin struct {
	cfg    *simontype.GpuPluginCfg
	handle framework.Handle
}

var _ framework.ScorePlugin = &CosineSimPackingPlugin{}

func NewCosineSimPackingPlugin(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	var cfg *simontype.GpuPluginCfg
	if err := frameworkruntime.DecodeInto(configuration, &cfg); err != nil {
		return nil, err
	}

	plugin := &CosineSimPackingPlugin{
		cfg:    cfg,
		handle: handle,
	}
	allocateGpuIdFunc[plugin.Name()] = allocateGpuIdBasedOnCosineSimilarity
	return plugin, nil
}

func (plugin *CosineSimPackingPlugin) Name() string {
	return simontype.CosineSimPackingPluginName
}

func (plugin *CosineSimPackingPlugin) Score(ctx context.Context, state *framework.CycleState,
	p *v1.Pod, nodeName string) (int64, *framework.Status) {

	node, err := plugin.handle.ClientSet().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get node(%s): %s", nodeName, err.Error()))
	}

	nodeResPtr := utils.GetNodeResourceViaHandle(plugin.handle, node)
	if nodeResPtr == nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get nodeRes(%s)\n", nodeName))
	}
	nodeRes := *nodeResPtr
	podRes := utils.GetPodResource(p)

	// Cosine Similarity Score
	scoreCosSim, _, _ := calculateCosineSimilarityScore(nodeRes, podRes, plugin.cfg.DimExtMethod, node)
	similarity := float64(scoreCosSim) / float64(framework.MaxNodeScore) // range: 0-1

	// Packing Score
	scorePack, _ := getPackingScore(podRes, nodeRes)

	// Combine two scores with multiplication
	score := int64(similarity * float64(scorePack))

	return score, framework.NewStatus(framework.Success)
}

func (plugin *CosineSimPackingPlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}
