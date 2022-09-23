import os
import yaml
import shutil
import argparse
import subprocess
from hashlib import md5
from pathlib import Path
""" Usage: paib 0 =dotprod_divide_2K=> 2K pods
EXPDIR="experiments/0705/seed233/01-fragshare" 
mkdir -p ${EXPDIR} && touch "${EXPDIR}/terminal.out"
python3 scripts/h_generate_config_and_run.py -d "${EXPDIR}" \
-seed 233 \
-e -b \
-f data/cluster_paib-pod_paib_0318_gpu_3000 \
-fragshare 1000 \
-y "${EXPDIR}/snapshot/ds01" | tee -a "${EXPDIR}/terminal.out" \
&& \ 
python3 scripts/analysis.py -f -g ${EXPDIR} | tee -a "${EXPDIR}/terminal.out"
"""

""" Usage: paib 0 =dotprod_divide_2K=> 2K pods
EXPDIR="experiments/0622/seed233/01-dotprod-divide-random" 
mkdir -p ${EXPDIR} && touch "${EXPDIR}/terminal.out"
python3 scripts/h_generate_config_and_run.py -d "${EXPDIR}" \
-seed 233 \
-e -b \
-f data/cluster_paib-pod_paib_0318_gpu_3000 \
-dotprod 1000 \
-dimext divide \
-gpusel random \
-y "${EXPDIR}/snapshot/ds01" | tee -a "${EXPDIR}/terminal.out" \
&& \ 
python3 scripts/analysis.py -f -g ${EXPDIR} | tee -a "${EXPDIR}/terminal.out"
"""

""" Usage: paib 0 =sim_1K=> 3K pods =desch_10%=> snapshot_01 =sim_1K=> 3K pods =desch_10%=> snapshot_02 =frag_1K=> 3K pods
python3 scripts/h_generate_config_and_run.py -d experiments/exp0516_1 \
-e -b \
-f data/cluster_paib-pod_paib_0318_gpu_1000 \
-sim 1000 \
-r 0.1 -p fragMultiPod \
-y experiments/exp0516_1/snapshot/dr01x1000 \
&& \
python3 scripts/i_inject_origin_workload_into_snapshot.py \
data/cluster_paib-pod_paib_0318_gpu_1000 \
experiments/exp0516_1/snapshot/dr01x1000/PostDeschedule
&& \
python3 scripts/h_generate_config_and_run.py -d experiments/exp0516_1 \
-e -b \
-f experiments/exp0516_1/snapshot/dr01x1000/PostDeschedule \
-sim 1000 \
-r 0.1 -p fragMultiPod \
-y experiments/exp0516_1/snapshot/dr01x2000 \
&& \
python3 scripts/i_inject_origin_workload_into_snapshot.py \
data/cluster_paib-pod_paib_0318_gpu_1000 \
experiments/exp0516_1/snapshot/dr01x2000/PostDeschedule
&& \
python3 scripts/h_generate_config_and_run.py -d experiments/exp0516_1 \
-e -b \
-f experiments/exp0516_1/snapshot/dr01x2000/PostDeschedule \
-frag 1000 \
-r 0.1 -p fragMultiPod \
-y experiments/exp0516_1/snapshot/dr01x3000 \
&& \
python3 scripts/analysis.py experiments/exp0516_1
"""

# Add new scheduler policy here
SCORE_POLICY_ABBR = {
    "GpuFragScore":                   "Frag",
    "GpuFragBellmanScore":            "FragBellman",
    "GpuFragSimScore":                "FragSim",
    "GpuShareFragScore":              "FragShare",
    "GpuShareFragLinearNormScore":    "FragShareLinearNorm",
    "GpuShareFragSimScore":           "FragShareSim",
    "GpuShareFragSimLinearNormScore": "FragShareSimLinearNorm",
    "GpuShareFragSimTuneScore":       "FragShareSimTune",
    "GpuShareFragDotProductScore":    "FragShareDotProd",
    "GpuShareFragBestFitScore":       "FragShareBestFit",
    "GpuShareFragL2NormRatioScore":   "FragShareL2NormRatio",
    "GpuShareFragPackingScore":       "FragSharePacking",
    "CosineSimilarityScore":          "Sim",
    "GpuPackingScore":                "Packing",
    "CosineSimPackingScore":          "SimPacking",
    "GpuPackingSimScore":             "PackingSim",
    "BestFitScore":                   "BestFit",
    "WorstFitScore":                  "WorstFit",
    "FirstFitScore":                  "FirstFit",
    "DotProductScore":                "DotProd",
    "L2NormDiffScore":                "L2Diff",
    "L2NormRatioScore":               "L2Ratio",
    "GandivaScore":                   "Gandiva",
    "SynergyScore":                   "Synergy",
}

