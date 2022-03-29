package simulator

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	externalclientset "k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	kubetypes "k8s.io/kubernetes/pkg/kubelet/types"
	"k8s.io/kubernetes/pkg/scheduler"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"

	"github.com/alibaba/open-simulator/pkg/algo"
	simonplugin "github.com/alibaba/open-simulator/pkg/simulator/plugin"
	simontype "github.com/alibaba/open-simulator/pkg/type"
	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
	"github.com/alibaba/open-simulator/pkg/utils"
)

// Simulator is used to simulate a cluster and pods scheduling
type Simulator struct {
	// kube client
	// externalclient  externalclientset.Interface
	client          externalclientset.Interface
	informerFactory informers.SharedInformerFactory

	// scheduler
	scheduler *scheduler.Scheduler

	// stopCh
	updateBarrier map[string]chan struct{}

	// context
	ctx        context.Context
	cancelFunc context.CancelFunc

	status status

	//
	typicalPods     simontype.TargetPodList
	nodeResourceMap map[string]simontype.NodeResource
}

// status captures reason why one pod fails to be scheduled
type status struct {
	stopReason string
}

type simulatorOptions struct {
	kubeconfig      string
	schedulerConfig string
}

// Option configures a Simulator
type Option func(*simulatorOptions)

var defaultSimulatorOptions = simulatorOptions{
	kubeconfig:      "",
	schedulerConfig: "",
}

// New generates all components that will be needed to simulate scheduling and returns a complete simulator
func New(opts ...Option) (Interface, error) {
	var err error
	// Step 0: configures a Simulator by opts
	options := defaultSimulatorOptions
	for _, opt := range opts {
		opt(&options)
	}

	// Step 2: get scheduler CompletedConfig and set the list of scheduler bind plugins to Simon.
	kubeSchedulerConfig, err := GetAndSetSchedulerConfig(options.schedulerConfig)
	if err != nil {
		return nil, err
	}

	// Step 3: create fake client
	var client externalclientset.Interface
	if options.kubeconfig != "" {
		config, err := clientcmd.BuildConfigFromFlags("", options.kubeconfig)
		if err != nil {
			fmt.Printf("[Error] %s\n", err.Error())
		}
		client, err = externalclientset.NewForConfig(config)
	} else {
		client = fakeclientset.NewSimpleClientset()
	}
	kubeSchedulerConfig.Client = client
	sharedInformerFactory := informers.NewSharedInformerFactory(client, 0)

	// Step 4: Create the simulator
	ctx, cancel := context.WithCancel(context.Background())
	updateBarrier := map[string]chan struct{}{
		simontype.SimulatorName: make(chan struct{}),
	}

	storagev1Informers := sharedInformerFactory.Storage().V1()
	scInformer := storagev1Informers.StorageClasses().Informer()
	sharedInformerFactory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), scInformer.HasSynced)

	sim := &Simulator{
		client:          client,
		updateBarrier:   updateBarrier,
		informerFactory: sharedInformerFactory,
		ctx:             ctx,
		cancelFunc:      cancel,
	}

	// Step 6: create scheduler for fake cluster
	bindRegistry := frameworkruntime.Registry{
		simontype.SimonPluginName: func(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
			return simonplugin.NewSimonPlugin(configuration, handle)
		},
		simontype.OpenLocalPluginName: func(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
			return simonplugin.NewLocalPlugin(client, storagev1Informers, configuration, handle)
		},
		simontype.OpenGpuSharePluginName: func(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
			return simonplugin.NewGpuSharePlugin(configuration, handle)
		},
		simontype.GpuFragScorePluginName: func(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
			return simonplugin.NewGpuFragScorePlugin(configuration, handle, &sim.typicalPods)
		},
		simontype.GpuPackingScorePluginName: func(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
			return simonplugin.NewGpuPackingScorePlugin(configuration, handle)
		},
	}
	sim.scheduler, err = scheduler.New(
		sim.client,
		sim.informerFactory,
		GetRecorderFactory(kubeSchedulerConfig),
		sim.ctx.Done(),
		scheduler.WithProfiles(kubeSchedulerConfig.ComponentConfig.Profiles...),
		scheduler.WithAlgorithmSource(kubeSchedulerConfig.ComponentConfig.AlgorithmSource),
		scheduler.WithPercentageOfNodesToScore(kubeSchedulerConfig.ComponentConfig.PercentageOfNodesToScore),
		scheduler.WithFrameworkOutOfTreeRegistry(bindRegistry),
		scheduler.WithPodMaxBackoffSeconds(kubeSchedulerConfig.ComponentConfig.PodMaxBackoffSeconds),
		scheduler.WithPodInitialBackoffSeconds(kubeSchedulerConfig.ComponentConfig.PodInitialBackoffSeconds),
		scheduler.WithExtenders(kubeSchedulerConfig.ComponentConfig.Extenders...),
	)
	if err != nil {
		return nil, err
	}

	return sim, nil
}

