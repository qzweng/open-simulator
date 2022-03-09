package simontype

import (
	"fmt"
	"sort"
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
	MilliCpu  int64
	GpuNumber int
	GpuMemory int64 // GPU Memory per GPU
	//Memory	  int64
}

type TargetNodeResource struct {
	NodeName       string
	MilliCpu       int64
	GpuMemLeftList []int64
	GpuMemTotal    int64
	GpuNumber      int
	//Memory	  int64
}

func (tpr TargetPodResource) Repr() string {
	outStr := "<"
	outStr += fmt.Sprintf("CPU: %6.2f", float64(tpr.MilliCpu)/1000)
	outStr += fmt.Sprintf(", GPU: %d", tpr.GpuNumber)
	gpuMemMiB := float64(tpr.GpuMemory / 1024 / 1024)
	outStr += fmt.Sprintf(" x %5.0f MiB", gpuMemMiB)
	outStr += ">"
	return outStr
}

func (tnr TargetNodeResource) Repr() string {
	outStr := tnr.NodeName + " <"
	outStr += fmt.Sprintf("CPU: %6.2f", float64(tnr.MilliCpu)/1000)
	outStr += fmt.Sprintf(", GPU: %d", tnr.GpuNumber)
	if tnr.GpuNumber > 0 {
		gpuMemMiB := float64(tnr.GpuMemTotal/int64(tnr.GpuNumber)) / 1024.0 / 1024.0
		outStr += fmt.Sprintf(" x %6.0f MiB, Left:", gpuMemMiB)
		for _, gML := range tnr.GpuMemLeftList {
			outStr += fmt.Sprintf(" %5.0f MiB", float64(gML/1024.0/1024.0))
		}
	}
	outStr += ">"
	return outStr
}

func (tnr TargetNodeResource) Copy() TargetNodeResource {
	gpuMemLeftList := make([]int64, len(tnr.GpuMemLeftList))
	for i := 0; i < len(tnr.GpuMemLeftList); i++ {
		gpuMemLeftList[i] = tnr.GpuMemLeftList[i]
	}

	return TargetNodeResource{
		NodeName:       tnr.NodeName,
		MilliCpu:       tnr.MilliCpu,
		GpuMemLeftList: gpuMemLeftList,
		GpuMemTotal:    tnr.GpuMemTotal,
		GpuNumber:      tnr.GpuNumber,
	}
}

func (tnr TargetNodeResource) Sub(tpr TargetPodResource) (TargetNodeResource, error) {
	out := tnr.Copy()
	if out.MilliCpu < tpr.MilliCpu || out.GpuMemTotal < tpr.GpuMemory || out.GpuNumber < tpr.GpuNumber {
		return out, fmt.Errorf("node: %s failed to accommodate pod: %s", tnr.Repr(), tpr.Repr())
	}
	out.MilliCpu -= tpr.MilliCpu

	if tpr.GpuNumber == 0 {
		return out, nil
	}

	gpuRequest := tpr.GpuNumber
	//fmt.Printf("[DEBUG] [ORIN] out.GpuMemLeftList: %v\n", out.GpuMemLeftList)
	sort.Slice(out.GpuMemLeftList, func(i, j int) bool { return out.GpuMemLeftList[i] > out.GpuMemLeftList[j] })
	//fmt.Printf("[DEBUG] [SORT] out.GpuMemLeftList: %v\n", out.GpuMemLeftList)

	for i := 0; i < len(out.GpuMemLeftList); i++ {
		if tpr.GpuMemory <= out.GpuMemLeftList[i] {
			gpuRequest -= 1
			out.GpuMemLeftList[i] -= tpr.GpuMemory
			if gpuRequest <= 0 {
				//fmt.Printf("[DEBUG] [DONE] out.GpuMemLeftList: %v\n", out.GpuMemLeftList)
				return out, nil
			}
		}
	}
	return out, fmt.Errorf("node: %s failed to accommodate pod: %s (%d GPU requests left)", tnr.Repr(), tpr.Repr(), gpuRequest)
}