SCORE_PLUGINS_WITH_DIM_NORM_GPU_METHOD = [
    "DotProductScore",                # dot product
    "GpuShareFragDotProductScore",    # dot product
    "GpuFragSimScore",                # cosine similarity
    "GpuShareFragSimScore",           # cosine similarity
    "GpuShareFragSimTuneScore"        # cosine similarity
    "GpuShareFragSimLinearNormScore", # cosine similarity
    "CosineSimilarityScore",          # cosine similarity
    "CosineSimPackingScore",          # cosine similarity
    "GpuPackingSimScore",             # cosine similarity
]
SCORE_PLUGINS_WITH_PRE_FILTER = [
    "GpuShareFragSimScore",
    "GpuShareFragSimTuneScore",
    "GpuShareFragSimLinearNormScore",
    "GpuShareFragDotProductScore",
    "GpuShareFragBestFitScore",
    "GpuShareFragL2NormRatioScore",
    "GpuShareFragPackingScore",
    "GpuFragSimScore",
]

def get_args():
    parser = argparse.ArgumentParser(description='generate cluster configuration yaml')
    # exp
    parser.add_argument('-d', '--experiment-dir', type=str, default='./', help='Experiment directory (default: ./)')
    parser.add_argument('-e', '--execute', dest="execute", action="store_true", default=False, help="execute simon binary")
    parser.add_argument('-b', '--block', dest="block", action="store_true", default=False, help="Blocked execution, wait until simon finishes")

    # cluster config
    parser.add_argument('-f', '--custom-config', type=str, help='path to YAML file containing origin cluster node and pod list')
    parser.add_argument('-r', '--deschedule-ratio', type=float, default=0.0, help='deschedule ratio')
    parser.add_argument('-p', '--deschedule-policy', type=str, default=None, help='deschedule policy: fragMultiPod, cosSim, fragOnePod. default: None')
    parser.add_argument('-n', '--new-workload-config', type=str, default=None, help='path to YAML file containing new workload pod list')

    parser.add_argument('-y', '--export-pod-snapshot-yaml-file-prefix', type=str, default=None, help='path to which pod snapshot are exported as yaml file')
    parser.add_argument('-z', '--export-node-snapshot-csv-file-prefix', type=str, default=None, help='path to which node snapshot are exported as csv file')
    parser.add_argument('--is-involved-cpu-pods', type=str, default="true", help='whether to consider CPU pods in typical pods (default: true)')
    parser.add_argument('--pod-popularity-threshold', type=int, default=95, help='pod popularity threshold (default: 95)')
    parser.add_argument('--pod-increase-step', type=int, default=1, help='pod increase step (default: 1)')
    parser.add_argument('--is-consider-gpu-res-weight', type=str, default="false", help='whether to consider GPU resource weight (default: false)')

    parser.add_argument('--cluster-name', type=str, default='simon-paib-config', help='name of the cluster config')
    parser.add_argument('-a', '--applist-path', type=str, default=None, help='path to the app list')
    parser.add_argument('--applist-name', type=str, default=None, help='name of the app list')
    parser.add_argument('--new-node', type=str, default="example/newnode/gpushare")
    parser.add_argument('--shuffle-pod', type=str, default="false", help='whether to shuffle pod. true is to shuffle, false (default) respect the submission order')
    parser.add_argument('--workload-inflation-ratio', type=float, default=1, help='workload inflation ratio, <= 1 takes no effects')
    parser.add_argument('-seed','--workload-inflation-seed', type=int, default=233, help='workload inflation seed')
    parser.add_argument('-tune', '--workload-tuning-ratio', type=float, default=0, help='workload tuning ratio, <= 0 takes no effects')
    parser.add_argument('-tuneseed','--workload-tuning-seed', type=int, default=233, help='workload tuning seed')

    # scheduler config
    for policy_name, policy_abbr in SCORE_POLICY_ABBR.items():
        parser.add_argument("-%s" % policy_abbr, type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-frag", '--gpu-frag-score', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-bellman", '--gpu-frag-score-bellman', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-fragshare", '--gpu-share-frag-score', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-fragsharesim", '--gpu-share-frag-sim-score', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-pack", '--gpu-packing-score', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-packsim", '--gpu-packing-sim-score', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-sim", '--cosine-similarity', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-simpack", '--cosine-sim-packing', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-bestfit", '--best-fit-score', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-worstfit", '--worst-fit-score', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-dotprod", '--dot-prod-score', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-l2diff", '--l2-norm-diff-score', type=int, default=0, help="score (default: 0)")
    # parser.add_argument("-l2ratio", '--l2-norm-ratio-score', type=int, default=0, help="score (default: 0)")

    # scheduler plugin config
    parser.add_argument("-gpusel", "--gpu-sel-method", dest="gpu_sel_method", type=str, default="best", 
                            help="GPU selection method: best, worst, random, default: best")
    parser.add_argument("-dimext", "--dim-ext-method", dest="dim_ext_method", type=str, default="share", 
                            help="Dimension extend method: merge, share, divide, extend, default: share")
    parser.add_argument("-norm", "--norm-method", dest="norm_method", type=str, default="max", 
                            help="Norm method: node, pod, max, default: max")

    # parser.add_argument('-c', '--cluster-config', dest="cluster_config", action="store_true", default=False, help="Generate cluster configuration yaml")
    # parser.add_argument('-s', '--scheduler-config', dest="scheduler_config", action="store_true", default=False, help="Generate scheduler configuration yaml")

    args = parser.parse_args()
    return args