// RunCluster
func (sim *Simulator) RunCluster(cluster ResourceTypes) (*simontype.SimulateResult, error) {
	// start scheduler
	sim.runScheduler()

	switch t := sim.client.(type) {
	case *externalclientset.Clientset:
		return &simontype.SimulateResult{}, nil
	case *fakeclientset.Clientset:
		return sim.syncClusterResourceList(cluster)
	default:
		return nil, fmt.Errorf("unknown client type: %T", t)
	}
}

func (sim *Simulator) ScheduleApp(apps AppResource) (*simontype.SimulateResult, error) {
	// 由 AppResource 生成 Pods
	appPods, err := GenerateValidPodsFromAppResources(sim.client, apps.Name, apps.Resource)
	if err != nil {
		return nil, err
	}
	affinityPriority := algo.NewAffinityQueue(appPods)
	sort.Sort(affinityPriority)
	tolerationPriority := algo.NewTolerationQueue(appPods)
	sort.Sort(tolerationPriority)
	failedPod, err := sim.schedulePods(appPods)
	if err != nil {
		return nil, err
	}

	return &simontype.SimulateResult{
		UnscheduledPods: failedPod,
		NodeStatus:      sim.getClusterNodeStatus(),
	}, nil
}

func (sim *Simulator) Deschedule(pods []*corev1.Pod) (*simontype.SimulateResult, error) {
	podMap := make(map[string]*corev1.Pod)
	for _, pod := range pods {
		podMap[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)] = pod
	}

	nodeStatus := sim.getClusterNodeStatus()
	var descheduledPod []string
	for _, ns := range nodeStatus {
		victimPod := sim.findVictimPodOnNode(ns.Node, ns.Pods)
		if victimPod != nil {
			descheduledPod = append(descheduledPod, utils.GeneratePodKey(victimPod))
			sim.deletePod(victimPod)
		}

		//for _, pod := range ns.Pods {
		//	sim.deletePod(pod) // delete all pods
		//	podCopy := podMap[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)].DeepCopy()
		//	sim.createPod(podCopy)
		//	sim.deletePod(podCopy)
		//	podCopy = podMap[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)].DeepCopy()
		//	sim.createPod(podCopy)
		//	sim.deletePod(podCopy)
		//}
	}

	fmt.Printf("deschedule pod list %v\n", descheduledPod)
	var failedPods []simontype.UnscheduledPod
	for _, podKey := range descheduledPod {
		podCopy := podMap[podKey].DeepCopy()
		MakePodUnassigned(podCopy)
		sim.createPod(podCopy)

		if strings.Contains(sim.status.stopReason, "failed") {
			sim.deletePod(podCopy)
			failedPods = append(failedPods, simontype.UnscheduledPod{
				Pod:    podCopy,
				Reason: sim.status.stopReason,
			})
		}
	}

	return &simontype.SimulateResult{
		UnscheduledPods: failedPods,
		NodeStatus:      sim.getClusterNodeStatus(),
	}, nil
}

func (sim *Simulator) findVictimPodOnNode(node *corev1.Node, pods []*corev1.Pod) *corev1.Pod {
	var victimPod *corev1.Pod
	var victimPodSimilarity float64 = 1
	for _, pod := range pods {
		similarity, ok := sim.resourceSimilarity(pod, node)
		if !ok {
			fmt.Printf("failed to get resource similarity of pod %s to node %s\n", utils.GeneratePodKey(pod), node.Name)
			continue
		}
		if similarity >= 0 && similarity < victimPodSimilarity {
			victimPod = pod
			victimPodSimilarity = similarity
		}
	}
	if victimPod != nil {
		fmt.Printf("pod %s is selected to deschedule from node %s, resource similarity %.2f\n", utils.GeneratePodKey(victimPod), node.Name, victimPodSimilarity)
		return victimPod
	}
	return nil
}

