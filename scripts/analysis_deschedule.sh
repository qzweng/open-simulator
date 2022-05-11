#!/bin/bash
# 2022.05.06
LOGDIR="logs/0505_paib_snapshot"
cd $LOGDIR
for item in ls *best*.log; do
    echo $item
    cat $item | grep "escheduled" # captures "descheduled" and "Descheduled"
    echo ""
done