package simulator

import (
	"fmt"
	"math"

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
			log.Errorf("[Deschedule][FragOnePod] podGpuIdList of podRes(%s) error:%s\n", podRes.Repr(), err.Error())
			continue
		}
		newNodeRes, err := nodeRes.Add(podRes, podGpuIdList) // in contrast to schedule's "nodeRes.Sub()"
		if err != nil {
			log.Errorf("[Deschedule][FragOnePod] findVictimPodOnNodeFragAware: nodeRes(%s).Add(podRes(%s)) error:%s\n", nodeRes.Repr(), podRes.Repr(), err.Error())
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
		log.Debugf("[Deschedule][FragOnePod] pod %s is selected to deschedule from node %s, score %d\n", utils.GeneratePodKey(victimPod), nodeGpuFrag.NodeName, victimScore)
		return victimPod, victimNodeGpuFrag
	}
	log.Debugf("[Deschedule][FragOnePod] no pod is evicted from node %s\n", nodeGpuFrag.NodeName)
	return nil, nil
}
