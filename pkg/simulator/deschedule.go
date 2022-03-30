package simulator

import (
	log "github.com/sirupsen/logrus"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

const (
	DeschedulePolicyCosSim       = "cosSim"
	DeschedulePolicyFragOnePod   = "fragOnePod"
	DeschedulePolicyFragMultiPod = "fragMultiPod"
)

func (sim *Simulator) DescheduleCluster() ([]simontype.UnscheduledPod, error) {
	podMap := sim.getCurrentPodMap()

	nodeStatus := sim.GetClusterNodeStatus() // Note: the resources in nodeStatus.Node is the capacity instead of requests
	nodeStatusMap := make(map[string]simontype.NodeStatus)
	for _, ns := range nodeStatus {
		nodeStatusMap[ns.Node.Name] = ns
	}

	var failedPods []simontype.UnscheduledPod
	numPodsToDeschedule := sim.customConfig.DeschedulePodsMax
	log.Infof("DeschedulePodsMax: %d, DeschedulePolicy: %s\n", numPodsToDeschedule, sim.customConfig.DeschedulePolicy)

	switch sim.customConfig.DeschedulePolicy {
	case DeschedulePolicyCosSim:
		var descheduledPodKeys []string
		for _, ns := range nodeStatus {
			if numPodsToDeschedule <= 0 {
				break
			}
			victimPod := sim.findVictimPodOnNode(ns.Node, ns.Pods)
			if victimPod != nil {
				if err := sim.deletePod(victimPod); err != nil {
					log.Errorf("[DescheduleCluster] failed to delete pod(%s)\n", utils.GeneratePodKey(victimPod))
				} else {
					descheduledPodKeys = append(descheduledPodKeys, utils.GeneratePodKey(victimPod))
					numPodsToDeschedule -= 1
				}
			}
		}
		sim.ClusterAnalysis()
		descheduledPod := getPodfromPodMap(descheduledPodKeys, podMap)
		failedPods = sim.SchedulePods(descheduledPod)

	case DeschedulePolicyFragOnePod:
		var descheduledPodKeys []string
		nodeResourceMap := utils.GetNodeResourceMap(nodeStatus)
		nodeFragAmountList := sim.getNodeFragAmountList(nodeStatus)
		for _, nfa := range nodeFragAmountList { // from nodes with the largest amount of fragment
			if numPodsToDeschedule <= 0 {
				break
			}
			nsPods := nodeStatusMap[nfa.NodeName].Pods
			victimPod, _ := sim.findVictimPodOnNodeFragAware(nfa, nodeResourceMap[nfa.NodeName], nsPods) // evict one pod per node
			if victimPod != nil {
				descheduledPodKeys = append(descheduledPodKeys, utils.GeneratePodKey(victimPod))
				sim.deletePod(victimPod)
				numPodsToDeschedule -= 1
			}
		}
		sim.ClusterAnalysis()
		descheduledPod := getPodfromPodMap(descheduledPodKeys, podMap)
		failedPods = sim.SchedulePods(descheduledPod)

	case DeschedulePolicyFragMultiPod:
		var descheduledPodKeys []string
		nodeResourceMap := utils.GetNodeResourceMap(nodeStatus)
		nodeFragAmountMap := sim.NodeGpuFragAmountMap(nodeResourceMap)
		nodeFragAmountList := sim.getNodeFragAmountList(nodeStatus)
		var fakeNodeFragAmountList []utils.FragAmount
		for _, v := range nodeFragAmountMap {
			fakeNodeFragAmountList = append(nodeFragAmountList, v)
		}
		fakeNodeStatusMap := make(map[string]simontype.NodeStatus)
		for _, ns := range nodeStatus {
			fakeNodeStatusMap[ns.Node.Name] = ns // should not touch ns.Node since it is a pointer
		}
		numPodsToDescheduleLast := numPodsToDeschedule
		for numPodsToDeschedule > 0 {
			for i := 0; i < len(fakeNodeFragAmountList); i++ {
				log.Debugf("  i=%d, numPodsToDeschedule=%d\n", i, numPodsToDeschedule)
				if numPodsToDeschedule <= 0 {
					break
				}
				nfa := fakeNodeFragAmountList[i]
				nsPods := fakeNodeStatusMap[nfa.NodeName].Pods
				victimPod, victimNodeGpuFrag := sim.findVictimPodOnNodeFragAware(nfa, nodeResourceMap[nfa.NodeName], nsPods) // evict one pod per node
				if victimPod != nil {
					descheduledPodKeys = append(descheduledPodKeys, utils.GeneratePodKey(victimPod))
					sim.deletePod(victimPod)
					fakeNodeFragAmountList[i] = *victimNodeGpuFrag                                       // update the nodeFragAmount
					oldNode := fakeNodeStatusMap[nfa.NodeName].Node                                      // not changed
					newPods := utils.RemovePodFromPodSliceByPod(nsPods, victimPod)                       // remove one pod
					fakeNodeStatusMap[nfa.NodeName] = simontype.NodeStatus{Node: oldNode, Pods: newPods} // update the nodeStatus
					numPodsToDeschedule -= 1
				}
			}
			log.Debugf(" numPodsToDeschedule=%d, numPodsToDescheduleLast=%d\n", numPodsToDeschedule, numPodsToDescheduleLast)
			if numPodsToDescheduleLast == numPodsToDeschedule {
				break
			}
			numPodsToDescheduleLast = numPodsToDeschedule
		}
		sim.ClusterAnalysis()
		descheduledPod := getPodfromPodMap(descheduledPodKeys, podMap)
		failedPods = sim.SchedulePods(descheduledPod)

	default:
		log.Errorf("DeschedulePolicy not found\n")
	}

	return failedPods, nil
}
