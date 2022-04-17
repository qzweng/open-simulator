#!/bin/bash
CLUSTER=$1
# CLUSTER="dsw"
sed -i '' "s/paib-node/${CLUSTER}-node/g" "${CLUSTER}_node_list.yaml"
sed -i '' "s/paib-pod/${CLUSTER}-pod/g" "${CLUSTER}_pod_list.yaml"
sed -i '' "s/paib-gpu/${CLUSTER}-gpu/g" "${CLUSTER}_pod_list.yaml"