# Fleet E2E Tests (API version v1alpha1)

This directory includes the E2E test suites for Fleet agents (v1beta1 API). To run the 
test suites, follow the steps below:

1. Install [Docker](https://www.docker.com) and [Kind](https://kind.sigs.k8s.io/).

2. Change to project root directory.

3. Build the agent images:

    ```sh
    export KUBECONFIG=~/.kube/config
    export OUTPUT_TYPE=type=docker
    make docker-build-member-agent docker-build-hub-agent docker-build-refresh-token
    ```

4. Provision the `Kind` clusters:

    ```sh
    # This is equivalent of running the following three make targets: 
    # create-hub-kind-cluster, create-member-kind-cluster, and install-helm
    make create-kind-cluster
    ```

    Please note that the version of the kind binary installed locally can have very specific
    requirements on the Kubernetes image version to use; if the clusters fail to start,
    consider using a supported pre-built image instead. You can checkout the images [here](https://github.com/kubernetes-sigs/kind/releases).

5. Run the tests:

    ```shell
    make run-e2e-v1alpha1
    ```

## Access the `Kind` clusters

To access the `Kind` clusters, after the test environment is set up using `setup.sh`, switch
between the following contexts using the command `kubectl config use-context`:

* `kind-hub-testing` for accessing the hub cluster
* `kind-member-testing` for accessing the bravelion cluster

Fleet agents run in the namespace `fleet-system`; to retrieve their logs, switch to a `Kind`
cluster, and run the following steps:

1. Find out the name of the pod with the command:

    ```sh
    kubectl get pods -n fleet-system
    ```

    You should see the agent pod in the output list; write down the name of the pod.

2. Retrieve the logs:

    ```sh
    # Replace YOUR-POD-NAME with a value of your own.
    kubectl logs YOUR-POD-NAME -n fleet-system
    ```

To retrieve the metrics on the hub cluster, switch to the `kind-hub-testing` context, and run
the command `kubectl -n fleet-system port-forward YOUR-AGENT-POD-NAME 13622:8080`. This will
set up a port forwarding between the agent port `8080` and the local port `13622`. You can then
use the Prometheus binary to connect to the local port (`prometheus --config.file=test/e2e/prometheus.yml`),
or curl it directly (`curl http://127.0.0.1:13622/metrics`).

## Tear down the test environment

To tear down the test environment, run the command below:

    ```sh
    make uninstall-helm
    make clean-e2e-tests
    ```