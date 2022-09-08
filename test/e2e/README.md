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
kubectl --context=kind-hub-testing apply -f examples/fleet_v1alpha1_membercluster.yaml
kubectl --context=kind-hub-testing apply -f test/integration/manifests/resources
kubectl --context=kind-hub-testing apply -f test/integration/manifests/resources
kubectl --context=kind-hub-testing apply -f test/integration/manifests/placement/select-namespace.yaml
```

4. Check the controller logs 

check the logs of the hub cluster controller
```shell
kubectl --context=kind-hub-testing -n fleet-system get pod 
```

check the logs of the member cluster controller
```shell
kubectl --context=kind-member-testing -n fleet-system get pod 
```

5.uninstall the resources
```shell
make uninstall-helm
```