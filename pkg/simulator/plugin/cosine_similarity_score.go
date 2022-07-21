package plugin

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

type CosineSimilarityPlugin struct {
	cfg    *simontype.GpuPluginCfg
	handle framework.Handle
}

var _ framework.ScorePlugin = &CosineSimilarityPlugin{}

func NewCosineSimilarityPlugin(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	var cfg *simontype.GpuPluginCfg
	if err := frameworkruntime.DecodeInto(configuration, &cfg); err != nil {
		return nil, err
	}

	plugin := &CosineSimilarityPlugin{
		cfg:    cfg,
		handle: handle,
	}
	allocateGpuIdFunc[plugin.Name()] = allocateGpuIdBasedOnCosineSimilarity
	return plugin, nil
}

func (plugin *CosineSimilarityPlugin) Name() string {
	return simontype.CosineSimilarityPluginName
}

func (plugin *CosineSimilarityPlugin) Score(ctx context.Context, state *framework.CycleState,
	p *v1.Pod, nodeName string) (int64, *framework.Status) {

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

	score, _, _ := calculateCosineSimilarityScore(nodeRes, podRes, plugin.cfg.DimExtMethod, node)
	return score, framework.NewStatus(framework.Success)
}

func (plugin *CosineSimilarityPlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

func calculateCosineSimilarityScore(nodeRes simontype.NodeResource, podRes simontype.PodResource,
	method simontype.GpuDimExtMethod, node *v1.Node) (int64, []float64, []float64) {

	var score float64 = -1
	var matchedNodeVec []float64
	var matchedPodVec []float64

	nodeVecList := utils.GetNormalizedNodeVecListAfterDimExt(method, nodeRes, podRes, node)
	podVecList := utils.GetNormalizedPodVecListAfterDimExt(method, nodeRes, podRes, node)

	for _, nodeVec := range nodeVecList {
		for _, podVec := range podVecList {
			curScore := utils.CalculateVectorCosineSimilarity(nodeVec, podVec)
			if curScore == -1 {
				continue
			}

			log.Tracef("cosine similarity score between nodeVec(%v) and podVec(%v): %.4f\n",
				nodeVec, podVec, curScore)

			if score < curScore {
				score = curScore
				matchedNodeVec = make([]float64, len(nodeVec))
				copy(matchedNodeVec, nodeVec)
				matchedPodVec = make([]float64, len(podVec))
				copy(matchedPodVec, podVec)
			}
		}
	}

	if len(matchedNodeVec) == 0 || len(matchedPodVec) == 0 {
		panic(fmt.Sprintf("failed to match any nodeVec or podVec, nodeVecList(%v), podVecList(%v)", nodeVecList, podVecList))
	}

	if score == -1 {
		return framework.MinNodeScore, nil, nil
	}

	return int64(float64(framework.MaxNodeScore) * score), matchedNodeVec, matchedPodVec
}

func allocateGpuIdBasedOnCosineSimilarity(nodeRes simontype.NodeResource, podRes simontype.PodResource,
	method simontype.GpuDimExtMethod, node *v1.Node) (gpuId string) {

	_, nodeVec, podVec := calculateCosineSimilarityScore(nodeRes, podRes, method, node)
	return utils.ConvertMatchedVecToGpuId(nodeVec, podVec, nodeRes, podRes, method)
}
