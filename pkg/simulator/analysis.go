package simulator

import (
	"fmt"

	"github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

func (sim *Simulator) ClusterAnalysis(nodeStatus []simontype.NodeStatus, verbose int) (utils.FragAmount, []utils.ResourceSummary) {
	// verbose: 0 -- no print; 1 -- only cluster analysis; 2 -- all info
	if len(nodeStatus) == 0 {
		return utils.FragAmount{}, nil
	}
	sim.nodeResourceMap = utils.GetNodeResourceMap(nodeStatus)

	ch := make(chan utils.FragAmount)
	for _, ns := range nodeStatus {
		go func(ns simontype.NodeStatus) {
			var nodeFragAmount utils.FragAmount
			if nodeRes, ok := sim.nodeResourceMap[ns.Node.Name]; ok {
				nodeFragAmount = sim.NodeGpuFragAmount(nodeRes)
			} else {
				fmt.Printf("[ERROR] nodeName %s not found in nodeResourceMap\n", ns.Node.Name)
			}
			ch <- nodeFragAmount
		}(ns)
	}

	chCount := 0
	data := make([]float64, len(utils.FragRatioDataMap))
	clusterFragAmount := utils.FragAmount{NodeName: "cluster", Data: data}
	for nodeFragAmount := range ch {
		if err := clusterFragAmount.Add(nodeFragAmount); err != nil {
			fmt.Printf("[ERROR][ClusterAnalysis] %s\n", err.Error())
		}
		if verbose >= 2 {
			fmt.Printf("[%3d] Frag %s\n", chCount, nodeFragAmount.Repr())
		}
		//fmt.Printf("[%3d] Collected: %s\n", chCount, clusterFragAmount.Repr())
		//fmt.Printf("[%3d] Frag %s\n", chCount, nodeFragAmount.Repr())
		chCount += 1
		if chCount == len(nodeStatus) {
			break
		}
	}

	nodeAllocMap, err := utils.GetNodeAllocMap(nodeStatus)
	if err != nil {
		fmt.Printf("[ERROR][ClusterAnalysis] %s\n", err.Error())
	}

	if verbose >= 1 {
		fmt.Println("\n========== Cluster Analysis Results ==========")
	}
	resourceSummaries := utils.ReportNodeAllocationRate(nodeAllocMap, verbose)

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
		if verbose >= 1 {
			fmt.Printf("\n%-13s: %6.2f x 10^3 (%5.2f%%)", k, val/1000, 100*val/gpuFragSum)
		}
	}
	if verbose >= 1 {
		fmt.Printf("\n--------------------\n")
		fmt.Printf("%-13s: %6.2f x 10^3 (100.0%%)\n", "Idle GPU Milli", gpuFragSum/1000)
		val := clusterFragAmount.FragAmountSumExceptQ3()
		fmt.Printf("%-13s: %6.2f x 10^3 (%5.2f%%)\n", "Frag GPU Milli", val/1000, 100*val/gpuFragSum)
		fmt.Printf("==============================================\n\n")
	}
	return clusterFragAmount, resourceSummaries
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
