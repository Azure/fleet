# Eviction & Placement Disruption Budget

This document explains the concept of `Eviction` and `Placement Disruption Budget` in the context of the fleet.

## Overview

`Eviction` provides a way to force remove resources from a target cluster once the resources have already been propagated from the hub cluster by a `Placement` object. 
`Eviction` is considered as an voluntary disruption triggered by the user. `Eviction` alone doesn't guarantee that resources won't be propagated to target cluster again by the scheduler.
The users need to use [taints](../../howtos/taint-toleration.md) in conjunction with `Eviction` to prevent the scheduler from picking the target cluster again.

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

- MaxUnavailable - specifies the maximum number of clusters in which a placement can be unavailable due to any form of disruptions.
- MinAvailable - specifies the minimum number of clusters in which placements are available despite any form of disruptions.

for both `MaxUnavailable` and `MinAvailable`, the user can specify the number of clusters as an integer or as a percentage of the total number of clusters in the fleet.

> **Note:** For both MaxUnavailable and MinAvailable, involuntary disruptions are not subject to the disruption budget but will still count against it.

When specifying a disruption budget for a particular `ClusterResourcePlacement`, the user needs to consider the following cases:

| CRP type     | `MinAvailable` DB with an integer | `MinAvailable` DB with a percentage | `MaxUnavailable` DB with an integer | `MaxUnavailable` DB with a percentage |
|--------------|-----------------------------------|-------------------------------------|-------------------------------------|---------------------------------------|
| `PickFixed`  | ❌                                 | ❌                                   | ❌                                   | ❌                                |
| `PickAll`    | ✅                                 | ❌                                   | ❌                                   | ❌                                |
| `PickN`      | ✅                                 | ✅                                   | ✅                                   | ✅                                |

> **Note:** We don't allow eviction for `PickFixed` CRP and hence specifying a `ClusterResourcePlacementDisruptionBudget` for `PickFixed` CRP does nothing. 
> And for `PickAll` CRP, the user can only specify `MinAvailable` because total number of clusters selected by a `PickAll` CRP is non-deterministic.
> If the user creates an invalid `ClusterResourcePlacementDisruptionBudget` object, when an eviction is created, the eviction won't be successfully executed.