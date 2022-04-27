package simontype

import (
	"fmt"
	"sort"

	log "github.com/sirupsen/logrus"

	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
)

type TargetPod struct {
	TargetPodResource PodResource
	Percentage        float64 // range: 0.0 - 1.0 (100%)
}

type TargetPodList []TargetPod

func (p TargetPodList) Len() int           { return len(p) }
func (p TargetPodList) Less(i, j int) bool { return p[i].Percentage < p[j].Percentage }
func (p TargetPodList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

const (
	DefaultTypicalPodPopularityThreshold = 60 // 60%
	DefaultTypicalPodIncreaseStep        = 10
)

type PodResource struct { // typical pod, without name and namespace.
	MilliCpu  int64
	MilliGpu  int64 // Milli GPU request per GPU, 0-1000
	GpuNumber int
	GpuType   string //
	//Memory	  int64
}

type NodeResource struct {
	NodeName         string
	MilliCpu         int64
	MilliGpuLeftList []int64
	GpuNumber        int
	GpuType          string
	//Memory           int64 // TODO
}

type NodeResourceFlat struct {
	MilliCpu int64
	MilliGpu string
	GpuType  string
	Remark   string
	//Memory   int64
}

func (tpr PodResource) Repr() string {
	outStr := "<"
	outStr += fmt.Sprintf("CPU: %6.2f", float64(tpr.MilliCpu)/1000)
	outStr += fmt.Sprintf(", GPU: %d", tpr.GpuNumber)
	outStr += fmt.Sprintf(" x {%-4d}m", tpr.MilliGpu)
	outStr += fmt.Sprintf(" (%s)", tpr.GpuType)
	outStr += ">"
	return outStr
}

func (tnr NodeResource) Repr() string {
	outStr := "<"
	outStr += fmt.Sprintf("CPU: %6.2f", float64(tnr.MilliCpu)/1000)
	outStr += fmt.Sprintf(", GPU (%s): %d", tnr.GpuType, tnr.GpuNumber)
	if tnr.GpuNumber > 0 {
		outStr += fmt.Sprintf(" x %dm, Left:", gpushareutils.MILLI)
		for _, gML := range tnr.MilliGpuLeftList {
			outStr += fmt.Sprintf(" %dm", gML)
		}
	}
	outStr += ">"
	return outStr
}

func (tnr NodeResource) Flatten(remark string) NodeResourceFlat {
	nrf := NodeResourceFlat{tnr.MilliCpu, "", tnr.GpuType, remark}

	// Sort NodeRes's GpuLeft in descending
	sort.Slice(tnr.MilliGpuLeftList, func(i, j int) bool { // largest one first
		return tnr.MilliGpuLeftList[i] > tnr.MilliGpuLeftList[j]
	})

	// Append 0 to MilliGpu if number of GPUs is fewer than MaxNumGpuPerNode
	for i := 0; i < MaxNumGpuPerNode; i++ {
		if i < len(tnr.MilliGpuLeftList) {
			nrf.MilliGpu += fmt.Sprintf("%d,", tnr.MilliGpuLeftList[i])
		} else {
			nrf.MilliGpu += "0,"
		}
	}

	return nrf
}

// ToResourceVec returns a resource vector: [milli cpu request, milli gpu request].
func (tpr PodResource) ToResourceVec() []float64 {
	var vec []float64
	// milli cpu left
	vec = append(vec, float64(tpr.MilliCpu))

	// milli gpu request, e.g., 500, 2000
	vec = append(vec, float64(tpr.MilliGpu*int64(tpr.GpuNumber)))
	return vec
}

// ToResourceVec returns a resource vector: [milli cpu left, total milli gpu left].
func (tnr NodeResource) ToResourceVec() []float64 {
	var vec []float64
	// milli cpu left
	vec = append(vec, float64(tnr.MilliCpu))

	var totalMilliGpuLeft int64 = 0
	for _, milliGpuLeft := range tnr.MilliGpuLeftList {
		totalMilliGpuLeft += milliGpuLeft
	}
	// total milli gpu left
	vec = append(vec, float64(totalMilliGpuLeft))
	return vec
}

func (tnr NodeResource) Copy() NodeResource {
	milliGpuLeftList := make([]int64, len(tnr.MilliGpuLeftList))
	for i := 0; i < len(tnr.MilliGpuLeftList); i++ {
		milliGpuLeftList[i] = tnr.MilliGpuLeftList[i]
	}

	return NodeResource{
		NodeName:         tnr.NodeName,
		MilliCpu:         tnr.MilliCpu,
		MilliGpuLeftList: milliGpuLeftList,
		GpuNumber:        tnr.GpuNumber,
		GpuType:          tnr.GpuType,
	}
}

func (tnr NodeResource) Sub(tpr PodResource) (NodeResource, error) {
	out := tnr.Copy()
	if out.MilliCpu < tpr.MilliCpu || out.GpuNumber < tpr.GpuNumber {
		return out, fmt.Errorf("node: %s failed to accommodate pod: %s", tnr.Repr(), tpr.Repr())
	}
	out.MilliCpu -= tpr.MilliCpu

	gpuRequest := tpr.GpuNumber
	if gpuRequest == 0 {
		return out, nil
	}

	// Sort NodeRes's GpuLeft in ascending, then Pack it (Subtract from the least sufficient one).
	sort.Slice(out.MilliGpuLeftList, func(i, j int) bool { // smallest one first
		return out.MilliGpuLeftList[i] < out.MilliGpuLeftList[j]
	})
	for i := 0; i < len(out.MilliGpuLeftList); i++ {
		if tpr.MilliGpu <= out.MilliGpuLeftList[i] {
			gpuRequest -= 1
			out.MilliGpuLeftList[i] -= tpr.MilliGpu
			if gpuRequest <= 0 {
				//fmt.Printf("[DEBUG] [DONE] out.MilliGpuLeftList: %v\n", out.MilliGpuLeftList)
				return out, nil
			}
		}
	}
	return out, fmt.Errorf("node: %s failed to accommodate pod: %s (%d GPU requests left)", tnr.Repr(), tpr.Repr(), gpuRequest)
}

func (tnr NodeResource) Add(tpr PodResource, idl []int) (NodeResource, error) {
	out := tnr.Copy()
	out.MilliCpu += tpr.MilliCpu

	gpuRequest := tpr.GpuNumber
	if tpr.GpuNumber == 0 {
		return out, nil
	}

	if len(idl) > 0 || len(idl) != gpuRequest { // input is valid
		for i := 0; i < len(idl); i++ {
			if idl[i] > len(out.MilliGpuLeftList)-1 || idl[i] < 0 {
				err := fmt.Errorf("[ERROR] idl: %v of pod %s", idl, tpr.Repr())
				log.Errorln(err.Error())
				return out, err
			}
			if out.MilliGpuLeftList[idl[i]]+tpr.MilliGpu > gpushareutils.MILLI {
				err := fmt.Errorf("[ERROR] idl[%d]=%d of pod %s exceeds %d", i, idl[i], tpr.Repr(), gpushareutils.MILLI)
				log.Errorln(err.Error())
				return out, err
			}
			out.MilliGpuLeftList[idl[i]] += tpr.MilliGpu
			gpuRequest -= 1
		}
		return out, nil

	} else { // evict pod from the smallest remaining resource GPU, may not reflect the real cases
		log.Infof("Pod (%s) has no valid GPU index list: %v\n", tpr.Repr(), idl)
		sort.Slice(out.MilliGpuLeftList, func(i, j int) bool { // smallest one first
			return out.MilliGpuLeftList[i] < out.MilliGpuLeftList[j]
		})
		for i := 0; i < len(out.MilliGpuLeftList); i++ {
			if tpr.MilliGpu+out.MilliGpuLeftList[i] <= gpushareutils.MILLI {
				gpuRequest -= 1
				out.MilliGpuLeftList[i] += tpr.MilliGpu
				if gpuRequest <= 0 {
					//fmt.Printf("[DEBUG] [DONE] out.MilliGpuLeftList: %v\n", out.MilliGpuLeftList)
					return out, nil
				}
			}
		}
	}

	return out, fmt.Errorf("node: %s failed to evict pod: %s (%d GPU requests left)", tnr.Repr(), tpr.Repr(), gpuRequest)
}
