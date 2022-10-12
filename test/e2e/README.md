Here is how to run e2e locally. Make sure that you have installed Docker and Kind.

1. Build the docker images
```shell
export KUBECONFIG=~/.kube/config
OUTPUT_TYPE=type=docker make docker-build-member-agent docker-build-hub-agent docker-build-refresh-token
```

2. Create the kind clusters and install the helm.
```shell
make creat-kind-cluster
```
or 
```shell
make create-hub-kind-cluster
make create-member-kind-cluster
make install-helm
```

3. Run the test

run the e2e test suite
 ```shell
make run-e2e
```
or test manually
```shell
kubectl --context=kind-hub-testing delete ns local-path-storage
kubectl --context=kind-hub-testing apply -f examples/fleet_v1alpha1_membercluster.yaml
kubectl --context=kind-hub-testing apply -f test/integration/manifests/resources
kubectl --context=kind-hub-testing apply -f test/integration/manifests/resources
kubectl --context=kind-hub-testing apply -f test/integration/manifests/placement/select-namespace.yaml
```

4. Check the controller logs 

check the logs of the hub cluster controller
```shell
kubectl --context=kind-hub-testing -n fleet-system get pod 

NAME                       READY   STATUS    RESTARTS   AGE
hub-agent-8bb6d658-6jj7n   1/1     Running   0          11m

```

check the logs of the member cluster controller
```shell
kubectl --context=kind-member-testing -n fleet-system get pod 
```

5.  check the hub metrics
```shell
kubectl --context=kind-hub-testing -n fleet-system  port-forward hub-agent-8bb6d658-6jj7n 13622:8080

Forwarding from 127.0.0.1:13622 -> 8080
Forwarding from [::1]:13622 -> 8080

curl http://127.0.0.1:13622/metrics
```

Use a local prometheus to draw graphs. Download prometheus binary for your local machine. Start the prometheus.
```shell
prometheus --config.file=test/e2e/prometheus.yml 
```

6.uninstall the resources
```shell
make uninstall-helm
make clean-e2e-tests
```