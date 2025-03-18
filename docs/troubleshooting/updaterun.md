# Staged Update Run Troubleshooting Guide

This guide provides troubleshooting steps for common issues related to Staged Update Run.

## CRP status without Staged Update Run

When a `ClusterResourcePlacement` is created with `spec.strategy.type` set to `External`, the rollout does not start immediately.

A sample status of such `ClusterResourcePlacement` is as follows:

```bash
$ kubectl describe crp example-placement
...
Status:
  Conditions:
    Last Transition Time:   2025-03-12T23:01:32Z
    Message:                found all cluster needed as specified by the scheduling policy, found 2 cluster(s)
    Observed Generation:    1
    Reason:                 SchedulingPolicyFulfilled
    Status:                 True
    Type:                   ClusterResourcePlacementScheduled
    Last Transition Time:   2025-03-12T23:01:32Z
    Message:                There are still 2 cluster(s) in the process of deciding whether to roll out the latest resources or not
    Observed Generation:    1
    Reason:                 RolloutStartedUnknown
    Status:                 Unknown
    Type:                   ClusterResourcePlacementRolloutStarted
  Observed Resource Index:  0
  Placement Statuses:
    Cluster Name:  member1
    Conditions:
      Last Transition Time:  2025-03-12T23:01:32Z
      Message:               Successfully scheduled resources for placement in "member1" (affinity score: 0, topology spread score: 0): picked by scheduling policy
      Observed Generation:   1
      Reason:                Scheduled
      Status:                True
      Type:                  Scheduled
      Last Transition Time:  2025-03-12T23:01:32Z
      Message:               In the process of deciding whether to roll out the latest resources or not
      Observed Generation:   1
      Reason:                RolloutStartedUnknown
      Status:                Unknown
      Type:                  RolloutStarted
    Cluster Name:            member2
    Conditions:
      Last Transition Time:  2025-03-12T23:01:32Z
      Message:               Successfully scheduled resources for placement in "member2" (affinity score: 0, topology spread score: 0): picked by scheduling policy
      Observed Generation:   1
      Reason:                Scheduled
      Status:                True
      Type:                  Scheduled
      Last Transition Time:  2025-03-12T23:01:32Z
      Message:               In the process of deciding whether to roll out the latest resources or not
      Observed Generation:   1
      Reason:                RolloutStartedUnknown
      Status:                Unknown
      Type:                  RolloutStarted
  Selected Resources:
    ...
Events:         <none>
```

`SchedulingPolicyFulfilled` condition indicates the CRP has been fully scheduled, while `RolloutStartedUnknown` condition shows that the rollout has not started.

In the `Placement Statuses` section, it displays the detailed status of each cluster. Both selected clusters are in the `Scheduled` state, but the `RolloutStarted` condition is still `Unknown` because the rollout has not kicked off yet.

## Investigate ClusterStagedUpdateRun initialization failure

An updateRun initialization failure can be easily detected by getting the resource:
```bash
$ kubectl get crsur example-run 
NAME          PLACEMENT           RESOURCE-SNAPSHOT-INDEX   POLICY-SNAPSHOT-INDEX   INITIALIZED   SUCCEEDED   AGE
example-run   example-placement   1                         0                       False                     2s
```
The `INITIALIZED` field is `False`, indicating the initialization failed.

Describe the updateRun to get more details:
```bash
$ kubectl describe crsur example-run
...
Status:
  Conditions:
    Last Transition Time:  2025-03-13T07:28:29Z
    Message:               cannot continue the ClusterStagedUpdateRun: failed to initialize the clusterStagedUpdateRun: failed to process the request due to a client error: no clusterResourceSnapshots with index `1` found for clusterResourcePlacement `example-placement`
    Observed Generation:   1
    Reason:                UpdateRunInitializedFailed
    Status:                False
    Type:                  Initialized
  Deletion Stage Status:
    Clusters:
    Stage Name:                   kubernetes-fleet.io/deleteStage
  Policy Observed Cluster Count:  2
  Policy Snapshot Index Used:     0
...
```
The condition clearly indicates the initialization failed. And the condition message gives more details about the failure. 
In this case, I used a not-existing resource snapshot index `1` for the updateRun.

