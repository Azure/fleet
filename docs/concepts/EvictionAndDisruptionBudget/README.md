# Eviction & Placement Disruption Budget

This document explains the concept of `Eviction` and `Placement Disruption Budget` in the context of the fleet.

## Overview

`Eviction` pertains to the act of removing resources from a target cluster propagated by a resource placement object from the hub cluster.

The `Placement Disruption Budget` object protects against voluntary disruptions.

The only voluntary disruption that can occur in the fleet is the eviction of resources from a target cluster which can be achieved by creating the `ClusterResourcePlacementEviction` object.

Some cases of involuntary disruptions in the context of fleet,
- The removal of resources from a member cluster by the scheduler due to scheduling policy changes.
- Users manually deleting workload resources running on a member cluster.
- Users manually deleting the `ClusterResourceBinding` object which is an internal resource the represents the placement of resources on a member cluster.
- Workloads failing to run properly on a member cluster due to misconfiguration or cluster related issues.

For all the cases of involuntary disruptions described above, the `Placement Disruption Budget` object does not protect against them.

## ClusterResourcePlacementEviction

An eviction object is used to remove resources from a member cluster once the resources have already been propagated from the hub cluster.

The eviction object is only reconciled once after which it reaches a terminal state. Below is the list of terminal states for `ClusterResourcePlacementEviction`,
- `ClusterResourcePlacementEviction` is valid and it's executed successfully.
- `ClusterResourcePlacementEviction` is invalid.
- `ClusterResourcePlacementEviction` is valid but it's not executed.

To successfully evict resources from a cluster, the user needs to specify:

- The name of the `ClusterResourcePlacement` object which propagated resources to the target cluster.
- The name of the target cluster from which we need to evict resources.

When specifying the `ClusterResourcePlacement` object in the eviction's spec, the user needs to consider the following cases:

- For `PickFixed` CRP, eviction is not allowed; it is recommended that one directly edit the list of target clusters on the CRP object.
- For `PickAll` & `PickN` CRPs, eviction is allowed because the users cannot deterministically pick or unpick a cluster based on the placement strategy; it's up to the scheduler.

> **Note:** After an eviction is executed, there is no guarantee that the cluster won't be picked again by the scheduler to propagate resources for a `ClusterResourcePlacement` resource.
> The user needs to specify a [taint](../../howtos/taint-toleration.md) on the cluster to prevent the scheduler from picking the cluster again. This is especially true for `PickAll ClusterResourcePlacement` because 
> the scheduler will try to propagate resources to all the clusters in the fleet.

## ClusterResourcePlacementDisruptionBudget

The `ClusterResourcePlacementDisruptionBudget` is used to protect resources propagated by a `ClusterResourcePlacement` to a target cluster from voluntary disruption, i.e., `ClusterResourcePlacementEviction`.

> **Note:** When specifying a `ClusterResourcePlacementDisruptionBudget`, the name should be the same as the `ClusterResourcePlacement` that it's trying to protect.

Users are allowed to specify one of two fields in the `ClusterResourcePlacementDisruptionBudget` spec since they are mutually exclusive:

- MaxUnavailable - specifies the maximum number of clusters in which a placement can be unavailable due to voluntary disruptions.
- MinAvailable - specifies the minimum number of clusters in which placements are available despite voluntary disruptions.

for both `MaxUnavailable` and `MinAvailable`, the user can specify the number of clusters as an integer or as a percentage of the total number of clusters in the fleet.

> **Note:** For both MaxUnavailable and MinAvailable, involuntary disruptions are not subject to the disruption budget but will still count against it.

When specifying a disruption budget for a particular `ClusterResourcePlacement`, the user needs to consider the following cases:

- For `PickFixed` CRP, whether a `ClusterResourcePlacementDisruptionBudget` is specified or not, if an eviction object is created, the user will receive an invalid eviction error message in the eviction status.
- For `PickAll` CRP, if the `ClusterResourcePlacementDisruptionBudget` is specified for the following cases, the user will receive a misconfigured placement disruption budget error message in the eviction status because total number of clusters selected is non-deterministic
  - If the `MaxUnavailable` field is set either as integer or as a percentage.
  - If the `MinAvailable` field is set as a percentage.
- For `PickN` CRP, if a `ClusterResourcePlacementDisruptionBudget` is specified, the user can either set `MaxUnavailable` or `MinAvailable` field as an integer or percentage since the fields are mutually exclusive.