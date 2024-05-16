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

We currently only support the `RollingUpdate` rollout strategy. It updates the resources in the selected target clusters
gradually based on the 'maxUnavailable' and 'maxSurge' settings.

## In place update policy

We always try to do in-place update if there is no change in the selected clusters. This is to avoid unnecessary
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

`MaxSurge` specifies the maximum number of clusters that can be scheduled with resources above the `target number of 
clusters` specified in `ClusterResourcePlacement` policy.

`MaxSurge` only applies to rollouts to newly scheduled clusters, and doesn't apply to rollouts of workload triggered by 
changes in selected resources. For updates to selected resources, we always try to do the updates in place with no surge.

`target number of clusters` changes based on the `ClusterResourcePlacement` policy.

- For PickAll, it's the number of clusters picked by the scheduler.
- For PickN, it's the number of clusters specified in the `ClusterResourcePlacement` policy.
- For PickFixed, it's the length of the list of cluster names specified in the `ClusterResourcePlacement` policy.

### UnavailablePeriodSeconds

`UnavailablePeriodSeconds` is used to configure the waiting time between rollout phases when we cannot determine if the 
resources have rolled out successfully or not. This field is used only if the availability of resources we propagate 
are not trackable.

## Availability based Rollout
We have built-in mechanisms to determine the availability of some common Kubernetes native resources. We only mark them 
as available in the target clusters when they meet the criteria we defined.

### How It Works
We have an agent running in the target cluster to check the status of the resources. We have specific criteria for each 
of the following resources to determine if they are available or not. Here are the list of resources we support:

#### Deployment
We only mark a `Deployment` as available when all its pods are running, ready and updated according to the latest spec. 

#### DaemonSet 
We only mark a `DaemonSet` as available when all its pods are running, ready and updated according to the latest spec.

#### StatefulSet
We only mark a `StatefulSet` as available when all its pods are running, ready and updated according to the latest revision.

#### Job
We only mark a `Job` as available when it has at least one succeeded pod or one ready pod.

#### Service
For `Service` based on the service type the availability is determined as follows:

- For `ClusterIP` & `NodePort` service, we mark it as available when a cluster IP is assigned.
- For `LoadBalancer` service, we mark it as available when a `LoadBalancerIngress` has been assigned along with an IP or Hostname.
- For `ExternalName` service, we don't know how to determine the availability.

#### Data only objects

For the objects described below since they are a data resource we mark them as available immediately after creation,

- Namespace
- Secret
- ConfigMap
- Role
- ClusterRole
- RoleBinding
- ClusterRoleBinding