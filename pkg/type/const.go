package simontype

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SimonPluginName                        = "Simon"
	OpenLocalPluginName                    = "Open-Local"
	OpenGpuSharePluginName                 = "Open-Gpu-Share"
	GpuFragScorePluginName                 = "Gpu-Frag-Score"
	GpuFragScoreBellmanPluginName          = "Gpu-Frag-Score-Bellman"
	GpuShareFragScorePluginName            = "Gpu-Share-Frag-Score"
	GpuShareFragSimScorePluginName         = "Gpu-Share-Frag-Sim-Score"
	GpuShareFragSimNormScorePluginName     = "Gpu-Share-Frag-Sim-Norm-Score"
	GpuShareFragBestFitScorePluginName     = "Gpu-Share-Frag-Best-Fit-Score"
	GpuShareFragDotProductScorePluginName  = "Gpu-Share-Frag-Dot-Product-Score"
	GpuShareFragL2NormRatioScorePluginName = "Gpu-Share-Frag-L2-Norm-Ratio-Score"
	GpuShareFragPackingScorePluginName     = "Gpu-Share-Frag-Packing-Score"
	GpuFragSimScorePluginName              = "Gpu-Frag-Sim-Score"
	GpuPackingScorePluginName              = "Gpu-Packing-Score"
	GpuPackingSimScorePluginName           = "Gpu-Packing-Sim-Score"
	CosineSimilarityPluginName             = "CosineSimilarityScore"
	CosineSimPackingPluginName             = "CosineSimPackingScore"
	BestFitScorePluginName                 = "BestFitScore"
	WorstFitScorePluginName                = "WorstFitScore"
	DotProductScorePluginName              = "DotProductScore"
	L2NormDiffScorePluginName              = "L2NormDiffScore"
	L2NormRatioScorePluginName             = "L2NormRatioScore"
	FFDProdScoreName                       = "FFDProdScore"
	FFDSumScoreName                        = "FFDSumScore"
	GandivaScorePluginName                 = "GandivaScore"
	SynergyScorePluginName                 = "SynergyScore"
	NewNodeNamePrefix                      = "simon"
	SimulatorName                          = "Main"
	DefaultSchedulerName                   = "simon-scheduler"

	MaxNodeCpuCapacity int64 = 104000
	MaxNodeGpuCapacity int64 = 8000

	StopReasonSuccess   = "everything is ok"
	StopReasonDoNotStop = "do not stop"
	CreatePodError      = "failed to create pod"
	DeletePodError      = "failed to delete pod"

	AnnoWorkloadKind      = "simon/workload-kind"
	AnnoWorkloadName      = "simon/workload-name"
	AnnoWorkloadNamespace = "simon/workload-namespace"
	AnnoNodeLocalStorage  = "simon/node-local-storage"
	AnnoPodLocalStorage   = "simon/pod-local-storage"
	AnnoNodeGpuShare      = "simon/node-gpu-share"
	AnnoPodUnscheduled    = "simon/pod-unscheduled"

	LabelNewNode = "simon/new-node"
	LabelAppName = "simon/app-name"

	EnvMaxCPU    = "MaxCPU"
	EnvMaxMemory = "MaxMemory"
	EnvMaxVG     = "MaxVG"

	Pod                   = "Pod"
	Deployment            = "Deployment"
	ReplicaSet            = "ReplicaSet"
	ReplicationController = "ReplicationController"
	StatefulSet           = "StatefulSet"
	DaemonSet             = "DaemonSet"
	Job                   = "Job"
	CronJob               = "CronJob"

	HostName = "kubernetes.io/hostname"
	NodeIp   = "node-ip"

	ConfigMapName      = "simulator-plan"
	ConfigMapNamespace = metav1.NamespaceSystem
	ConfigMapFileName  = "configmap-simon.yaml"

	NotesFileSuffix       = "NOTES.txt"
	SeparateSymbol        = "-"
	WorkLoadHashCodeDigit = 10
	PodHashCodeDigit      = 5
	MaxNumNewNode         = 0
	MaxNumGpuPerNode      = 8
)
