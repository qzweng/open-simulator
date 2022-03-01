package simulator

import (
	"context"
	"fmt"
	"github.com/pquerna/ffjson/ffjson"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubeinformers "k8s.io/client-go/informers"
	externalclientset "k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	kubetypes "k8s.io/kubernetes/pkg/kubelet/types"
	"k8s.io/kubernetes/pkg/scheduler"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	"sort"
	"strings"
	"time"

	gpusharecache "github.com/alibaba/open-gpu-share/pkg/cache"
	"github.com/alibaba/open-simulator/pkg/algo"
	simonplugin "github.com/alibaba/open-simulator/pkg/simulator/plugin"
	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

// Simulator is used to simulate a cluster and pods scheduling
type Simulator struct {
	// kube client
	// externalclient  externalclientset.Interface
	fakeclient      externalclientset.Interface
	informerFactory informers.SharedInformerFactory

	// scheduler
	scheduler *scheduler.Scheduler

	// stopCh
	updateBarrier map[string]chan struct{}

	// context
	ctx        context.Context
	cancelFunc context.CancelFunc

	status status
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
	fakeClient := fakeclientset.NewSimpleClientset()
	sharedInformerFactory := informers.NewSharedInformerFactory(fakeClient, 0)

	// Step 4: Create the simulator
	ctx, cancel := context.WithCancel(context.Background())
	updateBarrier := map[string]chan struct{}{
		simontype.SimulatorName: make(chan struct{}),
	}
	sim := &Simulator{
		// externalclient:  kubeClient,
		fakeclient:      fakeClient,
		updateBarrier:   updateBarrier,
		informerFactory: sharedInformerFactory,
		ctx:             ctx,
		cancelFunc:      cancel,
	}

	// Step 5: add event handler for pods
	sim.informerFactory.Core().V1().Pods().Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				if pod, ok := obj.(*corev1.Pod); ok && pod.Spec.SchedulerName == simontype.DefaultSchedulerName {
					return true
				}
				return false
			},
			Handler: cache.ResourceEventHandlerFuncs{
				// AddFunc: func(obj interface{}) {
				// 	if pod, ok := obj.(*corev1.Pod); ok {
				// 		fmt.Printf("test add pod %s/%s\n", pod.Namespace, pod.Name)
				// 	}
				// },
				UpdateFunc: func(oldObj, newObj interface{}) {
					if pod, ok := newObj.(*corev1.Pod); ok {
						//fmt.Printf("test update pod %s/%s\n", pod.Namespace, pod.Name)
						sim.update(pod)
						//sim.updateBarrier <- struct{}{}
					}
				},
				DeleteFunc: func(obj interface{}) {
					if pod, ok := obj.(*corev1.Pod); ok {
						//fmt.Printf("test delete pod %s/%s\n", pod.Namespace, pod.Name)
						//time.Sleep(2 * time.Second)
						namespace, name := pod.Namespace, pod.Name
						nodeName := pod.Spec.NodeName
						fmt.Printf("delete4: pod %s/%s, node %s\n", namespace, name, nodeName)
						for {
							time.Sleep(2 * time.Millisecond)

							podFoundInCache := false
							if p, _ := sim.scheduler.SchedulerCache.GetPod(pod); p != nil {
								podFoundInCache = true
							}

							node, _ := sim.fakeclient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
							nodeGpuInfoStr, found := node.ObjectMeta.Annotations[simontype.AnnoNodeGpuShare]
							if !found {
								if !podFoundInCache {
									break
								}
								continue
							}
							var nodeGpuInfo gpusharecache.NodeGpuInfo
							err = ffjson.Unmarshal([]byte(nodeGpuInfoStr), &nodeGpuInfo)
							if err != nil {
								panic("unmarshal panic")
							}
							podFoundInAnno := false
							for _, dev := range nodeGpuInfo.DevsBrief {
								for _, pStr := range dev.PodList {
									if fmt.Sprintf("%s:%s", pod.Namespace, pod.Name) == pStr {
										podFoundInAnno = true
									}
								}
							}

							if !podFoundInAnno && !podFoundInCache {
								break
							}
						}
						sim.updateBarrier[simontype.SimulatorName] <- struct{}{}
						fmt.Printf("delete5: pod %s/%s\n", namespace, name)
					}
				},
			},
		},
	)

	//sim.informerFactory.Core().V1().Nodes().Informer().AddEventHandler(
	//	cache.ResourceEventHandlerFuncs{
	//		UpdateFunc: func(oldObj, newObj interface{}) {
	//			if node, ok := newObj.(*corev1.Node); ok {
	//				fmt.Printf("NODE UPDATE %s\n", node.Name)
	//				sim.updateBarrier <- struct{}{}
	//			}
	//		},
	//	},
	//)

	// Step 6: create scheduler for fake cluster
	kubeSchedulerConfig.Client = fakeClient
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(sim.fakeclient, 0)
	storagev1Informers := kubeInformerFactory.Storage().V1()
	scInformer := storagev1Informers.StorageClasses().Informer()
	kubeInformerFactory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), scInformer.HasSynced)
	bindRegistry := frameworkruntime.Registry{
		simontype.SimonPluginName: func(configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
			return simonplugin.NewSimonPlugin(sim.fakeclient, configuration, f)
		},
		simontype.OpenLocalPluginName: func(configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
			return simonplugin.NewLocalPlugin(fakeClient, storagev1Informers, configuration, f)
		},
		simontype.OpenGpuSharePluginName: func(configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
			return simonplugin.NewGpuSharePlugin(fakeClient, configuration, f)
		},
	}
	sim.scheduler, err = scheduler.New(
		sim.fakeclient,
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
func (sim *Simulator) RunCluster(cluster ResourceTypes) (*SimulateResult, error) {
	// start scheduler
	sim.runScheduler()

	return sim.syncClusterResourceList(cluster)
}

func (sim *Simulator) ScheduleApp(apps AppResource) (*SimulateResult, error) {
	// 由 AppResource 生成 Pods
	appPods, err := GenerateValidPodsFromAppResources(sim.fakeclient, apps.Name, apps.Resource)
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

	return &SimulateResult{
		UnscheduledPods: failedPod,
		NodeStatus:      sim.getClusterNodeStatus(),
	}, nil
}

func (sim *Simulator) ClusterAnalysis() {

}

func (sim *Simulator) nodeAnalysis(node corev1.Node) {

}

func (sim *Simulator) Deschedule(apps AppResource) (*SimulateResult, error) {
	appPods, _ := GenerateValidPodsFromAppResources(sim.fakeclient, apps.Name, apps.Resource)
	appPodsMap := make(map[string]*corev1.Pod)
	for _, pod := range appPods {
		appPodsMap[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)] = pod
	}

	nodeStatus := sim.getClusterNodeStatus()
	for _, ns := range nodeStatus {
		for _, pod := range ns.Pods {
			sim.deletePod(pod) // delete all pods
			sim.createPod(appPodsMap[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)])
			//sim.deletePod(appPodsMap[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)])
		}
	}

	//sim.createPod()
	//time.Sleep(60 * time.Second)
	return &SimulateResult{
		UnscheduledPods: nil,
		NodeStatus:      sim.getClusterNodeStatus(),
	}, nil
}

