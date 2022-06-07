package utils

import (
	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGetResourceSimilarity(t *testing.T) {
	nodeRes := simontype.NodeResource{NodeName: "Hello", MilliCpu: 1000, MilliGpuLeftList: []int64{200, 600, 350, 0}, GpuNumber: 4, GpuType: "1080"}
	podRes := simontype.PodResource{MilliCpu: 500, MilliGpu: 1000, GpuNumber: 2, GpuType: "1080"}
	assert.InDelta(t, GetResourceSimilarity(nodeRes, podRes), 0.89122160265, 1e-3) //(1000*500+1150*2000)/(sqrt((1000^2+1150^2))*sqrt((500^2+2000^2)))

	nodeRes = simontype.NodeResource{NodeName: "Hello", MilliCpu: 0, MilliGpuLeftList: []int64{1000, 1000}, GpuNumber: 2, GpuType: "P4"}
	podRes = simontype.PodResource{MilliCpu: 1000, MilliGpu: 10, GpuNumber: 2, GpuType: "1080"}
	assert.InDelta(t, GetResourceSimilarity(nodeRes, podRes), 0.0199960012, 1e-3) //(0*1000+2000*20)/(sqrt((0^2+2000^2))*sqrt((1000^2+20^2)))
}

func TestCalculateVectorDotProduct(t *testing.T) {
	nodeRes := simontype.NodeResource{NodeName: "Hello", MilliCpu: 1000, MilliGpuLeftList: []int64{200, 600, 350, 0}, GpuNumber: 4, GpuType: "1080"}
	podRes := simontype.PodResource{MilliCpu: 500, MilliGpu: 1000, GpuNumber: 2, GpuType: "1080"}

	nodeCap := []float64{2000, 4000}
	nodeVec := NormalizeVector(nodeRes.ToResourceVec(), nodeCap) // (0.5, 0.2875)
	podVec := NormalizeVector(podRes.ToResourceVec(), nodeCap)   // (0.25, 0.5)
	assert.InDelta(t, CalculateVectorDotProduct(nodeVec, podVec), 0.26875, 1e-3)

	nodeRes = simontype.NodeResource{NodeName: "Hello", MilliCpu: 0, MilliGpuLeftList: []int64{1000, 1000}, GpuNumber: 2, GpuType: "P4"}
	podRes = simontype.PodResource{MilliCpu: 1000, MilliGpu: 10, GpuNumber: 2, GpuType: "1080"}

	nodeCap = []float64{2000, 2000}
	nodeVec = NormalizeVector(nodeRes.ToResourceVec(), nodeCap) // (0, 1)
	podVec = NormalizeVector(podRes.ToResourceVec(), nodeCap)   // (0.5, 0.01)
	assert.InDelta(t, CalculateVectorDotProduct(nodeVec, podVec), 0.01, 1e-3)
}
