package plugin

import (
	"context"
	"fmt"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	"math"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

// Key idea: use cosine similarity to schedule at beginning while switch to frag later.

// GpuFragSimScorePlugin is a plugin for scheduling framework, scoring pods by GPU fragmentation amount
type GpuFragSimScorePlugin struct {
	cfg         *simontype.GpuPluginCfg
	handle      framework.Handle
	typicalPods *simontype.TargetPodList
	fragGpuRate float64
}

// Just to check whether the implemented struct fits the interface
var _ framework.ScorePlugin = &GpuFragSimScorePlugin{}

func NewGpuFragSimScorePlugin(configuration runtime.Object, handle framework.Handle, typicalPods *simontype.TargetPodList) (framework.Plugin, error) {
	var cfg *simontype.GpuPluginCfg
	if err := frameworkruntime.DecodeInto(configuration, &cfg); err != nil {
		return nil, err
	}

	gpuFragScorePlugin := &GpuFragSimScorePlugin{
		cfg:         cfg,
		handle:      handle,
		typicalPods: typicalPods,
		fragGpuRate: 0.0,
	}
	return gpuFragScorePlugin, nil
}

// Name returns name of the plugin. It is used in logs, etc.
func (plugin *GpuFragSimScorePlugin) Name() string {
	return simontype.GpuFragSimScorePluginName
}

func (plugin *GpuFragSimScorePlugin) PreScore(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodes []*corev1.Node) *framework.Status {
	data := make([]float64, len(utils.FragRatioDataMap))
	clusterFragAmount := utils.NewFragAmount("cluster", data)

	for _, node := range nodes {
		nodeResPtr := utils.GetNodeResourceViaHandle(plugin.handle, node)
		nodeFragAmount := utils.NodeGpuFragAmount(*nodeResPtr, *plugin.typicalPods)
		if err := clusterFragAmount.Add(nodeFragAmount); err != nil {
			return framework.NewStatus(framework.Error, fmt.Sprintf("[ClusterAnalysis] %s\n", err.Error()))
		}
	}

	var gpuFragSum float64
	for _, v := range utils.FragRatioDataMap { // 7 entries
		gpuFragSum += clusterFragAmount.Data[v]
	}

	val := clusterFragAmount.FragAmountSumExceptQ3()
	plugin.fragGpuRate = val / gpuFragSum
	log.Infof("PreScore: frag_gpu_milli=%5.2f%%\n", plugin.fragGpuRate*100)
	return framework.NewStatus(framework.Success)
}

// Score invoked at the score extension point.
func (plugin *GpuFragSimScorePlugin) Score(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
	//fmt.Printf("score_gpu: pod %s/%s, nodeName %s\n", pod.Namespace, pod.Name, nodeName)
	podReq, _ := resourcehelper.PodRequestsAndLimits(pod)
	if len(podReq) == 0 {
		return framework.MaxNodeScore, framework.NewStatus(framework.Success)
	}

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
		return int64(0), framework.NewStatus(framework.Error, fmt.Sprintf("Node (%s) %s does not match GPU type request of pod %s\n", nodeName, nodeRes.Repr(), podRes.Repr()))
	}
	newNodeRes, err := nodeRes.Sub(podRes)
	if err != nil {
		log.Errorf(err.Error())
		return int64(0), framework.NewStatus(framework.Error, fmt.Sprintf("Node (%s) %s does not have sufficient resource for pod (%s) %s\n", nodeName, nodeRes.Repr(), pod.Name, podRes.Repr()))
	}

	//plugin.SetTypicalPods()
	if plugin.typicalPods == nil {
		log.Errorf("typical pods list is empty\n")
		return framework.MinNodeScore, framework.NewStatus(framework.Error, "typical pods list is empty\n")
	}

	// < frag score>
	nodeGpuFrag := utils.NodeGpuFragAmount(nodeRes, *plugin.typicalPods)
	newNodeGpuFrag := utils.NodeGpuFragAmount(newNodeRes, *plugin.typicalPods)
	fragScore := nodeGpuFrag.FragAmountSumExceptQ3() - newNodeGpuFrag.FragAmountSumExceptQ3() // The higher, the better. Negative means fragment amount increases, which is among the worst cases.
	// </frag score>

	// < cosine similarity score>
	cosScore, _, _ := calculateCosineSimilarityScore(nodeRes, podRes, plugin.cfg.DimExtMethod, node)
	// </cosine similarity score>

	score := int64(fragScore*plugin.fragGpuRate + float64(cosScore)*(1.0-plugin.fragGpuRate))
	log.Debugf("[Score][%s] %d = fragScore[%5.2f] * fragGpuRate[%5.2f%%] + cosScore[%d] * (1-fragGpuRate)\n", node.Name, score, fragScore, 100*plugin.fragGpuRate, cosScore)

	return score, framework.NewStatus(framework.Success)
}

// ScoreExtensions of the Score plugin.
func (plugin *GpuFragSimScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return plugin
}

// NormalizeScore invoked after scoring all nodes.
func (plugin *GpuFragSimScorePlugin) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, scores framework.NodeScoreList) *framework.Status {
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
	log.Tracef("[GpuFragScore] [Normalized] highest: %d, lowest: %d\n", highest, lowest)

	// Transform the highest to the lowest score range to fit the framework's min to max node score range.
	oldRange := highest - lowest
	newRange := framework.MaxNodeScore - framework.MinNodeScore
	for i, nodeScore := range scores {
		if oldRange == 0 {
			scores[i].Score = framework.MinNodeScore
		} else {
			scores[i].Score = ((nodeScore.Score - lowest) * newRange / oldRange) + framework.MinNodeScore
		}
		log.Tracef("[GpuFragScore] [Normalized] Node %s, Score: %d\n", scores[i].Name, scores[i].Score)
	}
	return framework.NewStatus(framework.Success)
}
