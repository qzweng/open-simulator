import os
import re
import argparse
import matplotlib
import pandas as pd
import seaborn as sns
import matplotlib.pyplot as plt
from pathlib import Path

# LOG_RELATIVE_PATH = 'muchong/logs/logs'
# OUT_CSVNAME = 'analysis_0316.csv'
LOG_RELATIVE_PATH = 'muchong/logs/0425_artifical_cluster_bellman/'
OUT_CSVNAME = 'muchong/results/analysis_0425_artifical_cluster_bellman.csv'
# LOG_RELATIVE_PATH = 'muchong/logs/logs/testing/'
# LOG_RELATIVE_PATH = 'muchong/logs/test'
# OUT_CSVNAME = 'analysis_test.csv'

ALLO_KEYS = ['MilliCpu','Memory','Gpu','MilliGpu']
QUAD_KEYS = ["q1_lack_both", 'q2_lack_gpu', 'q3_satisfied', 'q4_lack_cpu', 'xl_satisfied', 'xr_lack_cpu', 'no_access', "frag_gpu_milli"]

DESCHEDULE_POLICY_LIST = ["cosSim", "fragOnePod", "fragMultiPod"]
DESCHEDULE_POLICY_DICT = {}
for i, v in enumerate(DESCHEDULE_POLICY_LIST):
    DESCHEDULE_POLICY_DICT[i+1] = v
    DESCHEDULE_POLICY_DICT[str(i+1)] = v

def camel_to_snake(name):
    name = re.sub('(.)([A-Z][a-z]+)', r'\1_\2', name)
    return re.sub('([a-z0-9])([A-Z])', r'\1_\2', name).lower()


TAG_LIST = ["InitSchedule", "PostEviction", "PostDeschedule", "ScheduleInflation", "DescheduleInflation"]
TAG_SNAKE_LIST = [camel_to_snake(x) for x in TAG_LIST]
HASTAG_COL = [camel_to_snake(x) for x in ALLO_KEYS]
HASTAG_COL.extend([camel_to_snake(x) for x in [ y + "Amount" for y in ALLO_KEYS]])
HASTAG_COL.extend(QUAD_KEYS)
NONTAG_COL = ['data_date','inflation','deschedule_ratio','deschedule_policy','snapshot_sc','gpu_pack_score','gpu_frag_score','pack_x_frag','trial','unscheduled','origin_pods']
NONTAG_COL.extend([camel_to_snake(x) for x in [y+"Total" for y in ALLO_KEYS]])

def move_tag_to_new_column(df, tag_list=TAG_SNAKE_LIST):
    meta_col = []
    data_col = []
    for col in df.columns:
        is_data_col = False
        for tag in tag_list:
            if col.endswith("_" + tag):
                data_col.append(col)
                is_data_col = True
                break
        if is_data_col == False:
            meta_col.append(col)
    # print(meta_col)
    # print(data_col)
    
    out_row_list = []
    for _, row in df.iterrows():
        orig_dict = dict(row)
        meta_dict = {}
        for col in meta_col:
        # for col in NONTAG_COL:
            if col in orig_dict:
                meta_dict[col] = orig_dict[col]
        # print("meta_dict:", meta_dict)

        for tag in tag_list:
            data_dict = {}
            data_dict.update(meta_dict)
            data_dict['tag'] = tag
            found = 0
            for col in data_col:
                if col.endswith("_" + tag):
                    key = col[:-(len(tag)+1)]
                    # print(tag, '+', key,'=',col)
                    data_dict[key] = orig_dict.get(col)
                    found = 1
            if found == 1:
                # print("data_dict:", data_dict)
                data_row = pd.DataFrame().from_dict(data_dict, orient='index').T
                out_row_list.append(data_row)
    return pd.concat(out_row_list)

def fillna_columns_with_tag(df):
    for x in ['milli_cpu', 'memory', 'gpu', 'milli_gpu', 'milli_cpu_amount', 'memory_amount', 'gpu_amount', 'milli_gpu_amount', 'q1_lack_both', 'q2_lack_gpu', 'q3_satisfied', 'q4_lack_cpu', 'xl_satisfied', 'xr_lack_cpu', 'no_access', 'frag_gpu_milli']:
        # df.loc[(df['workload']=='ShareGpu80')&(df['num_gpu']==4500), x+"_schedule_inflation"] = \
        # df.loc[(df['workload']=='ShareGpu80')&(df['num_gpu']==4500), x+"_init_schedule"]
        df.loc[df.isnull().any(axis=1), x+"_schedule_inflation"] = \
        df.loc[df.isnull().any(axis=1), x+"_init_schedule"]
    return df

