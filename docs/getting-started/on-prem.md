# Getting started with Fleet using on-premises clusters

In this tutorial, you will try Fleet out using a few of your own Kubernetes clusters; Fleet can 
help you manage workloads seamlessly across these clusters, greatly simplifying the experience
of day-to-day Kubernetes management.

> Note
>
> This tutorial assumes that you have some experience of performing administrative tasks for
> Kubernetes clusters. If you are just gettings started with Kubernetes, or do not have much
> experience of setting up a Kubernetes cluster, it is recommended that you follow the
> [Getting started with Fleet using Kind clusters](kind.md) tutorial instead.

## Before you begin

To complete this tutorial, you will need:

* At least two Kubernetes clusters of your own.
    * Note that one of these clusters will serve as your hub cluster; other clusters must be able
    to reach it via the network.
* The following tools on your local machine:
    * `kubectl`, the Kubernetes CLI tool.
    * `git`
    * `curl`
    * `helm`, the Kubernetes package manager
    * `jq`
    * `base64`

## Set up a Fleet hub cluster

The Fleet open-source project manages a multi-cluster environment using a hub-spoke pattern,
which consists of one hub cluster and one or more member clusters: 

* The hub cluster is the portal to which every member cluster connects; it also serves as an
interface for centralized management, through which you can perform a number of tasks,
primarily orchestrating workloads across different clusters.
* A member cluster connects to the hub cluster and runs your workloads as orchestrated by the
hub cluster.

