# Eviction & Placement Disruption Budget

This document explains the concept of `Eviction` and `Placement Disruption Budget` in the context of the fleet.

## Overview

`Eviction` pertains to the act of removing resources from a target cluster propagated by a resource placement object from the hub cluster.

The `Placement Disruption Budget` object protects against voluntary disruption, and in the case of the fleet, the only allowed voluntary disruption as of now is eviction.

## Eviction

An eviction object is used to remove resources from a member cluster once the resources have already been propagated from the hub cluster.

Once the resources are successfully removed or if eviction cannot be executed, the eviction object won't be reconciled again.

To successfully evict resources from a cluster, the user needs to specify:

- The name of the `ClusterResourcePlacement` object which propagated resources to the target cluster
- The name of the target cluster from which we need to evict resources

When specifying the `ClusterResourcePlacement` object in the eviction's spec, the user needs to consider the following cases:

- For `PickFixed` CRP, eviction is not allowed because if users wanted to remove resources from a cluster, they could choose to remove the cluster name from the `ClusterResourcePlacement` spec or pick a different cluster.
- For `PickAll` & `PickN` CRPs, eviction is allowed because the users cannot deterministically pick or unpick a cluster based on the placement strategy; it's up to the scheduler.

> **Note:** After an eviction is executed, there is no guarantee that the cluster won't be picked again by the scheduler to propagate resources for a `Placement` resource.
> The user needs to specify a taint on the cluster to prevent the scheduler from picking the cluster again.

## ClusterResourcePlacementDisruptionBudget

The `ClusterResourcePlacementDisruptionBudget` is used to protect resources propagated by a `ClusterResourcePlacement` to a target cluster from voluntary disruption, i.e., `ClusterResourcePlacementEviction`.

> **Note:** When specifying a `ClusterResourcePlacementDisruptionBudget`, the name should be the same as the `ClusterResourcePlacement` that it's trying to protect.

Users are allowed to specify one of two fields in the `ClusterResourcePlacementDisruptionBudget` spec since they are mutually exclusive:

- MaxUnavailable - specifies the maximum number of clusters in which a placement can be unavailable due to voluntary disruptions.
- MinAvailable - specifies the minimum number of clusters in which placements are available despite voluntary disruptions.

> **Note:** For both MaxUnavailable and MinAvailable, involuntary disruptions are not subject to the disruption budget but will still count against it.

When specifying a disruption budget for a particular `ClusterResourcePlacement`, the user needs to consider the following cases:

- For `PickFixed` CRP, whether a `ClusterResourcePlacementDisruptionBudget` is specified or not, if an eviction is carried out, the user will receive an invalid eviction error message in the eviction status.
- For `PickAll` CRP, if a `ClusterResourcePlacementDisruptionBudget` is specified and the `MaxUnavailable` field is set, the user will receive a misconfigured placement disruption budget error message in the eviction status because total number of clusters selected is non-deterministic.
- For `PickN` CRP, if a `ClusterResourcePlacementDisruptionBudget` is specified, the user can either set `MaxUnavailable` or `MinAvailable` field since the fields are mutually exclusive.