def log_to_csv(log_relative_path, out_csvname):
    script_path = Path(os.path.dirname(os.path.realpath(__file__)))
    log_path = script_path.parent / log_relative_path
    out_path = script_path.parent / out_csvname
    out_frag_csvname = out_csvname[:-4] + '_frag.csv'
    out_frag_path = script_path.parent / out_frag_csvname
    print("Handling logs under:", log_path)
    
    NUM_CLUSTER_ANALYSIS_LINE = 16
    out_row_list = []
    out_frag_col_dict = {}
    log_file_counter = 0
    for log in os.listdir(log_path):
        file = log_path / log
        if file.suffix != '.log':
            print('[INFO] skip file:', file)
            continue
        with open(file, 'r') as f:
            try:
                meta_dict = {}
                meta = log.split('-')
                # e.g., paib_ShareGpu60_gpu1500_seed235.yaml-pure_sim1000.yaml.log
                cconfig, sconfig = meta[0].split('.yaml')[0], meta[1].split('.yaml')[0]
                cconfigs = cconfig.split('_')
                meta_dict['base'] = cconfigs[0] # paib
                meta_dict['workload'] = cconfigs[1] # ShareGpu60
                meta_dict['num_gpu'] = cconfigs[2].split('gpu')[1] # gpu1500 -> 1500
                meta_dict['seed'] = int(cconfigs[3].split('seed')[1]) # seed235 -> 235
                meta_dict['policy'] = sconfig.split('pure_')[1].split('1000')[0]

                ## e.g., paib_dpfragMultiPod_dr5_seed234_pod2000ns.yaml-pure_bestfit1000.yaml.log
                """
                cconfig, sconfig = meta[0].split('.yaml')[0], meta[1].split('.yaml')[0]
                cconfigs = cconfig.split('_')
                meta_dict['new_workload'] = cconfigs[0]
                meta_dict['deschedule_policy'] = cconfigs[1].split('dp')[1]
                meta_dict['deschedule_ratio'] = round(int(cconfigs[2].split('dr')[1]) / 10, 1)
                meta_dict['seed'] = int(cconfigs[3].split('seed')[1])
                meta_dict['num_paib_pod'] = int(cconfigs[4].split('pod')[1].split('ns')[0])
                meta_dict['policy'] = sconfig.split('pure_')[1].split('1000')[0]
                """

                ## e.g., experiments_235_mit.yaml-frag0_pack700_sim300.yaml.log
                """
                meta_dict['seed'] = meta[0].split('_')[1] # 235
                meta_dict['pod_dist'] = meta[0].split('_')[2].split('.yaml')[0] # mit
                frag_str, pack_str, sim_str = meta[1].split('.')[0].split('_')
                meta_dict['frag'] = frag_str.split('frag')[1]
                meta_dict['pack'] = pack_str.split('pack')[1]
                meta_dict['sim'] = sim_str.split('sim')[1]
                """

                ## e.g,. paib-2022_03_18_11_36_45-ir11-dr01-dp1-0x1000-2.log
                """
                _, data_date = meta[0], meta[1]
                meta_dict = {'data_date': data_date}
                for item in meta[2:]:
                    if 'ir' in item:
                        inflation = item # ir15
                        inflation = int(inflation.split('ir')[1]) * 10 # 150
                        meta_dict['inflation'] = inflation
                    elif 'dr' in item:                
                        deschedule_ratio = item # dr01
                        deschedule_ratio = int(deschedule_ratio.split('dr')[1]) * 10 # 10
                        meta_dict['deschedule_ratio'] = deschedule_ratio
                    elif 'dp' in item:
                        deschedule_policy = item # dp1
                        deschedule_policy = deschedule_policy.split('dp')[1] # 1
                        deschedule_policy = DESCHEDULE_POLICY_DICT.get(deschedule_policy, deschedule_policy) # cosSim
                        meta_dict['deschedule_policy'] = deschedule_policy
                    elif 'ss' in item and 'x' in item:
                        snapshot_sc = item # ss900x100
                        snapshot_sc = snapshot_sc.split('ss')[1]  # 900x100
                        meta_dict['snapshot_sc'] = snapshot_sc
                    elif 'x' in item:
                        score_weights = item # 900x100
                        [pack, frag] = score_weights.split('x') # 900,100
                        meta_dict['pack_x_frag'] = score_weights
                        meta_dict['pack'], meta_dict['frag'] = pack, frag
                    elif '.log' in item:
                        trial = item.split('.log')[0] # 1.log
                        meta_dict['trial'] = trial
                """

                log_file_counter += 1
                print('[%4d] %s => %s' % (log_file_counter, log, meta_dict))

                fail_dict = {'unscheduled': 0}
                allo_dict = {}
                quad_dict = {}
                amnt_dict = {}
                totl_dict = {}
                frag_list_dict = {}

                counter = 0
                tag = ""
                for i, line in enumerate(f.readlines()):
                    INFOMSG="level=info msg="
                    if INFOMSG not in line:
                        continue
                    line = line.split(INFOMSG)[1]
                    line = line[1:-2] # get rid of " and \n"

                    if "Number of original workload pods" in line:
                        fail_dict['origin_pods'] = int(line.split(":")[1].strip())

                    if 'there are' in line:
                        fail_dict['unscheduled'] = int(line.split("unscheduled pods")[0].split("there are")[1].strip())
                        break

                    if 'Cluster Analysis' in line:
                        tag = line.split(')')[0].split('(')[1]
                        counter += 1
                    if 0 < counter <= NUM_CLUSTER_ANALYSIS_LINE:
                        counter = 0 if counter == NUM_CLUSTER_ANALYSIS_LINE else counter + 1                        
                        
                        line = line.strip()
                        item = line.split(":")
                        if len(item) <= 1:
                            continue

                        key, value = item[0].strip(), item[1].strip()
                        if key in ALLO_KEYS:
                            ratio = float(value.split('%')[0])
                            allo_dict[camel_to_snake(key+tag)] = ratio
                            amount = float(value.split('(')[1].split('/')[0])
                            amnt_dict[camel_to_snake(key+'Amount'+tag)] = amount

                            total = float(value.split(')')[0].split('/')[1])
                            totl_dict[camel_to_snake(key+'Total')] = total # update without tag
                        elif key in QUAD_KEYS:
                            quad_dict[camel_to_snake(key+tag)] = float(value.split('(')[1].split('%')[0].strip())
                
                    # out_frag_col_dict
                    if line.startswith("[Report]"): # e.g., "[Report] Frag amount: 37541.99 (origin)"
                        frag, remark = float(line.split()[3]), line.split()[-1]
                        remark = remark.split(')')[0].split('(')[1] # get rid of '(' and ')'
                        if remark not in frag_list_dict:
                            frag_list_dict[remark] = [frag]
                        else:
                            frag_list_dict[remark].append(frag)

                out_dict = {}
                out_dict.update(meta_dict)
                out_dict.update(fail_dict)
                out_dict.update(allo_dict)
                out_dict.update(amnt_dict)
                out_dict.update(quad_dict)
                out_dict.update(totl_dict)
                out_row = pd.DataFrame().from_dict(out_dict, orient='index').T
                out_row_list.append(out_row)

                meta_as_key = "-".join(["%s_%s" % (k, v) for k, v in meta_dict.items()])
                for k, v in frag_list_dict.items():
                    out_frag_col_dict[meta_as_key+"-"+k] = v
            except Exception as e:
                print("[Error] Failed at", file, " with error:", e)

    outdf = pd.concat(out_row_list)
    outdf.to_csv(out_path, index=False)
    if len(out_frag_col_dict) > 0:
        pd.DataFrame().from_dict(out_frag_col_dict, orient='index').T.to_csv(out_frag_path, index=None) 
        print("Export frag report at:", out_frag_path)

