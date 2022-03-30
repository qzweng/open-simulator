package simulator

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	storagev1 "k8s.io/api/storage/v1"

	"github.com/alibaba/open-simulator/pkg/api/v1alpha1"
	"github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

type ResourceTypes struct {
	Nodes                  []*corev1.Node
	Pods                   []*corev1.Pod
	DaemonSets             []*appsv1.DaemonSet
	StatefulSets           []*appsv1.StatefulSet
	Deployments            []*appsv1.Deployment
	ReplicationControllers []*corev1.ReplicationController
	ReplicaSets            []*appsv1.ReplicaSet
	Services               []*corev1.Service
	PersistentVolumeClaims []*corev1.PersistentVolumeClaim
	StorageClasss          []*storagev1.StorageClass
	PodDisruptionBudgets   []*policyv1beta1.PodDisruptionBudget
	Jobs                   []*batchv1.Job
	CronJobs               []*batchv1beta1.CronJob
}

type AppResource struct {
	Name     string
	Resource ResourceTypes
}

type Interface interface {
	RunCluster(cluster ResourceTypes) (*simontype.SimulateResult, error)
	ScheduleApp(AppResource) (*simontype.SimulateResult, error)
	SchedulePods(pods []*corev1.Pod) []simontype.UnscheduledPod

	ClusterAnalysis(result []simontype.NodeStatus, verbose int) (utils.FragAmount, []utils.ResourceSummary)
	GetClusterNodeStatus() []simontype.NodeStatus

	SetOriginalWorkloadPods(pods []*corev1.Pod)
	SetTypicalPods()

	SortClusterPods(pods []*corev1.Pod)

	GenerateWorkloadInflationPods(tag string) []*corev1.Pod

	GetCustomConfig() v1alpha1.CustomConfig

	Deschedule() (*simontype.SimulateResult, error)

	Close()
}

// Simulate
// 参数
// 1. 由使用方自己生成 cluster 和 apps 传参
// 2. apps 将按照顺序模拟部署
// 3. 存储信息以 Json 形式填入对应的 Node 资源中
// 返回值
// 1. error 不为空表示函数执行失败
// 2. error 为空表示函数执行成功，通过 SimulateResult 信息获取集群模拟信息。其中 UnscheduledPods 表示无法调度的 Pods，若其为空表示模拟调度成功；NodeStatus 会详细记录每个 Node 上的 Pod 情况。
func Simulate(cluster ResourceTypes, apps []AppResource, opts ...Option) (*simontype.SimulateResult, error) {
	// init simulator
	sim, err := New(opts...)
	if err != nil {
		return nil, err
	}
	defer sim.Close()

	cluster.Pods, err = GetValidPodExcludeDaemonSet(cluster)
	if err != nil {
		return nil, err
	}
	sim.SetOriginalWorkloadPods(cluster.Pods)
	sim.SetTypicalPods()

	sim.SortClusterPods(cluster.Pods)

	for _, item := range cluster.DaemonSets {
		validPods, err := utils.MakeValidPodsByDaemonset(item, cluster.Nodes)
		if err != nil {
			return nil, err
		}
		cluster.Pods = append(cluster.Pods, validPods...)
	}

	var failedPods []simontype.UnscheduledPod

	// run cluster
	result, err := sim.RunCluster(cluster) // Existing pods in the cluster are scheduled here.
	if err != nil {
		return nil, err
	}
	failedPods = append(failedPods, result.UnscheduledPods...)
	reportFailedPods(failedPods)
	sim.ClusterAnalysis(result.NodeStatus, 1)

	inflationPods := sim.GenerateWorkloadInflationPods("schedule")
	fp := sim.SchedulePods(inflationPods)
	reportFailedPods(fp)
	failedPods = append(failedPods, fp...)

	customConfig := sim.GetCustomConfig()
	if customConfig.DeschedulePolicy != "" {
		result, _ = sim.Deschedule()
		failedPods = append(failedPods, result.UnscheduledPods...)
		sim.ClusterAnalysis(result.NodeStatus, 1)
	}

	// schedule pods
	for _, app := range apps {
		result, err = sim.ScheduleApp(app)
		if err != nil {
			return nil, err
		}
		failedPods = append(failedPods, result.UnscheduledPods...)
	}
	//result.UnscheduledPods = failedPods
	//sim.ClusterAnalysis(result)

	return &simontype.SimulateResult{
		UnscheduledPods: failedPods,
		NodeStatus:      sim.GetClusterNodeStatus(),
	}, nil
}

func reportFailedPods(fp []simontype.UnscheduledPod) {
	if len(fp) == 0 {
		return
	}
	fmt.Printf("Failed Pods in detail:\n")
	for _, up := range fp {
		podResoure := utils.GetPodResource(up.Pod)
		fmt.Printf("  %s: %s\n", utils.GeneratePodKey(up.Pod), podResoure.Repr())
	}
	fmt.Println()
}
