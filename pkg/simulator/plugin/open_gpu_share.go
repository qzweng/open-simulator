package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/pquerna/ffjson/ffjson"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	schedulerutil "k8s.io/kubernetes/pkg/scheduler/util"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	gpusharecache "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/cache"
	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
	"github.com/alibaba/open-simulator/pkg/utils"
)

// GpuSharePlugin is a plugin for scheduling framework
type GpuSharePlugin struct {
	sync.RWMutex
	cache  *gpusharecache.SchedulerCache
	cfg    *simontype.GpuPluginCfg
	handle framework.Handle
}

// Just to check whether the implemented struct fits the interface
var _ framework.FilterPlugin = &GpuSharePlugin{}
var _ framework.ReservePlugin = &GpuSharePlugin{}

var allocateGpuIdFunc = map[string]func(nodeRes simontype.NodeResource, podRes simontype.PodResource,
	method simontype.GpuDimExtMethod, node *v1.Node) (gpuId string){}

func NewGpuSharePlugin(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	var cfg *simontype.GpuPluginCfg
	if err := frameworkruntime.DecodeInto(configuration, &cfg); err != nil {
		return nil, err
	}

	gpuSharePlugin := &GpuSharePlugin{
		cfg:    cfg,
		handle: handle,
	}
	gpuSharePlugin.initSchedulerCache()
	handle.SharedInformerFactory().Core().V1().Pods().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			DeleteFunc: func(obj interface{}) {
				if pod, ok := obj.(*v1.Pod); ok {
					if gpushareutils.GetGpuMilliFromPodAnnotation(pod) > 0 {
						namespace, name := pod.Namespace, pod.Name
						err := gpuSharePlugin.removePod(pod, pod.Spec.NodeName)
						if err != nil {
							log.Errorf("removePod (%s) error: %s\n", utils.GeneratePodKeyByName(namespace, name), err.Error())
						}
					}
				}
			}})
	return gpuSharePlugin, nil
}

// Name returns name of the plugin. It is used in logs, etc.
func (plugin *GpuSharePlugin) Name() string {
	return simontype.OpenGpuSharePluginName
}

// Filter Plugin
// Filter filters out non-allocatable nodes
func (plugin *GpuSharePlugin) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	//fmt.Printf("filter_gpu: pod %s/%s, nodeName %s\n", pod.Namespace, pod.Name, nodeInfo.Node().Name)
	// Pass if the pod does not require GPU resources
	if podGpuMilli := gpushareutils.GetGpuMilliFromPodAnnotation(pod); podGpuMilli <= 0 {
		return framework.NewStatus(framework.Success)
	}
	node := nodeInfo.Node()
	// Reject if the node has no GPU resource
	if nodeGpuCount := gpushareutils.GetGpuCountOfNode(node); nodeGpuCount == 0 {
		return framework.NewStatus(framework.Unschedulable, "Node:"+nodeInfo.Node().Name)
	}

	// Reject if the GPU type does not match
	nodeGpuType := gpushareutils.GetGpuModelOfNode(node)
	podGpuType := gpushareutils.GetGpuModelFromPodAnnotation(pod)
	if utils.IsNodeAccessibleToPodByType(nodeGpuType, podGpuType) == false {
		return framework.NewStatus(framework.Unschedulable, "Node:"+nodeInfo.Node().Name)
	}

	gpuNodeInfo, err := plugin.cache.GetGpuNodeInfo(node.Name)
	if err != nil {
		return framework.NewStatus(framework.Unschedulable, "Node:"+nodeInfo.Node().Name)
	}
	_, found := gpuNodeInfo.AllocateGpuId(pod)
	if !found {
		return framework.NewStatus(framework.Unschedulable, "Node:"+nodeInfo.Node().Name)
	}

	return framework.NewStatus(framework.Success)
}

func (plugin *GpuSharePlugin) updateNode(node *v1.Node) error {
	nodeGpuInfoStr, err := plugin.ExportGpuNodeInfoAsNodeGpuInfo(node.Name)
	if err != nil {
		return err
	}
	if data, err := ffjson.Marshal(nodeGpuInfoStr); err != nil {
		return err
	} else {
		metav1.SetMetaDataAnnotation(&node.ObjectMeta, simontype.AnnoNodeGpuShare, string(data))
	}
	//fmt.Printf("updateNode: %v with anno: %s\n", nodeGpuInfoStr, node.ObjectMeta.Annotations)

	if _, err := plugin.handle.ClientSet().CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to Update node %s", node.Name)
	}
	return nil
}

