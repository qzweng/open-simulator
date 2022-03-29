package simulator

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	storagev1 "k8s.io/api/storage/v1"

	"github.com/alibaba/open-simulator/pkg/api/v1alpha1"
	"github.com/alibaba/open-simulator/pkg/type"
	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
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
	SetTypicalPods(cluster ResourceTypes)
	ClusterAnalysis(result *simontype.SimulateResult)
	Deschedule() (*simontype.SimulateResult, error)
	GetCustomConfig() v1alpha1.CustomConfig
	AddParaSet()
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
	sim.SetTypicalPods(cluster)

	shufflePod := sim.GetCustomConfig().ShufflePod
	if shufflePod {
		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(cluster.Pods), func(i, j int) {
			cluster.Pods[i], cluster.Pods[j] = cluster.Pods[j], cluster.Pods[i]
		})
	} else {
		timeNow := time.Now() //.Format(time.RFC3339)
		sort.SliceStable(cluster.Pods, func(i, j int) bool {
			var timeI, timeJ time.Time
			if timeStr, ok := cluster.Pods[i].Annotations[gpushareutils.CreationTime]; ok {
				timeI, err = time.Parse(time.RFC3339, timeStr)
				if err != nil {
					fmt.Printf("[Error] Time Parse %s err: %s\n", timeStr, err.Error())
					timeI = timeNow
				}
			} else {
				//fmt.Printf("[Info] No timestamp for pod %s\n", utils.GeneratePodKey(cluster.Pods[i]))
				timeI = timeNow
			}

			if timeStr, ok := cluster.Pods[j].Annotations[gpushareutils.CreationTime]; ok {
				timeJ, err = time.Parse(time.RFC3339, timeStr)
				if err != nil {
					fmt.Printf("[Error] Time Parse %s err: %s\n", timeStr, err.Error())
					timeJ = timeNow
				}
			} else {
				//fmt.Printf("[Info] No timestamp for pod %s\n", utils.GeneratePodKey(cluster.Pods[i]))
				timeJ = timeNow
			}
			return timeI.Before(timeJ) || (timeI.Equal(timeJ) && cluster.Pods[i].Name < cluster.Pods[j].Name)
		})
	}

	// workload inflation
	workloadInflationRatio := sim.GetCustomConfig().WorkloadInflationRatio
	if workloadInflationRatio > 1 {
		var inflationPods []*corev1.Pod
		numBeforeInflation := len(cluster.Pods)
		numAfterInflation := int(math.Ceil(float64(numBeforeInflation) * workloadInflationRatio))
		fmt.Printf("[INFO] workload inflation ratio: %.4f, before: %d, after: %d\n", workloadInflationRatio, numBeforeInflation, numAfterInflation)
		for i := 0; i < numAfterInflation-numBeforeInflation; i++ {
			rand.Seed(time.Now().UnixNano())
			idx := rand.Intn(numBeforeInflation)
			podCloned, err := utils.MakeValidPodByPod(cluster.Pods[idx].DeepCopy())
			if err != nil {
				fmt.Printf("[ERROR] failed to clone pod(%s)\n", utils.GeneratePodKey(cluster.Pods[idx]))
				continue
			}
			podCloned.Name = fmt.Sprintf("%s-clone-%d", podCloned.Name, i)
			inflationPods = append(inflationPods, podCloned)
		}
		cluster.Pods = append(cluster.Pods, inflationPods...)
	} else {
		fmt.Printf("[INFO] workload inflation ratio(%.4f) is not larger than than 1\n", workloadInflationRatio)
	}

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
	ReportFailedPods(failedPods)
	//sim.ClusterAnalysis(result)

	customConfig := sim.GetCustomConfig()
	if customConfig.DeschedulePolicy != "" {
		result, _ = sim.Deschedule()
		failedPods = append(failedPods, result.UnscheduledPods...)
		//sim.ClusterAnalysis(result)
	}

	// schedule pods
	for _, app := range apps {
		result, err = sim.ScheduleApp(app)
		if err != nil {
			return nil, err
		}
		failedPods = append(failedPods, result.UnscheduledPods...)
	}
	result.UnscheduledPods = failedPods
	//sim.ClusterAnalysis(result)

	return result, nil
}

func ReportFailedPods(fp []simontype.UnscheduledPod) {
	fmt.Printf("Failed Pods in detail:\n")
	for _, up := range fp {
		podResoure := utils.GetPodResource(up.Pod)
		fmt.Printf("  %s: %s\n", utils.GeneratePodKey(up.Pod), podResoure.Repr())
	}
	fmt.Println()
}
