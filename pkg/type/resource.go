package simontype

import (
	"fmt"
	"sort"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"

	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
)

type TargetPod struct {
	TargetPodResource PodResource
	Percentage        float64 // range: 0.0 - 1.0 (100%)
}

type TargetPodList []TargetPod

func (p TargetPodList) Len() int { return len(p) }
func (p TargetPodList) Less(i, j int) bool {
	if p[i].Percentage != p[j].Percentage {
		return p[i].Percentage < p[j].Percentage
	} else { // to stabilize the order if two TargetPod has the same frequency
		return p[i].TargetPodResource.Less(p[j].TargetPodResource)
	}
}

func (tpr PodResource) Less(other PodResource) bool {
	if tpr.MilliCpu != other.MilliCpu {
		return tpr.MilliCpu < other.MilliCpu
	} else if tpr.MilliGpu != other.MilliGpu {
		return tpr.MilliGpu < other.MilliGpu
	} else if tpr.GpuNumber != other.GpuNumber {
		return tpr.GpuNumber < other.GpuNumber
	} else {
		return tpr.GpuType < other.GpuType
	}
}

func (p TargetPodList) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

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
	NodeName string
	MilliCpu int64
	// MilliCpuCapacity int64
	MilliGpuLeftList []int64 // Do NOT sort it directly, using SortedMilliGpuLeftIndexList instead. Its order matters; the index is the GPU device index.
	GpuNumber        int
	GpuType          string
	// MemoryLeft       int64
	// MemoryCapacity   int64
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

func (tnr NodeResource) GetTotalMilliGpuLeft() (total int64) {
	for _, v := range tnr.MilliGpuLeftList {
		total += v
	}
	return total
}

func (tnr NodeResource) GetFullyFreeGpuNum() (free int) {
	for _, gpuMilliLeft := range tnr.MilliGpuLeftList {
		if gpuMilliLeft == gpushareutils.MILLI {
			free++
		}
	}
	return free
}

func (tnr NodeResource) SortedMilliGpuLeftIndexList(ascending bool) []int {
	indexList := make([]int, len(tnr.MilliGpuLeftList)) // len == cap == len(tnr.MilliGpuLeftList)
	for i, _ := range tnr.MilliGpuLeftList {
		indexList[i] = i
	}

	if ascending {
		// smallest one (minimum GPU Milli left) first
		sort.SliceStable(indexList, func(i, j int) bool {
			return tnr.MilliGpuLeftList[indexList[i]] < tnr.MilliGpuLeftList[indexList[j]]
		})
	} else {
		// largest one (maximum GPU Milli left) first
		sort.SliceStable(indexList, func(i, j int) bool {
			return tnr.MilliGpuLeftList[indexList[i]] > tnr.MilliGpuLeftList[indexList[j]]
		})
	}
	return indexList
}

func (tnr NodeResource) Flatten(remark string) NodeResourceFlat {
	nrf := NodeResourceFlat{tnr.MilliCpu, "", tnr.GpuType, remark}

	// Sort NodeRes's GpuLeft in descending
	sortedIndex := tnr.SortedMilliGpuLeftIndexList(false)

	// Append 0 to MilliGpu if number of GPUs is fewer than MaxNumGpuPerNode
	for i := 0; i < MaxNumGpuPerNode; i++ {
		if i < len(sortedIndex) {
			nrf.MilliGpu += fmt.Sprintf("%d,", tnr.MilliGpuLeftList[sortedIndex[i]])
		} else {
			nrf.MilliGpu += "0,"
		}
	}

	return nrf
}

// ToDimExtResourceVec returns a list of extended pod resource vectors, depending on different extension methods.
func (tpr PodResource) ToDimExtResourceVec(method GpuDimExtMethod, nodeRes NodeResource) [][]float64 {
	var vecList [][]float64

	if method == ExtGpuDim {
		nodeFormalizedGpuResourceVec := nodeRes.ToFormalizedGpuResourceVec()
		for i, milliGpuLeft := range nodeFormalizedGpuResourceVec {
			if milliGpuLeft < float64(tpr.MilliGpu*int64(tpr.GpuNumber)) {
				continue
			}

			var vec []float64

			// milli cpu request
			vec = append(vec, float64(tpr.MilliCpu))

			// milli gpu request
			for j := 0; j < len(nodeFormalizedGpuResourceVec); j++ {
				if j == i {
					vec = append(vec, float64(tpr.MilliGpu*int64(tpr.GpuNumber)))
				} else {
					vec = append(vec, 0)
				}
			}

			vecList = append(vecList, vec)
		}
	} else {
		var vec []float64

		// milli cpu request
		vec = append(vec, float64(tpr.MilliCpu))

		// milli gpu request
		vec = append(vec, float64(tpr.MilliGpu*int64(tpr.GpuNumber)))

		vecList = append(vecList, vec)
	}

	return vecList
}

