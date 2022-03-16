for CONFIG in $(ls example/scheduler-config); do 
  echo ${CONFIG}
  for INFLATION in 100 105 110 115 120 125 130 150 200 250 300; do
    YAML="paib_config_2022_03_13_11_30_06_${INFLATION}.yaml"
    for i in {1..19}; do
      echo "logs/${CONFIG}-${INFLATION}-${i}.log"
      time ./bin/simon_linux apply --extended-resources "gpu" -f "example/paib/2022_03_13_11_30_06/paib_config_2022_03_13_11_30_06_${INFLATION}.yaml" --default-scheduler-config "example/scheduler-config/${CONFIG}" > "logs/${CONFIG}-${INFLATION}-${i}.log" & date
    done
    i=20
    echo "logs/${CONFIG}-${INFLATION}-${i}.log"
    time ./bin/simon_linux apply --extended-resources "gpu" -f "example/paib/2022_03_13_11_30_06/paib_config_2022_03_13_11_30_06_${INFLATION}.yaml" --default-scheduler-config "example/scheduler-config/${CONFIG}" > "logs/${CONFIG}-${INFLATION}-${i}.log"
    sleep 5
  done
done

# <TEST>
#CONFIG="scheduler-config-500x500.yaml"
#INFLATION=300
#i=20
#./bin/simon_linux apply --extended-resources "gpu" -f "example/paib/2022_03_13_11_30_06/paib_config_2022_03_13_11_30_06_${INFLATION}.yaml" --default-scheduler-config "example/scheduler-config/${CONFIG}" > "logs/${CONFIG}-${INFLATION}-${i}.log"
# </TEST>