func (plugin *GpuSharePlugin) addOrUpdatePod(pod *v1.Pod, nodeName string) error {
	if err := plugin.cache.AddOrUpdatePod(pod, nodeName); err != nil {
		return err
	}
	if pod.Spec.NodeName == "" {
		return fmt.Errorf("pod unscheduled: %s/%s", pod.Namespace, pod.Name)
	}
	node, err := plugin.handle.ClientSet().CoreV1().Nodes().Get(context.Background(), pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	//fmt.Printf("addOrUpdatePod: %s\n", pod.Name)
	if err = plugin.updateNode(node); err != nil {
		return err
	}
	return nil
}

func (plugin *GpuSharePlugin) removePod(pod *v1.Pod, nodeName string) error {
	if nodeName == "" {
		return nil
	}
	node, err := plugin.handle.ClientSet().CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	plugin.cache.RemovePod(pod, nodeName)
	if err = plugin.updateNode(node); err != nil {
		return err
	}
	return nil
}

// Reserve Plugin
// Reserve updates the GPU resource of the given node, according to the pod's request.
func (plugin *GpuSharePlugin) Reserve(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) *framework.Status {
	plugin.Lock()
	defer plugin.Unlock()

	log.Debugf("reserve pod(%s) on node(%s)\n", utils.GeneratePodKey(pod), nodeName)
	if gpushareutils.GetGpuMilliFromPodAnnotation(pod) <= 0 {
		return framework.NewStatus(framework.Success) // non-GPU pods are skipped
	}

	podCopy, err := plugin.updatePodGpuAnno(pod, nodeName)
	if err != nil {
		log.Errorf("The node %s can't place the pod %s in ns %s,and the pod spec is %v. err: %s", pod.Spec.NodeName, pod.Name, pod.Namespace, pod, err)
		return framework.NewStatus(framework.Error, err.Error())
	}

	if err = plugin.addOrUpdatePod(podCopy, nodeName); err != nil {
		//fmt.Printf("addOrUpdatePod: pod %s/%s, nodeName %s, error %v\n", pod.Namespace, pod.Name, nodeName, err)
		return framework.NewStatus(framework.Error, err.Error())
	}

	if err = schedulerutil.PatchPod(plugin.handle.ClientSet(), pod, podCopy); err != nil {
		return framework.NewStatus(framework.Error, err.Error())
	}

	return framework.NewStatus(framework.Success)
}

// Unreserve undoes the GPU resource updated in Reserve function.
func (plugin *GpuSharePlugin) Unreserve(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) {
	plugin.Lock()
	defer plugin.Unlock()

	if err := plugin.removePod(pod, nodeName); err != nil {
		log.Errorln(err.Error())
	}
}

// Util Functions

func (plugin *GpuSharePlugin) ExportGpuNodeInfoAsNodeGpuInfo(nodeName string) (*gpusharecache.GpuNodeInfoStr, error) {
	if gpuNodeInfo, err := plugin.cache.GetGpuNodeInfo(nodeName); err != nil {
		return nil, err
	} else {
		nodeGpuInfoStr := gpuNodeInfo.ExportGpuNodeInfoAsStr()
		return nodeGpuInfoStr, nil
	}
}

func (plugin *GpuSharePlugin) NodeGet(name string) (*v1.Node, error) {
	return plugin.handle.ClientSet().CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
}

func (plugin *GpuSharePlugin) PodGet(name string, namespace string) (*v1.Pod, error) {
	return plugin.handle.ClientSet().CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}

func (plugin *GpuSharePlugin) initSchedulerCache() {
	plugin.cache = gpusharecache.NewSchedulerCache(plugin) // here `plugin` implements the NodePodGetter interface
}

func (plugin *GpuSharePlugin) updatePodGpuAnno(pod *v1.Pod, nodeName string) (*v1.Pod, error) {
	gpuId := plugin.allocateGpuId(pod, nodeName)
	if gpuId == "" {
		return nil, fmt.Errorf("failed to allocate gpu to pod(%s) to node(%s)", utils.GeneratePodKey(pod), nodeName)
	}

	podCopy := gpushareutils.UpdatePodDeviceAnnoSpec(pod, gpuId)
	return podCopy, nil
}

func (plugin *GpuSharePlugin) allocateGpuId(pod *v1.Pod, nodeName string) string {
	node, err := plugin.NodeGet(nodeName)
	if err != nil {
		return ""
	}

	nodeResPtr := utils.GetNodeResourceViaHandle(plugin.handle, node)
	if nodeResPtr == nil {
		return ""
	}
	nodeRes := *nodeResPtr
	podRes := utils.GetPodResource(pod)

	if f, ok := allocateGpuIdFunc[string(plugin.cfg.GpuSelMethod)]; ok {
		gpuId := f(nodeRes, podRes, plugin.cfg.DimExtMethod, node)
		return gpuId
	} else {
		return ""
	}
}
