package simulator

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
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
	"github.com/alibaba/open-simulator/pkg/api/v1alpha1"
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

	// context
	ctx        context.Context
	cancelFunc context.CancelFunc

	status status

	//
	originalWorkloadPods []*corev1.Pod
	typicalPods          simontype.TargetPodList
	nodeResourceMap      map[string]simontype.NodeResource
	customConfig         v1alpha1.CustomConfig
}

// status captures reason why one pod fails to be scheduled
type status struct {
	stopReason string
}

type simulatorOptions struct {
	kubeconfig      string
	schedulerConfig string
	customConfig    v1alpha1.CustomConfig
}

// Option configures a Simulator
type Option func(*simulatorOptions)

var defaultSimulatorOptions = simulatorOptions{
	kubeconfig:      "",
	schedulerConfig: "",
	customConfig:    v1alpha1.CustomConfig{},
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

	storagev1Informers := sharedInformerFactory.Storage().V1()
	scInformer := storagev1Informers.StorageClasses().Informer()
	sharedInformerFactory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), scInformer.HasSynced)

	sim := &Simulator{
		client:          client,
		informerFactory: sharedInformerFactory,
		ctx:             ctx,
		cancelFunc:      cancel,
		customConfig:    options.customConfig,
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
	failedPod := sim.SchedulePods(appPods)

	return &simontype.SimulateResult{
		UnscheduledPods: failedPod,
		NodeStatus:      sim.GetClusterNodeStatus(),
	}, nil
}

func (sim *Simulator) GetCustomConfig() v1alpha1.CustomConfig {
	return sim.customConfig
}