func (sim *Simulator) resourceSimilarity(pod *corev1.Pod, node *corev1.Node) (float64, bool) {
	var podVec, nodeVec []float64

	// cpu
	var scaleFactor float64 = 1000
	podVec = append(podVec, float64(pod.Spec.Containers[0].Resources.Requests.Cpu().MilliValue())/scaleFactor)
	nodeVec = append(nodeVec, float64(node.Status.Allocatable.Cpu().MilliValue())/scaleFactor)

	// mem
	scaleFactor = 1024 * 1024 * 1024
	podVec = append(podVec, float64(pod.Spec.Containers[0].Resources.Requests.Memory().Value())/scaleFactor)
	nodeVec = append(nodeVec, float64(node.Status.Allocatable.Memory().Value())/scaleFactor)

	// gpu mem
	scaleFactor = 1024 * 1024 * 1024
	if nodeGpuMem, ok := node.Status.Allocatable[gpushareutils.ResourceName]; ok {
		//nodeVec = append(nodeVec, float64())
		if podGpuMemAnno, ok := pod.Annotations[gpushareutils.ResourceName]; ok {
			podGpuMem := resource.MustParse(podGpuMemAnno)

			podGpuCountAnno := pod.Annotations[gpushareutils.CountName]
			//podGpuCount := resource.MustParse(podGpuCountAnno)
			podGpuCount, _ := strconv.ParseFloat(podGpuCountAnno, 64)

			fmt.Printf("debug: pod %s, podGpuMemAnno %v, podGpuMem %v, podGpuCountAnno %v, podGpuCount %v\n", utils.GeneratePodKey(pod), podGpuMemAnno, podGpuMem, podGpuCountAnno, podGpuCount)

			podVec = append(podVec, float64(podGpuMem.Value())*podGpuCount/scaleFactor)
		} else {
			podVec = append(podVec, 0)
		}
		nodeGpuCount := node.Status.Allocatable[gpushareutils.CountName]
		fmt.Printf("debug: node %s, nodeGpuMem %v, nodeGpuCount %v\n", node.Name, nodeGpuMem, nodeGpuCount)
		nodeVec = append(nodeVec, float64(nodeGpuMem.Value())*float64(nodeGpuCount.Value())/scaleFactor)
	}

	similarity, err := calculateVectorSimilarity(podVec, nodeVec)
	if err != nil {
		return -1, false
	}
	fmt.Printf("similarity of pod %s with node %s is %.2f, pod vec %v, node vec %v\n",
		utils.GeneratePodKey(pod), node.Name, similarity, podVec, nodeVec)
	return similarity, true
}

func calculateVectorSimilarity(vec1, vec2 []float64) (float64, error) {
	if len(vec1) == 0 || len(vec2) == 0 || len(vec1) != len(vec2) {
		err := fmt.Errorf("empty vector(s) or vectors of unequal size, vec1 %v, vec2 %v\n", vec1, vec2)
		return -1, err
	}
	var magnitude1, magnitude2, innerProduct float64
	for index, num1 := range vec1 {
		num2 := vec2[index]
		magnitude1 += num1 * num1
		magnitude2 += num2 * num2
		innerProduct += num1 * num2
	}
	magnitude1 = math.Sqrt(magnitude1)
	magnitude2 = math.Sqrt(magnitude2)

	if magnitude1 == 0 || magnitude2 == 0 {
		err := fmt.Errorf("vector(s) of zero magnitude. vec1: %v, vec2: %v", vec1, vec2)
		return -1, err
	}

	similarity := innerProduct / (magnitude1 * magnitude2)
	return similarity, nil
}

func (sim *Simulator) AddParaSet() {

}

