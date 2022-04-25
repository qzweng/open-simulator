package utils

import (
	"fmt"
	"sort"
	"sync"

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
	Data []float64
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

func (fa FragAmount) AddGamma(faOther FragAmount, gamma float64) error {
	if len(fa.Data) == 0 {
		fa.Data = make([]float64, len(FragRatioDataMap))
		for i := 0; i < len(FragRatioDataMap); i++ {
			fa.Data[i] = 0
		}
	}
	if len(fa.Data) != len(faOther.Data) {
		return fmt.Errorf("this (%d) does not match the other (%d)", len(fa.Data), len(faOther.Data))
	}
	for i := 0; i < len(fa.Data); i++ {
		fa.Data[i] += gamma * faOther.Data[i]
	}
	return nil
}

func (fa FragAmount) Add(faOther FragAmount) error {
	return fa.AddGamma(faOther, 1.0)
}

func (fr FragRatio) Repr() (outStr string) {
	outStr += "["
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
	fragRatio := FragRatio{data}
	for _, pod := range typicalPods {
		freq := pod.Percentage
		if freq < 0 || freq > 1 {
			log.Errorf("pod %v has bad freq: %f\n", pod.TargetPodResource, freq)
			continue
		}
		fragType := GetNodePodFrag(nodeRes, pod.TargetPodResource)
		log.Tracef("nodeRes: %s; pod: %s => fragType: %s (freq: %.2f)\n", nodeRes.Repr(), pod.TargetPodResource.Repr(), fragType, freq)
		if err := fragRatio.AddRatio(fragType, freq); err != nil {
			log.Errorln(err.Error())
		}
	}
	return fragRatio
}

func NodeGpuFragAmount(nodeRes simontype.NodeResource, typicalPods simontype.TargetPodList) FragAmount {
	fragRatio := NodeGpuFragRatio(nodeRes, typicalPods)
	return GetFragAmountByNodeResAndFragRatio(nodeRes, fragRatio)
}

func GetFragAmountByNodeResAndFragRatio(nodeRes simontype.NodeResource, fragRatio FragRatio) FragAmount {
	fragAmount := FragAmount{nodeRes.NodeName, fragRatio.Data}
	gpuMilliLeftTotal := GetGpuMilliLeftTotal(nodeRes)
	for i := 0; i < len(fragAmount.Data); i++ {
		fragAmount.Data[i] *= float64(gpuMilliLeftTotal)
	}
	return fragAmount
}

func GetGpuMilliLeftTotal(nodeRes simontype.NodeResource) (gpuMilliLeftTotal int64) {
	for _, gpuMilliLeft := range nodeRes.MilliGpuLeftList {
		gpuMilliLeftTotal += gpuMilliLeft
	}
	return gpuMilliLeftTotal
}

func NodeGpuFragAmountBellman(nodeRes simontype.NodeResource, typicalPods simontype.TargetPodList, dp *sync.Map) FragAmount {
	log.Traceln()
	log.Debugf("Enter bellman with nodeRes(%s)\n", nodeRes.Repr())
	// Read dp memo
	nodeResKey := nodeRes.Flatten()
	if fa, ok := dp.Load(nodeResKey); ok {
		if fragAmount, ok2 := fa.(FragAmount); ok2 {
			log.Tracef("Hit Cache! %v\n", nodeResKey)
			return FragAmount{nodeRes.NodeName, fragAmount.Data}
		}
	}

	fragAmount := FragAmount{nodeRes.NodeName, make([]float64, len(FragRatioDataMap))} // r(s) init as all zero
	if GetGpuMilliLeftTotal(nodeRes) == 0 {
		return fragAmount // If there are no GPUs left, the frag amount should be all zero
	}

	gamma := 0.9 // gamma in (0, 1]
	delta := 1e-6
	// If missed, calculate and update dp memo
	fragRatio := NodeGpuFragRatio(nodeRes, typicalPods)
	if fragRatio.FragRatioSumExceptQ3() < delta {
		log.Tracef("Zero frag ratio: %.2f\n", fragRatio.FragRatioSumExceptQ3())
		// Bellman equation: V(s) = r(s) + gamma * sum( p(s'|s) * V(s') )
		pvSum := FragAmount{nodeRes.NodeName, make([]float64, len(FragRatioDataMap))} // init of pvSum = 0
		for i, pod := range typicalPods {
			newNodeRes, err := nodeRes.Sub(pod.TargetPodResource)
			if err != nil {
				log.Errorf("nodeRes.Sub(podRes) errs in Bellman: " + err.Error())
				continue
			}
			log.Tracef(" pod[%d](%s) calls Bellman\n", i, pod.TargetPodResource.Repr())
			p := pod.Percentage
			v := NodeGpuFragAmountBellman(newNodeRes, typicalPods, dp)
			log.Tracef("pvSum (before): %s + %s (p=%.2f)\n", pvSum.Repr(), v.Repr(), p)
			if err = pvSum.AddGamma(v, p); err != nil { // sum( p(s'|s) * V(s') )
				log.Errorf("pvSum.AddGamma errs in Bellman: " + err.Error())
			}
			log.Tracef("pvSum (after) : %s\n", pvSum.Repr())
		}
		log.Tracef("gamma (before): %s + %s (gamma=%.2f)\n", fragAmount.Repr(), pvSum.Repr(), gamma)
		if err := fragAmount.AddGamma(pvSum, gamma); err != nil { // V(s) = r(s) + gamma * sum()
			log.Errorf("pvSum.AddGamma errs in Bellman: " + err.Error())
		}
		log.Tracef("gamma (after) : %s\n", fragAmount.Repr())

	} else {
		log.Tracef("Got non-zero frag ratio: %s\n", fragRatio.Repr())
		// else: early cut-off: V(s) = r(s) (if r(s) > 0.001)
		fragAmount = GetFragAmountByNodeResAndFragRatio(nodeRes, fragRatio)
	}
	dp.Store(nodeResKey, FragAmount{"", fragAmount.Data})
	log.Debugf("dp: Update key(%v) as %s\n", nodeResKey, FragAmount{"", fragAmount.Data}.Repr())
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

func (fr FragRatio) FragRatioSumExceptQ3() (out float64) {
	for i := 0; i < len(FragRatioDataMap); i++ {
		if i != FragRatioDataMap[Q3Satisfied] {
			out += fr.Data[i]
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
