# Getting started with Fleet using kind clusters

In this tutorial, you will try Fleet out using 
[kind](https://kind.sigs.k8s.io/) clusters, which are Kubernetes clusters running on your own
local machine via [Docker](https://docker.com) containers. This is the easiest way
to get started with Fleet, which can help you understand how Fleet simiplify the day-to-day multi-cluster management experience with very little setup needed.

> Note
>
> kind is a tool for setting up a Kubernetes environment for experimental purposes;
> some instructions below for running Fleet in the kind environment may not apply to other
> environments, and there might also be some minor differences in the Fleet
> experience.

## Before you begin

To complete this tutorial, you will need:

* The following tools on your local machine:
    * `kind`, for running Kubernetes clusters on your local machine
    * Docker
    * `git`
    * `curl`
    * `helm`, the Kubernetes package manager
    * `jq`
    * `base64`

## Spin up a few kind clusters

The Fleet open-source project manages a multi-cluster environment using a hub-spoke pattern,
which consists of one hub cluster and one or more member clusters: 

* The hub cluster is the portal to which every member cluster connects; it also serves as an
interface for centralized management, through which you can perform a number of tasks,
primarily orchestrating workloads across different clusters.
* A member cluster connects to the hub cluster and runs your workloads as orchestrated by the
hub cluster.

In this tutorial you will create two kind clusters; one of which serves as the Fleet
hub cluster, and the other the Fleet member cluster. Run the commands below to create them:

```sh
# Replace YOUR-KIND-IMAGE with a kind node image name of your
# choice. It should match with the version of kind installed
# on your system; for more information, see
# [kind releases](https://github.com/kubernetes-sigs/kind/releases).
export KIND_IMAGE=YOUR-KIND-IMAGE
# Replace YOUR-KUBECONFIG-PATH with the path to a Kubernetes
# configuration file of your own, typically $HOME/.kube/config.
export KUBECONFIG_PATH=YOUR-KUBECONFIG-PATH

# The names of the kind clusters; you may use values of your own if you'd like to.
export HUB_CLUSTER=hub
export MEMBER_CLUSTER=member-1

kind create cluster --name $HUB_CLUSTER \
    --image=$KIND_IMAGE \
    --kubeconfig=$KUBECONFIG_PATH
kind create cluster --name $MEMBER_CLUSTER \
    --image=$KIND_IMAGE \
    --kubeconfig=$KUBECONFIG_PATH

# Export the configurations for the kind clusters.
kind export kubeconfig -n $HUB_CLUSTER
kind export kubeconfig -n $MEMBER_CLUSTER
```

# Set up the Fleet hub cluster

To set up the hub cluster, run the commands below:

```sh
export HUB_CLUSTER_CONTEXT=kind-$HUB_CLUSTER
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

# Install the helm chart for running Fleet agents on the hub cluster.
helm install hub-agent fleet/charts/hub-agent/ \
    --set image.pullPolicy=Always \
    --set image.repository=$REGISTRY/$HUB_AGENT_IMAGE \
    --set image.tag=$FLEET_VERSION \
    --set logVerbosity=2 \
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

## Set up the Fleet member custer

Next, you will set up the other kind cluster you created earlier as the Fleet
member cluster, which requires that you install the Fleet member agent on
the cluster and connect it to the Fleet hub cluster.

For your convenience, Fleet provides a script that can automate the process of joining a cluster
into a fleet. To use the script, follow the steps below:

```sh
# Query the API server address of the hub cluster.
export HUB_CLUSTER_ADDRESS="https://$(docker inspect $HUB_CLUSTER-control-plane --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'):6443"

export MEMBER_CLUSTER_CONTEXT=kind-$MEMBER_CLUSTER

# Run the script.
chmod +x fleet/hack/membership/join.sh
./fleet/hack/membership/join.sh
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

