package v1alpha1

type AppInfo struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Chart bool   `json:"chart,omitempty"`
}

type Cluster struct {
	CustomCluster string `json:"customConfig,omitempty"`
	KubeConfig    string `json:"kubeConfig,omitempty"`
}

type SimonSpec struct {
	Cluster      Cluster      `json:"cluster"`
	AppList      []AppInfo    `json:"appList"`
	NewNode      string       `json:"newNode"`
	CustomConfig CustomConfig `json:"customConfig,omitempty"`
}

type SimonMetaData struct {
	Name string `json:"name"`
}

type CustomConfig struct {
	ShufflePod             bool    `json:"shufflePod,omitempty"`
	WorkloadInflationRatio float64 `json:"workloadInflationRatio,omitempty"`
	DescheduleRatio        float64 `json:"descheduleRatio,omitempty"`
	DeschedulePolicy       string  `json:"deschedulePolicy,omitempty"`
	ExportScheduleSnapshot bool    `json:"exportScheduleSnapshot,omitempty"`
	SnapshotFilePath       string  `json:"snapshotFilePath,omitempty"`
}

type Simon struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	MetaData   SimonMetaData `json:"metadata"`
	Spec       SimonSpec     `json:"spec"`
}
