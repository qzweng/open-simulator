package simulator

import (
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"sync"

	"github.com/alibaba/open-simulator/pkg/type"
	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
	"github.com/alibaba/open-simulator/pkg/utils"
)

const (
	TagInitSchedule        = "InitSchedule"
	TagPostEviction        = "PostEviction"
	TagPostDeschedule      = "PostDeschedule"
	TagScheduleInflation   = "ScheduleInflation"
	TagDescheduleInflation = "DescheduleInflation"
)

// ClusterGpuFragReport Reports the Gpu Frag Amount of all nodes
func (sim *Simulator) ClusterGpuFragReport() {
	nodeStatus := sim.GetClusterNodeStatus()
	if len(nodeStatus) == 0 {
		return
	}
	sim.nodeResourceMap = utils.GetNodeResourceMap(nodeStatus)

	clusterFragAmount := utils.NewFragAmount("cluster", make([]float64, len(utils.FragRatioDataMap)))
	var clusterFragBellman float64
	for _, ns := range nodeStatus {
		if nodeRes, ok := sim.nodeResourceMap[ns.Node.Name]; ok {
			clusterFragAmount.Add(sim.NodeGpuFragAmount(nodeRes)) // easy to calculate. The regular Frag definition
			clusterFragBellman += utils.NodeGpuFragBellman(nodeRes, sim.typicalPods, &sim.fragMemo, 1.0)
		}
	}
	log.Infof("[Report] Frag amount: %.2f (origin)\n", clusterFragAmount.FragAmountSumExceptQ3())
	log.Infof("[Report] Frag amount: %.2f (bellman)\n", clusterFragBellman)
}

func (sim *Simulator) ClusterAnalysis(tag string) (utils.FragAmount, []utils.ResourceSummary) {
	nodeStatus := sim.GetClusterNodeStatus()
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
				log.Errorf("nodeName %s not found in nodeResourceMap\n", ns.Node.Name)
			}
			ch <- nodeFragAmount
		}(ns)
	}

	chCount := 0
	data := make([]float64, len(utils.FragRatioDataMap))
	clusterFragAmount := utils.NewFragAmount("cluster", data)
	for nodeFragAmount := range ch {
		if err := clusterFragAmount.Add(nodeFragAmount); err != nil {
			log.Errorf("[ClusterAnalysis] %s\n", err.Error())
		}
		log.Tracef("[%3d] Frag %s\n", chCount, nodeFragAmount.Repr())
		chCount += 1
		if chCount == len(nodeStatus) {
			break
		}
	}

	nodeAllocMap, err := utils.GetNodeAllocMap(nodeStatus)
	if err != nil {
		log.Errorf("[ClusterAnalysis] %s\n", err.Error())
	}

	log.Infoln()
	log.Infof("========== Cluster Analysis Results (%s) ==========", tag)
	resourceSummaries := utils.ReportNodeAllocationRate(nodeAllocMap)

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
		log.Infof("%-13s: %6.2f x 10^3 (%5.2f%%)\n", k, val/1000, 100*val/gpuFragSum)
	}
	log.Infoln("--------------------")
	log.Infof("%-13s: %6.2f x 10^3 (100.0%%)\n", "idle_gpu_milli", gpuFragSum/1000)
	val := clusterFragAmount.FragAmountSumExceptQ3()
	log.Infof("%-13s: %6.2f x 10^3 (%5.2f%%)\n", "frag_gpu_milli", val/1000, 100*val/gpuFragSum)
	log.Infoln("==============================================")
	log.Infoln()

	return clusterFragAmount, resourceSummaries
}

func (sim *Simulator) NodeGpuFragAmount(nodeRes simontype.NodeResource) utils.FragAmount {
	if len(sim.typicalPods) <= 0 {
		log.Errorf("Typical pods are not set.\n")
		return utils.FragAmount{}
	}

	nodeResKey := nodeRes.Flatten("origin")
	if fa, ok := sim.fragMemo.Load(nodeResKey); ok {
		if frag, ok2 := fa.(utils.FragAmount); ok2 {
			return frag
		}
	} else {
		frag := utils.NodeGpuFragAmount(nodeRes, sim.typicalPods)
		sim.fragMemo.Store(nodeResKey, frag)
		return frag
	}
	return utils.FragAmount{}
}

// RecordPodTotalResourceReq will record the total resource requests of all pods,
// and return the list: [total milli cpu request of all pods, total milli gpu request of all pods]
func (sim *Simulator) RecordPodTotalResourceReq(pods []*corev1.Pod) (int64, int64) {
	// initialization
	sim.podTotalMilliCpuReq = 0
	sim.podTotalMilliGpuReq = 0

	// count them all
	for _, p := range pods {
		podRes := utils.GetPodResource(p)
		sim.podTotalMilliCpuReq += podRes.MilliCpu
		sim.podTotalMilliGpuReq += podRes.MilliGpu * int64(podRes.GpuNumber)
	}
	log.Infof("Total milli cpu request of all pods: %d, milli gpu request: %d\n",
		sim.podTotalMilliCpuReq, sim.podTotalMilliGpuReq)
	return sim.podTotalMilliCpuReq, sim.podTotalMilliGpuReq
}

// RecordNodeTotalResource will record the total resources of all nodes,
// and return the list: [total milli cpu of all nodes, total milli gpu of all nodes]
func (sim *Simulator) RecordNodeTotalResource(nodes []*corev1.Node) (int64, int64) {
	// initialization
	sim.nodeTotalMilliCpu = 0
	sim.nodeTotalMilliGpu = 0

	// count them all
	for _, n := range nodes {
		sim.nodeTotalMilliCpu += n.Status.Capacity.Cpu().MilliValue()
		sim.nodeTotalMilliGpu += int64(gpushareutils.GetGpuMilliOfNode(n))
	}
	log.Infof("Total milli cpu of all nodes: %d, milli gpu: %d\n", sim.nodeTotalMilliCpu, sim.nodeTotalMilliGpu)
	return sim.nodeTotalMilliCpu, sim.nodeTotalMilliGpu
}

func (sim *Simulator) SetTypicalPods() {
	sim.typicalPods = utils.GetTypicalPods(sim.workloadPods, sim.customConfig.TypicalPodsConfig)
	sim.fragMemo = sync.Map{}
}

func (sim *Simulator) NodeGpuFragAmountMap(nodeResourceMap map[string]simontype.NodeResource) map[string]utils.FragAmount {
	nodeFragAmountMap := make(map[string]utils.FragAmount)
	for nodeName, nodeRes := range nodeResourceMap {
		nodeFragAmountMap[nodeName] = sim.NodeGpuFragAmount(nodeRes)
	}
	return nodeFragAmountMap
}
