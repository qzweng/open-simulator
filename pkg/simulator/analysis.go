package simulator

import (
	"fmt"

	"github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

func (sim *Simulator) ClusterAnalysis(result *simontype.SimulateResult) {
	if result == nil || len(result.NodeStatus) == 0 {
		return
	}
	sim.nodeResourceMap = utils.GetNodeResourceMap(result.NodeStatus)

	ch := make(chan utils.FragAmount)
	for _, ns := range result.NodeStatus {
		go func(ns simontype.NodeStatus) {
			var nodeFragAmount utils.FragAmount
			if nodeRes, ok := sim.nodeResourceMap[ns.Node.Name]; ok {
				nodeFragAmount = sim.NodeGpuFragAmount(nodeRes)
			} else {
				fmt.Printf("[Error] nodeName %s not found in nodeResourceMap\n", ns.Node.Name)
			}
			ch <- nodeFragAmount
		}(ns)
	}

	chCount := 0
	data := make([]float64, len(utils.FragRatioDataMap))
	printCache := make([]string, len(result.NodeStatus))
	clusterFragAmount := utils.FragAmount{NodeName: "cluster", Data: data}
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

	nodeAllocMap, err := utils.GetNodeAllocMap(result)
	if err != nil {
		fmt.Printf("[Error] %s\n", err.Error())
	}

	fmt.Println("\n========== Cluster Analysis Results ==========")
	utils.ReportNodeAllocationRate(nodeAllocMap)

	var gpuFragSum float64
	var FragRatioDataReverseMap = map[int]string{}
	for k, v := range utils.FragRatioDataMap {
		val := clusterFragAmount.Data[v]
		gpuFragSum += val
		FragRatioDataReverseMap[v] = k
	}

	for v := 0; v < len(utils.FragRatioDataMap); v++ {
		k := FragRatioDataReverseMap[v]
		val := clusterFragAmount.Data[v]
		fmt.Printf("%-13s: %6.2f x 10^3 (%5.2f%%)\n", k, val/1000, 100*val/gpuFragSum)
	}
	fmt.Printf("--------------------\n%-13s: %6.2f x 10^3 (100.0%%)\n", "GPU Milli Idle", gpuFragSum/1000)
	fmt.Println("==============================================\n")

	for _, line := range printCache {
		fmt.Printf(line)
	}
	fmt.Println()
}

func (sim *Simulator) NodeGpuFragAmount(nodeRes simontype.NodeResource) utils.FragAmount {
	if len(sim.typicalPods) <= 0 {
		fmt.Printf("[ERROR] Typical pods are not set.\n")
		return utils.FragAmount{}
	}
	return utils.NodeGpuFragAmount(nodeRes, sim.typicalPods)
}

func (sim *Simulator) SetTypicalPods() {
	sim.typicalPods = utils.GetTypicalPods(sim.originalWorkloadPods, true)
}

func (sim *Simulator) NodeGpuFragAmountMap(nodeResourceMap map[string]simontype.NodeResource) map[string]utils.FragAmount {
	nodeFragAmountMap := make(map[string]utils.FragAmount)
	for nodeName, nodeRes := range nodeResourceMap {
		nodeFragAmountMap[nodeName] = sim.NodeGpuFragAmount(nodeRes)
	}
	return nodeFragAmountMap
}
