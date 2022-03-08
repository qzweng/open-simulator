package simulator

import (
	"fmt"
	"sort"

	"github.com/alibaba/open-simulator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	schedutil "k8s.io/kubernetes/pkg/scheduler/util"

	gpushareutils "github.com/alibaba/open-gpu-share/pkg/utils"
)

func (sim *Simulator) ClusterAnalysis(result *SimulateResult) {
	ch := make(chan FragAmount)

	var allPods []corev1.Pod
	for _, ns := range result.NodeStatus {
		for _, pod := range ns.Pods {
			allPods = append(allPods, *pod)
		}
	}
	//fmt.Printf("[DEBUG] Before GO LOOP: len of allPods: %d\n", len(allPods))

	for _, ns := range result.NodeStatus {
		go func(ns NodeStatus) {
			node := ns.Node
			//fmt.Printf("IN LOOP ns.NodeName: %s\n", ns.Node.Name)
			allocatable := node.Status.Allocatable
			reqs, _ := utils.GetPodsTotalRequestsAndLimitsByNodeName(allPods, node.Name)
			nodeCpuReq, _ := reqs[corev1.ResourceCPU], reqs[corev1.ResourceMemory]

			gpuMemTotal := gpushareutils.GetTotalGpuMemory(node)
			gpuNumber := gpushareutils.GetGpuCountInNode(node)
			gpuMemLeftList := make([]int64, gpuNumber)
			for i := 0; i < gpuNumber; i++ {
				gpuMemLeftList[i] = gpuMemTotal / int64(gpuNumber)
			}

			if gpuNodeInfoStr, err := utils.GetGpuNodeInfoFromAnnotation(node); err != nil {
				klog.Errorf(err.Error())
			} else {
				if gpuNodeInfoStr != nil {
					for _, dev := range gpuNodeInfoStr.DevsBrief {
						gpuMemLeftList[dev.Idx] -= dev.GpuUsedMemory.Value()
					}
				}
			}

			nodeRes := TargetNodeResource{
				NodeName:       node.Name,
				MilliCpu:       allocatable.Cpu().MilliValue() - nodeCpuReq.MilliValue(),
				GpuMemLeftList: gpuMemLeftList,
				GpuMemTotal:    gpuMemTotal,
				GpuNumber:      gpuNumber,
			}
			//fmt.Printf("[DEBUG] nodeRes: %s\n", nodeRes.Repr())
			//fmt.Printf("GPU Mem Idle: %6.2f GiB\n", float64(nodeRes.GpuMemory)/1024/1024/1024)

			nodeFragAmount := sim.NodeGpuFragAmount(nodeRes)
			//fmt.Printf("END OF GO FUNC node %s: nodeResource: %v\n", node.Name, nodeRes)
			ch <- nodeFragAmount
		}(ns)
	}

	chCount := 0

	data := make([]float64, len(FragRatioDataMap))
	printCache := make([]string, len(result.NodeStatus))
	clusterFragAmount := FragAmount{"cluster", data}
	for nodeFragAmount := range ch {
		if err := clusterFragAmount.Add(nodeFragAmount); err != nil {
			fmt.Printf("error: %s\n", err.Error())
		}
		printCache[chCount] = fmt.Sprintf("[%3d] Frag %s\n", chCount, nodeFragAmount.Repr())
		//fmt.Printf("[%3d] Collected: %s\n", chCount, clusterFragAmount.Repr())
		//fmt.Printf("[%3d] Frag %s\n", chCount, nodeFragAmount.Repr())
		chCount += 1
		if chCount == len(result.NodeStatus) {
			break
		}
	}

	fmt.Println("\n========== Cluster Analysis Results ==========")
	var gpuFragSum float64
	var FragRatioDataReverseMap = map[int]string{}
	for k, v := range FragRatioDataMap {
		val := clusterFragAmount.data[v]
		gpuFragSum += val
		FragRatioDataReverseMap[v] = k
	}

	for v := 0; v < len(FragRatioDataMap); v++ {
		k := FragRatioDataReverseMap[v]
		val := clusterFragAmount.data[v]
		valGiB := val / 1024 / 1024 / 1024
		if valGiB < 1000 {
			fmt.Printf("%-13s: %6.2f GiB (%5.2f%%)\n", k, valGiB, 100*val/gpuFragSum)
		} else {
			fmt.Printf("%-13s: %6.2f TiB (%5.2f%%)\n", k, valGiB/1024, 100*val/gpuFragSum)
		}
	}
	gpuFragSumGiB := gpuFragSum / 1024 / 1024 / 1024
	if gpuFragSumGiB < 1000 {
		fmt.Printf("--------------------\n%-13s: %6.2f GiB (100.0%%)\n", "GPU Mem Idle", gpuFragSumGiB)
	} else {
		fmt.Printf("--------------------\n%-13s: %6.2f TiB (100.0%%)\n", "GPU Mem Idle", gpuFragSumGiB/1024)
	}
	fmt.Println("==============================================\n")

	for _, line := range printCache {
		fmt.Printf(line)
	}
	fmt.Println()
}

