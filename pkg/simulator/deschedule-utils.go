package simulator

import (
	"container/heap"
	"fmt"
	"math"
	"sort"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
	"github.com/alibaba/open-simulator/pkg/utils"
)

func (sim *Simulator) findVictimPodOnNode(node *corev1.Node, pods []*corev1.Pod) *corev1.Pod {
	var victimPod *corev1.Pod
	var victimPodSimilarity float64 = 1
	for _, pod := range pods {
		similarity, ok := sim.resourceSimilarity(pod, node)
		if !ok {
			log.Errorf("[findVictimPodOnNode] failed to get resource similarity of pod(%s) to node(%s)\n",
				utils.GeneratePodKey(pod), node.Name)
			continue
		}
		if similarity >= 0 && similarity < victimPodSimilarity {
			victimPod = pod
			victimPodSimilarity = similarity
		}
	}
	if victimPod != nil {
		log.Debugf("[findVictimPodOnNode] pod(%s) is selected to deschedule from node(%s), resource similarity is %.2f\n",
			utils.GeneratePodKey(victimPod), node.Name, victimPodSimilarity)
		return victimPod
	}
	return nil
}

func (sim *Simulator) resourceSimilarity(pod *corev1.Pod, node *corev1.Node) (float64, bool) {
	var podVec, nodeVec []float64

	// cpu
	var scaleFactor float64 = 1000
	podVec = append(podVec, float64(pod.Spec.Containers[0].Resources.Requests.Cpu().MilliValue())/scaleFactor)
	nodeVec = append(nodeVec, float64(node.Status.Allocatable.Cpu().MilliValue())/scaleFactor)

	// mem
	scaleFactor = 1024 * 1024 * 1024
	podVec = append(podVec, float64(pod.Spec.Containers[0].Resources.Requests.Memory().Value())/scaleFactor)
	nodeVec = append(nodeVec, float64(node.Status.Allocatable.Memory().Value())/scaleFactor)

	// gpu milli
	scaleFactor = 1000
	if nodeGpuMilli := gpushareutils.GetGpuMilliOfNode(node); nodeGpuMilli > 0 {
		//nodeVec = append(nodeVec, float64())
		podGpuMilli := gpushareutils.GetGpuMilliFromPodAnnotation(pod)
		podGpuCount := gpushareutils.GetGpuCountFromPodAnnotation(pod)
		podVec = append(podVec, float64(podGpuMilli*int64(podGpuCount))/scaleFactor)
		//fmt.Printf("debug: pod %s, podGpuMilli %d, podGpuCount %d\n", utils.GeneratePodKey(pod), podGpuMilli, podGpuCount)

		nodeGpuCount := gpushareutils.GetGpuCountOfNode(node)
		//fmt.Printf("debug: node %s, nodeGpuMilli %d, nodeGpuCount %d\n", node.Name, nodeGpuMilli, nodeGpuCount)
		nodeVec = append(nodeVec, float64(nodeGpuMilli*nodeGpuCount)/scaleFactor)
	}

	similarity, err := calculateVectorSimilarity(podVec, nodeVec)
	if err != nil {
		return -1, false
	}
	log.Debugf("similarity of pod %s with node %s is %.2f, pod vec %v, node vec %v\n", utils.GeneratePodKey(pod), node.Name, similarity, podVec, nodeVec)
	return similarity, true
}

func calculateVectorSimilarity(vec1, vec2 []float64) (float64, error) {
	if len(vec1) == 0 || len(vec2) == 0 || len(vec1) != len(vec2) {
		err := fmt.Errorf("empty vector(s) or vectors of unequal size, vec1 %v, vec2 %v\n", vec1, vec2)
		return -1, err
	}
	var magnitude1, magnitude2, innerProduct float64
	for index, num1 := range vec1 {
		num2 := vec2[index]
		magnitude1 += num1 * num1
		magnitude2 += num2 * num2
		innerProduct += num1 * num2
	}
	magnitude1 = math.Sqrt(magnitude1)
	magnitude2 = math.Sqrt(magnitude2)

	if magnitude1 == 0 || magnitude2 == 0 {
		err := fmt.Errorf("vector(s) of zero magnitude. vec1: %v, vec2: %v", vec1, vec2)
		return -1, err
	}

	similarity := innerProduct / (magnitude1 * magnitude2)
	return similarity, nil
}

