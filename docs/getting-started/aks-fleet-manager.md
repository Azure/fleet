# Getting started with Fleet using Azure Fleet Manager

In this tutorial, you will try Fleet out using
[Azure Fleet Manager](https://azure.microsoft.com/en-us/products/kubernetes-fleet-manager),
an Azure service that aims to help seamless manage Kubernetes clusters at scale.
Azure Fleet Manager is built on top of the Fleet open-source project, which greatly simplifies
the experience of setting things up while providing additional features; if you have
deployed Azure Kubernetes Service (AKS) clusters before, or if you are
familar with Azure services, this tutorial can be a perfect place for you to get started with
Fleet. This tutorial is also a great option if you are less familar with Kubernetes
administration tasks, or if you just would like a playground where you can play with
the features Fleet offers, as Azure Fleet Manager can handle many setup tasks automatically
for you.

Specifically, this tutorial will show you how to create a Fleet consisting of multiple Kubernetes
clusters, and how to orchestrate some workload across these Kubernetes clusters.

## Before you begin

To complete this tutorial, you will need:

* An Azure account with an active subscription.

    [You can create an account for free](https://azure.microsoft.com/free/) if you do not have one
    yet.

    Write down your subscription ID; you can find it following the instructions in
    [Find your Azure subscription](https://learn.microsoft.com/en-us/azure/azure-portal/get-subscription-tenant-id#find-your-azure-subscription).

* The latest version of Azure CLI.

    Follow the instructios at [Install or upgrade Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli).

* The latest version of `kubectl`, the Kubernetes CLI, and `kubelogin`, a plugin for authenticating
with Kubernetes clusters.

    Once you have Azure CLI installed or upgraded, run the commands to below to install `kubectl`
    and `kubelogin`:

    ```sh
    az aks install-cli
    ```

In addition, run the steps below:

* Set up your Azure CLI installation with your subscription:

    ```sh
    # Replace YOUR-SUBSCRIPTION-ID with the value of your own.
    export SUBSCRIPTION_ID=YOUR-SUBSCRIPTION-ID
    az account -s $SUBSCRIPTION_ID
    ```

* Install the Azure Fleet Manager CLI extension for your Azure CLI installation.

    ```sh
    az extension add --name fleet
    ```

    It may take a few seconds to complete the installation.

### Cost

Currently, Azure Fleet Manager is in preview and is provided free of charge. However, to complete
this tutorial and try multi-cluster setup out, you will need to provision a few Kubernetes clusters
via Azure Kubernetes Service, which may incur additional costs. However, if you complete
this tutorial within a reasonable amount of time and have all the resources deallocated as soon
as you finish it, the charge should be minimal. In addition, Azure offers credits and free
services, which may be applicable to your account; see the [Azure Free Account](https://azure.microsoft.com/en-us/free)
page for more information.

## Create a resource group

Azure resource groups allow users to deploy and manage Azure resources in a logical way; to use
Azure Fleet Manager, you must create a resource group first following the commands below:

```sh
# You may use different names and/or locations as you see fit.
#
# Azure Fleet Manager may not be available in certain locations.
export RESOURCE_GROUP=fleet-getting-started
export LOCATION=eastus
az group create --name $RESOURCE_GROUP --location $LOCATION
```

It may take a few seconds to complete the commands.

## Create a fleet

The Fleet open-source project manages a multi-cluster environment using a hub-spoke pattern.
Specifically, in each Fleet deployment exists

* one hub cluster, which serves as a portal that connects to all the member clusters, and a
centralized management interface, through which you may perform a number of tasks, primarily
orchestrating workloads among the member clusters.
* many (1+) member clusters, which run your workloads as orchestrated by the hub cluster.

To use Azure Fleet Manager, your must create an Azure Fleet first, which embodies the concept
of the hub cluster, explained above. To be more specific, Azure Fleet Manager will provision
a Kubernetes cluster, running on Azure Kubernetes Service, and set it up as the Fleet hub cluster
for you to use. Azure Fleet Manager also performs a number of other tasks, such as membership
management, which you will try out in the sections below.

To create an Azure Fleet, run the commands below:

```sh
# You may use a different name for your fleet.
export FLEET=demo-fleet
az fleet create \
    --resource-group ${RESOURCE_GROUP} \
    --name ${FLEET} \
    --location ${LOCATION} \
    --enable-hub
```

It may take a few minutes to complete the commands. Once they are completed, you will see the details
about the Azure Fleet resource returned in the JSON format.

## Create member clusters

To use the fleet you just created, you will need a few member clusters, which actually runs 
workloads for you. These member clusters are just regular Azure Kubernetes Service clusters; to
create them, run the commands below:

```sh
# You may use different names for your member clusters.
export MEMBER_CLUSTER_1=cluster-1
export MEMBER_CLUSTER_2=cluster-2

az aks create \
    --resource-group ${RESOURCE_GROUP} \
    --location ${LOCATION} \
    --name ${MEMBER_CLUSTER_1} \
    --node-count 1

az aks create \
    --resource-group ${RESOURCE_GROUP} \
    --location ${LOCATION} \
    --name ${MEMBER_CLUSTER_2} \
    --node-count 1
```

It may take a few minutes to complete the commands. Once they are completed, you will see the
details about the member clusters returned in the JSON format.

> Note
>
> For simplicity reasons, in this tutorial you create only 2 Kubernetes clusters in the same
> location as Fleet member clusters; the Fleet open-source project (and Azure Fleet Manager)
> can support more Kubernetes clusters joining in a fleet from many different locations.

## Join the member clusters into the fleet

Next, you will need to join the two member clusters you just create to the hub cluster, 
represented by the Azure Fleet resource, so that the hub cluster can manage them on your
behalf. Normally this would require you to set up credentials, permissions, and many more
on the clusters involved, but Azure Fleet Manager can automate most of the tasks for you with
the commands below:

```sh
# You may use different names for the member clusters here; these names are used by the
# fleet to recognize these member clusters.
export MEMBER_CLUSTER_ID_1=/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.ContainerService/managedClusters/$MEMBER_CLUSTER_1
export MEMBER_NAME_1=$MEMBER_CLUSTER_1

export MEMBER_CLUSTER_ID_2=/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.ContainerService/managedClusters/$MEMBER_CLUSTER_2
export MEMBER_NAME_2=$MEMBER_CLUSTER_2

az fleet member create \
    --resource-group ${GROUP} \
    --fleet-name ${FLEET} \
    --name ${MEMBER_NAME_1} \
    --member-cluster-id ${MEMBER_CLUSTER_ID_1}

az fleet member create \
    --resource-group ${GROUP} \
    --fleet-name ${FLEET} \
    --name ${MEMBER_NAME_2} \
    --member-cluster-id ${MEMBER_CLUSTER_ID_1}
```

It may take a few minutes to complete the commands. Once they are completed, you will see the
details about the memberships returned in the JSON format.

## Log into the hub cluster

Now everything needed for this tutorial have been set up. As explained earlier, you may use the
hub cluster as a management interface for orchestrating workloads among the member clusters; to
achieve this, log into the hub cluster first with the commands below:

```sh
az fleet get-credentials --resource-group ${RESOURCE_GROUP} --name ${FLEET}
```

With this command, Azure Fleet Manager will write a kubeconfig about the hub cluster to your
local machine. The command may prompt you to sign in via your browser; follow the instructions
given to complete the process.

To verify that you have log into the hub cluster, run the command below:

```sh
kubectl get memberclusters
```

It will return the two member clusters just join into the fleet, with names you assign. Check if
both clusters have the `JOINED` status field set to `True`.

## Use the `ClusterResourcePlacement` API to orchestrate resources among member clusters

Fleet offers an API, `ClusterResourcePlacement`, which helps orchestrate workloads, i.e., any group
Kubernetes resources, among all member clusters. In this last part of the tutorial, you will use
this API to place some Kubernetes resources automatically to the member clusters via the hub
cluster, saving the trouble of having to create them one by one in each member cluster.

### Create the resources for placement

Run the commands below to create a namespace and a config map, which will be placed onto the
memberclusters.

```sh
kubectl create namespace work
kubectl create configmap app --from-literal=data=test
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
just create. This will instruct the CRP to place the namespace itself, and all resources
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

Now, log into the member clusters to confirm that the placement has been completed. Firstly, run the
commands to get the credentials:

```sh
az aks get-credentials -n $MEMBER_CLUSTER_1 -g $RESOURCE_GROUP
# Switch to the kubeconfig.
kubectl config use-context $MEMBER_CLUSTER_1
```

Once the credentials are ready, run the commands below to check if the resource placement has
been completed:

```sh
kubectl get ns
kubectl get configmap -n work
```

You should see the namespace `work` and the config map `app` listed in the output.

## Clean things up

To remove all the resources you just create, run the commands below:

```sh
az group delete -n $RESOURCE_GROUP
```

The commands remove the resource group you create, along with all the resources, such as the
Azure Fleet and its member clusters.

## What's next

Congratulations! You have completed the getting started tutorial for Fleet. To learn more about
Fleet:

* [Read about Fleet concepts](../concepts/README.md)
* [Read about the ClusterResourcePlacement API](../howtos/crp.md)
* [Read the Fleet API reference](../api-references.md)
