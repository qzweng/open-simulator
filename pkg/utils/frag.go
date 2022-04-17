package utils

import (
	"fmt"
	"sort"

	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"

	"github.com/alibaba/open-simulator/pkg/api/v1alpha1"
	"github.com/alibaba/open-simulator/pkg/type"
)

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
	NodeName string
	Data     []float64
}

type FragAmount struct {
	NodeName string
	Data     []float64
}

func (fr FragRatio) AddRatio(fragType string, freq float64) error {
	if freq < 0 || freq > 1 {
		return fmt.Errorf("bad freq")
	}
	if index, ok := FragRatioDataMap[fragType]; !ok {
		return fmt.Errorf("bad fragType")
	} else {
		fr.Data[index] += freq
		return nil
	}
}

func (fa FragAmount) Add(faOther FragAmount) error {
	if len(fa.Data) == 0 {
		fa.Data = faOther.Data
		return nil
	}
	if len(fa.Data) != len(faOther.Data) {
		return fmt.Errorf("this (%d) does not match the other (%d)", len(fa.Data), len(faOther.Data))
	}
	for i := 0; i < len(fa.Data); i++ {
		fa.Data[i] += faOther.Data[i]
	}
	return nil
}

func (fr FragRatio) Repr() (outStr string) {
	outStr += fr.NodeName
	outStr += ": ["
	for i, v := range fr.Data {
		if i > 0 {
			outStr += ", "
		}
		outStr += fmt.Sprintf("%4.1f%%", 100*v)
	}
	outStr += "]"
	return outStr
}

func (fa FragAmount) Repr() (outStr string) {
	outStr += fa.NodeName
	outStr += ": ["
	for i, v := range fa.Data {
		if i > 0 {
			outStr += ", "
		}
		outStr += fmt.Sprintf("%6.1f", v)
	}
	outStr += "]"
	return outStr
}

func NodeGpuFragRatio(nodeRes simontype.NodeResource, typicalPods simontype.TargetPodList) FragRatio {
	data := make([]float64, len(FragRatioDataMap))
	fragRatio := FragRatio{nodeRes.NodeName, data}
	for _, pod := range typicalPods {
		freq := pod.Percentage
		if freq < 0 || freq > 1 {
			log.Errorf("pod %v has bad freq: %f\n", pod.TargetPodResource, freq)
			continue
		}
		fragType := GetNodePodFrag(nodeRes, pod.TargetPodResource)
		log.Tracef("nodeRes: %s; pod: %s => fragType: %s\n", nodeRes.Repr(), pod.TargetPodResource.Repr(), fragType)
		if err := fragRatio.AddRatio(fragType, freq); err != nil {
			log.Errorln(err.Error())
		}
	}
	return fragRatio
}

func NodeGpuFragAmount(nodeRes simontype.NodeResource, typicalPods simontype.TargetPodList) FragAmount {
	if len(typicalPods) <= 0 {
		log.Errorf("Typical Pods list is empty\n")
		return FragAmount{}
	}
	fragRatio := NodeGpuFragRatio(nodeRes, typicalPods)
	fragAmount := FragAmount{nodeRes.NodeName, fragRatio.Data}

	var gpuMilliLeftTotal int64
	for _, gpuMilliLeft := range nodeRes.MilliGpuLeftList {
		gpuMilliLeftTotal += gpuMilliLeft
	}

	for i := 0; i < len(fragAmount.Data); i++ {
		fragAmount.Data[i] *= float64(gpuMilliLeftTotal)
	}
	//fmt.Printf("IN NodeGpuFragAmount: %s\n", fragAmount.Repr())
	return fragAmount
}

func GetTypicalPods(allPods []*v1.Pod, config v1alpha1.TypicalPodsConfig) simontype.TargetPodList {
	tgtPodResCntMap := map[simontype.PodResource]float64{}
	var total float64 = 0
	for _, pod := range allPods {
		tgtPodRes := GetPodResource(pod)
		if !config.IsInvolvedCpuPods && tgtPodRes.GpuNumber == 0 {
			continue
		}

		var weightedCnt float64 = 1
		if config.IsConsideredGpuResWeight {
			weightedCnt = float64(int64(tgtPodRes.GpuNumber) * tgtPodRes.MilliGpu)
		}
		if cnt, ok := tgtPodResCntMap[tgtPodRes]; ok {
			tgtPodResCntMap[tgtPodRes] = cnt + weightedCnt
		} else {
			tgtPodResCntMap[tgtPodRes] = weightedCnt
		}
		total += weightedCnt
	}

	tgtPodList := SortTargetPodInDecreasingCount(tgtPodResCntMap)
	log.Debugf("Num of Total Pods: %d\n", len(allPods))
	log.Debugf("Num of Total Pod Sepc: %d\n", len(tgtPodList))
	var expectedNumPods float64 = 0
	if config.PodPopularityThreshold > 0 {
		expectedNumPods = float64(config.PodPopularityThreshold) * total / 100.0
	} else {
		expectedNumPods = float64(simontype.DefaultTypicalPodPopularityThreshold) * total / 100.0
	}
	var i, podResNum int
	var numPods float64 = 0
	for numPods < expectedNumPods {
		if config.PodIncreaseStep > 0 {
			podResNum += config.PodIncreaseStep
		} else {
			podResNum += simontype.DefaultTypicalPodIncreaseStep
		}
		for i < podResNum && i < len(tgtPodList) {
			numPods += tgtPodList[i].Percentage
			log.Debugf("[%d] %s: %.0f\n", i, tgtPodList[i].TargetPodResource.Repr(), tgtPodList[i].Percentage)
			i += 1
		}
	}

	log.Debugf("Count top %d pod resource spec as typical ones, accounting for %.2f%% of all pods\n", i, 100.0*numPods/total)

	for j, tp := range tgtPodList[:i] {
		tgtPodList[j].Percentage = tp.Percentage / total
		log.Debugf("[%d] %s: %.1f%%\n", j, tp.TargetPodResource.Repr(), tgtPodList[j].Percentage*100)
	}
	log.Debugln()

	return tgtPodList[:i]
	//sim.typicalPods = tgtPodList[:i]
}

func (fa FragAmount) FragAmountSumExceptQ3() (out float64) {
	for i := 0; i < len(FragRatioDataMap); i++ {
		if i != FragRatioDataMap[Q3Satisfied] {
			out += fa.Data[i]
		}
	}
	return out
}

func SortTargetPodInDecreasingCount(tgtPodResMap map[simontype.PodResource]float64) simontype.TargetPodList {
	pl := make(simontype.TargetPodList, len(tgtPodResMap))
	i := 0
	for k, v := range tgtPodResMap {
		pl[i] = simontype.TargetPod{TargetPodResource: k, Percentage: v}
		i++
	}
	sort.Sort(sort.Reverse(pl))
	return pl
}

func CanNodeHostPodOnGpuMemory(nodeRes simontype.NodeResource, podRes simontype.PodResource) bool {
	gpuRequest := podRes.GpuNumber
	for _, gpuHostMem := range nodeRes.MilliGpuLeftList {
		if gpuHostMem >= podRes.MilliGpu {
			gpuRequest -= 1
			if gpuRequest <= 0 {
				return true
			}
		}
	}
	return false
}

func GetNodePodFrag(nodeRes simontype.NodeResource, podRes simontype.PodResource) string {
	if podRes.MilliGpu == 0 {
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