LOGSEP, FILESEP = "-", "_"
TagInitSchedule = "InitSchedule"
TagPostDeschedule = "PostDeschedule"

CLUSTER_CONFIG_TEMPLATE="""
apiVersion: simon/v1alpha1
kind: Config
metadata:
  name: simon-paib-config
spec:
  appList:
  # - name: pai_gpu
  #   path: data/pai_b/application
  cluster:
    customConfig: "data/cluster_paib-origin_workload"
  newNode: example/newnode/gpushare
  customConfig:
    shufflePod: false
    exportConfig:
      # podSnapshotYamlFilePrefix: "snapshot/pod_snapshot"
      # nodeSnapshotCSVFilePrefix: "snapshot/node_snapshot"
    workloadInflationConfig:
      ratio: 1
      seed: 233
    workloadTuningConfig:
      ratio: 0
      seed: 233
    descheduleConfig:
      ratio: 0.1
      # policy: cosSim
      # policy: fragOnePod
      # policy: fragMultiPod
    newWorkloadConfig: # "data/cluster_paib-new_workload"
    typicalPodsConfig:
      isInvolvedCpuPods: true
      podPopularityThreshold: 95
      podIncreaseStep: 1
      isConsideredGpuResWeight: false
"""

def generate_cluster_config(args, outdir):
    template = yaml.safe_load(CLUSTER_CONFIG_TEMPLATE)
    for k, v in template.items():
        if k == "metadata":
            v["name"] = args.cluster_name
        elif k == "spec":
            if args.applist_path is not None:
                applist_name = "pai_gpu" if args.applist_name is None else args.applist_name
                v["appList"] = [dict()]
                v["appList"][0]["name"] = applist_name
                v["appList"][0]["path"] = args.applist_path

            v['cluster']["customConfig"] = args.custom_config
            v['newNode'] = args.new_node

            v['customConfig']['shufflePod'] = True if args.shuffle_pod.lower() == "true" else False
            v['customConfig']['exportConfig'] = {}
            v['customConfig']['exportConfig']['podSnapshotYamlFilePrefix'] = args.export_pod_snapshot_yaml_file_prefix
            v['customConfig']['exportConfig']['nodeSnapshotCSVFilePrefix'] = args.export_node_snapshot_csv_file_prefix
            v['customConfig']['workloadInflationConfig']['ratio'] = args.workload_inflation_ratio
            v['customConfig']['workloadInflationConfig']['seed'] = args.workload_inflation_seed
            v['customConfig']['workloadTuningConfig']['ratio'] = args.workload_tuning_ratio
            v['customConfig']['workloadTuningConfig']['seed'] = args.workload_tuning_seed
            v['customConfig']['descheduleConfig']['ratio'] = args.deschedule_ratio
            if args.deschedule_policy is not None:
                v['customConfig']['descheduleConfig']['policy'] = args.deschedule_policy
            
            if args.new_workload_config is not None:
                v['customConfig']['newWorkloadConfig'] = args.new_workload_config
            v['customConfig']['typicalPodsConfig']['isInvolvedCpuPods'] = True if args.is_involved_cpu_pods.lower() == "true" else False
            v['customConfig']['typicalPodsConfig']['podPopularityThreshold'] = args.pod_popularity_threshold
            v['customConfig']['typicalPodsConfig']['podIncreaseStep'] = args.pod_increase_step
            v['customConfig']['typicalPodsConfig']['isConsideredGpuResWeight'] = True if args.is_consider_gpu_res_weight.lower() == "true" else False
    
    # print(template)
    content = yaml.dump(template)
    md = md5()
    md.update(content.encode())
    filename = "cc" # cluster-config

    def path_str_shorten(str):
        if "pod_paib_0613_" in str and "_gpu2000_no_spec" in str:
            str = str.split("pod_paib_0613_")[1].split("_gpu2000_no_spec")[0]
            return str
  
        str = "" if str is None or len(str) == 0 else str
        str = str.split("/")[-1] if "/" in str else str
        str = str.split("-")[-1] if "-" in str else str
        str = str.split("_")[-1] if "_" in str else str
        str = str.split(".yaml")[0] if ".yaml" in str else str
        str = str[-12:] if len(str) > 12 else str
        return str
    
    filename += FILESEP + "ow%s" % path_str_shorten(args.custom_config) if args.custom_config is not None else "" # original-workload
    filename += FILESEP + "nw%s" % path_str_shorten(args.new_workload_config) if args.new_workload_config is not None else "" # new-workload
    filename += FILESEP + "dr%.1f" % args.deschedule_ratio # deschedule-ratio
    filename += FILESEP + "dp%s" % args.deschedule_policy if args.deschedule_policy is not None else "" # deschedule-policy
    filename += FILESEP + "pe" if args.export_pod_snapshot_yaml_file_prefix is not None else "" # pod-export
    filename += FILESEP + "tn%.1f" % args.workload_tuning_ratio # workload-tuning-ratio
    filename += FILESEP + "ts%d" % args.workload_tuning_seed # workload-tuning-ratio
    filename += FILESEP + "if%.1f" % args.workload_inflation_ratio # workload-inflation-ratio
    filename += FILESEP + "md" + md.hexdigest()[:4] # md5
    filename += ".yaml"
    outfile = outdir / filename
    with open(outfile, "w") as f:
        yaml.dump(template, f)
    return outfile

