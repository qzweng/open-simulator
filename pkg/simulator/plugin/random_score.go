package plugin

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"math/rand"

	simontype "github.com/alibaba/open-simulator/pkg/type"
)

type RandomScorePlugin struct {
	handle framework.Handle
}

var _ framework.ScorePlugin = &RandomScorePlugin{}

func NewRandomScorePlugin(configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &RandomScorePlugin{
		handle: handle,
	}, nil
}

func (plugin *RandomScorePlugin) Name() string {
	return simontype.RandomScorePluginName
}

func (plugin *RandomScorePlugin) Score(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
	rand.Seed(time.Now().UnixNano())
	score := int64(rand.Int() % 101)
	return score, framework.NewStatus(framework.Success)
}

func (plugin *RandomScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}
