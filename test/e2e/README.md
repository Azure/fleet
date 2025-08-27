# Fleet E2E Tests (API version v1beta1)

This directory includes the E2E test suites for Fleet agents (v1beta1 API). To run the 
test suites, follow the steps below:

1. Install [Docker](https://www.docker.com) and [Kind](https://kind.sigs.k8s.io/).

2. Change to the current directory, and run `setup.sh` to set up the test environment.

    ```sh
    cd ./test/e2e
    chmod +x ./setup.sh
    
    # Use a different path if the local set up is different.
    export KUBECONFIG=~/.kube/config
    export OUTPUT_TYPE=type=docker
    # optional, used to test the placement features with custom configurations
    # It defaults to 0m.
    # export RESOURCE_SNAPSHOT_CREATION_MINIMUM_INTERVAL=30s
    # export RESOURCE_CHANGES_COLLECTION_DURATION=15s
    ./setup.sh ${number of member clusters}
    ```

    The setup script will perform the following tasks:

    * Provision a number of `Kind` clusters: `hub` as the hub cluster, with `bravelion`, `smartfish`
      and `singingbutterfly` as the member clusters
    * Build the agent images
    * Upload the images to the `Kind` clusters
    * Install the agent images

3. Run the command below to run non custom configuration e2e tests with the following command:

    ```sh
   ginkgo --label-filter="!custom" -v -p .
   ```

   or run the custom configuration e2e tests with the following command:
   ```sh
   ginkgo --label-filter="custom" -v -p .
   ```

   or run tests involving member cluster join/leave scenarios with the following command (serially):
   ```sh
   ginkgo --label-filter="joinleave" -v .
   ```

   or run tests related to resourcePlacement (rp) only with the following command:
   ```sh
   ginkgo --label-filter="resourceplacement" -v -p .
   ```

   or create a launch.json in your vscode workspace.
   ```yaml
   {
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Launch E2E Tests",
            "type": "go",
            "request": "launch",
            "mode": "test",
            "program": "${workspaceFolder}/test/e2e",
            "args": [],
            "env": {
                "KUBECONFIG": "~/.kube/config",
                #"RESOURCE_SNAPSHOT_CREATION_MINIMUM_INTERVAL": "30s",
                #"RESOURCE_CHANGES_COLLECTION_DURATION": "15s",
            },
            "buildFlags": "-tags=e2e",
            "showLog": true
        }
    ]
   }
   ```

## Access the `Kind` clusters

To access the `Kind` clusters, after the test environment is set up using `setup.sh`, switch
between the following contexts using the command `kubectl config use-context`:

* `kind-hub` for accessing the hub cluster
* `kind-cluster-1` for accessing the first cluster
* `kind-cluster-2` for accessing the second cluster
* `kind-cluster-3` for accessing the third cluster

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

## Tear down the test environment.

To stop the `Kind` clusters, run the script `stop.sh`:

    ```sh
    chmod +x ./stop.sh
    ./stop.sh ${number of member clusters}
    ```