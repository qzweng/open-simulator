package utils

const (
	ResourceName = "alibabacloud.com/gpu-milli"      // GPU milli, i.e., 1000 == 1 GPU, for pod only, node is 1000 by default
	CountName    = "alibabacloud.com/gpu-count"      // GPU number request (or allocatable), for pod and node
	DeviceIndex  = "alibabacloud.com/gpu-index"      // Exists when the pod are assigned/predefined to a GPU device
	ModelName    = "alibabacloud.com/gpu-card-model" // GPU card model, for pod and node
	AssumeTime   = "alibabacloud.com/assume-time"    // To retrieve the scheduling latency
	CreationTime = "alibabacloud.com/creation-time"  // creation timestamp
	DeletionTime = "alibabacloud.com/deletion-time"  // deletion timestamp
	PodNsNameSep = "/"
	DevIdSep     = "-"
	MILLI        = 1000
	MaxSpecCpu   = 104000 // CPU MILLI
	MaxSpecMem   = 773405 // Mem MiB
	MaxSpecGpu   = 8000   // GPU MILLI
	NoGpuTag     = "no-gpu"
	ShareGpuTag  = "share-gpu"
	OneGpuTag    = "one-gpu"
	TwoGpuTag    = "two-gpu"
	FourGpuTag   = "four-gpu"
	EightGpuTag  = "eight-gpu"
)

var MapGpuTypeMemoryMiB = map[string]int{
	"P4":      int(7980711936 / 1024 / 1024),  //  7611 MiB, "Tesla-P4"
	"2080":    int(11554258944 / 1024 / 1024), // 11019 MiB, "GeForce-RTX-2080-Ti", "NVIDIA-GeForce-RTX-2080-Ti"
	"1080":    int(11720982528 / 1024 / 1024), // 11178 MiB, "GeForce-GTX-1080-Ti"
	"M40":     int(12004098048 / 1024 / 1024), // 11448 MiB, "Tesla-M40"
	"T4":      int(15842934784 / 1024 / 1024), // 15109 MiB, "Tesla-T4"
	"V100M16": int(16944988160 / 1024 / 1024), // 16160 MiB, "Tesla-V100-SXM2-16GB"
	"P100":    int(17070817280 / 1024 / 1024), // 16280 MiB, "Tesla-P100-PCIE-16GB"
	"A10":     int(23835181056 / 1024 / 1024), // 22731 MiB, "A10", "NVIDIA-A10"
	"3090":    int(25446842368 / 1024 / 1024), // 24268 MiB, "GeForce-RTX-3090"
	"V100M32": int(34089205760 / 1024 / 1024), // 32510 MiB, "Tesla-V100-SXM2-32GB", "Tesla-V100S-PCIE-32GB"
}

/*
| model                | num_gpu | num_node | gpu_mem_each |
| -------------------- | ------- | -------- | ------------ |
| CPU                  | 0       | 727      | 0            |
| GeForce-RTX-2080-Ti  | 3248    | 406      | 11019        |
| GeForce-GTX-1080-Ti  | 16      | 2        | 11178        |
| Tesla-T4             | 802     | 382      | 15109        |
| Tesla-V100-SXM2-16GB | 201     | 61       | 16130        |
| Tesla-V100-SXM2-16GB | 201     | 61       | 16160        |
| Tesla-P100-PCIE-16GB | 382     | 200      | 16280        |
| Tesla-V100-SXM2-32GB | 284     | 40       | 32510        |
*/
