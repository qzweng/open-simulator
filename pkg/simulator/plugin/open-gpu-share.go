package plugin

import (
	"context"
	"fmt"
	"math"

	gpusharecache "github.com/alibaba/open-gpu-share/pkg/cache"
	gpushareutils "github.com/alibaba/open-gpu-share/pkg/utils"
	"github.com/alibaba/open-simulator/pkg/algo"
	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/pquerna/ffjson/ffjson"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	externalclientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// GpuSharePlugin is a plugin for scheduling framework
type GpuSharePlugin struct {
	fakeclient externalclientset.Interface
	cache      *gpusharecache.SchedulerCache
}

// Just to check whether the implemented struct fits the interface
var _ framework.FilterPlugin = &GpuSharePlugin{}
var _ framework.ScorePlugin = &GpuSharePlugin{}
var _ framework.ReservePlugin = &GpuSharePlugin{}

//var _ framework.BindPlugin = &GpuSharePlugin{}

func NewGpuSharePlugin(fakeclient externalclientset.Interface, configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
	gpuSharePlugin := &GpuSharePlugin{fakeclient: fakeclient}
	gpuSharePlugin.InitSchedulerCache()
	return gpuSharePlugin, nil
}

// Name returns name of the plugin. It is used in logs, etc.
func (plugin *GpuSharePlugin) Name() string {
	return simontype.OpenGpuSharePluginName
}

// Filter Plugin

// Filter filters out non-allocatable nodes
func (plugin *GpuSharePlugin) Filter(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	// check if the pod requires GPU resources
	podGpuMem := gpushareutils.GetGPUMemoryFromPodResource(pod)
	if podGpuMem <= 0 {
		// the node is schedulable if pod does not require GPU resources
		//klog.Infof("[Filter] Pod: %v/%v, podGpuMem <= 0: %v", pod.GetNamespace(), pod.GetName(), podGpuMem)
		return framework.NewStatus(framework.Success)
	}
	//klog.Infof("[Filter] Pod: %v/%v, podGpuMem: %v", pod.GetNamespace(), pod.GetName(), podGpuMem)

	// check if the node have GPU resources
	node := nodeInfo.Node()
	nodeGpuMem := gpushareutils.GetTotalGPUMemory(node)
	if nodeGpuMem < podGpuMem {
		//klog.Infof("[Filter] Unschedulable, Node: %v, nodeGpuMem: %v", node.GetName(), nodeGpuMem)
		return framework.NewStatus(framework.Unschedulable, "Node:"+nodeInfo.Node().Name)
	}
	//klog.Infof("[Filter] Schedulable, Node: %v, nodeGpuMem: %v", node.GetName(), nodeGpuMem)

	return framework.NewStatus(framework.Success)
}

// Score Plugin

// Score invoked at the score extension point.
func (plugin *GpuSharePlugin) Score(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
	podReq, _ := resourcehelper.PodRequestsAndLimits(pod)
	if len(podReq) == 0 {
		return framework.MaxNodeScore, framework.NewStatus(framework.Success)
	}

	node, err := plugin.fakeclient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return int64(framework.MinNodeScore), framework.NewStatus(framework.Error, fmt.Sprintf("failed to get node %s: %s\n", nodeName, err.Error()))
	}

	res := float64(0)
	for resourceName := range node.Status.Allocatable {
		podAllocatedRes := podReq[resourceName]
		nodeAvailableRes := node.Status.Allocatable[resourceName]
		nodeAvailableRes.Sub(podAllocatedRes)
		share := algo.Share(podAllocatedRes.AsApproximateFloat64(), nodeAvailableRes.AsApproximateFloat64())
		if share > res {
			res = share
		}
	}

	score := int64(float64(framework.MaxNodeScore-framework.MinNodeScore) * res)
	//klog.Infof("[Score] Pod: %v at Node: %v => Score: %d", pod.Name, nodeName, score)
	return score, framework.NewStatus(framework.Success)
}

// ScoreExtensions of the Score plugin.
func (plugin *GpuSharePlugin) ScoreExtensions() framework.ScoreExtensions {
	return plugin // if there is no NormalizeScore, return nil.
}

