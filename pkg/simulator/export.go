package simulator

import (
	"encoding/csv"
	gpushareutils "github.com/alibaba/open-simulator/pkg/type/open-gpu-share/utils"
	"os"
	"strconv"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/printers"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
)

func (sim *Simulator) ExportPodSnapshotInYaml(unschedulePods []simontype.UnscheduledPod, filePath string) {
	var err error
	if filePath == "" {
		log.Infof("[ExportPodSnapshotInYaml] the file path is empty\n")
		return
	}

	var podList *corev1.PodList
	podList, err = sim.client.CoreV1().Pods(corev1.NamespaceAll).List(sim.ctx, metav1.ListOptions{})
	if err != nil {
		log.Errorf("[ExportPodSnapshotInYaml] failed to get pod list\n")
		return
	}

	var file *os.File
	file, err = os.Create(filePath)
	if err != nil {
		log.Errorf("[ExportPodSnapshotInYaml] failed to create file(%s)\n", filePath)
		return
	}
	defer file.Close()

	y := printers.YAMLPrinter{}
	for _, p := range podList.Items {
		pod := p.DeepCopy()
		if pod.Spec.NodeSelector == nil {
			pod.Spec.NodeSelector = map[string]string{}
		}
		if pod.Spec.NodeName == "" {
			MarkPodUnscheduledAnno(pod)
		} else {
			pod.Spec.NodeSelector[simontype.HostName] = pod.Spec.NodeName
			pod.Spec.NodeName = ""
			pod.Status = corev1.PodStatus{}
		}
		err = y.PrintObj(pod, file)
		if err != nil {
			log.Errorf("[ExportPodSnapshotInYaml] failed to export pod(%s) yaml to file(%s)\n",
				utils.GeneratePodKey(pod), filePath)
			return
		}
	}

	for _, unschedulePod := range unschedulePods {
		pod := unschedulePod.Pod.DeepCopy()
		MarkPodUnscheduledAnno(pod)
		err = y.PrintObj(pod, file)
		if err != nil {
			log.Errorf("[ExportPodSnapshotInYaml] failed to export pod(%s) yaml to file(%s)\n",
				utils.GeneratePodKey(pod), filePath)
			return
		}
	}
}

func (sim *Simulator) ExportNodeSnapshotInCSV(filePath string) {
	log.Infof("[ExportNodeSnapshotInCSV] attempt to export node snapshot to csv file(%s)\n", filePath)
	var file *os.File
	var err error
	file, err = os.Create(filePath)
	if err != nil {
		log.Errorf("[ExportNodeSnapshotInCSV] failed to create file(%s): %v\n", filePath, err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)

	nodeStatus := sim.GetClusterNodeStatus()
	nodeResMap := utils.GetNodeResourceMap(nodeStatus)

	header := []string{
		"name", "ip", "model", "cpu", "gpu", "memory_mib", "gpu_mem_mib_each", "num_pod",
		"cpu_milli_left", "memory_mib_left", "gpu_mem_mib_left", "gpu_milli_left",
		"gpu_mem_mib_left_0", "gpu_milli_left_0",
		"gpu_mem_mib_left_1", "gpu_milli_left_1",
		"gpu_mem_mib_left_2", "gpu_milli_left_2",
		"gpu_mem_mib_left_3", "gpu_milli_left_3",
		"gpu_mem_mib_left_4", "gpu_milli_left_4",
		"gpu_mem_mib_left_5", "gpu_milli_left_5",
		"gpu_mem_mib_left_6", "gpu_milli_left_6",
		"gpu_mem_mib_left_7", "gpu_milli_left_7",
	}
	err = writer.Write(header)
	if err != nil {
		return
	}

	for _, ns := range nodeStatus {
		node := ns.Node
		nodeRes, ok := nodeResMap[node.Name]
		if !ok {
			continue
		}
		var data []string

		name := node.Name
		data = append(data, name)

		ip, _ := node.ObjectMeta.Labels[simontype.NodeIp]
		data = append(data, ip)

		model := gpushareutils.GetGpuModelOfNode(node)
		if model == "" {
			model = "CPU"
		}
		data = append(data, model)

		cpu := node.Status.Capacity.Cpu().Value()
		data = append(data, strconv.FormatInt(cpu, 10))

		gpu := gpushareutils.GetGpuCountOfNode(node)
		data = append(data, strconv.Itoa(gpu))

		memory := node.Status.Capacity.Memory().Value()
		data = append(data, strconv.FormatInt(memory, 10))

		// gpu_mem_mib_each
		data = append(data, "0")

		numPod := len(ns.Pods)
		data = append(data, strconv.Itoa(numPod))

		// cpu_milli_left
		data = append(data, strconv.FormatInt(nodeRes.MilliCpuLeft, 10))

		// memory_mib_left
		data = append(data, "0")

		// gpu_mem_mib_left
		data = append(data, "0")

		// gpu_milli_left
		data = append(data, "0")

		for i := 0; i < 8; i++ {
			// gpu_mem_mib_left_$i
			data = append(data, "0")

			// gpu_milli_left_$i
			var gpuMilliLeft int64
			if i < len(nodeRes.MilliGpuLeftList) {
				gpuMilliLeft = nodeRes.MilliGpuLeftList[i]
			}
			data = append(data, strconv.FormatInt(gpuMilliLeft, 10))
		}

		err = writer.Write(data)
		if err != nil {
			return
		}
	}
	writer.Flush()
}
