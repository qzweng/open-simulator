#!/bin/bash
CLUSTER=$1
# CLUSTER="dsw"
# on mac
sed -i '' "s/paib-node/${CLUSTER}-node/g" "${CLUSTER}_node_list.yaml"
sed -i '' "s/paib-pod/${CLUSTER}-pod/g" "${CLUSTER}_pod_list.yaml"
sed -i '' "s/paib-gpu/${CLUSTER}-gpu/g" "${CLUSTER}_pod_list.yaml"

# 0418: mac seed, node-selector
cd $OSIM/config/cluster-config/0418_templates
TARGET_DIR="../0418_snapshot_2345k_pure_bestfit"
for dpolicy in fragMultiPod cosSim; do
    for dratio in 0 1 3 5; do
        for pod in 2000 3000 4000 5000; do
            for seed in 233 234; do
                cat "dsw.yaml"   | sed "s/# policy: ${dpolicy}/policy: ${dpolicy}/g" \
                                 | sed "s/ratio: 0.1/ratio: 0.${dratio}/g" \
                                 | sed "s/0318_2000_ns/0318_${pod}_ns/g" \
                                 | sed "s/seed: 233/seed: ${seed}/g" \
                                 > "${TARGET_DIR}/dsw_dp${dpolicy}_dr${dratio}_seed${seed}_pod${pod}ns.yaml"
                cat "mit.yaml"   | sed "s/# policy: ${dpolicy}/policy: ${dpolicy}/g" \
                                 | sed "s/ratio: 0.1/ratio: 0.${dratio}/g" \
                                 | sed "s/0318_2000_ns/0318_${pod}_ns/g" \
                                 | sed "s/seed: 233/seed: ${seed}/g" \
                                 > "${TARGET_DIR}/mit_dp${dpolicy}_dr${dratio}_seed${seed}_pod${pod}ns.yaml"
                cat "mvap.yaml"  | sed "s/# policy: ${dpolicy}/policy: ${dpolicy}/g" \
                                 | sed "s/ratio: 0.1/ratio: 0.${dratio}/g" \
                                 | sed "s/0318_2000_ns/0318_${pod}_ns/g" \
                                 | sed "s/seed: 233/seed: ${seed}/g" \
                                 > "${TARGET_DIR}/mvap_dp${dpolicy}_dr${dratio}_seed${seed}_pod${pod}ns.yaml"
                cat "na61.yaml"  | sed "s/# policy: ${dpolicy}/policy: ${dpolicy}/g" \
                                 | sed "s/ratio: 0.1/ratio: 0.${dratio}/g" \
                                 | sed "s/0318_2000_ns/0318_${pod}_ns/g" \
                                 | sed "s/seed: 233/seed: ${seed}/g" \
                                 > "${TARGET_DIR}/na61_dp${dpolicy}_dr${dratio}_seed${seed}_pod${pod}ns.yaml"
                cat "na630.yaml" | sed "s/# policy: ${dpolicy}/policy: ${dpolicy}/g" \
                                 | sed "s/ratio: 0.1/ratio: 0.${dratio}/g" \
                                 | sed "s/0318_2000_ns/0318_${pod}_ns/g" \
                                 | sed "s/seed: 233/seed: ${seed}/g" \
                                 > "${TARGET_DIR}/na630_dp${dpolicy}_dr${dratio}_seed${seed}_pod${pod}ns.yaml"
                cat "paib.yaml"  | sed "s/# policy: ${dpolicy}/policy: ${dpolicy}/g" \
                                 | sed "s/ratio: 0.1/ratio: 0.${dratio}/g" \
                                 | sed "s/0318_2000_ns/0318_${pod}_ns/g" \
                                 | sed "s/seed: 233/seed: ${seed}/g" \
                                 > "${TARGET_DIR}/paib_dp${dpolicy}_dr${dratio}_seed${seed}_pod${pod}ns.yaml"
            done
        done
    done
done

# 0417: mac cat
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