// NormalizeScore invoked after scoring all nodes.
func (plugin *GpuSharePlugin) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, scores framework.NodeScoreList) *framework.Status {
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

	// Transform the highest to lowest score range to fit the framework's min to max node score range.
	oldRange := highest - lowest
	newRange := framework.MaxNodeScore - framework.MinNodeScore
	for i, nodeScore := range scores {
		if oldRange == 0 {
			scores[i].Score = framework.MinNodeScore
		} else {
			scores[i].Score = ((nodeScore.Score - lowest) * newRange / oldRange) + framework.MinNodeScore
		}
	}

	return framework.NewStatus(framework.Success)
}

// Resource Plugin
func (plugin *GpuSharePlugin) Reserve(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) *framework.Status {
	if gpushareutils.GetGPUMemoryFromPodResource(pod) <= 0 {
		return framework.NewStatus(framework.Success) // non-GPU pods are skipped
	}

	// get node from fakeclient
	node, _ := plugin.fakeclient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})

	gpuNodeInfo, err := plugin.cache.GetGpuNodeInfo(nodeName)
	if err != nil {
		klog.Errorf("warn: Failed to handle pod %s in ns %s due to error %v", pod.Name, pod.Namespace, err)
		return framework.NewStatus(framework.Error, err.Error())
	}

	devId, found := gpuNodeInfo.AllocateGPUID(pod)
	if found {
		//klog.Infof("Allocate() Allocate GPU ID %d to pod %s in ns %s.----", devId, pod.Name, pod.Namespace)
		if patchedAnnotationBytes, err := gpushareutils.PatchPodAnnotationSpec(pod, devId, gpuNodeInfo.GetTotalGPUMemory()/gpuNodeInfo.GetGPUCount()); err != nil {
			return framework.NewStatus(framework.Error, fmt.Sprintf("failed to generate patched annotations,reason: %v", err))
		} else {
			metav1.SetMetaDataAnnotation(&pod.ObjectMeta, simontype.AnnoPodGpuShare, string(patchedAnnotationBytes))
		}
	} else {
		err = fmt.Errorf("The node %s can't place the pod %s in ns %s,and the pod spec is %v", pod.Spec.NodeName, pod.Name, pod.Namespace, pod)
		return framework.NewStatus(framework.Error, err.Error())
	}

	// update Pod
	podCopy := pod.DeepCopy()
	podCopy.Spec.NodeName = nodeName
	podCopy.Status.Phase = corev1.PodRunning
	_, err = plugin.fakeclient.CoreV1().Pods(podCopy.Namespace).Update(context.TODO(), podCopy, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("fake update error %v", err)
		return framework.NewStatus(framework.Error, fmt.Sprintf("Unable to add new pod: %v", err))
	}
	//klog.Infof("Allocate() ---- pod %s in ns %s is allocated to node %s ----", podCopy.Name, podCopy.Namespace, podCopy.Spec.NodeName)

	// update Node
	if err := plugin.cache.AddOrUpdatePod(podCopy); err != nil { // requires pod.Spec.NodeName specified
		return framework.NewStatus(framework.Error, err.Error())
	}
	nodeGpuInfo, err := plugin.ExportGpuNodeInfoAsNodeGpuInfo(nodeName)
	if data, err := ffjson.Marshal(nodeGpuInfo); err != nil {
		return framework.NewStatus(framework.Error, err.Error())
	} else {
		metav1.SetMetaDataAnnotation(&node.ObjectMeta, simontype.AnnoNodeGpuShare, string(data))
	}

	if _, err := plugin.fakeclient.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{}); err != nil {
		return framework.NewStatus(framework.Error, err.Error())
	}
	return framework.NewStatus(framework.Success)
}

// TODO
func (plugin *GpuSharePlugin) Unreserve(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) {
}

// Util Functions

func (plugin *GpuSharePlugin) ExportGpuNodeInfoAsNodeGpuInfo(nodeName string) (*gpusharecache.NodeGpuInfo, error) {
	if gpuNodeInfo, err := plugin.cache.GetGpuNodeInfo(nodeName); err != nil {
		return nil, err
	} else {
		nodeGpuInfo := gpuNodeInfo.ExportGpuNodeInfoAsNodeGpuInfo()
		return nodeGpuInfo, nil
	}
}

func (plugin *GpuSharePlugin) NodeGet(name string) (*corev1.Node, error) {
	return plugin.fakeclient.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
}

func (plugin *GpuSharePlugin) PodGet(name string, namespace string) (*corev1.Pod, error) {
	return plugin.fakeclient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}

func (plugin *GpuSharePlugin) InitSchedulerCache() {
	plugin.cache = gpusharecache.NewSchedulerCache(plugin) // here `plugin` implements the NodePodGetter interface
}