func (sim *Simulator) getClusterNodeStatus() []simontype.NodeStatus {
	var nodeStatues []simontype.NodeStatus
	var err error

	nodes, err := sim.client.CoreV1().Nodes().List(sim.ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	pods, err := sim.client.CoreV1().Pods(corev1.NamespaceAll).List(sim.ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	nodeToPodListMap := map[string][]*corev1.Pod{}
	for _, pod := range pods.Items {
		nodeName := pod.Spec.NodeName
		if nodeName != "" {
			if nodeToPodListMap[nodeName] == nil {
				nodeToPodListMap[nodeName] = []*corev1.Pod{}
			}
			nodeToPodListMap[nodeName] = append(nodeToPodListMap[nodeName], pod.DeepCopy())
		}
	}

	for _, node := range nodes.Items {
		nodeStatus := simontype.NodeStatus{}
		nodeStatus.Node = node.DeepCopy()
		nodeStatus.Pods = nodeToPodListMap[node.Name]
		nodeStatues = append(nodeStatues, nodeStatus)
	}
	return nodeStatues
}

// runScheduler
func (sim *Simulator) runScheduler() {
	// Step 1: start all informers.
	sim.informerFactory.Start(sim.ctx.Done())
	sim.informerFactory.WaitForCacheSync(sim.ctx.Done())

	// Step 2: run scheduler
	go func() {
		sim.scheduler.Run(sim.ctx)
	}()
}

func (sim *Simulator) createPod(p *corev1.Pod) error {
	if _, err := sim.client.CoreV1().Pods(p.Namespace).Create(sim.ctx, p, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("%s(%s): %s", simontype.CreatePodError, utils.GeneratePodKey(p), err.Error())
	}

	// synchronization
	sim.syncPodCreate(p.Namespace, p.Name, 2*time.Millisecond)
	pod, _ := sim.client.CoreV1().Pods(p.Namespace).Get(sim.ctx, p.Name, metav1.GetOptions{})
	if pod != nil {
		if pod.Spec.NodeName != "" {
			sim.syncNodeUpdateOnPodCreate(pod.Spec.NodeName, pod, 2*time.Millisecond)
		}
	} else {
		fmt.Printf("[Error] [createPod] pod(%s) not created, should not happen", utils.GeneratePodKey(p))
	}
	return nil
}

func (sim *Simulator) deletePod(p *corev1.Pod) error {
	pod, _ := sim.client.CoreV1().Pods(p.Namespace).Get(sim.ctx, p.Name, metav1.GetOptions{})
	nodeName := ""
	if pod != nil {
		nodeName = pod.Spec.NodeName
	} else {
		fmt.Printf("[Info] [deletePod] attempt to delete a non-existed pod(%s)\n", utils.GeneratePodKey(p))
		return nil
	}

	// delete the pod
	if err := sim.client.CoreV1().Pods(p.Namespace).Delete(sim.ctx, p.Name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("%s(%s): %s", simontype.DeletePodError, utils.GeneratePodKey(p), err.Error())
	}

	// synchronization
	sim.syncPodDelete(p.Namespace, p.Name, 2*time.Millisecond)
	if nodeName != "" {
		sim.syncNodeUpdateOnPodDelete(nodeName, pod, 2*time.Millisecond)
	} else {
		fmt.Printf("[Info] [deletePod] attempt to delete a non-scheduled pod(%s)\n", utils.GeneratePodKey(p))
	}
	return nil
}

// Run starts to schedule pods
func (sim *Simulator) schedulePods(pods []*corev1.Pod) ([]simontype.UnscheduledPod, error) {
	var failedPods []simontype.UnscheduledPod
	var err error
	for i, pod := range pods {
		err = sim.createPod(pod)
		fmt.Printf("[%d] pod %s created\n", i, utils.GeneratePodKey(pod))
		if err != nil || sim.isPodUnscheduled(pod.Namespace, pod.Name) {
			if err = sim.deletePod(pod); err != nil {
				fmt.Printf("[Info] [schedulePods] failed to delete pod(%s)\n", utils.GeneratePodKey(pod))
				return nil, err
			}
			failedPods = append(failedPods, simontype.UnscheduledPod{Pod: pod})
		}
	}
	return failedPods, nil
}

func (sim *Simulator) Close() {
	sim.cancelFunc()
	for _, ub := range sim.updateBarrier {
		close(ub)
	}
}

func (sim *Simulator) isPodScheduled(ns, name string) bool {
	pod, _ := sim.client.CoreV1().Pods(ns).Get(sim.ctx, name, metav1.GetOptions{})
	return pod != nil && pod.Spec.NodeName != ""
}

func (sim *Simulator) isPodUnscheduled(ns, name string) bool {
	pod, _ := sim.client.CoreV1().Pods(ns).Get(sim.ctx, name, metav1.GetOptions{})
	if pod != nil && pod.Spec.NodeName == "" {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse &&
				condition.Reason == corev1.PodReasonUnschedulable {
				return true
			}
		}
	}
	return false
}

func (sim *Simulator) isPodCreated(ns, name string) bool {
	return sim.isPodScheduled(ns, name) || sim.isPodUnscheduled(ns, name)
}

func (sim *Simulator) isPodDeleted(ns, name string) bool {
	pod, _ := sim.client.CoreV1().Pods(ns).Get(sim.ctx, name, metav1.GetOptions{})
	return pod == nil || (pod.Namespace == "" && pod.Name == "")
}

func (sim *Simulator) isNodeCreated(name string) bool {
	node, _ := sim.client.CoreV1().Nodes().Get(sim.ctx, name, metav1.GetOptions{})
	return node != nil
}

func (sim *Simulator) isPodFoundInNodeGpuAnno(node *corev1.Node, p *corev1.Pod) bool {
	gpuNodeInfoStr, err := utils.GetGpuNodeInfoFromAnnotation(node)
	if err != nil {
		panic(fmt.Sprintf("failed to parse gpu info annotation on node(%s): %v", node.Name, err.Error()))
	}
	if gpuNodeInfoStr == nil {
		panic(fmt.Sprintf("gpu node(%s) don't have gpu annotation, should not happen", node.Name))
	}

	for _, dev := range gpuNodeInfoStr.DevsBrief {
		for _, pStr := range dev.PodList {
			if utils.GeneratePodKey(p) == pStr {
				return true
			}
		}
	}
	return false
}

func (sim *Simulator) syncPodCreate(ns, name string, d time.Duration) {
	for {
		if sim.isPodCreated(ns, name) {
			break
		}
		time.Sleep(d)
	}
}

func (sim *Simulator) syncPodDelete(ns, name string, d time.Duration) {
	for {
		fmt.Printf("[Debug] check if pod(%s) has been deleted\n", name)
		if sim.isPodDeleted(ns, name) {
			break
		}
		time.Sleep(d)
	}
}

func (sim *Simulator) syncNodeUpdateOnPodCreate(nodeName string, p *corev1.Pod, d time.Duration) {
	for {
		node, _ := sim.client.CoreV1().Nodes().Get(sim.ctx, nodeName, metav1.GetOptions{})
		if node == nil {
			fmt.Printf("[Error] [syncNodeUpdateOnPodCreate] failed to get node(%s) when creating pod(%s)\n",
				nodeName, utils.GeneratePodKey(p))
			break
		}

		// only check gpu pod because creating it will update node gpu annotation
		if gpushareutils.GetGpuMilliFromPodAnnotation(p) > 0 {
			if sim.isPodFoundInNodeGpuAnno(node, p) {
				break
			}
		} else {
			break
		}

		time.Sleep(d)
	}
}

func (sim *Simulator) syncNodeUpdateOnPodDelete(nodeName string, p *corev1.Pod, d time.Duration) {
	for {
		node, _ := sim.client.CoreV1().Nodes().Get(sim.ctx, nodeName, metav1.GetOptions{})
		if node == nil {
			fmt.Printf("[Error] [syncNodeUpdateOnPodDelete] failed to get node(%s)\n", nodeName)
			break
		}

		// only check gpu pod because deleting it will update node gpu annotation
		if gpushareutils.GetGpuMilliFromPodAnnotation(p) > 0 {
			if !sim.isPodFoundInNodeGpuAnno(node, p) {
				break
			}
		} else {
			break
		}

		time.Sleep(d)
	}
}

func (sim *Simulator) syncNodeCreate(name string, d time.Duration) {
	for {
		if sim.isNodeCreated(name) {
			break
		}
		time.Sleep(d)
	}
	fmt.Printf("[Debug] node(%s) has been successfully created\n", name)
}

func (sim *Simulator) syncClusterResourceList(resourceList ResourceTypes) (*simontype.SimulateResult, error) {
	//sync node
	for _, item := range resourceList.Nodes {
		if _, err := sim.client.CoreV1().Nodes().Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy node: %v", err)
		}
		sim.syncNodeCreate(item.Name, 2*time.Millisecond)
	}

	//sync pdb
	for _, item := range resourceList.PodDisruptionBudgets {
		if _, err := sim.client.PolicyV1beta1().PodDisruptionBudgets(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy PDB: %v", err)
		}
	}

	//sync svc
	for _, item := range resourceList.Services {
		if _, err := sim.client.CoreV1().Services(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy service: %v", err)
		}
	}

	//sync storage class
	for _, item := range resourceList.StorageClasss {
		if _, err := sim.client.StorageV1().StorageClasses().Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy storage class: %v", err)
		}
	}

	//sync pvc
	for _, item := range resourceList.PersistentVolumeClaims {
		if _, err := sim.client.CoreV1().PersistentVolumeClaims(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy pvc: %v", err)
		}
	}

	//sync rc
	for _, item := range resourceList.ReplicationControllers {
		if _, err := sim.client.CoreV1().ReplicationControllers(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy RC: %v", err)
		}
	}

	//sync deployment
	for _, item := range resourceList.Deployments {
		if _, err := sim.client.AppsV1().Deployments(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy deployment: %v", err)
		}
	}

	//sync rs
	for _, item := range resourceList.ReplicaSets {
		if _, err := sim.client.AppsV1().ReplicaSets(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy replica set: %v", err)
		}
	}

	//sync statefulset
	for _, item := range resourceList.StatefulSets {
		if _, err := sim.client.AppsV1().StatefulSets(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy stateful set: %v", err)
		}
	}

	//sync daemonset
	for _, item := range resourceList.DaemonSets {
		if _, err := sim.client.AppsV1().DaemonSets(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy daemon set: %v", err)
		}
	}

	// sync pods
	failedPods, err := sim.schedulePods(resourceList.Pods)
	if err != nil {
		return nil, err
	}

	return &simontype.SimulateResult{
		UnscheduledPods: failedPods,
		NodeStatus:      sim.getClusterNodeStatus(),
	}, nil
}

