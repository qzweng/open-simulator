package plugin

import (
	"fmt"
	"testing"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestCalculateCosineSimilarityScore(t *testing.T) {
	// case-2
	nodeRes := simontype.NodeResource{"Hello", 1000, []int64{200, 1000, 1000, 500}, 4, "1080"}
	podRes := simontype.PodResource{100, 1000, 2, "1080"}
	fmt.Printf("IsNodeAccessibleToPod: %v\n", utils.IsNodeAccessibleToPod(nodeRes, podRes))
	method := simontype.GpuDimExtMethod("merge")
	score, _, _ := calculateCosineSimilarityScore(nodeRes, podRes, method)
	assert.Equal(t, int64(77), score)

	// case-3 4-GPU
	nodeRes = simontype.NodeResource{"Hello", 1000, []int64{1000, 1000, 1000, 1000}, 4, "1080"}
	podRes = simontype.PodResource{100, 1000, 2, "1080"}
	score, _, _ = calculateCosineSimilarityScore(nodeRes, podRes, method)
	assert.Equal(t, int64(77), score)

	// case-3 8-GPU
	nodeRes = simontype.NodeResource{"Hello", 1000, []int64{1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000}, 8, "1080"}
	podRes = simontype.PodResource{100, 1000, 2, "1080"}
	score, _, _ = calculateCosineSimilarityScore(nodeRes, podRes, method)
	assert.Equal(t, int64(77), score)

	// case-1 only use existing GPUs
	nodeRes = simontype.NodeResource{"Hello", 1000, []int64{200, 1000, 1000, 500}, 4, "1080"}
	podRes = simontype.PodResource{100, 200, 2, "1080"}
	score, _, _ = calculateCosineSimilarityScore(nodeRes, podRes, method)
	assert.Equal(t, int64(94), score)
	assert.Equal(t, simontype.NodeResource{"Hello", 1000, []int64{200, 1000, 1000, 500}, 4, "1080"}, nodeRes) // The order of origin MilliGpuLeftList should not change
}