func (sim *Simulator) AddParaSet() {

}

func (sim *Simulator) getClusterNodeStatus() []NodeStatus {
	var nodeStatues []NodeStatus
	nodes, _ := sim.fakeclient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	allPods, _ := sim.fakeclient.CoreV1().Pods(corev1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
	for _, node := range nodes.Items {
		nodeStatus := NodeStatus{}
		nodeStatus.Node = node.DeepCopy()
		nodeStatus.Pods = make([]*corev1.Pod, 0)
		for _, pod := range allPods.Items {
			if pod.Spec.NodeName != node.Name {
				continue
			}
			nodeStatus.Pods = append(nodeStatus.Pods, pod.DeepCopy())
		}
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

func (sim *Simulator) createPod(pod *corev1.Pod) error {
	if _, err := sim.fakeclient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("%s %s/%s: %s", simontype.CreatePodError, pod.Namespace, pod.Name, err.Error())
	}

	<-sim.updateBarrier[simontype.SimulatorName]
	//if gpushareutils.GetGpuMemoryFromPodAnnotation(pod) > 0 {
	//	<-sim.updateBarrier
	//}
	return nil
}

func (sim *Simulator) deletePod(pod *corev1.Pod) error {
	namespace, name := pod.Namespace, pod.Name
	if err := sim.fakeclient.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("%s %s/%s: %s", simontype.DeletePodError, pod.Namespace, pod.Name, err.Error())
	}

	fmt.Printf("-------------------------\ndelete0: pod %s/%s\n", namespace, name)
	<-sim.updateBarrier[simontype.SimulatorName] // wait for pod update
	fmt.Printf("delete1: pod %s/%s\n=========================", namespace, name)
	//if gpushareutils.GetGpuMemoryFromPodAnnotation(pod) > 0 {
	//	<-sim.updateBarrier // wait for node update
	//}
	return nil
}

// Run starts to schedule pods
func (sim *Simulator) schedulePods(pods []*corev1.Pod) ([]UnscheduledPod, error) {
	var failedPods []UnscheduledPod
	for _, pod := range pods {
		sim.createPod(pod)

		//sim.deletePod(pod)

		//sim.status.stopReason = ""
		if strings.Contains(sim.status.stopReason, "failed") {
			sim.deletePod(pod)
			failedPods = append(failedPods, UnscheduledPod{
				Pod:    pod,
				Reason: sim.status.stopReason,
			})
			sim.status.stopReason = ""
		}
	}
	return failedPods, nil
}

func (sim *Simulator) DeletePod(pod *corev1.Pod) error {
	nodeName := pod.Spec.NodeName
	if nodeName == "" {
		return fmt.Errorf("Empty pod.Spec.NodeName")
	}
	node, err := sim.fakeclient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := sim.fakeclient.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("%s %s/%s: %s", simontype.DeletePodError, pod.Namespace, pod.Name, err.Error())
	}
	nodeGpuInfoStr, found := node.ObjectMeta.Annotations[simontype.AnnoNodeGpuShare]
	if !found {
		return fmt.Errorf("GPU annotation not found in node")
	}
	var nodeGpuInfo gpusharecache.NodeGpuInfo
	err = ffjson.Unmarshal([]byte(nodeGpuInfoStr), &nodeGpuInfo)
	if err != nil {
		return err
	}

	return nil
}

