package simontype

import (
	"fmt"
	"sort"

	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
	"github.com/alibaba/open-simulator/pkg/utils"
)

type TargetPod struct {
	TargetPodResource TargetPodResource
	Percentage        float64
}

type TargetPodList []TargetPod

func (p TargetPodList) Len() int           { return len(p) }
func (p TargetPodList) Less(i, j int) bool { return p[i].Percentage < p[j].Percentage }
func (p TargetPodList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

const (
	TypicalPodPopularityThreshold = 60 // 60%
	TypicalPodResourceNumber      = 10
)

type TargetPodResource struct {
	Namespace string
	Name      string
	MilliCpu  int64
	MilliGpu  int64 // Milli GPU request per GPU, 0-1000
	GpuNumber int
	GpuType   string //
	//Memory	  int64
}

type TargetNodeResource struct {
	NodeName         string
	MilliCpu         int64
	MilliGpuLeftList []int64
	GpuNumber        int
	GpuType          string
	//Memory	  int64
}

func (tpr TargetPodResource) Repr() string {
	outStr := "<"
	outStr += fmt.Sprintf("CPU: %6.2f", float64(tpr.MilliCpu)/1000)
	outStr += fmt.Sprintf(", GPU: %d", tpr.GpuNumber)
	outStr += fmt.Sprintf(" x %dm", tpr.MilliGpu)
	outStr += ">"
	return outStr
}

func (tnr TargetNodeResource) Repr() string {
	outStr := tnr.NodeName + " <"
	outStr += fmt.Sprintf("CPU: %6.2f", float64(tnr.MilliCpu)/1000)
	outStr += fmt.Sprintf(", GPU: %d", tnr.GpuNumber)
	if tnr.GpuNumber > 0 {
		outStr += fmt.Sprintf(" x %dm, Left:", gpushareutils.MILLI)
		for _, gML := range tnr.MilliGpuLeftList {
			outStr += fmt.Sprintf(" %dm", gML)
		}
	}
	outStr += ">"
	return outStr
}

func (tnr TargetNodeResource) Copy() TargetNodeResource {
	milliGpuLeftList := make([]int64, len(tnr.MilliGpuLeftList))
	for i := 0; i < len(tnr.MilliGpuLeftList); i++ {
		milliGpuLeftList[i] = tnr.MilliGpuLeftList[i]
	}

	return TargetNodeResource{
		NodeName:         tnr.NodeName,
		MilliCpu:         tnr.MilliCpu,
		MilliGpuLeftList: milliGpuLeftList,
		GpuNumber:        tnr.GpuNumber,
		GpuType:          tnr.GpuType,
	}
}

func (tnr TargetNodeResource) Sub(tpr TargetPodResource) (TargetNodeResource, error) {
	out := tnr.Copy()
	if out.MilliCpu < tpr.MilliCpu || out.GpuNumber < tpr.GpuNumber || !utils.IsNodeAccessibleToPod(tnr, tpr) {
		return out, fmt.Errorf("node: %s failed to accommodate pod: %s", tnr.Repr(), tpr.Repr())
	}
	out.MilliCpu -= tpr.MilliCpu

	if tpr.GpuNumber == 0 {
		return out, nil
	}

	gpuRequest := tpr.GpuNumber
	//fmt.Printf("[DEBUG] [ORIN] out.MilliGpuLeftList: %v\n", out.MilliGpuLeftList)
	sort.Slice(out.MilliGpuLeftList, func(i, j int) bool { return out.MilliGpuLeftList[i] > out.MilliGpuLeftList[j] })
	//fmt.Printf("[DEBUG] [SORT] out.MilliGpuLeftList: %v\n", out.MilliGpuLeftList)

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
