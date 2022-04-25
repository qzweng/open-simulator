package simontype

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNodeResource_Flatten(t *testing.T) {
	tnr := NodeResource{"Hello", 1000, []int64{200, 600, 350, 0}, 4, "1080Ti"}
	assert.Equal(t, NodeResourceFlat{1000, "600,350,200,0,0,0,0,0,", "1080Ti"}, tnr.Flatten())

	tnr = NodeResource{"", 300, []int64{0, 0, 0, 0}, 1, ""}
	assert.Equal(t, NodeResourceFlat{300, "0,0,0,0,0,0,0,0,", ""}, tnr.Flatten())

	tnr = NodeResource{"", 65535, []int64{1000, 2000, 3000, 4000, 5000, 6000, 7000, 8000, 9000}, 9, ""} // invalid input
	assert.Equal(t, NodeResourceFlat{65535, "9000,8000,7000,6000,5000,4000,3000,2000,", ""}, tnr.Flatten())
}