func sortNodeStatusByResource(milliCpuBar int64, nodeStatus []simontype.NodeStatus, nodeResMap map[string]simontype.NodeResource) {
	sort.SliceStable(nodeStatus, func(i, j int) bool {
		nodeI := nodeStatus[i].Node.Name
		nodeResI := nodeResMap[nodeI]
		milliGpuLeftI := int64(0)
		for _, milliGpuLeft := range nodeResI.MilliGpuLeftList {
			milliGpuLeftI += milliGpuLeft
		}

		nodeJ := nodeStatus[j].Node.Name
		nodeResJ := nodeResMap[nodeJ]
		milliGpuLeftJ := int64(0)
		for _, milliGpuLeft := range nodeResJ.MilliGpuLeftList {
			milliGpuLeftJ += milliGpuLeft
		}

		if nodeResI.MilliCpu < milliCpuBar {
			if nodeResJ.MilliCpu < milliCpuBar {
				return milliGpuLeftI > milliGpuLeftJ || (milliGpuLeftI == milliGpuLeftJ && nodeI < nodeJ)
			} else {
				return true
			}
		} else {
			if nodeResJ.MilliCpu < milliCpuBar {
				return false
			} else {
				return milliGpuLeftI > milliGpuLeftJ || (milliGpuLeftI == milliGpuLeftJ && nodeI < nodeJ)
			}
		}
	})
}

func (sim *Simulator) findVictimPodOnNodeFragAware(nodeGpuFrag utils.FragAmount, nodeRes simontype.NodeResource, pods []*corev1.Pod) (*corev1.Pod, *utils.FragAmount) {
	//victimScore := int64(math.MinInt64)
	victimScore := int64(0) // if score < victimScore, i.e., negative, it means descheduling brings more fragment.
	var victimPod *corev1.Pod
	var victimNodeGpuFrag *utils.FragAmount
	log.Debugf(" node:%s (%s)\n", nodeRes.Repr(), nodeRes.NodeName)
	for _, pod := range pods {
		podRes := utils.GetPodResource(pod)
		podGpuIdList, err := gpushareutils.GetGpuIdListFromAnnotation(pod)
		if err != nil {
			log.Errorf("[DescheduleCluster][FragOnePod] podGpuIdList of podRes(%s) error:%s\n", podRes.Repr(), err.Error())
			continue
		}
		newNodeRes, err := nodeRes.Add(podRes, podGpuIdList) // in contrast to schedule's "nodeRes.Sub()"
		if err != nil {
			log.Errorf("[DescheduleCluster][FragOnePod] findVictimPodOnNodeFragAware: nodeRes(%s).Add(podRes(%s)) error:%s\n", nodeRes.Repr(), podRes.Repr(), err.Error())
			continue
		}
		newNodeGpuFrag := utils.NodeGpuFragAmount(newNodeRes, sim.typicalPods)
		score := int64(nodeGpuFrag.FragAmountSumExceptQ3() - newNodeGpuFrag.FragAmountSumExceptQ3()) // same as gpu-frag-score, the higher, the better
		if score > victimScore {
			victimScore = score
			victimPod = pod
			victimNodeGpuFrag = &newNodeGpuFrag
		}
		log.Debugf("  pod:%s, newNodeRes:%s, score:%.2f-%.2f=%d (%s)\n", podRes.Repr(), newNodeRes.Repr(), nodeGpuFrag.FragAmountSumExceptQ3(), newNodeGpuFrag.FragAmountSumExceptQ3(), score, pod.Name)
	}
	if victimPod != nil {
		log.Debugf("[DescheduleCluster][FragOnePod] pod %s is selected to deschedule from node %s, score %d\n", utils.GeneratePodKey(victimPod), nodeGpuFrag.NodeName, victimScore)
		return victimPod, victimNodeGpuFrag
	}
	log.Debugf("[DescheduleCluster][FragOnePod] no pod is evicted from node %s\n", nodeGpuFrag.NodeName)
	return nil, nil
}

// An Item is something we manage in a priority queue.
type Item struct {
	value    utils.FragAmount // The value of the item; arbitrary.
	priority float64          // The priority of the item in the queue.
	// The index is needed by update and is maintained by the heap.Interface methods.
	index int // The index of the item in the heap.
}

// A PriorityQueue implements heap.Interface and holds Items.
type PriorityQueue []*Item

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	// We want Pop to give us the highest, not lowest, priority so we use greater than here.
	return pq[i].priority > pq[j].priority
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*Item)
	item.index = n
	*pq = append(*pq, item)
	heap.Fix(pq, item.index)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.index = -1 // for safety
	*pq = old[0 : n-1]
	return item
}

// update modifies the priority and value of an Item in the queue.
func (pq *PriorityQueue) update(item *Item, value utils.FragAmount, priority float64) {
	item.value = value
	item.priority = priority
	heap.Fix(pq, item.index)
}

const pqShowLimit = 3

func (pq PriorityQueue) show() {
	for i := 0; i < len(pq) && i < pqShowLimit; i++ {
		log.Debugf("PRIQ: [%d][pri:%.2f] %s\n", pq[i].index, pq[i].priority, pq[i].value.Repr())
	}
	log.Debugln()
}
