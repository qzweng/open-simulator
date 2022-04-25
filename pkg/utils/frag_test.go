package utils

import (
	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
)

func TestNodeGpuFragAmountBellman_Direct(t *testing.T) {
	nodeRes := simontype.NodeResource{"node_1C_4x1080", 1000,
		[]int64{200, 600, 350, 100}, 4, "1080"} // GpuType: See MapGpuTypeMemoryMiB
	typicalPods := simontype.TargetPodList{}
	pod := simontype.TargetPod{}

	// Q1LackBoth
	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 2000, MilliGpu: 1000, GpuNumber: 4, GpuType: "",
	}, Percentage: 0.1}
	typicalPods = append(typicalPods, pod)

	// Q2LackGpu
	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 1000, MilliGpu: 1000, GpuNumber: 4, GpuType: "",
	}, Percentage: 0.2}
	typicalPods = append(typicalPods, pod)

	// Q3Satisfied
	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 1000, MilliGpu: 100, GpuNumber: 4, GpuType: "",
	}, Percentage: 0.3}
	typicalPods = append(typicalPods, pod)

	// Q4LackCpu
	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 2000, MilliGpu: 100, GpuNumber: 1, GpuType: "",
	}, Percentage: 0.1}
	typicalPods = append(typicalPods, pod)

	// XLSatisfied
	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 1000, MilliGpu: 0, GpuNumber: 0, GpuType: "",
	}, Percentage: 0.1}
	typicalPods = append(typicalPods, pod)

	// XRLackCPU
	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 2000, MilliGpu: 0, GpuNumber: 0, GpuType: "",
	}, Percentage: 0.1}
	typicalPods = append(typicalPods, pod)

	// NoAccess
	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 1000, MilliGpu: 200, GpuNumber: 2, GpuType: "V100M32",
	}, Percentage: 0.1}
	typicalPods = append(typicalPods, pod)

	dp := sync.Map{}
	fragAmountBellman := NodeGpuFragAmountBellman(nodeRes, typicalPods, &dp)
	fragAmount := NodeGpuFragAmount(nodeRes, typicalPods)
	assert.Equal(t, FragAmount{"node_1C_4x1080", []float64{125, 250, 375, 125, 125, 125, 125}}, fragAmount)
	assert.Equal(t, fragAmount, fragAmountBellman)
	// Q1LackBoth: 0, Q2LackGpu: 1, Q3Satisfied: 2, Q4LackCpu: 3, XLSatisfied: 4, XRLackCPU: 5, NoAccess: 6
}

func TestNodeGpuFragAmountBellman_NonDirect(t *testing.T) {
	nodeRes := simontype.NodeResource{"node_2C_4x1080", 2000,
		[]int64{1000, 1000, 1000, 1000}, 4, "V100M32"} // GpuType: See MapGpuTypeMemoryMiB
	typicalPods := simontype.TargetPodList{}
	pod := simontype.TargetPod{}

	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 1500, MilliGpu: 900, GpuNumber: 4, GpuType: "",
	}, Percentage: 0.1}
	typicalPods = append(typicalPods, pod)

	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 1000, MilliGpu: 900, GpuNumber: 4, GpuType: "",
	}, Percentage: 0.2}
	typicalPods = append(typicalPods, pod)

	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 500, MilliGpu: 100, GpuNumber: 4, GpuType: "",
	}, Percentage: 0.3}
	typicalPods = append(typicalPods, pod)

	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 1500, MilliGpu: 100, GpuNumber: 1, GpuType: "",
	}, Percentage: 0.2}
	typicalPods = append(typicalPods, pod)

	pod = simontype.TargetPod{TargetPodResource: simontype.PodResource{
		MilliCpu: 1000, MilliGpu: 200, GpuNumber: 2, GpuType: "1080",
	}, Percentage: 0.2}
	typicalPods = append(typicalPods, pod)

	dp := sync.Map{}
	fragAmount := NodeGpuFragAmountBellman(nodeRes, typicalPods, &dp)
	assert.Equal(t, FragAmount{"node_2C_4x1080", []float64{211.014, 205.056, 730.296, 870.534, 0, 0, 0}}.Repr(), fragAmount.Repr())
	// Q1LackBoth: 0, Q2LackGpu: 1, Q3Satisfied: 2, Q4LackCpu: 3, XLSatisfied: 4, XRLackCPU: 5, NoAccess: 6
}