// ToDimExtResourceVec returns a list of extended node resource vectors, depending on different extension methods.
func (tnr NodeResource) ToDimExtResourceVec(method GpuDimExtMethod, podRes PodResource) [][]float64 {
	var vecList [][]float64

	if method == MergeGpuDim {
		var vec []float64

		// milli cpu left
		vec = append(vec, float64(tnr.MilliCpu))

		// total milli gpu left
		vec = append(vec, float64(tnr.GetTotalMilliGpuLeft()))

		vecList = append(vecList, vec)
	} else if method == SeparateGpuDimAndShareOtherDim {
		formalizedGpuResourceVec := tnr.ToFormalizedGpuResourceVec()

		for _, milliGpuLeft := range formalizedGpuResourceVec {
			if milliGpuLeft < float64(podRes.MilliGpu*int64(podRes.GpuNumber)) {
				continue
			}

			var vec []float64

			// milli cpu left
			vec = append(vec, float64(tnr.MilliCpu))

			// milli gpu left
			vec = append(vec, milliGpuLeft)

			vecList = append(vecList, vec)
		}
	} else if method == SeparateGpuDimAndDivideOtherDim {
		formalizedGpuResourceVec := tnr.ToFormalizedGpuResourceVec()

		totalMilliGpuLeft := float64(tnr.GetTotalMilliGpuLeft())
		for _, milliGpuLeft := range formalizedGpuResourceVec {
			if milliGpuLeft < float64(podRes.MilliGpu*int64(podRes.GpuNumber)) {
				continue
			}

			var vec []float64

			// milli cpu left, divided by the percentage of milli gpu left
			vec = append(vec, float64(tnr.MilliCpu)*milliGpuLeft/totalMilliGpuLeft)

			// milli gpu left
			vec = append(vec, milliGpuLeft)

			vecList = append(vecList, vec)
		}
	} else {
		formalizedGpuResourceVec := tnr.ToFormalizedGpuResourceVec()

		var vec []float64

		// milli cpu left
		vec = append(vec, float64(tnr.MilliCpu))

		// milli gpu left
		vec = append(vec, formalizedGpuResourceVec...)

		vecList = append(vecList, vec)
	}

	return vecList
}

// ToFormalizedGpuResourceVec returns a formalized gpu resource vector.
// The first few items of the vector are the remaining gpu resources of the shared gpus.
// The last item is the total gpu resources of fully free gpus.
// Gpus with empty resources are not counted in the vector.
// Example:
// - original <200 GPU, 500 GPU, 1000 GPU, 1000 GPU, 1000 GPU, 1000 GPU, 1000 GPU, 1000 GPU>
// - formalized <200 GPU, 500 GPU, 6000 GPU>
func (tnr NodeResource) ToFormalizedGpuResourceVec() []float64 {
	var vec []float64

	var fullyFreeGpuNum = 0
	for _, millGpuLeft := range tnr.MilliGpuLeftList {
		if millGpuLeft == gpushareutils.MILLI {
			fullyFreeGpuNum++
		} else if millGpuLeft > 0 {
			vec = append(vec, float64(millGpuLeft))
		}
	}
	if fullyFreeGpuNum > 0 {
		vec = append(vec, float64(fullyFreeGpuNum*gpushareutils.MILLI))
	}

	return vec
}

func (tnr NodeResource) ToFormalizedAllocatableResourceVec(node *v1.Node) []float64 {
	var vec []float64

	// milli cpu allocatable
	vec = append(vec, float64(node.Status.Allocatable.Cpu().MilliValue()))

	formalizedGpuResourceVec := tnr.ToFormalizedGpuResourceVec()
	// formalized milli gpu allocatable
	for _, milliGpuLeft := range formalizedGpuResourceVec {
		if milliGpuLeft >= gpushareutils.MILLI {
			vec = append(vec, milliGpuLeft)
		} else {
			vec = append(vec, float64(gpushareutils.MILLI))
		}
	}

	return vec
}

// IsGpuShare returns true if pod is a GPU-share pod, otherwise false.
func (tpr PodResource) IsGpuShare() bool {
	if (tpr.GpuNumber == 1) && (tpr.MilliGpu < gpushareutils.MILLI) {
		return true
	}
	return false
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

	totalMilliGpuLeft := tnr.GetTotalMilliGpuLeft()
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
	sortedIndex := out.SortedMilliGpuLeftIndexList(true)

	for i := 0; i < len(sortedIndex); i++ {
		if tpr.MilliGpu <= out.MilliGpuLeftList[sortedIndex[i]] {
			gpuRequest -= 1
			out.MilliGpuLeftList[sortedIndex[i]] -= tpr.MilliGpu
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
		sortedIndex := out.SortedMilliGpuLeftIndexList(true) // smallest one first

		for i := 0; i < len(sortedIndex); i++ {
			if tpr.MilliGpu+out.MilliGpuLeftList[sortedIndex[i]] <= gpushareutils.MILLI {
				gpuRequest -= 1
				out.MilliGpuLeftList[sortedIndex[i]] += tpr.MilliGpu
				if gpuRequest <= 0 {
					//fmt.Printf("[DEBUG] [DONE] out.MilliGpuLeftList: %v\n", out.MilliGpuLeftList)
					return out, nil
				}
			}
		}
	}

	return out, fmt.Errorf("node: %s failed to evict pod: %s (%d GPU requests left)", tnr.Repr(), tpr.Repr(), gpuRequest)
}
