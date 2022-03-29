package simulator

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

const (
	DeschedulePolicyCosSim       = "cosSim"
	DeschedulePolicyFragOnePod   = "fragOnePod"
	DeschedulePolicyFragMultiPod = "fragMultiPod"
)

func (sim *Simulator) Deschedule() (*simontype.SimulateResult, error) {
	podMap := make(map[string]*corev1.Pod)
	podList, _ := sim.client.CoreV1().Pods(metav1.NamespaceAll).List(sim.ctx, metav1.ListOptions{})
	pods := utils.GetPodsPtrFromPods(podList.Items)
	for _, pod := range pods {
		podMap[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)] = pod
		//fmt.Printf("[DEBUG][Pod] pod %s -> Spec.NodeName %s\n", pod.Name, pod.Spec.NodeName)
	}

	nodeStatus := sim.getClusterNodeStatus() // Note: the resources in nodeStatus.Node is the capacity instead of requests
	nodeStatusMap := make(map[string]simontype.NodeStatus)
	for _, ns := range nodeStatus {
		nodeStatusMap[ns.Node.Name] = ns
	}

	nodeResourceMap := utils.GetNodeResourceMap(nodeStatus)
	nodeFragAmountMap := sim.NodeGpuFragAmountMap(nodeResourceMap)

	var nodeFragAmountList []utils.FragAmount
	for _, v := range nodeFragAmountMap {
		nodeFragAmountList = append(nodeFragAmountList, v)
	}
	sort.Slice(nodeFragAmountList, func(i int, j int) bool {
		return nodeFragAmountList[i].FragAmountSumExceptQ3() > nodeFragAmountList[j].FragAmountSumExceptQ3()
	})

	var descheduledPod []string
	numPodsToDeschedule := sim.customConfig.DeschedulePodsMax
	fmt.Printf("[INFO] DeschedulePodsMax: %d, DeschedulePolicy: %s\n", numPodsToDeschedule, sim.customConfig.DeschedulePolicy)
	switch sim.customConfig.DeschedulePolicy {
	case DeschedulePolicyCosSim:
		for _, ns := range nodeStatus {
			if numPodsToDeschedule <= 0 {
				break
			}
			victimPod := sim.findVictimPodOnNode(ns.Node, ns.Pods)
			if victimPod != nil {
				descheduledPod = append(descheduledPod, utils.GeneratePodKey(victimPod))
				sim.deletePod(victimPod)
				numPodsToDeschedule -= 1
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

	case DeschedulePolicyFragOnePod:
		for _, nfa := range nodeFragAmountList { // from nodes with the largest amount of fragment
			if numPodsToDeschedule <= 0 {
				break
			}
			nsPods := nodeStatusMap[nfa.NodeName].Pods
			victimPod, _ := sim.findVictimPodOnNodeFragAware(nfa, nodeResourceMap[nfa.NodeName], nsPods) // evict one pod per node
			if victimPod != nil {
				descheduledPod = append(descheduledPod, utils.GeneratePodKey(victimPod))
				sim.deletePod(victimPod)
				numPodsToDeschedule -= 1
			}
		}

	case DeschedulePolicyFragMultiPod:
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
					descheduledPod = append(descheduledPod, utils.GeneratePodKey(victimPod))
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
	default:
		fmt.Printf("[ERROR] DeschedulePolicy not found\n")
	}

	fmt.Printf("[INFO] Deschedule pod list (%d pods): %v\n", len(descheduledPod), descheduledPod)
	var failedPods []simontype.UnscheduledPod
	for _, podKey := range descheduledPod {
		podCopy := podMap[podKey].DeepCopy()
		MakePodUnassigned(podCopy)
		sim.createPod(podCopy)

		if strings.Contains(sim.status.stopReason, "failed") {
			sim.deletePod(podCopy)
			failedPods = append(failedPods, simontype.UnscheduledPod{
				Pod:    podCopy,
				Reason: sim.status.stopReason,
			})
		}
	}

	return &simontype.SimulateResult{
		UnscheduledPods: failedPods,
		NodeStatus:      sim.getClusterNodeStatus(),
	}, nil
}
