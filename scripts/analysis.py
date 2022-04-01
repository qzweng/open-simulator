import os
import re
import pandas as pd
from pathlib import Path

# LOG_RELATIVE_PATH = 'muchong/logs/logs'
# OUT_CSVNAME = 'analysis_0316.csv'
LOG_RELATIVE_PATH = 'muchong/logs/logs/0331_gopanic_run'
OUT_CSVNAME = 'muchong/results/analysis_0331_gopanic.csv'
# LOG_RELATIVE_PATH = 'muchong/logs/test'
# OUT_CSVNAME = 'analysis_test.csv'

script_path = Path(os.path.dirname(os.path.realpath(__file__)))
log_path = script_path.parent / LOG_RELATIVE_PATH
out_path = script_path.parent / OUT_CSVNAME
print("Handling logs under:", log_path)

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
NONTAG_COL = ['data_date','inflation','deschedule_ratio','deschedule_policy','gpu_pack_score','trial','unscheduled']
NONTAG_COL.extend([camel_to_snake(x) for x in [y+"Total" for y in ALLO_KEYS]])

def move_tag_to_new_column(df):
    out_row_list = []
    for _, row in df.iterrows():
        orig_dict = dict(row)
        meta_dict = {}
        for col in NONTAG_COL:
            meta_dict[col] = orig_dict.get(col)
        # print("meta_dict:", meta_dict)

        data_dict = {}
        for tag in TAG_SNAKE_LIST:
            data_dict.update(meta_dict)
            data_dict['tag'] = tag
            for col in HASTAG_COL:
                key = col + "_" + tag
                # print("key:", key)
                data_dict[col] = orig_dict.get(key)
            # print("data_dict:", data_dict)
            data_row = pd.DataFrame().from_dict(data_dict, orient='index').T
            out_row_list.append(data_row)
    return pd.concat(out_row_list)

def log_to_csv():
    NUM_CLUSTER_ANALYSIS_LINE = 16
    out_row_list = []
    for log in os.listdir(log_path):
        file = log_path / log
        if file.suffix != '.log':
            print('[INFO] skip file:', file)
            continue
        with open(file, 'r') as f:
            try:
                meta = log.split('-')

                inflation = meta[2] # ir15
                inflation = int(inflation.split('ir')[1]) * 10 # 150
                
                deschedule_ratio = meta[3] # dr01
                deschedule_ratio = int(deschedule_ratio.split('dr')[1]) * 10 # 10

                deschedule_policy = meta[4] # dp1
                deschedule_policy = deschedule_policy.split('dp')[1] # 1
                deschedule_policy = DESCHEDULE_POLICY_DICT.get(deschedule_policy, deschedule_policy) # cosSim

                score_weights = meta[5].split('.yaml')[0]
                [pack, frag] = score_weights.split('x')
                
                meta_dict = {'data_date': meta[1],
                            'inflation': inflation, # meta[2]
                            'deschedule_ratio': deschedule_ratio, # meta[3]
                            'deschedule_policy': deschedule_policy, # meta[4]
                            'gpu_pack_score': pack, 'gpu_frag_score': frag, # meta[5]
                            'trial': meta[6].split('.log')[0]}
                print('  Log: %s => %s' % (log, meta_dict))

                fail_dict = {'unscheduled': 0}
                allo_dict = {}
                quad_dict = {}
                amnt_dict = {}
                totl_dict = {}

                counter = 0
                tag = ""
                for i, line in enumerate(f.readlines()):
                    INFOMSG="level=info msg="
                    if INFOMSG not in line:
                        continue
                    line = line.split(INFOMSG)[1]
                    line = line[1:-2] # get rid of " and \n"

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
                
                out_dict = {}
                out_dict.update(meta_dict)
                out_dict.update(fail_dict)
                out_dict.update(allo_dict)
                out_dict.update(amnt_dict)
                out_dict.update(quad_dict)
                out_dict.update(totl_dict)
                out_row = pd.DataFrame().from_dict(out_dict, orient='index').T
                out_row_list.append(out_row)
            except Exception as e:
                print("[Error] Failed at", file, " with error:", e)

    outdf = pd.concat(out_row_list)
    outdf.to_csv(out_path, index=False)

if __name__ == "__main__":
    log_to_csv()
