This folder contains a list of [clusterloader2](https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/docs/GETTING_STARTED.md) configuration manifests for large scale testing.

## Prerequisites

Follow [clusterloader2 GETTING STARTED](https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/docs/GETTING_STARTED.md) to clone the `perf-tests` repository.

## Execute Tests

Run `clusterloader2` under `perf-tests/clusterloader2` directory:
```bash
go run cmd/clusterloader.go --testconfig=<path to the test yaml> --provider=local --kubeconfig=<path to the hub cluster kubeconfig> --v=2 --enable-exec-service=false
```
We need to set `--enable-exec-service=false` to prevent creating agnostic deployment on the hub cluster as hub
cluster does not allow pod creation.

## Cleanup Resources After Tests

By default, clusterloader2 automatically deletes all the generated namespaces after the tests.
In addition, we also want to delete all the CRPs created. To facilitate cleanup process, run the simple script
provided in this folder:
```
export KUBECONFIG=<your path to the kubeconfig file>
./cleanup.sh
```