SCHEDULER_CONFIG_TEMPLATE="""
apiVersion: kubescheduler.config.k8s.io/v1beta1
kind: KubeSchedulerConfiguration
percentageOfNodesToScore: 100
profiles:
  - schedulerName: simon-scheduler
    plugins:
      filter:
        enabled:
          - name: Open-Local
          - name: Open-Gpu-Share
      preFilter:
        disabled:
          - name: GpuShareFragSimScore
          - name: GpuShareFragSimLinearNormScore
          - name: GpuShareFragDotProductScore
          - name: GpuShareFragBestFitScore
          - name: GpuShareFragL2NormRatioScore
          - name: GpuShareFragPackingScore
          - name: GpuFragSimScore
        enabled:
      score:
        disabled:
          - name: GpuFragScore
          - name: GpuFragBellmanScore
          - name: GpuFragSimScore
          - name: GpuShareFragScore
          - name: GpuShareFragLinearNormScore
          - name: GpuShareFragSimScore
          - name: GpuShareFragSimLinearNormScore
          - name: GpuShareFragDotProductScore
          - name: GpuShareFragBestFitScore
          - name: GpuShareFragL2NormRatioScore
          - name: GpuShareFragPackingScore
          - name: CosineSimilarityScore
          - name: GpuPackingScore
          - name: CosineSimPackingScore
          - name: GpuPackingSimScore
          - name: BestFitScore
          - name: WorstFitScore
          - name: FirstFitScore
          - name: DotProductScore
          - name: L2NormDiffScore
          - name: L2NormRatioScore
          - name: GandivaScore
          - name: SynergyScore
          # 
          - name: ImageLocality
          - name: NodeAffinity
          - name: PodTopologySpread
          - name: TaintToleration
          - name: NodeResourcesBalancedAllocation
          - name: InterPodAffinity
          - name: NodeResourcesLeastAllocated
          - name: NodePreferAvoidPods
        enabled:
      reserve:
        enabled:
          - name: Open-Gpu-Share
      bind:
        disabled:
          - name: DefaultBinder
          - name: Open-Local
        enabled:
          - name: Simon
"""