def analysis_table(dfn):
    SEED=233
    NEW_WORKLOAD="mit"
    DR=0.1
    NUM_PAIB_POD=5000

    dfnp = dfn.query('seed==%d'%SEED
            ).query('new_workload=="%s"'%NEW_WORKLOAD
            ).query('deschedule_ratio==%s'%DR
            ).query('tag=="deschedule_inflation" or tag=="init_schedule"'
            )

    print('deschedule_inflation')
    display(dfn.query('seed==%d'%SEED
            ).query('tag=="deschedule_inflation"'
            ).query('new_workload=="%s"'%NEW_WORKLOAD
            ).query('deschedule_ratio==%s'%DR
            ).query('num_paib_pod==%d'%NUM_PAIB_POD
            ).sort_values(['milli_gpu','gpu','milli_cpu'], ascending=False
            ).drop(columns=['memory','memory_total','tag','unscheduled','milli_cpu_total','gpu_total','milli_gpu_total','origin_pods','memory_amount','gpu_amount','milli_cpu_amount','milli_gpu_amount']))

    print('\nschedule_inflation')
    display(dfn.query('seed==%d'%SEED
            ).query('tag=="schedule_inflation"'
            ).query('new_workload=="%s"'%NEW_WORKLOAD
            ).query('deschedule_ratio==%s'%DR
            ).query('num_paib_pod==%d'%NUM_PAIB_POD
            ).sort_values(['milli_gpu','gpu','milli_cpu'], ascending=False
            ).drop(columns=['memory','memory_total','tag','unscheduled','milli_cpu_total','gpu_total','milli_gpu_total','origin_pods','memory_amount','gpu_amount','milli_cpu_amount','milli_gpu_amount']))

