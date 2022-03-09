package simulator

import (
	"fmt"

	"github.com/alibaba/open-simulator/pkg/simulator/plugin"
	"github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

func (sim *Simulator) ClusterAnalysis(result *simontype.SimulateResult) {
	sim.nodeResourceMap = plugin.GetNodeResourceMap(result)

	ch := make(chan utils.FragAmount)
	for _, ns := range result.NodeStatus {
		go func(ns simontype.NodeStatus) {
			var nodeFragAmount utils.FragAmount
			if nodeRes, ok := sim.nodeResourceMap[ns.Node.Name]; ok {
				nodeFragAmount = sim.NodeGpuFragAmount(nodeRes)
			}
			ch <- nodeFragAmount
		}(ns)
	}

	chCount := 0
	data := make([]float64, len(utils.FragRatioDataMap))
	printCache := make([]string, len(result.NodeStatus))
	clusterFragAmount := utils.FragAmount{"cluster", data}
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
	for k, v := range utils.FragRatioDataMap {
		val := clusterFragAmount.Data[v]
		gpuFragSum += val
		FragRatioDataReverseMap[v] = k
	}

	for v := 0; v < len(utils.FragRatioDataMap); v++ {
		k := FragRatioDataReverseMap[v]
		val := clusterFragAmount.Data[v]
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

func (sim *Simulator) NodeGpuFragAmount(nodeRes simontype.TargetNodeResource) utils.FragAmount {
	return utils.NodeGpuFragAmount(nodeRes, sim.typicalPods)
}

func (sim *Simulator) GetTypicalPods(cluster ResourceTypes) {
	sim.typicalPods = utils.GetTypicalPods(cluster.Pods)
}
