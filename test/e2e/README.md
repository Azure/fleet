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
 ```shell
make run-e2e
```

4. Check the logs of the hub cluster controller
```shell
kubectl --context=kind-hub-testing -n fleet-system get pod 
```

4. Check the logs of the member cluster controller
```shell
kubectl --context=kind-member-testing -n fleet-system get pod 
```

5. uninstall the resources
```shell
make uninstall-helm
```