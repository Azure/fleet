Here is how to run the load test locally. Make sure that you have installed go and git clone the repo.

## Prerequisites
1. Build a fleet. You can use any Kubernetes clusters you have and install the Fleet agents on those clusters, or follow the 
[Quickstart guide on how to build a Fleet using Azure Kubernetes Fleet Manager](https://learn.microsoft.com/en-us/azure/kubernetes-fleet/quickstart-create-fleet-and-members).

2. Ensure that you have access to the Fleet and hub cluster. If using Azure Kubernetes Fleet Manager, follow the 
[Quickstart guide on how to access the Kubernetes API of the Fleet Resource](https://learn.microsoft.com/en-us/azure/kubernetes-fleet/quickstart-create-fleet-and-members).

3. Please remember to save the kubeconfig file pointing to the hub cluster of the Fleet.
    ```
    export KUBECONFIG=xxxxx
    ```

## Running Load Test
 The load test will run locally on your machine .This performance load test concurrently places ClusterResourcePlacements 
 and deletes them. The latency and count of applied ClusterResourcePlacement is taken.

### (Optional) Set Up Resources:
Create any resources you would like to place and/or a crp file to use.
> **_NOTE:_**  place crp manifest file within `fleet/hack/loadtest/` directory in order for the test to read it correctly; or update one of the current test crp files.


### Parameters that can be defined:
- `crp-file`: ClusterResourcePlacement yaml used to specify policy. Default value is `test-crp.yaml`.
>  **_NOTE:_** Default crp file `test-crp.yaml` will use all default crp values. All test crp files use the following as the resource selector:
>
>     group: apiextensions.k8s.io
>     kind: CustomResourceDefinition
>     name: testresources.test.kubernetes-fleet.io
>     version: v1

- `max-current-placement`: The number of current placement load. Default value is `20`.
- `load-test-length-minute`: The length of the load test in minutes. Default value is `30`.
- `placement-deadline-second`: The deadline for a placement to be applied (in seconds). Default value is `300`.
- `poll-interval-millisecond`: The poll interval for verification (in milli-second). Default value is `250`.
-  `use-test-resources`: Boolean to include all test resources in the test. Default value is `false`.
>  **_NOTE:_** If this option is true, the test will create resources and add them to the crp for them to be placed. The following resources are added:
> `Namespace` that contains all the resources, `PodDisruptionBudget`, 2 `ConfigMap`'s, `Secret`, `Service`, `TestResource`, `Role`, and `RoleBinding`.
>  If this option is false, the test will only use the resources specified in the crp file.

### Run the Load Test:
```shell
go run hack/loadtest/main.go -max-current-placement 20 -crp-file test-crp.yaml -load-test-length-minute 30
```

### Check the metrics:
To manually check Prometheus metrics visit [Load Test Metrics](http://localhost:4848/metrics), or run:
```shell
curl http://localhost:4848/metrics | grep quantile_apply_crp_latency
```

Use a local prometheus to draw graphs. Download prometheus binary for your local machine. Start the prometheus. 
```shell
./prometheus --config.file=hack/loadtest/prometheus.yml 
```