func (sim *Simulator) update(pod *corev1.Pod) {
	var stop bool = false
	var stopReason string
	var stopMessage string
	for _, podCondition := range pod.Status.Conditions {
		// log.Infof("podCondition %v", podCondition)
		stop = podCondition.Type == corev1.PodScheduled && podCondition.Status == corev1.ConditionFalse && podCondition.Reason == corev1.PodReasonUnschedulable
		if stop {
			stopReason = podCondition.Reason
			stopMessage = podCondition.Message
			// fmt.Printf("stop is true: %s %s\n", stopReason, stopMessage)
			break
		}
	}
	// Only for pending pods provisioned by simon
	if stop {
		sim.status.stopReason = fmt.Sprintf("failed to schedule pod (%s/%s): %s: %s", pod.Namespace, pod.Name, stopReason, stopMessage)
	}
	sim.updateBarrier[simontype.SimulatorName] <- struct{}{}
}

// WithKubeConfig sets kubeconfig for Simulator, the default value is ""
func WithKubeConfig(kubeconfig string) Option {
	return func(o *simulatorOptions) {
		o.kubeconfig = kubeconfig
	}
}

// WithSchedulerConfig sets schedulerConfig for Simulator, the default value is ""
func WithSchedulerConfig(schedulerConfig string) Option {
	return func(o *simulatorOptions) {
		o.schedulerConfig = schedulerConfig
	}
}

