# Tutorial: Installing Agent on Clusters
This tutorial will guide you through the process of installing fleet agent on hub & member clusters.

Follow these guides to ...:

- [Quickstart: Create an Azure Kubernetes Fleet Manager resource and join member clusters using Azure CLI (With Hub Cluster)](https://learn.microsoft.com/en-us/azure/kubernetes-fleet/quickstart-create-fleet-and-members?tabs=with-hub-cluster)
- [Quickstart: Access the Kubernetes API of the Fleet hub cluster](https://learn.microsoft.com/en-us/azure/kubernetes-fleet/quickstart-access-fleet-kubernetes-api)

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Helm](https://github.com/helm/helm#install)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)

## Steps to installing fleet agent on clusters

> **Note:** Please read through the entire document before running the scripts.

### Run the join script in the fleet repository

Clone the [fleet repository](https://github.com/Azure/fleet) and navigate to the root directory of the repo.

Run the following script which sets up the resources on the hub cluster and each cluster to allow
the member agent installed by the script on each cluster to communicate with the hub cluster.

> **Note:** The script creates resources on the hub cluster in a namespace called `connect-to-fleet` and also creates
> a secret on each cluster please don't update/delete these resources.

The latest fleet image tag could be found here in [fleet releases](https://github.com/Azure/fleet/releases).

> **Note:** Please ensure kubectl can access the kube-config of the hub cluster and all the clusters.

Ex:
- `./hack/membership/joinMC.sh v0.1.0 hub test-cluster-1`
- `./hack/membership/joinMC.sh v0.1.0 hub test-cluster-1 test-cluster-2`

```shell
chmod +x ./hack/membership/joinMC.sh
./hack/membership/joinMC.sh <FLEET-IMAGE-TAG> <HUB-CLUSTER-NAME> <MEMBER-CLUSTER-NAME-1> <MEMBER-CLUSTER-NAME-2> <MEMBER-CLUSTER-NAME-3> ...
```

The output should look like:

```
% kubectl get membercluster -A
NAME               JOINED    AGE     MEMBER-AGENT-LAST-SEEN   NODE-COUNT   AVAILABLE-CPU   AVAILABLE-MEMORY
test-cluster-1     Unknown   3m33s   5s                       1            890m            4074756Ki
test-cluster-2     Unknown   3m16s   4s                       1            890m            4074756Ki
```

> **Note:** The `JOINED` column will be `Unknown` until the fleet networking member agent charts are installed on each cluster.

We can confirm that the member agent was installed correctly when we see `MEMBER-AGENT-LAST-SEEN`, `NODE-COUNT`, `AVAILABLE-CPU`, and `AVAILABLE-MEMORY` columns populated.
The columns mentioned can take upto a minute to populate.

> **Note:** The script in the fleet-networking repo should only be run once the script in the fleet repo has been
> run to ensure the member agents can communicate with the hub cluster.

### Run the join script in the fleet networking repository

Clone the [fleet-networking repository](https://github.com/Azure/fleet-networking) and navigate to the root directory of the repo.

Run the following script to install the fleet networking member agents on each cluster.

The latest fleet-networking image tag could be found here [fleet-networking releases](https://github.com/Azure/fleet-networking/releases).

Ex:
- `./hack/membership/joinMC.sh v0.1.0 v0.2.0 hub test-cluster-1`
- `./hack/membership/joinMC.sh v0.1.0 v0.2.0 hub test-cluster-1 test-cluster-2`

```shell
chmod +x ./hack/membership/joinMC.sh
./hack/membership/joinMC.sh <FLEET-IMAGE-TAG> <FLEET-NETWORKING-IMAGE-TAG> <HUB-CLUSTER-NAME> <MEMBER-CLUSTER-NAME-1> <MEMBER-CLUSTER-NAME-2> <MEMBER-CLUSTER-NAME-3> ...
```

The output should look like:

```
% kubectl get membercluster -A
NAME               JOINED   AGE     MEMBER-AGENT-LAST-SEEN   NODE-COUNT   AVAILABLE-CPU   AVAILABLE-MEMORY
test-cluster-1     True     6m49s   6s                       1            490m            3550468Ki
test-cluster-2     True     6m32s   4s                       1            490m            3550468Ki
```

The `JOINED` column will be `True` once both fleet networking member agent charts are installed on each cluster and the networking
member agents are able to communicate with fleet hub cluster.
The column can take upto a minute to populate. The `JOINED` column indicates that all three fleet member agents have all joined once.
The column is not meant for tracking each member agent's health status.
