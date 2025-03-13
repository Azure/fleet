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

## Understand ClusterStagedUpdateRun status

Let's take a deep look into the status of a completed `ClusterStagedUpdateRun`. It displays details about the rollout status for every clusters and stages.

```bash
$ kubectl describe crsur run example-run
...
Status:
  Conditions:
    Last Transition Time:  2025-03-12T23:21:39Z
    Message:               ClusterStagedUpdateRun initialized successfully
    Observed Generation:   1
    Reason:                UpdateRunInitializedSuccessfully
    Status:                True
    Type:                  Initialized
    Last Transition Time:  2025-03-12T23:21:39Z
    Message:               
    Observed Generation:   1
    Reason:                UpdateRunStarted
    Status:                True
    Type:                  Progressing
    Last Transition Time:  2025-03-12T23:26:15Z
    Message:               
    Observed Generation:   1
    Reason:                UpdateRunSucceeded
    Status:                True
    Type:                  Succeeded
  Deletion Stage Status:
    Clusters:
    Conditions:
      Last Transition Time:       2025-03-12T23:26:15Z
      Message:                    
      Observed Generation:        1
      Reason:                     StageUpdatingStarted
      Status:                     True
      Type:                       Progressing
      Last Transition Time:       2025-03-12T23:26:15Z
      Message:                    
      Observed Generation:        1
      Reason:                     StageUpdatingSucceeded
      Status:                     True
      Type:                       Succeeded
    End Time:                     2025-03-12T23:26:15Z
    Stage Name:                   kubernetes-fleet.io/deleteStage
    Start Time:                   2025-03-12T23:26:15Z
  Policy Observed Cluster Count:  2
  Policy Snapshot Index Used:     0
  Staged Update Strategy Snapshot:
    Stages:
      After Stage Tasks:
        Type:       Approval
        Wait Time:  0s
        Type:       TimedWait
        Wait Time:  1m0s
      Label Selector:
        Match Labels:
          Environment:  staging
      Name:             staging
      After Stage Tasks:
        Type:       Approval
        Wait Time:  0s
      Label Selector:
        Match Labels:
          Environment:    canary
      Name:               canary
      Sorting Label Key:  name
      After Stage Tasks:
        Type:       TimedWait
        Wait Time:  1m0s
        Type:       Approval
        Wait Time:  0s
      Label Selector:
        Match Labels:
          Environment:    production
      Name:               production
      Sorting Label Key:  order
  Stages Status:
    After Stage Task Status:
      Approval Request Name:  example-run-staging
      Conditions:
        Last Transition Time:  2025-03-12T23:21:54Z
        Message:               
        Observed Generation:   1
        Reason:                AfterStageTaskApprovalRequestCreated
        Status:                True
        Type:                  ApprovalRequestCreated
        Last Transition Time:  2025-03-12T23:22:55Z
        Message:               
        Observed Generation:   1
        Reason:                AfterStageTaskApprovalRequestApproved
        Status:                True
        Type:                  ApprovalRequestApproved
      Type:                    Approval
      Conditions:
        Last Transition Time:  2025-03-12T23:22:54Z
        Message:               
        Observed Generation:   1
        Reason:                AfterStageTaskWaitTimeElapsed
        Status:                True
        Type:                  WaitTimeElapsed
      Type:                    TimedWait
    Clusters:
      Cluster Name:  member1
      Conditions:
        Last Transition Time:  2025-03-12T23:21:39Z
        Message:               
        Observed Generation:   1
        Reason:                ClusterUpdatingStarted
        Status:                True
        Type:                  Started
        Last Transition Time:  2025-03-12T23:21:54Z
        Message:               
        Observed Generation:   1
        Reason:                ClusterUpdatingSucceeded
        Status:                True
        Type:                  Succeeded
    Conditions:
      Last Transition Time:  2025-03-12T23:21:54Z
      Message:               
      Observed Generation:   1
      Reason:                StageUpdatingWaiting
      Status:                False
      Type:                  Progressing
      Last Transition Time:  2025-03-12T23:22:55Z
      Message:               
      Observed Generation:   1
      Reason:                StageUpdatingSucceeded
      Status:                True
      Type:                  Succeeded
    End Time:                2025-03-12T23:22:55Z
    Stage Name:              staging
    Start Time:              2025-03-12T23:21:39Z
    After Stage Task Status:
      Approval Request Name:  example-run-canary
      Conditions:
        Last Transition Time:  2025-03-12T23:23:10Z
        Message:               
        Observed Generation:   1
        Reason:                AfterStageTaskApprovalRequestCreated
        Status:                True
        Type:                  ApprovalRequestCreated
        Last Transition Time:  2025-03-12T23:25:15Z
        Message:               
        Observed Generation:   1
        Reason:                AfterStageTaskApprovalRequestApproved
        Status:                True
        Type:                  ApprovalRequestApproved
      Type:                    Approval
    Clusters:
      Cluster Name:  member2
      Conditions:
        Last Transition Time:  2025-03-12T23:22:55Z
        Message:               
        Observed Generation:   1
        Reason:                ClusterUpdatingStarted
        Status:                True
        Type:                  Started
        Last Transition Time:  2025-03-12T23:23:10Z
        Message:               
        Observed Generation:   1
        Reason:                ClusterUpdatingSucceeded
        Status:                True
        Type:                  Succeeded
    Conditions:
      Last Transition Time:  2025-03-12T23:23:10Z
      Message:               
      Observed Generation:   1
      Reason:                StageUpdatingWaiting
      Status:                False
      Type:                  Progressing
      Last Transition Time:  2025-03-12T23:25:15Z
      Message:               
      Observed Generation:   1
      Reason:                StageUpdatingSucceeded
      Status:                True
      Type:                  Succeeded
    End Time:                2025-03-12T23:25:15Z
    Stage Name:              canary
    Start Time:              2025-03-12T23:22:55Z
    After Stage Task Status:
      Conditions:
        Last Transition Time:  2025-03-12T23:26:15Z
        Message:               
        Observed Generation:   1
        Reason:                AfterStageTaskWaitTimeElapsed
        Status:                True
        Type:                  WaitTimeElapsed
      Type:                    TimedWait
      Approval Request Name:   example-run-production
      Conditions:
        Last Transition Time:  2025-03-12T23:25:15Z
        Message:               
        Observed Generation:   1
        Reason:                AfterStageTaskApprovalRequestCreated
        Status:                True
        Type:                  ApprovalRequestCreated
        Last Transition Time:  2025-03-12T23:25:25Z
        Message:               
        Observed Generation:   1
        Reason:                AfterStageTaskApprovalRequestApproved
        Status:                True
        Type:                  ApprovalRequestApproved
      Type:                    Approval
    Clusters:
    Conditions:
      Last Transition Time:  2025-03-12T23:25:15Z
      Message:               
      Observed Generation:   1
      Reason:                StageUpdatingWaiting
      Status:                False
      Type:                  Progressing
      Last Transition Time:  2025-03-12T23:26:15Z
      Message:               
      Observed Generation:   1
      Reason:                StageUpdatingSucceeded
      Status:                True
      Type:                  Succeeded
    End Time:                2025-03-12T23:26:15Z
    Stage Name:              production
Events:                      <none>
```