def generate_scheduler_config(args, outdir):
    template = yaml.safe_load(SCHEDULER_CONFIG_TEMPLATE)
    for k, v in template.items():
        if k == "profiles":
            # generate score plugin entries
            s = v[0]['plugins']['score']
            s['enabled'] = []

            for policy_name, policy_abbr in SCORE_POLICY_ABBR.items():
                if args.__dict__.get(policy_abbr, 0) > 0:
                    s['enabled'].append({'name': policy_name, 'weight': args.__dict__.get(policy_abbr, 0)})

                    # for fragsharesim, add prefilter
                    if policy_name in SCORE_PLUGINS_WITH_PRE_FILTER:
                        prefilter = v[0]['plugins']['preFilter']
                        if 'enabled' not in prefilter or type(prefilter['enabled']) != list:
                            prefilter['enabled'] = []
                        prefilter['enabled'].append({'name': policy_name})

            # if args.gpu_frag_score > 0:
            #     s['enabled'].append({'name': "Gpu-Frag-Score", 'weight': args.gpu_frag_score})
            # if args.gpu_frag_score_bellman > 0:
            #     s['enabled'].append({'name': "Gpu-Frag-Score-Bellman", 'weight': args.gpu_frag_score_bellman})
            # if args.gpu_share_frag_score > 0:
            #     s['enabled'].append({'name': "Gpu-Share-Frag-Score", 'weight': args.gpu_share_frag_score})
            # if args.gpu_share_frag_sim_score > 0:
            #     s['enabled'].append({'name': "Gpu-Share-Frag-Sim-Score", 'weight': args.gpu_share_frag_sim_score})
            # if args.gpu_packing_score > 0:
            #     s['enabled'].append({'name': "Gpu-Packing-Score", 'weight': args.gpu_packing_score})
            # if args.gpu_packing_sim_score > 0:
            #     s['enabled'].append({'name': "Gpu-Packing-Sim-Score", 'weight': args.gpu_packing_sim_score})
            # if args.cosine_similarity > 0:
            #     s['enabled'].append({'name': "CosineSimilarityScore", 'weight': args.cosine_similarity})
            # if args.cosine_sim_packing > 0:
            #     s['enabled'].append({'name': "CosineSimPackingScore", 'weight': args.cosine_sim_packing})
            # if args.best_fit_score > 0:
            #     s['enabled'].append({'name': "BestFitScore", 'weight': args.best_fit_score})
            # if args.worst_fit_score > 0:
            #     s['enabled'].append({'name': "WorstFitScore", 'weight': args.worst_fit_score})
            # if args.dot_prod_score > 0:
            #     s['enabled'].append({'name': "DotProductScore", 'weight': args.dot_prod_score})
            # if args.l2_norm_diff_score > 0:
            #     s['enabled'].append({'name': "L2NormDiffScore", 'weight': args.l2_norm_diff_score})
            # if args.l2_norm_ratio_score > 0:
            #     s['enabled'].append({'name': "L2NormRatioScore", 'weight': args.l2_norm_ratio_score})

            # generate pluginConfig via "enabled"
            v[0]['pluginConfig'] = []
            pc = v[0]['pluginConfig']
            ##: select the score with max weight as gpuSelMethod in Open-Gpu-Share's Reserve
            maxScoreName, maxScoreWeight = None, 0
            for item in template['profiles'][0]['plugins']['score']['enabled']:
                if item['weight'] > maxScoreWeight:
                    maxScoreName = item['name']
                    maxScoreWeight = item['weight']
                ###: configure score plugins with the input "dimExtMethod" and "normMethod", works for SCORE_PLUGINS_WITH_DIM_NORM_GPU_METHOD
                pc.append({'name': item['name'], 'args': {'dimExtMethod': args.dim_ext_method, 'normMethod': args.norm_method}})
            if maxScoreName in SCORE_PLUGINS_WITH_DIM_NORM_GPU_METHOD:
                ##: if the max-weight score requires gpuSelMethod and has its own opinions on "dim_ext_method"
                if args.dim_ext_method != "merge":
                    ###: replacing the default gpuSelMethods (best, worst, random) with the implemented score plugins (use score plugin name as key).
                    args.gpu_sel_method = maxScoreName
            ###: normMethod should be set twice: in Open-Gpu-Share and in each score plugins
            pc.append({'name': "Open-Gpu-Share", 'args': {'gpuSelMethod': args.gpu_sel_method, 'dimExtMethod': args.dim_ext_method, 'normMethod':args.norm_method}})

    # print(template)
    content = yaml.dump(template)
    md = md5()
    md.update(content.encode())
    filename = "sc" # scheduler-config
    for item in template['profiles'][0]['plugins']['score']['enabled']:
        filename += FILESEP + SCORE_POLICY_ABBR[item['name']] + str(item['weight'])
    filename += FILESEP + "de%s" % str(args.dim_ext_method) if args.dim_ext_method else ""  # dim-ext-method
    filename += FILESEP + "gs%s" % str(args.gpu_sel_method) if args.gpu_sel_method else ""  # gpu-sel-method
    filename += FILESEP + "md" + md.hexdigest()[:4]
    filename += ".yaml"
    outfile = outdir / filename
    with open(outfile, "w") as f:
      yaml.dump(template, f)
    return outfile

