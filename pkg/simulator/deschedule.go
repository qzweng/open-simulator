package simulator

import (
	"fmt"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

const (
	DeschedulePolicyCosSim       = "cosSim"
	DeschedulePolicyFragOnePod   = "fragOnePod"
	DeschedulePolicyFragMultiPod = "fragMultiPod"

	VERBOSE = 1
)

func (sim *Simulator) Deschedule() (*simontype.SimulateResult, error) {
	podMap := sim.getCurrentPodMap()

	nodeStatus := sim.GetClusterNodeStatus() // Note: the resources in nodeStatus.Node is the capacity instead of requests
	nodeStatusMap := make(map[string]simontype.NodeStatus)
	for _, ns := range nodeStatus {
		nodeStatusMap[ns.Node.Name] = ns
	}

	var failedPods []simontype.UnscheduledPod
	numPodsToDeschedule := sim.customConfig.DeschedulePodsMax
	fmt.Printf("[INFO] DeschedulePodsMax: %d, DeschedulePolicy: %s\n", numPodsToDeschedule, sim.customConfig.DeschedulePolicy)

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
					fmt.Printf("[Error] [Deschedule] failed to delete pod(%s)\n", utils.GeneratePodKey(victimPod))
				} else {
					descheduledPodKeys = append(descheduledPodKeys, utils.GeneratePodKey(victimPod))
					numPodsToDeschedule -= 1
				}
			}

			//for _, pod := range ns.Pods {
			//	sim.deletePod(pod) // delete all pods
			//	podCopy := podMap[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)].DeepCopy()
			//	sim.createPod(podCopy)
			//	sim.deletePod(podCopy)
			//	podCopy = podMap[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)].DeepCopy()
			//	sim.createPod(podCopy)
			//	sim.deletePod(podCopy)
			//}
		}
		sim.ClusterAnalysis(sim.GetClusterNodeStatus(), VERBOSE)
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
		sim.ClusterAnalysis(sim.GetClusterNodeStatus(), VERBOSE)
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
				//fmt.Printf("  [DEBUG] i=%d, numPodsToDeschedule=%d\n", i, numPodsToDeschedule)
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
			//fmt.Printf(" [DEBUG] numPodsToDeschedule=%d, numPodsToDescheduleLast=%d\n", numPodsToDeschedule, numPodsToDescheduleLast)
			if numPodsToDescheduleLast == numPodsToDeschedule {
				break
			}
			numPodsToDescheduleLast = numPodsToDeschedule
		}
		sim.ClusterAnalysis(sim.GetClusterNodeStatus(), VERBOSE)
		descheduledPod := getPodfromPodMap(descheduledPodKeys, podMap)
		failedPods = sim.SchedulePods(descheduledPod)

	default:
		fmt.Printf("[ERROR] DeschedulePolicy not found\n")
	}

	return &simontype.SimulateResult{
		UnscheduledPods: failedPods,
		NodeStatus:      sim.GetClusterNodeStatus(),
	}, nil
}