### UpdateRun overall status

At the very top, `Status.Conditions` gives the overall status of the updateRun. The execution an update run consists of two phases: initialization and execution.
During initialization, the controller performs a one-time setup where it captures a snapshot of the updateRun strategy, collects scheduled and to-be-deleted `ClusterResourceBindings`,
generates the cluster update sequence, and records all this information in the updateRun status. 
The `UpdateRunInitializedSuccessfully` condition indicates the initialization is successful.

After initialization, the controller starts executing the updateRun. The `UpdateRunStarted` condition indicates the execution has started.

After all clusters are updated, all after-stage tasks are completed, and thus all stages are finished, the `UpdateRunSucceeded` condition is set to `True`, indicating the updateRun has succeeded.

### Fields recorded in the updateRun status during initialization

During initialization, the controller records the following fields in the updateRun status:
- `PolicySnapshotIndexUsed`: the index of the policy snapshot used for the updateRun, it should be the latest one.
- `PolicyObservedClusterCount`: the number of clusters selected by the scheduling policy.
- `StagedUpdateStrategySnapshot`: the snapshot of the updateRun strategy, which ensures any strategy changes will not affect executing updateRuns.

### Stages and clusters status

The `Stages Status` section displays the status of each stage and cluster. As shown in the strategy snapshot, the updateRun has three stages: `staging`, `canary`, and `production`. During initialization, the controller generates the rollout plan, classifies the scheduled clusters
into these three stages and dumps the plan into the updateRun status. As the execution progresses, the controller updates the status of each stage and cluster. Take the `staging` stage as an example, `member1` is included in this stage. `ClusterUpdatingStarted` condition indicates the cluster is being updated and `ClusterUpdatingSucceeded` condition shows the cluster is updated successfully. 

After all clusters are updated in a stage, the controller executes the specified after-stage tasks. Stage `staging` has two after-stage tasks: `Approval` and `TimedWait`. The `Approval` task requires the admin to manually approve a `ClusterApprovalRequest` generated by the controller. The name of the `ClusterApprovalRequest` is also included in the status, which is `example-run-staging`. `AfterStageTaskApprovalRequestCreated` condition indicates the approval request is created and `AfterStageTaskApprovalRequestApproved` condition indicates the approval request has been approved. The `TimedWait` task enforces a suspension of the rollout until the specified wait time has elapsed and in this case, the wait time is 1 minute. `AfterStageTaskWaitTimeElapsed` condition indicates the wait time has elapsed and the rollout can proceed to the next stage.

Each stage also has its own conditions. When a stage starts, the `Progressing` condition is set to `True`. When all the cluster updates complete, the `Progressing` condition is set to `False` with reason `StageUpdatingWaiting` as shown above. It means the stage is waiting for
after-stage tasks to pass.
And thus the `lastTransitionTime` of the `Progressing` condition also serves as the start time of the wait in case there's a `TimedWait` task. When all after-stage tasks pass, the `Succeeded` condition is set to `True`. Each stage status also has `Start Time` and `End Time` fields, making it easier to read.

There's also a `Deletion Stage Status` section, which displays the status of the deletion stage. The deletion stage is the last stage of the updateRun. It deletes resources from the unscheduled clusters. The status is pretty much the same as a normal update stage, except that there are no after-stage tasks.

Note that all these conditions have `lastTransitionTime` set to the time when the controller updates the status. It can help debug and check
the progress of the updateRun. 

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