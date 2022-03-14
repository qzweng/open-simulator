package plugin

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externalclientset "k8s.io/client-go/kubernetes"

	"github.com/alibaba/open-simulator/pkg/type"
	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
	"github.com/alibaba/open-simulator/pkg/utils"
)

func GetNodeResourceMap(result *simontype.SimulateResult) map[string]simontype.TargetNodeResource {
	var allPods []corev1.Pod
	for _, ns := range result.NodeStatus {
		for _, pod := range ns.Pods {
			allPods = append(allPods, *pod)
		}
	}

	nodeResMap := make(map[string]simontype.TargetNodeResource)
	for _, ns := range result.NodeStatus {
		node := ns.Node
		if nodeRes, err := GetNodeResourceViaPodList(allPods, node); err != nil {
			nodeResMap[node.Name] = nodeRes
		}
	}
	return nodeResMap
}

func GetNodeResourceViaClient(client externalclientset.Interface, ctx context.Context, node *corev1.Node) (nodeRes simontype.TargetNodeResource, err error) {
	if podsOnNode, err := client.CoreV1().Pods(corev1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + node.Name}); err == nil {
		if nodeRes, err = GetNodeResourceViaPodList(podsOnNode.Items, node); err != nil {
			return nodeRes, err
		}
	}
	return nodeRes, err
}

func GetNodeResourceViaPodList(podList []corev1.Pod, node *corev1.Node) (nodeRes simontype.TargetNodeResource, err error) {
	allocatable := node.Status.Allocatable
	reqs, _ := utils.GetPodsTotalRequestsAndLimitsByNodeName(podList, node.Name)
	nodeCpuReq, _ := reqs[corev1.ResourceCPU], reqs[corev1.ResourceMemory]

	gpuNumber := gpushareutils.GetGpuCountOfNode(node)
	gpuMilliLeftList := make([]int64, gpuNumber)
	for i := 0; i < gpuNumber; i++ {
		gpuMilliLeftList[i] = gpushareutils.MILLI
	}

	if gpuNodeInfoStr, err := utils.GetGpuNodeInfoFromAnnotation(node); err == nil {
		if gpuNodeInfoStr != nil {
			for _, dev := range gpuNodeInfoStr.DevsBrief {
				gpuMilliLeftList[dev.Idx] -= dev.GpuUsedMilli
			}
		}
		nodeRes = simontype.TargetNodeResource{
			NodeName:         node.Name,
			MilliCpu:         allocatable.Cpu().MilliValue() - nodeCpuReq.MilliValue(),
			MilliGpuLeftList: gpuMilliLeftList,
			GpuNumber:        gpuNumber,
			GpuType:          gpushareutils.GetGpuModelOfNode(node),
		}
	}
	return nodeRes, err
}