// CreateClusterResourceFromClient returns a ResourceTypes struct by kube-client that connects a real cluster
func CreateClusterResourceFromClient(client externalclientset.Interface) (ResourceTypes, error) {
	var resource ResourceTypes
	var err error
	nodeItems, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list nodes: %v", err)
	}
	for _, item := range nodeItems.Items {
		newItem := item
		resource.Nodes = append(resource.Nodes, &newItem)
	}

	// TODO:
	// For all pods in the real cluster, we only retain static pods.
	// We will regenerate pods of all workloads in the follow-up stage.
	podItems, err := client.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list pods: %v", err)
	}
	for _, item := range podItems.Items {
		if kubetypes.IsStaticPod(&item) {
			newItem := item
			resource.Pods = append(resource.Pods, &newItem)
		}
	}

	pdbItems, err := client.PolicyV1beta1().PodDisruptionBudgets(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list PDBs: %v", err)
	}
	for _, item := range pdbItems.Items {
		newItem := item
		resource.PodDisruptionBudgets = append(resource.PodDisruptionBudgets, &newItem)
	}

	serviceItems, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list services: %v", err)
	}
	for _, item := range serviceItems.Items {
		newItem := item
		resource.Services = append(resource.Services, &newItem)
	}

	storageClassesItems, err := client.StorageV1().StorageClasses().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list storage classes: %v", err)
	}
	for _, item := range storageClassesItems.Items {
		newItem := item
		resource.StorageClasss = append(resource.StorageClasss, &newItem)
	}

	pvcItems, err := client.CoreV1().PersistentVolumeClaims(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list pvcs: %v", err)
	}
	for _, item := range pvcItems.Items {
		newItem := item
		resource.PersistentVolumeClaims = append(resource.PersistentVolumeClaims, &newItem)
	}

	rcItems, err := client.CoreV1().ReplicationControllers(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list RCs: %v", err)
	}
	for _, item := range rcItems.Items {
		newItem := item
		resource.ReplicationControllers = append(resource.ReplicationControllers, &newItem)
	}

	deploymentItems, err := client.AppsV1().Deployments(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list deployment: %v", err)
	}
	for _, item := range deploymentItems.Items {
		newItem := item
		resource.Deployments = append(resource.Deployments, &newItem)
	}

	replicaSetItems, err := client.AppsV1().ReplicaSets(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list replicas sets: %v", err)
	}
	for _, item := range replicaSetItems.Items {
		if !ownedByDeployment(item.OwnerReferences) {
			newItem := item
			resource.ReplicaSets = append(resource.ReplicaSets, &newItem)
		}
	}

	statefulSetItems, err := client.AppsV1().StatefulSets(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list stateful sets: %v", err)
	}
	for _, item := range statefulSetItems.Items {
		newItem := item
		resource.StatefulSets = append(resource.StatefulSets, &newItem)
	}

	daemonSetItems, err := client.AppsV1().DaemonSets(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list daemon sets: %v", err)
	}
	for _, item := range daemonSetItems.Items {
		newItem := item
		resource.DaemonSets = append(resource.DaemonSets, &newItem)
	}

	cronJobItems, err := client.BatchV1beta1().CronJobs(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list cronjob: %v", err)
	}
	for _, item := range cronJobItems.Items {
		newItem := item
		resource.CronJobs = append(resource.CronJobs, &newItem)
	}

	jobItems, err := client.BatchV1().Jobs(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return resource, fmt.Errorf("unable to list job: %v", err)
	}
	for _, item := range jobItems.Items {
		if !ownedByCronJob(item.OwnerReferences) {
			newItem := item
			resource.Jobs = append(resource.Jobs, &newItem)
		}
	}

	return resource, nil
}

// CreateClusterResourceFromClusterConfig return a ResourceTypes struct based on the cluster config
func CreateClusterResourceFromClusterConfig(path string) (ResourceTypes, error) {
	var resource ResourceTypes
	var content []string
	var err error

	if content, err = utils.GetYamlContentFromDirectory(path); err != nil {
		return ResourceTypes{}, fmt.Errorf("failed to get the yaml content from the cluster directory(%s): %v", path, err)
	}
	if resource, err = GetObjectFromYamlContent(content); err != nil {
		return resource, err
	}

	MatchAndSetLocalStorageAnnotationOnNode(resource.Nodes, path)

	return resource, nil
}

func ownedByDeployment(refs []metav1.OwnerReference) bool {
	for _, ref := range refs {
		if ref.Kind == simontype.Deployment {
			return true
		}
	}
	return false
}

func ownedByCronJob(refs []metav1.OwnerReference) bool {
	for _, ref := range refs {
		if ref.Kind == simontype.CronJob {
			return true
		}
	}
	return false
}
