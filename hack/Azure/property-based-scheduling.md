# Apply Fleet property-based scheduling (preview) on your Azure Kubernetes Service (AKS) cluster

This document explains how to try out the latest Fleet feature, property-based scheduling, using AKS clusters.

Fleet property-based scheduling (preview) adds more flexibility to Fleet's existing resource propagation capabilities, granting users the ability to pick clusters based on resource availability, usage, costs, node count, and more in the future. This can be very helpful in many common multi-cluster administration scenarios, including:

* eliminating hotspots in a multi-cluster environment; and
* balancing resource usage (CPU, memory, etc.) across clusters; and
* making full use of the cost benefits that AKS provides.

## Before you begin

To complete this tutorial, you will need:

* An active Azure subscription; if you do not have one, you can create an
[Azure free account](https://azure.microsoft.com/free).
* The latest version of the following tools installed on your local environment:
    * [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli)
    * [`Git` CLI](https://git-scm.com/book/en/v2/Getting-Started-Installing-Git)
    * [The Kubernetes CLI](https://kubernetes.io/docs/tasks/tools/)
    * [Helm](https://helm.sh), the Kubernetes package manager

    If you are having trouble setting up the tools in your local environment, you may consider using [the Bash environment on Azure Cloud Shell](https://learn.microsoft.com/en-us/azure/cloud-shell/quickstart), which has the tools pre-installed and is available via web browser.

This tutorial assumes that you have a basic understanding of how Kubernetes works; for a review of core Kubernetes concepts, see [Core Kubernetes Concepts for AKS](https://learn.microsoft.com/en-us/azure/aks/concepts-clusters-workloads). 

## Set up AKS clusters

In this tutorial, you will set up a Fleet using 4 AKS clusters, in which:

* one will be configured as the Fleet hub cluster; this cluster serves as the management portal for the fleet, through which you may perform many multi-cluster management tasks, such as resource propagation.
* the other three clusters will be configured as the Fleet member cluster; these clusters can run your workloads under the coordination of the Fleet hub cluster.

First, create the hub cluster using the commands below:

```sh
az login
# If you have multiple subscriptions, run the command below to set the default subscription:
#
# Replace YOUR-SUBSCRIPTION-ID with the value of your own. 
# export SUBSCRIPTION=YOUR-SUBSCRIPTION-ID
# az account set --subscription YOUR-SUBSCRIPTION-ID

# Replace eastus with any other Azure location, if necessary.
export RG=fleet-demo
export LOCATION_1=eastus
# Create a resource group.
az group create -n $RG -l $LOCATION_1

# Create the hub cluster.
export HUB_CLUSTER=hub
az aks create -n $HUB_CLUSTER \
    -l $LOCATION_1 \
    -g $RG \
    --node-count 2
```

It may take a few moments before the commands complete.

Next, create the following three member clusters using the commands below:

```sh
export MEMBER_CLUSTER_1=bravelion
export MEMBER_CLUSTER_2=smartfish
export MEMBER_CLUSTER_3=jumpingcat

# Similarly, replace the location values with ones of your own, if necessary.
export LOCATION_2=centralus
export LOCATION_3=westus

# Create the three member clusters.
az aks create -n $MEMBER_CLUSTER_1 \
    -l $LOCATION_1 \
    -g $RG \
    --node-count 2 

az aks create -n $MEMBER_CLUSTER_2 \
    -l $LOCATION_2 \
    -g $RG \
    --node-count 3

az aks create -n $MEMBER_CLUSTER_3 \
    -l $LOCATION_3 \
    -g $RG \
    --node-count 4 
```

It may take a few moments before the command completes.

After all the commands have completed successfully, use the command below to retrieve their credentials for later access:

```sh
az aks get-credentials -n $HUB_CLUSTER -g $RG --admin
az aks get-credentials -n $MEMBER_CLUSTER_1 -g $RG --admin
az aks get-credentials -n $MEMBER_CLUSTER_2 -g $RG --admin
az aks get-credentials -n $MEMBER_CLUSTER_3 -g $RG --admin
```

## Deploy Fleet

To set up Fleet using the clusters you just created, you will need to install Fleet hub and member agents on the respective clusters.

First, clone the Fleet source code repository, which contains the Helm charts used for Fleet agent installation:

```sh
git clone https://github.com/kubefleet-dev/kubefleet.git
cd fleet
git checkout demo
```

Then install the Fleet hub agent in the Fleet hub cluster:

```sh
kubectl config use-context $HUB_CLUSTER-admin

export REGISTRY=fleetdemo.azurecr.io
export TAG=demo
helm install hub-agent charts/hub-agent/ \
    --set image.pullPolicy=Always \
    --set image.repository=$REGISTRY/hub-agent \
    --set image.tag=$TAG \
    --set logVerbosity=2 \
    --set namespace=fleet-system \
    --set enableWebhook=true \
    --set webhookClientConnectionType=service \
    --set enableV1Beta1APIs=true
```

It will take a few moments to complete the installation. After the command returns, verify that the Fleet hub agent is up and running with this command:

```sh
kubectl get pods -n fleet-system
```

You should see all pods in the ready state. Note that the hub agent may take a few seconds to get ready after the installation.

Next, install the Fleet member agents in the Fleet member clusters. The member agent needs their credentials set up in the hub cluster so that they can access the hub cluster; see the commands below for details.

```sh
# Retrieve the hub cluster API server address.
export HUB_SERVER_ADDR=$(az aks show -n $HUB_CLUSTER -g $RG --query "fqdn" | tr -d '"')
export PROPERTY_PROVIDER=aks

declare -a MEMBER_CLUSTERS=($MEMBER_CLUSTER_1 $MEMBER_CLUSTER_2 $MEMBER_CLUSTER_3)

# For each member cluster, create a service account in the hub cluster, and retrieve
# the service account tokens.
for i in "${MEMBER_CLUSTERS[@]}"
do
    kubectl create serviceaccount fleet-member-agent-$i -n fleet-system
    cat <<EOF | kubectl apply -f -
    apiVersion: v1
    kind: Secret
    metadata:
        name: fleet-member-agent-$i-sa
        namespace: fleet-system
        annotations:
            kubernetes.io/service-account.name: fleet-member-agent-$i
    type: kubernetes.io/service-account-token
EOF
done

# Load the service account tokens to the respective member clusters.
for i in "${MEMBER_CLUSTERS[@]}"
do
    kubectl config use-context $HUB_CLUSTER-admin
    TOKEN=$(kubectl get secret fleet-member-agent-$i-sa -n fleet-system -o jsonpath='{.data.token}' | base64 -d)
    kubectl config use-context "$i-admin"
    kubectl delete secret hub-kubeconfig-secret --ignore-not-found
    kubectl create secret generic hub-kubeconfig-secret --from-literal=token=$TOKEN
done

# Install the member agent.
for (( i=0; i<3; i++ ));
do
    kubectl config use-context "${MEMBER_CLUSTERS[$i]}-admin"
    helm install member-agent charts/member-agent/ \
        --set config.hubURL=$HUB_SERVER_ADDR \
        --set image.repository=$REGISTRY/member-agent \
        --set image.tag=$TAG \
        --set refreshtoken.repository=$REGISTRY/refresh-token \
        --set refreshtoken.tag=$TAG \
        --set image.pullPolicy=Always \
        --set refreshtoken.pullPolicy=Always \
        --set config.memberClusterName="${MEMBER_CLUSTERS[$i]}" \
        --set logVerbosity=5 \
        --set namespace=fleet-system \
        --set enableV1Beta1APIs=true \
        --set propertyProvider=$PROPERTY_PROVIDER
done
```

After completing the commands, pick a cluster and verify that the member agent pod is up and running:

```sh
kubectl get pods -n fleet-system
```

You should see that the agent is in the ready state. Repeat the command on the other two member clusters; agents there should be ready as well.

At last, join the member clusters to the hub cluster:

```sh
kubectl config use-context $HUB_CLUSTER-admin

for i in "${MEMBER_CLUSTERS[@]}"
do
    cat <<EOF | kubectl apply -f -
    apiVersion: cluster.kubernetes-fleet.io/v1beta1
    kind: MemberCluster
    metadata:
        name: $i
    spec:
        identity:
            name: fleet-member-agent-$i
            kind: ServiceAccount
            namespace: fleet-system
            apiGroup: ""
EOF
done
```

You can verify the membership status using the command below; after a few moments all member clusters should have their `JOINED` status set to `True`:

```sh
kubectl get membercluster
```

## Try out Fleet property-based scheduling (preview)

The new property-based scheduling experience features an AKS property provider, which exposes many cluster properties about each joined AKS cluster; the properties include:

* the average cost of 1 CPU core in a cluster
* the average cost of 1 GB of memory in a cluster
* the total and allocatable CPU and memory capacities in a cluster, where

    * total capacity = amount of resources installed on all nodes
    * allocatable capacity = amount of resource that can be used for running user
    workloads, i.e., total capacity - resource reserved by the OS, kubelet, etc.

* the available CPU and memory capacities in a cluster, where

    * available capacity = allocatable capacity - requested capacity, i.e., the resources
    requested by each running pod in the cluster

* the count of nodes in a cluster

The command below will show the exposed properties with live updates:

```sh
watch -n 10 -d 'kubectl get membercluster -o custom-columns=Name:metadata.name,NodeCount:.status.properties."kubernetes\.azure\.com/node-count".value,CPUCost:.status.properties."kubernetes\.azure\.com/per-cpu-core-cost".value,MemoryCost:.status.properties."kubernetes\.azure\.com/per-gb-memory-cost".value,AllocatableCPU:.status.resourceUsage.allocatable.cpu,AvailableCPU:.status.resourceUsage.available.cpu,AllocatableMemory:.status.resourceUsage.allocatable.memory,AvailableMemory:.status.resourceUsage.available.memory'
```

### Schedule workloads with the properties

The exposed properties not only give additional insights about your clusters but can also be used for scheduling purposes. Fleet comes with an API, `ClusterResourcePlacement`, which allows you to place resources on the most appropriate clusters with the help of cluster affinity terms and topology spread constraints.

For example, if you would like to place an application in the cluster with the cheapest CPU cost, the configuration below can be helpful:

```sh
# Create a namespace and deployment on the hub cluster, which serves as the resource template.
kubectl create ns work-1
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: work-1-envelope
  namespace: work-1
  annotations:
    kubernetes-fleet.io/envelope-configmap: "true"
data:
  app.yaml: |
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: app
      namespace: work-1
    spec:
      replicas: 3
      selector:
        matchLabels:
          app: nginx
      template:
        metadata:
          labels:
            app: nginx
        spec:
            containers:
            - name: nginx
              image: nginx
              ports:
              - containerPort: 80
              resources:
                requests:
                  cpu: 1
                  memory: 1Gi
EOF

# Then use the CRP API to place the resources. Note that in the affinity terms a preference has
# been set to favor the cluster with the cheapest per core of CPU cost.
cat <<EOF | kubectl apply -f -
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: work-1
spec:
  resourceSelectors:
    - group: ""
      kind: Namespace
      name: work-1
      version: v1
  policy:
    # PickN with the numberOfClusters field set to 1 instructs Fleet to find exactly one cluster
    # based on the preference below.
    placementType: PickN
    numberOfClusters: 1
    affinity:
      clusterAffinity:
        preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 20
          preference:
            propertySorter:
              name: kubernetes.azure.com/per-cpu-core-cost
              sortOrder: Ascending
EOF
```

After the CRP object is created, verify that the resource placement is completed with the command

```sh
kubectl get crp work-1
```

You should see that the manifests are scheduled and applied successfully. Switch to the cluster with the lowest cost, and you will find the namespace and deployment there, distributed by the Fleet hub cluster.

You may also use this API to select clusters based on other conditions, such as the amount of available memory; the example below places the resources on a cluster only if it has at least 2 GB of available memory:

```sh
# Similarly, create a namespace and deployment as placement template.
kubectl create ns work-2
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: work-2-envelope
  namespace: work-2
  annotations:
    kubernetes-fleet.io/envelope-configmap: "true"
data:
  app.yaml: |
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: app
      namespace: work-2
    spec:
      replicas: 1
      selector:
        matchLabels:
          app: nginx
      template:
        metadata:
          labels:
            app: nginx
        spec:
            containers:
            - name: nginx
              image: nginx
              ports:
              - containerPort: 80
              resources:
                requests:
                  cpu: 1
                  memory: 2Gi
EOF

# Next, use the Fleet CRP API to place the workload.
#
# The workload will go to clusters with at least 16GB of memory.
cat <<EOF | kubectl apply -f -
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: work-2
spec:
  resourceSelectors:
    - group: ""
      kind: Namespace
      name: work-2
      version: v1
  policy:
    # PickAll instructs Fleet to select all clusters if it matches with the specified
    # affinity term.
    placementType: PickAll
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
          - propertySelector:
              matchExpressions:
              - name: resources.kubernetes-fleet.io/available-memory
                operator: Ge
                values:
                - "16Gi"
EOF
```

The CRP API enables great flexibility; you can set up different requirements/preferences in combination, such as finding all clusters with at least 5 nodes and 10 available CPU cores, or 4 of all the clusters with the cheapest memory cost and the most amount of available memory. [Read Fleet's API definition to learn more](https://github.com/kubefleet-dev/kubefleet/blob/main/apis/placement/v1beta1/clusterresourceplacement_types.go).

## Clean things up

To remove the resources you have created in this tutorial, simply delete the resource group with the command below:

```sh
az group delete -n $RG
```

All the AKS clusters in the resource group will be removed.

## What's next

Congrats! We hope that property-based scheduling (preview) has improved your overall Fleet experience. If you have any questions, feedback, or concerns, please raise [a GitHub issue](https://github.com/kubefleet-dev/kubefleet/issues).

Aside from property-based scheduling, Fleet offers many other scheduling features that are useful in a
multi-cluster environment; check out the [How-to Guide: Using the Fleet `ClusterResourcePlacement` API](https://kubefleet.dev/docs/how-tos/crp/) for more information.

You can also review Fleet's [source code](https://github.com/kubefleet-dev/kubefleet/) or review its [documentation](https://kubefleet.dev/docs/) on GitHub.