func (sim *Simulator) NodeGpuFragRatio(nodeRes TargetNodeResource) FragRatio {
	data := make([]float64, len(FragRatioDataMap))
	fragRatio := FragRatio{nodeRes.NodeName, data}
	for _, pod := range sim.typicalPods {
		freq := pod.percentage
		if freq < 0 || freq > 1 {
			klog.Errorf("pod %v has bad freq: %f\n", pod.targetPodResource, freq)
			continue
		}
		fragType := GetNodePodFrag(nodeRes, pod.targetPodResource)
		//fmt.Printf("[DEBUG] nodeRes: %s; pod: %s => fragType: %s\n", nodeRes.Repr(), pod.targetPodResource.Repr(), fragType)
		if err := fragRatio.AddRatio(fragType, freq); err != nil {
			fmt.Println(err.Error())
		}
	}
	return fragRatio
}

func (sim *Simulator) NodeGpuFragAmount(nodeRes TargetNodeResource) FragAmount {
	fragRatio := sim.NodeGpuFragRatio(nodeRes)
	fragAmount := FragAmount{nodeRes.NodeName, fragRatio.data}

	var gpuMemLeftTotal int64
	for _, gpuMemLeft := range nodeRes.GpuMemLeftList {
		gpuMemLeftTotal += gpuMemLeft
	}

	for i := 0; i < len(fragAmount.data); i++ {
		fragAmount.data[i] *= float64(gpuMemLeftTotal)
	}
	//fmt.Printf("IN NodeGpuFragAmount: %s\n", fragAmount.Repr())
	return fragAmount
}

func (sim *Simulator) GetTypicalPods(cluster ResourceTypes) {
	tgtPodResCntMap := map[TargetPodResource]float64{}
	for _, pod := range cluster.Pods {
		gpuNumber := gpushareutils.GetGpuCountFromPodAnnotation(pod)
		gpuMemory := gpushareutils.GetGpuMemoryFromPodAnnotation(pod)
		if gpuNumber > 1 {
			gpuMemory /= gpuNumber
		}

		var non0CPU, non0Mem int64
		for _, c := range pod.Spec.Containers {
			non0CPUReq, non0MemReq := schedutil.GetNonzeroRequests(&c.Resources.Requests)
			non0CPU += non0CPUReq
			non0Mem += non0MemReq
		}

		tgtPodRes := TargetPodResource{
			MilliCpu:  non0CPU,
			GpuNumber: gpuNumber,
			GpuMemory: gpuMemory,
		}

		if cnt, ok := tgtPodResCntMap[tgtPodRes]; ok {
			tgtPodResCntMap[tgtPodRes] = cnt + 1
		} else {
			tgtPodResCntMap[tgtPodRes] = 1
		}
	}

	tgtPodList := GetTargetPodListFromTargetPodResourceMap(tgtPodResCntMap)
	fmt.Printf("\nNum of Total Pods: %d\n", len(cluster.Pods))
	fmt.Printf("Num of Total Pod Sepc: %d\n", len(tgtPodList))
	ExpectedNumPods := int(TypicalPodPopularityThreshold * len(cluster.Pods) / 100)
	var i, podResNum int
	var numPods float64
	for int(numPods) < ExpectedNumPods {
		podResNum += TypicalPodResourceNumber
		for i < podResNum && i < len(tgtPodList) {
			numPods += tgtPodList[i].percentage
			fmt.Printf("[%d] %s: %.0f\n", i, tgtPodList[i].targetPodResource.Repr(), tgtPodList[i].percentage)
			i += 1
		}
	}

	fmt.Printf("\nCount top %d pod resource spec as typical ones, accounting for %.2f%% of all pods\n", i, 100.0*float64(numPods)/float64(len(cluster.Pods)))
	for j, tp := range tgtPodList[:i] {
		tgtPodList[j].percentage = tp.percentage / numPods
		fmt.Printf("[%d] %s: %.1f%%\n", j, tp.targetPodResource.Repr(), tgtPodList[j].percentage*100)
	}
	sim.typicalPods = tgtPodList[:i]
}