def analysis_figure_schedule(dfn):
    TAG="schedule_inflation"
    RATIO=80
    print(TAG, RATIO)
    matplotlib.rcdefaults()
    matplotlib.rcParams['lines.markersize'] = 12 # 6
    for WORKLOAD in ["EightGpu%d" % RATIO, "FourGpu%d" % RATIO, "TwoGpu%d" % RATIO, "OneGpu%d" % RATIO, "ShareGpu%d" % RATIO]:
        SEED=233
        POLICY_LIST=['frag', 'bellman', 'bestfit', 'sim', 'pack', 'worstfit']

        dfnp = dfn.query('seed==%d'%SEED
                ).query('workload=="%s"'%WORKLOAD
                ).query('tag=="%s"'%TAG
                )
        
        plt.figure(figsize=(8, 4), dpi=120)
        sns.scatterplot(data=dfnp, x='num_gpu', y='milli_gpu', alpha=0.8,
                        hue="policy", hue_order=POLICY_LIST, 
                        style="policy", style_order=POLICY_LIST)
        
        title_str = "Pods: %s, Seed: %d" % (WORKLOAD, SEED)
        plt.title(title_str)
        plt.grid(linestyle='-.', alpha=0.8)
        plt.legend(ncol=1)
        # plt.ylim(0, 100)

    matplotlib.rcdefaults()

def analysis_figure_deschedule(dfn):
    SEED=233
    NEW_WORKLOAD="mit"
    DR=0.1
    POLICY_LIST=['frag', 'bestfit' , 'pack', 'sim']

    TAG="deschedule_inflation"
    dfnp = dfn.query('seed==%d'%SEED
            ).query('new_workload=="%s"'%NEW_WORKLOAD
            ).query('deschedule_ratio==%s'%DR
            ).query('tag=="%s"'%TAG
            )
    plt.figure(figsize=(8, 4), dpi=120)
    sns.scatterplot(data=dfnp, x='origin_pods', y='milli_gpu', hue="policy", hue_order=POLICY_LIST, style='deschedule_policy', size='tag', sizes=(100, 50), alpha=0.6)
    title_str = "%s: New Pods: %s, DR: %s, Seed: %d" % (TAG, NEW_WORKLOAD, DR, SEED)
    plt.title(title_str)
    plt.grid(linestyle='-.', alpha=0.8)


    TAG="schedule_inflation"
    dfnp = dfn.query('seed==%d'%SEED
            ).query('new_workload=="%s"'%NEW_WORKLOAD
            ).query('deschedule_ratio==%s'%DR
            ).query('tag=="%s"'%TAG
            )
    plt.figure(figsize=(8, 4), dpi=120)
    sns.scatterplot(data=dfnp, x='origin_pods', y='milli_gpu', hue="policy", hue_order=POLICY_LIST, style='deschedule_policy', size='tag', sizes=(100, 50), alpha=0.6)
    title_str = "%s: New Pods: %s, DR: %s, Seed: %d" % (TAG, NEW_WORKLOAD, DR, SEED)
    plt.title(title_str)
    plt.grid(linestyle='-.', alpha=0.8)

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="add csv input")
    parser.add_argument("logfile", type=str, help="input log file", default=LOG_RELATIVE_PATH)
    parser.add_argument("outfile", type=str, help="output csv file", default=OUT_CSVNAME)
    args = parser.parse_args()
    print("In: ", args.logfile, "\nOut:", args.outfile)
    log_to_csv(args.logfile, args.outfile)
