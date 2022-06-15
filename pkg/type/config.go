package simontype

type GpuDimExtMethod string

const (
	// MergeGpuDim adds the resources of each gpu together as one dimension.
	MergeGpuDim GpuDimExtMethod = "merge"
	// SeparateGpuDimAndShareOtherDim splits each node into multiple virtual nodes to be consistent with pod resource dimension.
	// Each virtual node contains a shared gpu or multiple fully free gpus, shares resources in other dimensions such as cpu, memory, etc.
	SeparateGpuDimAndShareOtherDim GpuDimExtMethod = "share"
	// SeparateGpuDimAndDivideOtherDim is similar to SeparateGpuDimAndShareOtherDim.
	// The difference is that it divides the resources of other dimensions according to the amount of gpu resources left.
	SeparateGpuDimAndDivideOtherDim GpuDimExtMethod = "divide"
	// ExtGpuDim is used to raise the resource dimension at the pod level to be consistent with node gpu resource dimension.
	ExtGpuDim GpuDimExtMethod = "extend"
)

type GpuSelMethod string

const (
	SelBestFitGpu  GpuSelMethod = "best"
	SelWorstFitGpu GpuSelMethod = "worst"
	SelRandomGpu   GpuSelMethod = "random"
)

type GpuPluginCfg struct {
	DimExtMethod GpuDimExtMethod `json:"dimExtMethod,omitempty"`

	// only used for OpenGpuShare plugin.
	GpuSelMethod GpuSelMethod `json:"gpuSelMethod,omitempty"`
}
