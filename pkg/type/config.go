package simontype

type GpuDimExtMethod string

const (
	// Consider the following example:
	// 	- Pod <100 CPU, 100 GPU>
	//	- Node <3000 CPU, 200 GPU, 500 GPU, 1000 GPU, 1000 GPU, 1000 GPU, 1000 GPU, 1000 GPU, 1000 GPU>

	// MergeGpuDim adds the node resources of each gpu together as one dimension.
	// e.g., <3000 CPU, 6700 GPU>
	MergeGpuDim GpuDimExtMethod = "merge"

	// SeparateGpuDimAndShareOtherDim splits each node into multiple virtual nodes to be consistent with pod resource dimension.
	// Each virtual node contains a shared gpu or multiple fully free gpus, shares resources in other dimensions such as cpu, memory, etc.
	// e.g., <3000 CPU, 200 GPU>, <3000 CPU, 500 GPU>, <3000 CPU, 6000 GPU> =? <3000 CPU, 1000 GPU> * 6
	SeparateGpuDimAndShareOtherDim GpuDimExtMethod = "share"

	// SeparateGpuDimAndDivideOtherDim is similar to SeparateGpuDimAndShareOtherDim.
	// The difference is that it divides the resources of other dimensions according to the amount of gpu resources left.
	// e.g., <89.55 CPU, 200 GPU>, <223.88 CPU, 500 GPU>, <2686.57 CPU, 6000 GPU>
	SeparateGpuDimAndDivideOtherDim GpuDimExtMethod = "divide"

	// ExtGpuDim is used to raise the resource dimension at the pod level to be consistent with node gpu resource dimension.
	// e.g., 1) Pod <100 CPU, 100 GPU, 0 GPU, 0 GPU>, Node <3000 CPU, 200 GPU, 500 GPU, 6000 GPU>
	//       2) Pod <100 CPU, 0 GPU, 100 GPU, 0 GPU>, Node <3000 CPU, 200 GPU, 500 GPU, 6000 GPU>
	//       3) Pod <100 CPU, 0 GPU, 0 GPU, 100 GPU>, Node <3000 CPU, 200 GPU, 500 GPU, 6000 GPU>
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