def prepare_snapshot(args):
    if not args.export_pod_snapshot_yaml_file_prefix:
        # print("args.export_pod_snapshot_yaml_file_prefix is None")
        return []
    if not args.custom_config:
        print("args.custom_config is None")
        return []
    node_file = None
    for file in Path(args.custom_config).glob("*node*.yaml"):
        node_file = file
    if not node_file:
        print("no node.yaml file in %s" % args.custom_config)
        return []
    res = []

    tag = TagInitSchedule
    snapshot_dir = "%s/%s/" % (args.export_pod_snapshot_yaml_file_prefix, tag)
    os.makedirs(snapshot_dir, exist_ok=True)
    shutil.copy(node_file, Path(snapshot_dir))
    print("    sI:", snapshot_dir) # snapshot-InitSchedule
    res.append(snapshot_dir)

    if args.deschedule_ratio > 0 and args.deschedule_policy:
        tag = TagPostDeschedule
        snapshot_dir = "%s/%s/" % (args.export_pod_snapshot_yaml_file_prefix, tag)
        os.makedirs(snapshot_dir, exist_ok=True)
        shutil.copy(node_file, Path(snapshot_dir))
        print("    sP:", snapshot_dir) # snapshot-PostDeschedule
        res.append(snapshot_dir)

    return res

