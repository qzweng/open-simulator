package simulator

import (
	"context"
	"github.com/alibaba/open-simulator/pkg/api/v1alpha1"
	log "github.com/sirupsen/logrus"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/informers"
	externalclientset "k8s.io/client-go/kubernetes"
)

func TestGenerateWorkloadInflationPods(t *testing.T) {
	var client externalclientset.Interface
	ctx, cancel := context.WithCancel(context.Background())
	options := defaultSimulatorOptions
	sim := &Simulator{
		client:          client,
		informerFactory: informers.NewSharedInformerFactory(client, 0),
		ctx:             ctx,
		cancelFunc:      cancel,
		customConfig:    options.customConfig,
	}

	sim.customConfig.TypicalPodsConfig = v1alpha1.TypicalPodsConfig{IsInvolvedCpuPods: true, PodPopularityThreshold: 95, PodIncreaseStep: 1, IsConsideredGpuResWeight: false}
	customConfig := sim.GetCustomConfig()
	resources, _ := CreateClusterResourceFromClusterConfig(customConfig.NewWorkloadConfig)
	pods, _ := GetValidPodExcludeDaemonSet(resources)
	sim.SetWorkloadPods(pods)

	sim.SetTypicalPods()
	log.Infof("TypicalPodsConfig: %v\n", sim.customConfig.TypicalPodsConfig)
	assert.Equal(t, "s", "s")
}