func (sim *Simulator) NodeAnalysis(nodeStatus NodeStatus) FragAmount {
	return FragAmount{}
}

const (
	Q1LackBoth  = "q1_lack_both"
	Q2LackGpu   = "q2_lack_gpu"
	Q3Satisfied = "q3_satisfied"
	Q4LackCpu   = "q4_lack_cpu"
	XLSatisfied = "xl_satisfied"
	XRLackCPU   = "xr_lack_cpu"
	NoAccess    = "no_access"
)

var FragRatioDataMap = map[string]int{
	Q1LackBoth:  0,
	Q2LackGpu:   1,
	Q3Satisfied: 2,
	Q4LackCpu:   3,
	XLSatisfied: 4,
	XRLackCPU:   5,
	NoAccess:    6,
}

type FragRatio struct {
	nodeName string
	data     []float64
}

type FragAmount struct {
	nodeName string
	data     []float64
}

func (fr FragRatio) AddRatio(fragType string, freq float64) error {
	if freq < 0 || freq > 1 {
		return fmt.Errorf("bad freq")
	}
	if index, ok := FragRatioDataMap[fragType]; !ok {
		return fmt.Errorf("bad fragType")
	} else {
		fr.data[index] += freq
		return nil
	}
}

func (fa FragAmount) Add(faOther FragAmount) error {
	if len(fa.data) == 0 {
		fa.data = faOther.data
		return nil
	}
	if len(fa.data) != len(faOther.data) {
		return fmt.Errorf("this (%d) does not match the other (%d)", len(fa.data), len(faOther.data))
	}
	for i := 0; i < len(fa.data); i++ {
		fa.data[i] += faOther.data[i]
	}
	return nil
}

func (fa FragAmount) Repr() (outStr string) {
	outStr += fa.nodeName
	outStr += ": ["
	for i, v := range fa.data {
		if i > 0 {
			outStr += ", "
		}
		outStr += fmt.Sprintf("%6.2f GiB", v/1024/1024/1024)
	}
	outStr += "]"
	return outStr
}

func GetTargetPodListFromTargetPodResourceMap(tgtPodResMap map[TargetPodResource]float64) TargetPodList {
	pl := make(TargetPodList, len(tgtPodResMap))
	i := 0
	for k, v := range tgtPodResMap {
		pl[i] = TargetPod{k, v}
		i++
	}
	sort.Sort(sort.Reverse(pl))
	return pl
}

func CanNodeHostPodOnGpuMemory(nodeRes TargetNodeResource, podRes TargetPodResource) bool {
	gpuRequest := podRes.GpuNumber
	for _, gpuHostMem := range nodeRes.GpuMemLeftList {
		if gpuHostMem >= podRes.GpuMemory {
			gpuRequest -= 1
			if gpuRequest <= 0 {
				return true
			}
		}
	}
	return false
}

func IsNodeAccessibleToPod(nodeRes TargetNodeResource, podRes TargetPodResource) bool {
	if podRes.GpuMemory > 0 {
		if nodeRes.GpuNumber <= 0 {
			return false
		}
		gpuMemEach := nodeRes.GpuMemTotal / int64(nodeRes.GpuNumber)
		if gpuMemEach < podRes.GpuMemory {
			//fmt.Printf("[DEBUG] gpuMemEach (%d) < podRes.GpuMemory (%d) => no_access\n", gpuMemEach, podRes.GpuMemory)
			return false
		}
	}
	return true
}

func GetNodePodFrag(nodeRes TargetNodeResource, podRes TargetPodResource) string {
	if podRes.GpuMemory == 0 {
		if nodeRes.MilliCpu >= podRes.MilliCpu {
			return XLSatisfied
		} else {
			return XRLackCPU
		}
	}

	if IsNodeAccessibleToPod(nodeRes, podRes) == false {
		return NoAccess
	}

	if CanNodeHostPodOnGpuMemory(nodeRes, podRes) {
		if nodeRes.MilliCpu >= podRes.MilliCpu {
			return Q3Satisfied
		} else {
			return Q4LackCpu
		}
	} else {
		if nodeRes.MilliCpu >= podRes.MilliCpu {
			return Q2LackGpu
		} else {
			return Q1LackBoth
		}
	}
}
