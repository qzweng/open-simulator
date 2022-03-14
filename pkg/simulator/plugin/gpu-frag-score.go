package plugin

import (
	"context"
	"fmt"
	"math"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	externalclientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

// GpuFragScorePlugin is a plugin for scheduling framework, scoring pods by GPU fragmentation amount
type GpuFragScorePlugin struct {
	fakeclient externalclientset.Interface
}

// Just to check whether the implemented struct fits the interface
var _ framework.ScorePlugin = &GpuFragScorePlugin{}

func NewGpuFragScorePlugin(fakeclient externalclientset.Interface, configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
	gpuFragScorePlugin := &GpuFragScorePlugin{
		fakeclient: fakeclient,
	}
	return gpuFragScorePlugin, nil
}

// Name returns name of the plugin. It is used in logs, etc.
func (plugin *GpuFragScorePlugin) Name() string {
	return simontype.GpuFragScorePluginName
}

// Score invoked at the score extension point.
func (plugin *GpuFragScorePlugin) Score(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
	//fmt.Printf("score_gpu: pod %s/%s, nodeName %s\n", pod.Namespace, pod.Name, nodeName)
	podReq, _ := resourcehelper.PodRequestsAndLimits(pod)
	if len(podReq) == 0 {
		return framework.MaxNodeScore, framework.NewStatus(framework.Success)
	}

	node, err := plugin.fakeclient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return int64(framework.MinNodeScore), framework.NewStatus(framework.Error, fmt.Sprintf("failed to get node %s: %s\n", nodeName, err.Error()))
	}

	nodeRes, err := GetNodeResourceViaClient(plugin.fakeclient, ctx, node)
	if err != nil {
		return int64(framework.MinNodeScore), framework.NewStatus(framework.Error, fmt.Sprintf("failed to get nodeRes %s: %s\n", nodeName, err.Error()))
	}

	podRes := utils.GetTargetPodResource(pod)
	if !utils.IsNodeAccessibleToPod(nodeRes, podRes) {
		klog.Error("Node %s does not match GPU type request of pod %s. Should be filtered by GpuSharePlugin", nodeRes.Repr(), podRes.Repr())
		return int64(0), framework.NewStatus(framework.Success)
	}
	newNodeRes, err := nodeRes.Sub(podRes)
	if err != nil {
		klog.Errorf(err.Error())
		return int64(0), framework.NewStatus(framework.Success)
	}

	allocatablePodList := utils.GetAllocatablePodList(plugin.fakeclient)
	/* Transfer []*corev1.Pod => []Pod, since
	   Pods in ResourceTypes is []*corev1.Pod
	   PodList.Items returned by clientset.CoreV1().Pods.List() is []Pod
	*/
	podList := make([]*corev1.Pod, len(allocatablePodList))
	for i := 0; i < len(allocatablePodList); i++ {
		podList[i] = &allocatablePodList[i]
	}
	targetPodList := utils.GetTypicalPods(podList)

	nodeGpuFrag := utils.NodeGpuFragAmount(nodeRes, targetPodList)
	newNodeGpuFrag := utils.NodeGpuFragAmount(newNodeRes, targetPodList)

	score := int64(float64(framework.MaxNodeScore-framework.MinNodeScore) *
		(nodeGpuFrag.FragAmountSumExceptQ3MiB() - newNodeGpuFrag.FragAmountSumExceptQ3MiB())) // could be negative

	fmt.Printf("[GpuFragScore] Place Pod %s: %s\n"+
		"  [NodeRes]  %s \n"+
		"          => %s\n"+
		"  [NodeFrag] %s (%.1f)\n"+
		"          => %s (%.1f)\n"+
		"  [Score] Delta = %d\n",
		pod.Name, podRes.Repr(),
		nodeRes.Repr(), newNodeRes.Repr(),
		nodeGpuFrag.Repr(), nodeGpuFrag.FragAmountSumExceptQ3MiB(),
		newNodeGpuFrag.Repr(), newNodeGpuFrag.FragAmountSumExceptQ3MiB(),
		score)
	return score, framework.NewStatus(framework.Success)
}

// ScoreExtensions of the Score plugin.
func (plugin *GpuFragScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return plugin
}

// NormalizeScore invoked after scoring all nodes.
func (plugin *GpuFragScorePlugin) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, scores framework.NodeScoreList) *framework.Status {
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
	fmt.Printf("[GpuFragScore] [Normalized] highest: %d, lowest: %d\n", highest, lowest)

	// Transform the highest to the lowest score range to fit the framework's min to max node score range.
	oldRange := highest - lowest
	newRange := framework.MaxNodeScore - framework.MinNodeScore
	for i, nodeScore := range scores {
		if oldRange == 0 {
			scores[i].Score = framework.MinNodeScore
		} else {
			scores[i].Score = ((nodeScore.Score - lowest) * newRange / oldRange) + framework.MinNodeScore
		}
		fmt.Printf("[GpuFragScore] [Normalized] Node %s, Score: %d\n", scores[i].Name, scores[i].Score)
	}
	return framework.NewStatus(framework.Success)
}
