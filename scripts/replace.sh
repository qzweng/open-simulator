#!/bin/bash
CLUSTER=$1
# CLUSTER="dsw"
# on mac
sed -i '' "s/paib-node/${CLUSTER}-node/g" "${CLUSTER}_node_list.yaml"
sed -i '' "s/paib-pod/${CLUSTER}-pod/g" "${CLUSTER}_pod_list.yaml"
sed -i '' "s/paib-gpu/${CLUSTER}-gpu/g" "${CLUSTER}_pod_list.yaml"

# mac cat
for i in 4 5 6 7; do
    cat experiments_233_dsw.yaml | sed "s/seed: 233/seed: 23${i}/g" > "experiments_23${i}_dsw.yaml"
    cat experiments_233_mit.yaml | sed "s/seed: 233/seed: 23${i}/g" > "experiments_23${i}_mit.yaml"
    cat experiments_233_mvap.yaml | sed "s/seed: 233/seed: 23${i}/g" > "experiments_23${i}_mvap.yaml"
    cat experiments_233_na61.yaml | sed "s/seed: 233/seed: 23${i}/g" > "experiments_23${i}_na61.yaml"
    cat experiments_233_na630.yaml | sed "s/seed: 233/seed: 23${i}/g" > "experiments_23${i}_na630.yaml"
    cat experiments_233_paib.yaml | sed "s/seed: 233/seed: 23${i}/g" > "experiments_23${i}_paib.yaml"
done

# on linux
sed -i 's/cluster_paib-pod_paib_0318_4000/cluster_mit-pod_mit/g' "experiments_233.yaml"
ORIGIN=cluster_mit-pod_mit
TARGET=cluster_paib-pod_paib_0318_4000
sed -i "s#${ORIGIN}#${TARGET}#g" "experiments_233.yaml" # WRONG!

