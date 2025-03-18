# Safe Rollout

One of the most important features of Fleet is the ability to safely rollout changes across multiple clusters. We do
this by rolling out the changes in a controlled manner, ensuring that we only continue to propagate the changes to the
next target clusters if the resources are successfully applied to the previous target clusters.

## Overview

We automatically propagate any resource changes that are selected by a `ClusterResourcePlacement` from the hub cluster 
to the target clusters based on the placement policy defined in the `ClusterResourcePlacement`. In order to reduce the
blast radius of such operation, we provide users a way to safely rollout the new changes so that a bad release 
won't affect all the running instances all at once.

## Rollout Strategy

We currently support the `RollingUpdate` rollout strategy. It updates the resources in the selected target clusters
gradually based on the `maxUnavailable` and `maxSurge` settings.

## In place update policy

We always try to do in-place update by respecting the rollout strategy if there is no change in the placement. This is to avoid unnecessary
interrupts to the running workloads when there is only resource changes. For example, if you only change the tag of the
deployment in the namespace you want to place, we will do an in-place update on the deployments already placed on the 
targeted cluster instead of moving the existing deployments to other clusters even if the labels or properties of the 
current clusters are not the best to match the current placement policy.

## How To Use RollingUpdateConfig

RolloutUpdateConfig is used to control behavior of the rolling update strategy.

### MaxUnavailable and MaxSurge

`MaxUnavailable` specifies the maximum number of connected clusters to the fleet compared to `target number of clusters` 
specified in `ClusterResourcePlacement` policy in which resources propagated by the `ClusterResourcePlacement` can be 
unavailable. Minimum value for `MaxUnavailable` is set to 1 to avoid stuck rollout during in-place resource update.

`MaxSurge` specifies the maximum number of clusters that can be scheduled with resources above the `target number of clusters` 
specified in `ClusterResourcePlacement` policy.

> **Note:** `MaxSurge` only applies to rollouts to newly scheduled clusters, and doesn't apply to rollouts of workload triggered by 
updates to already propagated resource. For updates to already propagated resources, we always try to do the updates in 
place with no surge.

`target number of clusters` changes based on the `ClusterResourcePlacement` policy.

- For PickAll, it's the number of clusters picked by the scheduler.
- For PickN, it's the number of clusters specified in the `ClusterResourcePlacement` policy.
- For PickFixed, it's the length of the list of cluster names specified in the `ClusterResourcePlacement` policy.

#### Example 1:

Consider a fleet with 4 connected member clusters (cluster-1, cluster-2, cluster-3 & cluster-4) where every member 
cluster has label `env: prod`. The hub cluster has a namespace called `test-ns` with a deployment in it.

The `ClusterResourcePlacement` spec is defined as follows:

```yaml
spec:
  resourceSelectors:
    - group: ""
      kind: Namespace
      version: v1
      name: test-ns
  policy:
    placementType: PickN
    numberOfClusters: 3
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  env: prod
  strategy:
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
```

The rollout will be as follows:

- We try to pick 3 clusters out of 4, for this scenario let's say we pick cluster-1, cluster-2 & cluster-3.
- Since we can't track the initial availability for the deployment, we rollout the namespace with deployment to 
cluster-1, cluster-2 & cluster-3.

- Then we update the deployment with a bad image name to update the resource in place on cluster-1, cluster-2 & cluster-3.

- But since we have `maxUnavailable` set to 1, we will rollout the bad image name update for deployment to one of the clusters 
(which cluster the resource is rolled out to first is non-deterministic).

- Once the deployment is updated on the first cluster, we will wait for the deployment's availability to be true before 
rolling out to the other clusters
- And since we rolled out a bad image name update for the deployment it's availability will always be false and hence the 
rollout for the other two clusters will be stuck
- Users might think `maxSurge` of 1 might be utilized here but in this case since we are updating the resource in place
`maxSurge` will not be utilized to surge and pick cluster-4.

> **Note:** `maxSurge` will be utilized to pick cluster-4, if we change the policy to pick 4 cluster or change placement 
type to `PickAll`.

#### Example 2:

Consider a fleet with 4 connected member clusters (cluster-1, cluster-2, cluster-3 & cluster-4) where,

- cluster-1 and cluster-2 has label `loc: west`
- cluster-3 and cluster-4 has label `loc: east`

