# Staged Update Run Rollout

While users rely on the `RollingUpdate` rollout strategy to safely roll out their workloads, 
there is also a requirement for a staged rollout mechanism at the cluster level to enable more controlled and systematic continuous delivery (CD) across the fleet.
Introducing a staged update run feature would address this need by enabling gradual deployments, reducing risk, and ensuring greater reliability and consistency in workload updates across clusters.

![](updaterun.jpg)

## Overview

We introduce two new Custom Resources, `ClusterStagedUpdateStrategy` and `ClusterStagedUpdateRun`. 

`ClusterStagedUpdateStrategy` defines a reusable orchestration pattern that organizes member clusters into distinct stages, controlling both the rollout sequence within each stage and incorporating post-stage validation tasks that must succeed before proceeding to subsequent stages. For brevity, we'll refer to `ClusterStagedUpdateStrategy` as _updateRun strategy_ throughout this document.

`ClusterStagedUpdateRun` orchestrates resource deployment across clusters by executing a `ClusterStagedUpdateStrategy`. It requires three key inputs: the target `ClusterResourcePlacement` name, a resource snapshot index specifying the version to deploy, and the strategy name that defines the rollout rules. The term _updateRun_ will be used to represent `ClusterStagedUpdateRun` in this document.

## Specify Rollout Strategy for ClusterResourcePlacement

While `ClusterResourcePlacement` uses `RollingUpdate` as its default strategy, switching to staged updates requires setting the rollout strategy to `External`:
```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: example-placement
spec:
  resourceSelectors:
    - group: ""
      kind: Namespace
      name: test-namespace
      version: v1
  policy:
    placementType: PickAll
    tolerations:
      - key: gpu-workload
        operator: Exists
  strategy:
    type: External # specify External here to use the stagedUpdateRun strategy.
```

## Deploy a ClusterStagedUpdateStrategy

The `ClusterStagedUpdateStrategy` custom resource enables users to organize member clusters into stages and define their rollout sequence. This strategy is reusable across multiple updateRuns, with each updateRun creating an immutable snapshot of the strategy at startup. This ensures that modifications to the strategy do not impact any in-progress updateRun executions.

An example `ClusterStagedUpdateStrategy` looks like below:
```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterStagedUpdateStrategy
metadata:
  name: example-strategy
spec:
  stages:
    - name: staging
      labelSelector:
        matchLabels:
          environment: staging
      afterStageTasks:
        - type: TimedWait
          waitTime: 1h
    - name: canary
      labelSelector:
        matchLabels:
          environment: canary
      afterStageTasks:
        - type: Approval
    - name: production
      labelSelector:
        matchLabels:
          environment: production
      sortingLabelKey: order
      afterStageTasks:
        - type: Approval
        - type: TimedWait
          waitTime: 1h
```

`ClusterStagedUpdateStrategy` is cluster-scoped resource. Its spec contains a list of `stageConfig` entries defining the configuration for each stage.
Stages execute sequentially in the order specified. Each stage must have a unique name and uses a labelSelector to identify member clusters for update. In above example, we define 3 stages: `staging` selecting member clusters labeled with `environment: staging`, `canary` selecting member clusters labeled with `environment: canary` and `production` selecting member clusters labeled with `environment: production`. 

Each stage can optionally specify `sortingLabelKey` and `afterStageTasks`. `sortingLabelKey` is used to define a label whose integer value determines update sequence within a stage. With above example, assuming there are 3 clusters selected in the `production` (all 3 clusters have `environment: production` label), then the fleet admin can label them with `order: 1`, `order: 2`, and `order: 3` respectively to control the rollout sequence. Without `sortingLabelKey`, clusters are updated in alphabetical order by name.

By default, the next stage begins immediately after the current stage completes. A user can control this cross-stage behavior by specifying the `afterStageTasks` in each stage. These tasks execute after all clusters in a stage update successfully. We currently support two types of tasks: `Approval` and `Timedwait`. Each stage can include one task of each type (maximum of two tasks). Both tasks must be satisfied before advancing to the next stage.

`Timedwait` task requires a specified waitTime duration. The updateRun waits for the duration to pass before executing the next stage. For `Approval` task, the controller generates a `ClusterApprovalRequest` object automatically named as `<updateRun name>-<stage name>`. The name is also shown in the updateRun status. The `ClusterApprovalRequest` object is pretty simple:
```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterApprovalRequest
metadata:
  name: example-run-canary
  labels:
    kubernetes-fleet.io/targetupdaterun: example-run
    kubernetes-fleet.io/targetUpdatingStage: canary
    kubernetes-fleet.io/isLatestUpdateRunApproval: "true"
spec:
  parentStageRollout: example-run
  targetStage: canary
```

The user then need to manually approve the task by patching its status:
```bash
kubectl patch clusterapprovalrequests example-run-canary --type='merge' -p '{"status":{"conditions":[{"type":"Approved","status":"True","reason":"lgtm","message":"lgtm","lastTransitionTime":"'$(date --utc +%Y-%m-%dT%H:%M:%SZ)'","observedGeneration":1}]}}' --subresource=status
```
The updateRun will only continue to next stage after the `ClusterApprovalRequest` is approved.

## Trigger rollout with ClusterStagedUpdateRun

When using `External` rollout strategy, a `ClusterResourcePlacement` begins deployment only when triggered by a `ClusterStagedUpdateRun`. An example `ClusterStagedUpdateRun` is shown below:
```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterStagedUpdateRun
metadata:
  name: example-run
spec:
  placementName: example-placement
  resourceSnapshotIndex: "0"
  stagedRolloutStrategyName: example-strategy
```
This cluster-scoped resource requires three key parameters: the `placementName` specifying the target `ClusterResourcePlacement`, the `resourceSnapshotIndex` identifying which version of resources to deploy (learn how to find resourceSnapshotIndex [here](../../howtos/updaterun.md)), and the `stagedRolloutStrategyName` indicating the `ClusterStagedUpdateStrategy` to follow.

An updateRun executes in two phases. During the initialization phase, the controller performs a one-time setup where it captures a snapshot of the updateRun strategy, collects scheduled and to-be-deleted `ClusterResourceBindings`, generates the cluster update sequence, and records all this information in the updateRun status.

In the execution phase, the controller processes each stage sequentially, updates clusters within each stage one at a time, and enforces completion of after-stage tasks. It then executes a final delete stage to clean up resources from unscheduled clusters. The updateRun succeeds when all stages complete successfully. However, it will fail if any execution-affecting events occur, for example, the target ClusterResourcePlacement being deleted, and member cluster changes triggering new scheduling. In such cases, error details are recorded in the updateRun status. Remember that once initialized, an updateRun operates on its strategy snapshot, making it immune to subsequent strategy modifications.