func (sim *Simulator) GetClusterNodeStatus() []simontype.NodeStatus {
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

func (sim *Simulator) assumePod(pod *corev1.Pod) *simontype.UnscheduledPod {
	err := sim.createPod(pod)
	if err != nil || sim.isPodUnscheduled(pod.Namespace, pod.Name) {
		if err = sim.deletePod(pod); err != nil {
			fmt.Printf("[Error] [assumePod] failed to delete pod(%s)\n", utils.GeneratePodKey(pod))
		}
		return &simontype.UnscheduledPod{Pod: pod}
	}
	return nil
}

func (sim *Simulator) SchedulePods(pods []*corev1.Pod) []simontype.UnscheduledPod {
	var failedPods []simontype.UnscheduledPod
	for i, pod := range pods {
		fmt.Printf("[%d] attempt to create pod(%s)\n", i, utils.GeneratePodKey(pod))
		if unscheduledPod := sim.assumePod(pod); unscheduledPod != nil {
			failedPods = append(failedPods, *unscheduledPod)
		}
	}
	return failedPods
}

func (sim *Simulator) Close() {
	sim.cancelFunc()
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
	failedPods := sim.SchedulePods(resourceList.Pods)

	return &simontype.SimulateResult{
		UnscheduledPods: failedPods,
		NodeStatus:      sim.GetClusterNodeStatus(),
	}, nil
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

func WithCustomConfig(customConfig v1alpha1.CustomConfig) Option {
	return func(o *simulatorOptions) {
		o.customConfig = customConfig
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

func getPodfromPodMap(podKeys []string, podMap map[string]*corev1.Pod) []*corev1.Pod {
	var podList []*corev1.Pod
	for _, podKey := range podKeys {
		podCopy := MakePodUnassigned(podMap[podKey])
		podList = append(podList, podCopy)
	}
	return podList
}

func (sim *Simulator) getNodeFragAmountList(nodeStatus []simontype.NodeStatus) []utils.FragAmount {
	nodeResourceMap := utils.GetNodeResourceMap(nodeStatus)
	nodeFragAmountMap := sim.NodeGpuFragAmountMap(nodeResourceMap)

	var nodeFragAmountList []utils.FragAmount
	for _, v := range nodeFragAmountMap {
		nodeFragAmountList = append(nodeFragAmountList, v)
	}
	sort.Slice(nodeFragAmountList, func(i int, j int) bool {
		return nodeFragAmountList[i].FragAmountSumExceptQ3() > nodeFragAmountList[j].FragAmountSumExceptQ3()
	})
	return nodeFragAmountList
}

func (sim *Simulator) getCurrentPodMap() map[string]*corev1.Pod {
	podMap := make(map[string]*corev1.Pod)
	podList, _ := sim.client.CoreV1().Pods(metav1.NamespaceAll).List(sim.ctx, metav1.ListOptions{})
	for _, pod := range podList.Items {
		podMap[utils.GeneratePodKey(&pod)] = pod.DeepCopy()
	}
	return podMap
}

func (sim *Simulator) SetOriginalWorkloadPods(pods []*corev1.Pod) {
	sim.originalWorkloadPods = []*corev1.Pod{}
	for _, p := range pods {
		sim.originalWorkloadPods = append(sim.originalWorkloadPods, p.DeepCopy())
	}
}

func (sim *Simulator) SortClusterPods(pods []*corev1.Pod) {
	var err error
	shufflePod := sim.customConfig.ShufflePod
	if shufflePod {
		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(pods), func(i, j int) {
			pods[i], pods[j] = pods[j], pods[i]
		})
	} else {
		timeNow := time.Now() //.Format(time.RFC3339)
		sort.SliceStable(pods, func(i, j int) bool {
			var timeI, timeJ time.Time
			if timeStr, ok := pods[i].Annotations[gpushareutils.CreationTime]; ok {
				timeI, err = time.Parse(time.RFC3339, timeStr)
				if err != nil {
					fmt.Printf("[Error] Time Parse %s err: %s\n", timeStr, err.Error())
					timeI = timeNow
				}
			} else {
				//fmt.Printf("[Info] No timestamp for pod %s\n", utils.GeneratePodKey(cluster.Pods[i]))
				timeI = timeNow
			}

			if timeStr, ok := pods[j].Annotations[gpushareutils.CreationTime]; ok {
				timeJ, err = time.Parse(time.RFC3339, timeStr)
				if err != nil {
					fmt.Printf("[Error] Time Parse %s err: %s\n", timeStr, err.Error())
					timeJ = timeNow
				}
			} else {
				//fmt.Printf("[Info] No timestamp for pod %s\n", utils.GeneratePodKey(cluster.Pods[i]))
				timeJ = timeNow
			}
			return timeI.Before(timeJ) || (timeI.Equal(timeJ) && pods[i].Name < pods[j].Name)
		})
	}
}

func (sim *Simulator) GenerateWorkloadInflationPods(tag string) []*corev1.Pod {
	n := len(sim.originalWorkloadPods)
	if n == 0 {
		fmt.Printf("[Info] [GenerateWorkloadInflationPods] original workload is empty\n")
		return nil
	}

	workloadInflationRatio := sim.customConfig.WorkloadInflationRatio
	if workloadInflationRatio > 1 {
		var inflationPods []*corev1.Pod
		inflationNum := int(math.Ceil(float64(n)*workloadInflationRatio)) - n
		fmt.Printf("[INFO] [GenerateWorkloadInflationPods] workload inflation ratio: %.4f, the number of inflation pods: %d\n",
			workloadInflationRatio, inflationNum)
		for i := 0; i < inflationNum; i++ {
			rand.Seed(time.Now().UnixNano())
			idx := rand.Intn(n)
			podCloned, err := utils.MakeValidPodByPod(sim.originalWorkloadPods[idx].DeepCopy())
			if err != nil {
				fmt.Printf("[Error] failed to clone pod(%s)\n", utils.GeneratePodKey(sim.originalWorkloadPods[idx]))
				continue
			}
			podCloned.Name = fmt.Sprintf("%s-clone-%s-%d", podCloned.Name, tag, i)
			inflationPods = append(inflationPods, podCloned)
		}
		return inflationPods
	}
	return nil
}