def exp(args):
    """
    - home
      - experiment_dir_1
        - cc_1.yaml
        - sc_1.yaml
        - snapshot_1
          - snapshot_1_pod.yaml
        - log-1.log
        - cc_2.yaml
        - sc_2.yaml
        - snapshot_2
          - snapshot_2_pod.yaml
        - log-2.log
        ...
      - experiment_dir_2
        - cc_1.yaml
        - sc_1.yaml
        ...
    """

    expdir = Path(args.experiment_dir)
    os.makedirs(expdir, exist_ok=True)
    print("ExpDir: %s" % expdir)
    cluster_file = None
    scheduler_file = None

    # if args.cluster_config:
    custom_config_path = Path(args.custom_config)
    if not custom_config_path.exists() or not custom_config_path.is_dir():
        exit("[WARNING] --custom-config (-f) path not exist or not a dir: %s" % custom_config_path)
    if len([x for x in custom_config_path.glob("*.yaml")]) == 0:
        exit("[WARNING] --custom-config (-f) path has no yaml file: %s" % custom_config_path)

    cluster_dir = expdir
    cluster_file = generate_cluster_config(args, cluster_dir)
    cluster_file = Path(cluster_file)
    print("    cc: %s" % cluster_file)

    # if args.export_pod_snapshot_yaml_file_prefix:
    snapshots = prepare_snapshot(args)

    # if args.scheduler_config:
    scheduler_dir = expdir
    scheduler_file = generate_scheduler_config(args, scheduler_dir)
    scheduler_file = Path(scheduler_file)
    print("    sc: %s" % scheduler_file)

    log_file = ""
    command = ""
    if cluster_file and scheduler_file:
        log_dir = expdir
        log_file = log_dir / ("log%s%s%s%s.log" % (LOGSEP, cluster_file.name, LOGSEP, scheduler_file.name))
        ## start experiments
        command = './bin/simon apply --extended-resources "gpu" -f %s --default-scheduler-config %s' % (cluster_file, scheduler_file)
        print("    Ex:", command)
        print("    >>:", log_file, "\n")
        if args.execute:
            with open(log_file,"wb") as log, open(log_file,"wb") as log:
                if args.block:
                    subprocess.call(command.split(),stdout=log,stderr=log)  # it blocks. the python will exit but the process remains.
                else:
                    subprocess.Popen(command.split(),stdout=log,stderr=log)  # it is non-block. the python will exit but the process remains.
    else:
        print("  Exit without execution")

    return cluster_file, scheduler_file, log_file, command

    """
    - experiment_dir
      - cluster_config
        - cc_1.yaml
        - cc_2.yaml
      - scheduler_config
        - sc_1.yaml
        - sc_2.yaml
      - logs
        - log_1.log
        - log_2.log
    
    expdir = Path(args.experiment_dir)
    os.makedirs(expdir, exist_ok=True)
    cluster_file = None
    scheduler_file = None
    if args.cluster_config:
        cluster_dir = expdir / "cluster_config"
        os.makedirs(cluster_dir, exist_ok=True)
        cluster_file = generate_cluster_config(args, cluster_dir)
        cluster_file = Path(cluster_file)
        print("Generate cluster config at %s\n" % cluster_file)
    if args.scheduler_config:
        scheduler_dir = expdir / "scheduler_config"
        os.makedirs(scheduler_dir, exist_ok=True)
        scheduler_file = generate_scheduler_config(args, scheduler_dir)
        scheduler_file = Path(scheduler_file)
        print("Generate scheduler config at %s\n" % scheduler_file)

    if cluster_file and scheduler_file and args.execute:
        log_dir = expdir / "logs"
        log_file = "%s%s%s.log" % (cluster_file.name, LOGSEP, scheduler_file.name)
        log_file = log_dir / log_file
        ## start experiments
        command = 'bin/simon apply --extended-resources "gpu" -f %s --default-scheduler-config %s > %s 2>&1 & date' % (cluster_file, scheduler_file, log_file)
        print("Exec:", command)
    else:
        print("Done without execution")
    """

if __name__ == '__main__':
    args = get_args()
    exp(args)
