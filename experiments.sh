# ablation study of popularity threshold
pts=(60 70 80 90 95 100)
for pt in ${pts[@]}; do
    LOGLEVEL=DEBUG bin/simon apply --extended-resources "gpu" --default-scheduler-config config/scheduler-config/frag1000_pack0_sim0.yaml \
        -f config/cluster-config/pai_b-ir10-cpu-pt${pt}-no_deschedule.yaml > logs/pai_b-ir10-cpu-pt${pt}-no_deschedule-frag1000_pack0_sim0.log 2>&1
done

# ablation study of typical pods choice
choices=(cpu gpu gpu-w)
for choice in ${choices[@]}; do
    LOGLEVEL=DEBUG bin/simon apply --extended-resources "gpu" --default-scheduler-config config/scheduler-config/frag1000_pack0_sim0.yaml \
        -f config/cluster-config/pai_b-ir12-${choice}-pt95-no_deschedule.yaml > logs/pai_b-ir12-${choice}-pt95-no_deschedule-frag1000_pack0_sim0.log 2>&1
done

# ablation study of different scheduling strategies
choices=(frag0_pack0_sim1000 frag0_pack300_sim700 frag0_pack500_sim500 frag0_pack700_sim300 frag0_pack1000_sim0)
for choice in ${choices[@]}; do
    LOGLEVEL=DEBUG bin/simon apply --extended-resources "gpu" --default-scheduler-config config/scheduler-config/${choice}.yaml \
        -f config/cluster-config/pai_b-ir12-cpu-pt95-no_deschedule.yaml > logs/pai_b-ir12-cpu-pt95-no_deschedule-${choice}.log 2>&1
done
