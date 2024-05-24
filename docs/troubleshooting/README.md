# Troubleshooting guide

## Overview:

This TSG is meant to help you troubleshoot issues with the Fleet APIs.

## Cluster Resource Placement:

Internal Objects to keep in mind when troubleshooting CRP related errors on the hub cluster:
 - `ClusterResourceSnapshot`
 - `ClusterSchedulingPolicySnapshot`
 - `ClusterResourceBinding`
 - `Work`

Please read the API reference for more details about each object https://github.com/Azure/fleet/blob/main/docs/api-references.md.
____
The order in which the conditions are updated is important for understanding the status of a cluster resource placement and failures encountered. 
The order is as follows: 
1. `ClusterResourcePlacementScheduled` condition is updated to indicate that a resource has been scheduled for placement. 
    - If this condition is false, refer to [How can I debug when my CRP status is ClusterResourcePlacementScheduled condition status is set to false?](./clusterResourcePlacementScheduled.md). 
2. `ClusterResourcePlacementRolloutStarted` condition is updated to indicate that the rollout process has begun. 
   - If this condition is false refer to [How can I debug when my CRP status is ClusterResourcePlacementRolloutStarted condition status is set to false?](./clusterResourcePlacementRolloutStarted.md)
3. `ClusterResourcePlacementOverridden` condition is updated to indicate that the resource has been overridden. 
   - If this condition is false, refer to [How can I debug when my CRP status is ClusterResourcePlacementOverridden condition status is set to false?](./clusterResourcePlacementOverridden.md)
4. `ClusterResourcePlacementWorkSynchronized` condition is updated to indicate that the work objects have been synchronized. 
   - If this condition is false, refer to [How can I debug when my CRP status is ClusterResourcePlacementWorkSynchronized condition status is set to false?](./clusterResourcePlacementWorkSynchronized.md)
5. `ClusterResourcePlacementApplied` condition is updated to indicate that the resource has been applied. 
   - If this condition is false, refer to [How can I debug when my CRP status is ClusterResourcePlacementApplied condition is set to false?](./clusterResourcePlacementApplied.md)
6. `ClusterResourcePlacementAvailable` condition is updated to indicate that the resource is available. 
   - If this condition is false, refer to [How can I debug when my CRP status is ClusterResourcePlacementAvailable condition status is set to false?](./clusterResourcePlacementAvailable.md)

___
## How can I debug when some clusters are not selected as expected?

Check the status of the `ClusterSchedulingPolicySnapshot` to determine which clusters were selected along with the reason.

## How can I debug when a selected cluster does not have the expected resources on it or if CRP doesn't pick up the latest changes?

Please check the following cases,
- Check to see if `ClusterResourcePlacementRolloutStarted` condition in CRP status is set to `true` or `false`.
- If it's set to `false` check this [question](#how-can-i-debug-when-my-crp-status-is-clusterresourceplacementrolloutstarted-condition-status-is-set-to-false).
- If it's set to `true`,
  - Check to see if `ClusterResourcePlacementApplied` condition is set to `unknown`, `false` or `true`.
  - If it's set to `unknown`, please wait as the resources are still being applied to the member cluster (if it's stuck in unknown state for a while, please raise a github issue as it's an unexpected behavior).
  - If it's set to `false`, check this [question](#how-can-i-debug-when-my-crp-clusterresourceplacementapplied-condition-is-set-to-false).
  - If it's set to `true`, check to see if the resource exists on the hub cluster.

We can also take a look at the `placementStatuses` section in `ClusterResourcePlacement` status for that particular cluster. In `placementStatuses` we would find `failedPlacements` section which should have the reasons as to why resources failed to apply.

## How to find & verify the latest ClusterSchedulingPolicySnapshot for a CRP?

We need to have `ClusterResourcePlacement` name `{CRPName}`, replace `{CRPName}` in the command below,

```
kubectl get clusterschedulingpolicysnapshot -l kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP={CRPName}
```

- Compare `ClusterSchedulingPolicySnapshot` with the `ClusterResourcePlacement` policy to ensure they match (excluding `numberOfClusters` field from `ClusterResourcePlacement` spec).
- If placement type is `PickN`, check if the number of clusters requested in `ClusterResourcePlacment` placement policy matches the value for the label called `number-of-clusters`.

## How to find the latest ClusterResourceBinding resource?

We need to have `ClusterResourcePlacement` name `{CRPName}`, replace `{CRPName} `in the command below. The command below lists all `ClusterResourceBindings` associated with `ClusterResourcePlacement`,

```
kubectl get clusterresourcebinding -l kubernetes-fleet.io/parent-CRP={CRPName}
```

### Example:

In this case we have `ClusterResourcePlacement` called test-crp,

```
kubectl get crp test-crp
NAME       GEN   SCHEDULED   SCHEDULEDGEN   APPLIED   APPLIEDGEN   AGE
test-crp   1     True        1              True      1            15s
```

From the `placementStatuses` section of the `test-crp` status, we can observe that it has propagated resources to two member clusters and hence has two `ClusterResourceBindings`,

```
status:
  conditions:
  - lastTransitionTime: "2023-11-23T00:49:29Z"
    ...
  placementStatuses:
  - clusterName: kind-cluster-1
    conditions:
      ...
      type: ResourceApplied
  - clusterName: kind-cluster-2
    conditions:
      ...
      reason: ApplySucceeded
      status: "True"
      type: ResourceApplied
```

Output we receive after running the command listed above to get the `ClusterResourceBindings`,

```
kubectl get clusterresourcebinding -l kubernetes-fleet.io/parent-CRP=test-crp 
NAME                               WORKCREATED   RESOURCESAPPLIED   AGE
test-crp-kind-cluster-1-be990c3e   True          True               33s
test-crp-kind-cluster-2-ec4d953c   True          True               33s
```

The `ClusterResourceBinding` name follows this format `{CRPName}-{clusterName}-{suffix}`, so once we have all ClusterResourceBindings listed find the `ClusterResourceBinding` for the target cluster you are looking for based on the `clusterName`.

## How to find the latest ClusterResourceSnapshot resource?

Replace `{CRPName}` in the command below with name of `ClusterResourcePlacement`,

```
kubectl get clusterresourcesnapshot -l kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP={CRPName}
```

## How and where to find the correct Work resource?

We need to have the member cluster's namespace which follow this format `fleet-member-{clusterName}` and `ClusterResourcePlacement` name `{CRPName}`.

```
kubectl get work -n fleet-member-{clusterName} -l kubernetes-fleet.io/parent-CRP={CRPName}
```