The hub cluster has a namespace called `test-ns` with a deployment in it.

Initially, the `ClusterResourcePlacement` spec is defined as follows:

```yaml
spec:
  resourceSelectors:
    - group: ""
      kind: Namespace
      version: v1          
      name: test-ns
  policy:
    placementType: PickN
    numberOfClusters: 2
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
              - labelSelector:
                  matchLabels:
                    loc: west
  strategy:
    rollingUpdate:
      maxSurge: 2
```

The rollout will be as follows:
- We try to pick clusters (cluster-1 and cluster-2) by specifying the label selector `loc: west`.
- Since we can't track the initial availability for the deployment, we rollout the namespace with deployment to cluster-1
and cluster-2 and wait till they become available.

Then we update the `ClusterResourcePlacement` spec to the following:

```yaml
spec:
  resourceSelectors:
    - group: ""
      kind: Namespace
      version: v1          
      name: test-ns
  policy:
    placementType: PickN
    numberOfClusters: 2
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
              - labelSelector:
                  matchLabels:
                    loc: east
  strategy:
    rollingUpdate:
      maxSurge: 2
```

The rollout will be as follows:

- We try to pick clusters (cluster-3 and cluster-4) by specifying the label selector `loc: east`.
- But this time around since we have `maxSurge` set to 2 we are saying we can propagate resources to a maximum of 
4 clusters but our target number of clusters specified is 2, we will rollout the namespace with deployment to both 
cluster-3 and cluster-4 before removing the deployment from cluster-1 and cluster-2. 
- And since `maxUnavailable` is always set to 25% by default which is rounded off to 1, we will remove the 
resource from one of the existing clusters (cluster-1 or cluster-2) because when `maxUnavailable` is 1 the policy 
mandates at least one cluster to be available.

### UnavailablePeriodSeconds

`UnavailablePeriodSeconds` is used to configure the waiting time between rollout phases when we cannot determine if the 
resources have rolled out successfully or not. This field is used only if the availability of resources we propagate 
are not trackable. Refer to the [How It Works](#how-it-works) section for more details on a list of resources we can track.

## Availability based Rollout
We have built-in mechanisms to determine the availability of some common Kubernetes native resources. We only mark them 
as available in the target clusters when they meet the criteria we defined.

### How It Works
We have an agent running in the target cluster to check the status of the resources. We have specific criteria for each 
of the following resources to determine if they are available or not. Here are the list of resources we support:

#### Deployment/DaemonSet/StatefulSet
We only mark them as available if the number of updated replicas equals the number of desired replicas.

#### Service
For `Service` based on the service type the availability is determined as follows:

- For `ClusterIP` & `NodePort` service, we mark it as available when a cluster IP is assigned.
- For `LoadBalancer` service, we mark it as available when a `LoadBalancerIngress` has been assigned along with an IP or Hostname.
- For `ExternalName` service, checking availability is not supported, so it will be marked as available with a "not trackable" reason.


#### Custom Resource Definition
For `CustomResourceDefinition` availability is determined as follows:
- We mark as available when `Established` condition is set to true and `NamesAccepted` condition is set to true.


### Pod Disruption Budget
For `PodDisruptionBudget` based on the `DisruptionsAllowed` field the availability is determined as follows:
- For `DisruptionsAllowed` > 0 , we mark it as available when condition `DisruptionAllowed` is set to true and the reason
  is `SufficientPods`.
- Otherwise, we mark it as available when condition `DisruptionAllowed` is set to false and the reason
  is `InsufficientPodsReason`.


#### Service Export
The availability of a `ServiceExport` is determined based on the `networking.fleet.azure.com/weight` annotation as follows:
- For annotation weight == 0, we mark as available if the `ServiceExportValid` condition is true (will be false if annotation value is invalid).
- For annotation weight > 0, we mark as available if the `ServiceExportValid` condition is true and the `ServiceExportConflict` condition is false.

#### Data only objects

For the objects described below since they are a data resource we mark them as available immediately after creation:

- Namespace
- Secret
- ConfigMap
- Role
- ClusterRole
- RoleBinding
- ClusterRoleBinding
- NetworkPolicy
- CSIDriver
- CSINode
- StorageClass
- CSIStorageCapacity
- ControllerRevision
- IngressClass
- LimitRange
- ResourceQuota
- PriorityClass
