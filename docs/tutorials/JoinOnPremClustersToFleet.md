# Tutorial: Join On-Prem Clusters to Fleet hub cluster
This tutorial will guide you through the process of joining your on-prem clusters as member clusters to a Fleet hub cluster.

Follow these guides to setup your fleet and get access to your fleet hub cluster:

- [Quickstart: Create an Azure Kubernetes Fleet Manager resource and join member clusters using Azure CLI (With Hub Cluster)](https://learn.microsoft.com/en-us/azure/kubernetes-fleet/quickstart-create-fleet-and-members?tabs=with-hub-cluster)
- [Quickstart: Access the Kubernetes API of the Fleet hub cluster](https://learn.microsoft.com/en-us/azure/kubernetes-fleet/quickstart-access-fleet-kubernetes-api)

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Helm](https://github.com/helm/helm#install)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)

## Steps to join on-prem clusters to Fleet hub cluster

> **Note:** Please read through the entire document before running the scripts.

### Run the join script in the fleet repository

Clone the [fleet repository](https://github.com/Azure/fleet) and navigate to the root directory of the repo.

Run the following script which sets up the resources on the hub cluster and each on-prem cluster to allow
the member agent installed by the script on each on-prem cluster to communicate with the hub cluster.

> **Note:** The script creates resources on the hub cluster in a namespace called `connect-to-fleet` and also creates 
> a secret on each on-prem cluster please don't update/delete these resources.

The latest fleet image tag could be found here in [fleet releases](https://github.com/Azure/fleet/releases).

> **Note:** Please ensure kubectl can access the kube-config of the hub cluster and all the on-prem clusters.

Ex: 
- `./hack/membership/joinMC.sh v0.1.0 hub test-cluster-1`
- `./hack/membership/joinMC.sh v0.1.0 hub test-cluster-1 test-cluster-2`

```shell
chmod +x ./hack/membership/joinMC.sh
./hack/membership/joinMC.sh <FLEET-IMAGE-TAG> <HUB-CLUSTER-NAME> <MEMBER-CLUSTER-NAME-1> <MEMBER-CLUSTER-NAME-2> <MEMBER-CLUSTER-NAME-3> ...
```

The output should look like:

```
% kubeclt get membercluster -A
NAME               JOINED    AGE     NODE-COUNT   AVAILABLE-CPU   AVAILABLE-MEMORY
test-cluster-1     Unknown   2m40s   1            530m            2678020Ki
test-cluster-2     Unknown   2m30s   1            890m            3566856Ki
```

> **Note:** The `JOINED` column will be `Unknown` until the fleet networking member agent charts are installed on each on-prem cluster.

We can confirm that the member agent was installed correctly when we see `NODE-COUNT`, `AVAILABLE-CPU`, and `AVAILABLE-MEMORY` columns populated.
The columns mentioned can take upto a minute to populate.

> **Note:** The script in the fleet-networking repo should only be run once the script in the fleet repo has been 
> run to ensure the member agents can communicate with the hub cluster.

### Run the join script in the fleet networking repository

Clone the [fleet-networking repository](https://github.com/Azure/fleet-networking) and navigate to the root directory of the repo.

Run the following script to install the fleet networking member agents on each on-prem cluster.

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
NAME               JOINED   AGE     NODE-COUNT   AVAILABLE-CPU   AVAILABLE-MEMORY
test-cluster-1     True     3m17s   1            130m            2153732Ki
test-cluster-2     True     3m7s    1            690m            3304712Ki
```

The `JOINED` column will be `True` once both fleet networking member agent charts are installed on each on-prem cluster and the networking
member agents are able to communicate with fleet hub cluster.
The column can take upto a minute to populate. The `JOINED` column indicates that all three fleet member agents have all joined once.
The column is not meant for tracking each member agent's health status.

# Steps to make an on-prem cluster leave the Fleet hub cluster

Delete the `MemberCluster` resource for a particular on-prem cluster in the hub cluster.

The join script in the fleet repo creates `MemberCluster` resource with the same name as your on-prem cluster.
Replace <cluster-name> with the name of your on-prem cluster.

```
kubectl config use-context hub
kubectl delete membercluster <cluster-name>
```

Once the above delete command completes the on-prem cluster has successfully left the Fleet hub cluster. 
But we still need to clean-up residual resources on the hub and on-prem clusters.

> **Note:** There is a case where `MemberCluster` resource deletion is stuck, this occurs because we didn't install
> all the member agents required or some agents were uninstalled before the `MemberCluster` resource was deleted.
> If this case occurs, we need to delete the `InternalMemberCluster` resource for the on-prem cluster in the hub cluster.
> But this step is blocked by the **fleet-guard-rail** which is a validating webhook on the hub cluster.
> So we need to delete the associated `ValidatingWebhookConfiguration`
> (This is not advised, but to delete the `MemberCluster` we have to do this step)

```
kubectl delete validatingwebhookconfiguration fleet-guard-rail-webhook-configuration
kubectl delete internalmembercluster <cluster-name> -n fleet-member-<cluster-name>
```

This ensures the `MemberCluster` can be deleted, so the on-prem cluster can successfully leave the Fleet hub cluster.

# Clean up resources created by the join scripts

> **Note:** Before running the below script to clean up resources for on-prem clusters, please ensure that each
> on-prem cluster has left the Fleet hub cluster by manually deleting it's associated `MemberCluster` resource.

Navigate to the root directory of the [fleet repository](https://github.com/Azure/fleet).

Run the following script which cleans up all the resources we set up on the hub cluster and each on-prem cluster 
to allow the member agents to communicate with the hub cluster.

Ex: 
- `./hack/membership/cleanup.sh hub test-cluster-1`
- `./hack/membership/cleanup.sh hub test-cluster-1 test-cluster-2`

```
chmod +x ./hack/membership/cleanup.sh
./hack/membership/cleanup.sh <HUB-CLUSTER-NAME> <MEMBER-CLUSTER-NAME-1> <MEMBER-CLUSTER-NAME-2> <MEMBER-CLUSTER-NAME-3> ...
```

All the resources created by the join scripts will be deleted except the namespace `connect-to-fleet` on the hub cluster.
If you are sure that all the member clusters have left the Fleet hub cluster, you can delete the `connect-to-fleet` namespace.

# Troubleshooting

If at any point we need a clean slate to start over, we can run follow the steps from this [leave section](#steps-to-make-an-on-prem-cluster-leave-the-fleet-hub-cluster).
