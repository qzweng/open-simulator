from doctest import script_from_examples
import os
from black import err
import pandas as pd
from pathlib import Path

# LOG_RELATIVE_PATH = 'muchong/logs/logs'
# OUT_CSVNAME = 'analysis_0316.csv'
LOG_RELATIVE_PATH = 'muchong/logs/0318_timestamp'
OUT_CSVNAME = 'analysis_0318_timestamp.csv'
# LOG_RELATIVE_PATH = 'muchong/logs/test'
# OUT_CSVNAME = 'analysis_test.csv'

script_path = Path(os.path.dirname(os.path.realpath(__file__)))
log_path = script_path.parent / LOG_RELATIVE_PATH
out_path = script_path.parent / "scripts" / OUT_CSVNAME
print("Handling logs under:", log_path)

NUM_CLUSTER_ANALYSIS_LINE = 15
out_row_list = []
for log in os.listdir(log_path):
    file = log_path / log
    if file.suffix != '.log':
        print('[INFO] skip file:', file)
        continue
    with open(file, 'r') as f:
        try:
            meta = log.split('-')
            score_weights = meta[2].split('.yaml')[0]
            [pack, frag] = score_weights.split('x')
            meta_dict = {'gpu_pack_score': pack, 'gpu_frag_score': frag, 'inflation': meta[3], 'trial': meta[4].split('.log')[0]}
            print('  Log: %s => %s' % (log, meta_dict))

            fail_dict = {'unscheduled': 0}
            allo_dict = {i : 0 for i in ['MilliCpu','Memory','Gpu','MilliGpu']}
            quad_dict = {i : 0 for i in ["q1_lack_both", 'q2_lack_gpu', 'q3_satisfied', 'q4_lack_cpu', 'xl_satisfied', 'xr_lack_cpu', 'no_access']}
            amnt_dict = {i+'Amount' : 0 for i in ['MilliCpu','Memory','Gpu','MilliGpu']}
            # totl_dict = {i : 0 for i in ['MilliCpu','Memory','Gpu','MilliGpu']}

            counter = 0
            for i, line in enumerate(f.readlines()):
                if 'there are' in line:
                    fail_dict['unscheduled'] = int(line.split("unscheduled pods")[0].split("there are")[1].strip())
                    break
                if 'Cluster Analysis' in line:
                    counter += 1
                if 0 < counter <= NUM_CLUSTER_ANALYSIS_LINE:
                    counter = 0 if counter == NUM_CLUSTER_ANALYSIS_LINE else counter + 1
                    
                    line = line.strip()
                    item = line.split(":")
                    if len(item) <= 1:
                        continue

                    key, value = item[0].strip(), item[1].strip()
                    if key in allo_dict.keys():
                        ratio = float(value.split('%')[0])
                        allo_dict[key] = ratio
                        amount = float(value.split('(')[1].split('/')[0])
                        amnt_dict[key+'Amount'] = amount
                        # total = float(value.split(')')[0].split('/')[1])
                        # totl_dict[key] = total
                    elif key in quad_dict.keys():
                        quad_dict[key] = float(value.split('(')[1].split('%')[0].strip())
            
            out_dict = {}
            out_dict.update(meta_dict)
            out_dict.update(fail_dict)
            out_dict.update(allo_dict)
            out_dict.update(amnt_dict)
            out_dict.update(quad_dict)
            out_row = pd.DataFrame().from_dict(out_dict, orient='index').T
            out_row_list.append(out_row)
        except Exception as e:
            print("[Error] Failed at", file, " with error:", e)

outdf = pd.concat(out_row_list)
outdf.to_csv(out_path, index=False)