Any Kubernetes cluster running a supported version of Kubernetes can serve as the hub cluster;
it is recommended that you reserve a cluster
specifically for this responsibility, and do not run other workloads on it. For the best
experience, consider disabling the built-in `kube-controller-manager` controllers for the
cluster: you could achieve this by setting the `--controllers` CLI argument; for more information,
see the [`kube-controller-manager` documentation](https://kubernetes.io/docs/reference/command-line-tools-reference/kube-controller-manager/).

To set up the hub cluster, run the commands below:

```sh
# Replace YOUR-HUB-CLUSTER-CONTEXT with the name of the kubeconfig context for your hub cluster.
export HUB_CLUSTER_CONTEXT=YOUR-HUB-CLUSTER-CONTEXT

kubectl config use-context $HUB_CLUSTER_CONTEXT

# The variables below uses the Fleet images kept in the Microsoft Container Registry (MCR),
# and will retrieve the latest version from the Fleet GitHub repository.
#
# You can, however, build the Fleet images of your own; see the repository README for
# more information.
export REGISTRY="mcr.microsoft.com/aks/fleet"
export FLEET_VERSION=$(curl "https://api.github.com/repos/Azure/fleet/tags" | jq -r '.[0].name')
export HUB_AGENT_IMAGE="hub-agent"

# Clone the Fleet repository from GitHub.
git clone https://github.com/Azure/fleet.git
cd fleet

# Install the helm chart for running Fleet agents on the hub cluster.
helm install hub-agent ./charts/hub-agent/ \
    --set image.pullPolicy=Always \
    --set image.repository=$REGISTRY/$HUB_AGENT_IMAGE \
    --set image.tag=$FLEET_VERSION \
    --set logVerbosity=5 \
    --set namespace=fleet-system \
    --set enableWebhook=true \
    --set webhookClientConnectionType=service \
    --set enableV1Alpha1APIs=false \
    --set enableV1Beta1APIs=true
```

It may take a few seconds for the installation to complete. Once it finishes, verify that
the Fleet hub agents are up and running with the commands below:

```sh
kubectl get pods -n fleet-system
```

You should see that all the pods are in the ready state.

## Connect a member cluster to the hub cluster

Next, you will set up a cluster as the member cluster for your fleet. This cluster should
run a supported version of Kubernetes and be able to connect to the hub cluster via the network.

For your convenience, Fleet provides a script that can automate the process of joining a cluster
into a fleet. To use the script, follow the steps below:

```sh
# Replace the value of HUB_CLUSTER_ADDRESS with the address of your hub cluster API server.
export HUB_CLUSTER_ADDRESS=YOUR-HUB-CLUSTER-ADDRESS
# Replace the value of MEMBER_CLUSTER with the name you would like to assign to the new member
# cluster.
#
# Note that Fleet will recognize your cluster with this name once it joins.
export MEMBER_CLUSTER=YOUR-MEMBER-CLUSTER
# Replace the value of MEMBER_CLUSTER_CONTEXT with the name of the kubeconfig context you use
# for accessing your member cluster.
export MEMBER_CLUSTER_CONTEXT=YOUR-MEMBER-CLUSTER-CONTEXT

# Run the script.
chmod +x ./hack/Azure/setup/joinMC.sh
./hack/Azure/setup/joinMC.sh
```

It may take a few minutes for the script to finish running. Once it is completed, verify
that the cluster has joined successfully with the command below:

```sh
kubectl config use-context $HUB_CLUSTER_CONTEXT
kubectl get membercluster $MEMBER_CLUSTER
```

The newly joined cluster should have the `JOINED` status field set to `True`. If you see that
the cluster is still in an unknown state, it might be that the member cluster
is still connecting to the hub cluster. Should this state persist for a prolonged
period, refer to the [Troubleshooting Guide](../troubleshooting/README.md) for
more information.

> Note
>
> If you would like to know more about the steps the script runs, or would like to join
> a cluster into a fleet manually, refer to the [Managing Clusters](../howtos/clusters.md) How-To
> Guide.

Repeat the steps above to join more clusters into your fleet.

## Use the `ClusterResourcePlacement` API to orchestrate resources among member clusters.

Fleet offers an API, `ClusterResourcePlacement`, which helps orchestrate workloads, i.e., any group
Kubernetes resources, among all member clusters. In this last part of the tutorial, you will use
this API to place some Kubernetes resources automatically into the member clusters via the hub
cluster, saving the trouble of having to create them one by one in each member cluster.

### Create the resources for placement

Run the commands below to create a namespace and a config map, which will be placed onto the
member clusters.

```sh
kubectl create namespace work
kubectl create configmap app -n work --from-literal=data=test
```

It may take a few seconds for the commands to complete.

### Create the `ClusterResourcePlacement` API object

Next, create a `ClusterResourcePlacement` API object in the hub cluster:

```sh
kubectl apply -f - <<EOF
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: crp
spec:
  resourceSelectors:
    - group: ""
      kind: Namespace
      version: v1          
      name: work
  policy:
    placementType: PickAll
EOF
```

Note that the CRP object features a resource selector, which targets the `work` namespace you
just created. This will instruct the CRP to place the namespace itself, and all resources
registered under the namespace, such as the config map, to the target clusters. Also, in the `policy`
field, a `PickAll` placement type has been specified. This allows the CRP to automatically perform
the placement on all member clusters in the fleet, including those that join after the CRP object
is created.

It may take a few seconds for Fleet to successfully place the resources. To check up on the
progress, run the commands below:

```sh
kubectl get clusterresourceplacement crp
```

Verify that the placement has been completed successfully; you should see that the `APPLIED` status
field has been set to `True`. You may need to repeat the commands a few times to wait for 
the completion.

### Confirm the placement

Now, log into the member clusters to confirm that the placement has been completed.  

```sh
kubectl config use-context $MEMBER_CLUSTER_CONTEXT
kubectl get ns
kubectl get configmap -n work
```

You should see the namespace `work` and the config map `app` listed in the output.

## Clean things up

To remove all the resources you just created, run the commands below:

```sh
# This would also remove the namespace and config map placed in all member clusters.
kubectl delete crp crp

kubectl delete ns work
kubectl delete configmap app -n work
```

To uninstall Fleet, run the commands below:

```sh
kubectl config use-context $HUB_CLUSTER_CONTEXT
helm uninstall hub-agent
kubectl config use-context $MEMBER_CLUSTER_CONTEXT
helm uninstall member-agent
```

## What's next

Congratulations! You have completed the getting started tutorial for Fleet. To learn more about
Fleet:

* [Read about Fleet concepts](../concepts/README.md)
* [Read about the ClusterResourcePlacement API](../howtos/crp.md)
* [Read the Fleet API reference](../api-references.md)
