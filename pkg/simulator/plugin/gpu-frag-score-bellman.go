package plugin

import (
	"context"
	"fmt"
	"math"
	"sync"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

// GpuFragScoreBellmanPlugin is a plugin for scheduling framework, scoring pods by GPU fragmentation amount
type GpuFragScoreBellmanPlugin struct {
	handle        framework.Handle
	typicalPods   *simontype.TargetPodList
	fragRatioMemo *sync.Map
	sync.RWMutex
}

// Just to check whether the implemented struct fits the interface
var _ framework.ScorePlugin = &GpuFragScoreBellmanPlugin{}

func NewGpuFragScoreBellmanPlugin(configuration runtime.Object, handle framework.Handle, typicalPods *simontype.TargetPodList, fragRatioMemo *sync.Map) (framework.Plugin, error) {
	gpuFragScorePlugin := &GpuFragScoreBellmanPlugin{
		handle:        handle,
		typicalPods:   typicalPods,
		fragRatioMemo: fragRatioMemo,
	}
	return gpuFragScorePlugin, nil
}

// Name returns name of the plugin. It is used in logs, etc.
func (plugin *GpuFragScoreBellmanPlugin) Name() string {
	return simontype.GpuFragScoreBellmanPluginName
}

// Score invoked at the score extension point.
func (plugin *GpuFragScoreBellmanPlugin) Score(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
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
		return framework.MinNodeScore, framework.NewStatus(framework.Error, fmt.Sprintf("typical pods list is empty\n"))
	}

	// Key difference between Gpu-Frag-Score and Gpu-Frag-Score-Bellman:
	///*
	plugin.Lock()
	defer plugin.Unlock()
	nodeGpuFragValue := utils.NodeGpuFragBellman(nodeRes, *plugin.typicalPods, plugin.fragRatioMemo, 1.0)
	newNodeGpuFragValue := utils.NodeGpuFragBellman(newNodeRes, *plugin.typicalPods, plugin.fragRatioMemo, 1.0)
	//*/
	//nodeGpuFragValue := utils.NodeGpuFragBellmanEarlyStop(nodeRes, *plugin.typicalPods, plugin.fragRatioMemo)
	//newNodeGpuFragValue := utils.NodeGpuFragBellmanEarlyStop(newNodeRes, *plugin.typicalPods, plugin.fragRatioMemo)

	score := int64(nodeGpuFragValue - newNodeGpuFragValue) // The higher, the better. Negative means fragment amount increases, which is among the worst cases.
	return score, framework.NewStatus(framework.Success)
}

// ScoreExtensions of the Score plugin.
func (plugin *GpuFragScoreBellmanPlugin) ScoreExtensions() framework.ScoreExtensions {
	return plugin
}

// NormalizeScore invoked after scoring all nodes.
func (plugin *GpuFragScoreBellmanPlugin) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, scores framework.NodeScoreList) *framework.Status {
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
