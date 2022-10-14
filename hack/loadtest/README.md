Here is how to run the load test locally. Make sure that you have installed go and git clone the repo

1. Build a fleet.
  
You can use any Kubernetes clusters you have and install the fleet agents on those clusters. In this example, we built a fleet with four member clusters, namely, cluster-1 to cluster-4. 
Please remember to save the kubeconfig file pointing to the hub cluster of the fleet. 

3. Run the load test binary locally.
```shell
export KUBECONFIG=xxxxx
go run hack/loadtest/main.go -max-current-placement 10 --cluster cluster-1 --cluster cluster-2 --cluster cluster-3 --cluster cluster-4 
```

3. Manually check the metrics against the load test.
```shell
curl http://localhost:4848/metrics | grep workload 
```

4. Use a local prometheus to draw graphs. Download prometheus binary for your local machine. Start the prometheus. 
```shell
./prometheus --config.file=hack/loadtest/prometheus.yml 
```