## Investigate ClusterStagedUpdateRun rollout stuck

A `ClusterStagedUpdateRun` can get stuck when resource placement fails on some clusters. Describing the updateRun will show some cluster is stuck in `ClusterUpdatingStarted` condition:
```bash
$ date; kubectl describe crsur example-run
Thu Mar 13 07:44:28 UTC 2025
...
Stages Status:
    After Stage Task Status:
      Approval Request Name:  example-run-staging
      Type:                   Approval
      Type:                   TimedWait
    Clusters:
      Cluster Name:  member1
      Conditions:
        Last Transition Time:  2025-03-13T07:37:36Z
        Message:               
        Observed Generation:   1
        Reason:                ClusterUpdatingStarted
        Status:                True
        Type:                  Started
    Conditions:
      Last Transition Time:  2025-03-13T07:37:36Z
      Message:               
      Observed Generation:   1
      Reason:                StageUpdatingStarted
      Status:                True
      Type:                  Progressing
    Stage Name:              staging
    Start Time:              2025-03-13T07:37:36Z
```

As you can see, the cluster updating has started about 7 minutes ago, but the `ClusterUpdatingSucceeded` condition is still not set yet.
This usually indicates something wrong happened on the cluster. To further investigate, you can check the `ClusterResourcePlacement` status:
```bash
$ kubectl describe crp example-placement
...
Placement Statuses:
    Cluster Name:  member1
    Conditions:
      Last Transition Time:  2025-03-12T23:01:32Z
      Message:               Successfully scheduled resources for placement in "member1" (affinity score: 0, topology spread score: 0): picked by scheduling policy
      Observed Generation:   1
      Reason:                Scheduled
      Status:                True
      Type:                  Scheduled
      Last Transition Time:  2025-03-13T07:37:36Z
      Message:               Detected the new changes on the resources and started the rollout process, resourceSnapshotIndex: 3, clusterStagedUpdateRun: example-run
      Observed Generation:   1
      Reason:                RolloutStarted
      Status:                True
      Type:                  RolloutStarted
      Last Transition Time:  2025-03-13T07:37:36Z
      Message:               No override rules are configured for the selected resources
      Observed Generation:   1
      Reason:                NoOverrideSpecified
      Status:                True
      Type:                  Overridden
      Last Transition Time:  2025-03-13T07:37:36Z
      Message:               All of the works are synchronized to the latest
      Observed Generation:   1
      Reason:                AllWorkSynced
      Status:                True
      Type:                  WorkSynchronized
      Last Transition Time:  2025-03-13T07:37:39Z
      Message:               Work object example-placement-work has failed to apply
      Observed Generation:   1
      Reason:                NotAllWorkHaveBeenApplied
      Status:                False
      Type:                  Applied
    Failed Placements:
      Condition:
        Last Transition Time:  2025-03-13T07:37:36Z
        Message:               Manifest is trackable but not available yet
        Observed Generation:   1
        Reason:                ManifestNotAvailableYet
        Status:                False
        Type:                  Available
      Group:                   apps
      Kind:                    Deployment
      Name:                    nginx
      Namespace:               test-namespace
      Version:                 v1
      Condition:
        Last Transition Time:  2025-03-13T07:37:39Z
        Message:               Failed to apply manifest: failed to process the request due to a client error: resource exists and is not managed by the fleet controller and co-ownernship is disallowed
        Reason:                ManifestsAlreadyOwnedByOthers
        Status:                False
        Type:                  Applied
      Group:                   apps
      Kind:                    ReplicaSet
      Name:                    nginx-7b6df7c758
      Namespace:               test-namespace
      Version:                 v1
...
```

The `Applied` condition is `False` and says not all work have been applied. And in the "failed placements" section, it shows the detailed failure. For more debugging instructions, you can refer to [CRP troubleshooting guide](./README.md).

After resolving the issue, you can create always create a new updateRun to restart the rollout. Stuck updateRuns can be deleted.