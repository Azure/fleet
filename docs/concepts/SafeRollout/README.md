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

### MaxUnavailable and MaxSurge

### UnavailablePeriodSeconds


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
We only mark a `StatefulSet` as available when all its pods are running, ready and updated according to the latest spec.

#### Job

#### Service

#### Data only objects