func (sim *Simulator) Close() {
	sim.cancelFunc()
	for _, ub := range sim.updateBarrier {
		close(ub)
	}
}

func (sim *Simulator) syncClusterResourceList(resourceList ResourceTypes) (*SimulateResult, error) {
	//sync node
	for _, item := range resourceList.Nodes {
		if _, err := sim.fakeclient.CoreV1().Nodes().Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy node: %v", err)
		}
	}

	//sync pdb
	for _, item := range resourceList.PodDisruptionBudgets {
		if _, err := sim.fakeclient.PolicyV1beta1().PodDisruptionBudgets(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy PDB: %v", err)
		}
	}

	//sync svc
	for _, item := range resourceList.Services {
		if _, err := sim.fakeclient.CoreV1().Services(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy service: %v", err)
		}
	}

	//sync storage class
	for _, item := range resourceList.StorageClasss {
		if _, err := sim.fakeclient.StorageV1().StorageClasses().Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy storage class: %v", err)
		}
	}

	//sync pvc
	for _, item := range resourceList.PersistentVolumeClaims {
		if _, err := sim.fakeclient.CoreV1().PersistentVolumeClaims(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy pvc: %v", err)
		}
	}

	//sync rc
	for _, item := range resourceList.ReplicationControllers {
		if _, err := sim.fakeclient.CoreV1().ReplicationControllers(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy RC: %v", err)
		}
	}

	//sync deployment
	for _, item := range resourceList.Deployments {
		if _, err := sim.fakeclient.AppsV1().Deployments(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy deployment: %v", err)
		}
	}

	//sync rs
	for _, item := range resourceList.ReplicaSets {
		if _, err := sim.fakeclient.AppsV1().ReplicaSets(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy replica set: %v", err)
		}
	}

	//sync statefulset
	for _, item := range resourceList.StatefulSets {
		if _, err := sim.fakeclient.AppsV1().StatefulSets(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy stateful set: %v", err)
		}
	}

	//sync daemonset
	for _, item := range resourceList.DaemonSets {
		if _, err := sim.fakeclient.AppsV1().DaemonSets(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy daemon set: %v", err)
		}
	}

	// sync pods
	failedPods, err := sim.schedulePods(resourceList.Pods)
	if err != nil {
		return nil, err
	}

	return &SimulateResult{
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
