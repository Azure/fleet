# How-to Guide: Using `ClusterStagedUpdateRun` for Rollout and Rollback

This how-to guide demonstrates how to use `ClusterStagedUpdateRun` to rollout resources to member clusters in a staged manner and rollback resources to a previous version.

## Prerequisite

`ClusterStagedUpdateRun` CR is used to deploy resources from hub cluster to member clusters with `ClusterResourcePlacement` (or CRP) in a stage by stage manner. This tutorial is based on a demo fleet environment with 3 member clusters: 
| cluster name | labels                      |
|--------------|-----------------------------|
| member1      | environment=canary, order=2 |
| member2      | environment=staging         |
| member3      | environment=canary, order=1 |

To demonstrate the rollout and rollback behavior, we create a demo namespace and a sample configmap with very simple data on the hub cluster. The namespace with configmap will be deployed to the member clusters.
```bash
kubectl create ns test-namespace
kubectl create cm test-cm --from-literal=key=value1 -n test-namespace
```

Now we create a `ClusterResourcePlacement` to deploy the resources:
```bash
kubectl apply -f - << EOF
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
  strategy:
    type: External
EOF
```
**Note that `spec.strategy.type` is set to `External` to allow rollout triggered with a ClusterStagedUpdateRun.**
Both clusters should be scheduled since we use the `PickAll` policy but at the moment no resource should be deployed on the member clusters because we haven't created a `ClusterStagedUpdateRun` yet. The CRP is not AVAILABLE yet.
```bash
kubectl get crp example-placement
NAME                GEN   SCHEDULED   SCHEDULED-GEN   AVAILABLE   AVAILABLE-GEN   AGE
example-placement   1     True        1                                           8s
```

## Check resource snapshot versions

Fleet keeps a list of resource snapshots for version control and audit, (for more details, please refer to [api-references](../api-references.md)).

To check current resource snapshots:
```bash
kubectl get clusterresourcesnapshots --show-labels
NAME                           GEN   AGE     LABELS
example-placement-0-snapshot   1     7m31s   kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP=example-placement,kubernetes-fleet.io/resource-index=0
```
We only have one version of the snapshot. It is the current latest (`kubernetes-fleet.io/is-latest-snapshot=true`) and has resource-index 0 (`kubernetes-fleet.io/resource-index=0`).

Now we modify the our configmap with a new value `value2`:
```bash
kubectl edit cm test-cm -n test-namespace

kubectl get configmap test-cm -n test-namespace -o yaml
apiVersion: v1
data:
  key: value2     # value updated here, old value: value1
kind: ConfigMap
metadata:
  creationTimestamp: ...
  name: test-cm
  namespace: test-namespace
  resourceVersion: ...
  uid: ...
```

It now shows 2 versions of resource snapshots with index 0 and 1 respectively:
```bash
kubectl get clusterresourcesnapshots --show-labels
NAME                           GEN   AGE    LABELS
example-placement-0-snapshot   1     17m    kubernetes-fleet.io/is-latest-snapshot=false,kubernetes-fleet.io/parent-CRP=example-placement,kubernetes-fleet.io/resource-index=0
example-placement-1-snapshot   1     2m2s   kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP=example-placement,kubernetes-fleet.io/resource-index=1
```

The `latest` label set to `example-placement-1-snapshot` which contains the latest configmap data:
```bash
kubectl get clusterresourcesnapshots example-placement-1-snapshot -o yaml
apiVersion: placement.kubernetes-fleet.io/v1
kind: ClusterResourceSnapshot
metadata:
  ...
  labels:
    kubernetes-fleet.io/is-latest-snapshot: "true"
    kubernetes-fleet.io/parent-CRP: example-placement
    kubernetes-fleet.io/resource-index: "1"
  name: example-placement-1-snapshot
  ...
spec:
  selectedResources:
  - apiVersion: v1
    kind: Namespace
    metadata:
      labels:
        kubernetes.io/metadata.name: test-namespace
      name: test-namespace
    spec:
      finalizers:
      - kubernetes
  - apiVersion: v1
    data:
      key: value2 # latest value: value2, old value: value1
    kind: ConfigMap
    metadata:
      name: test-cm
      namespace: test-namespace
```

## Deploy a ClusterStagedUpdateStrategy

A `ClusterStagedUpdateStrategy` defines the orchestration pattern that groups clusters into stages and specifies the rollout sequence.
It selects member clusters by labels. For our demonstration, we create one with two stages:
```bash
kubectl apply -f - << EOF
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
          waitTime: 1m
    - name: canary
      labelSelector:
        matchLabels:
          environment: canary
      sortingLabelKey: order
      afterStageTasks:
        - type: Approval
EOF
```

## Deploy a ClusterStagedUpdateRun to rollout latest change

A `ClusterStagedUpdateRun` executes the rollout of a `ClusterResourcePlacement` following a `ClusterStagedUpdateStrategy`. To trigger the staged update run for our CRP, we create a `ClusterStagedUpdateRun` specifying the CRP name, updateRun strategy name, and the latest resource snapshot index ("1"):
```bash
kubectl apply -f - << EOF
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterStagedUpdateRun
metadata:
  name: example-run
spec:
  placementName: example-placement
  resourceSnapshotIndex: "1"
  stagedRolloutStrategyName: example-strategy
EOF
```

The staged update run is initialized and running:
```bash
kubectl get crsur example-run
NAME          PLACEMENT           RESOURCE-SNAPSHOT   POLICY-SNAPSHOT   INITIALIZED   SUCCEEDED   AGE
example-run   example-placement   1                   0                 True                      44s
```

A more detailed look at the status:
```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterStagedUpdateRun
metadata:
  ...
  name: example-run
  ...
spec:
  placementName: example-placement
  resourceSnapshotIndex: "1"
  stagedRolloutStrategyName: example-strategy
status:
  conditions:
  - lastTransitionTime: ...
    message: ClusterStagedUpdateRun initialized successfully
    observedGeneration: 1
    reason: UpdateRunInitializedSuccessfully
    status: "True" # the updateRun is initialized successfully
    type: Initialized
  - lastTransitionTime: ...
    message: ""
    observedGeneration: 1
    reason: UpdateRunStarted
    status: "True"
    type: Progressing # the updateRun is still running
  deletionStageStatus:
    clusters: [] # no clusters need to be cleaned up
    stageName: kubernetes-fleet.io/deleteStage
  policyObservedClusterCount: 3 # number of clusters to be updated
  policySnapshotIndexUsed: "0"
  stagedUpdateStrategySnapshot: # snapshot of the strategy
    stages:
    - afterStageTasks:
      - type: TimedWait
        waitTime: 1m0s
      labelSelector:
        matchLabels:
          environment: staging
      name: staging
    - afterStageTasks:
      - type: Approval
        waitTime: 1h0m0s
      labelSelector:
        matchLabels:
          environment: canary
      name: canary
      sortingLabelKey: order
  stagesStatus: # detailed status for each stage
  - afterStageTaskStatus:
    - conditions:
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: AfterStageTaskWaitTimeElapsed
        status: "True" # the wait after-stage task has completed
        type: WaitTimeElapsed
      type: TimedWait
    clusters:
    - clusterName: member2 # stage staging contains member2 cluster only
      conditions:
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: ClusterUpdatingStarted
        status: "True"
        type: Started
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: ClusterUpdatingSucceeded
        status: "True" # member2 is updated successfully
        type: Succeeded
    conditions:
    - lastTransitionTime: ...
      message: ""
      observedGeneration: 1
      reason: StageUpdatingWaiting
      status: "False"
      type: Progressing
    - lastTransitionTime: ...
      message: ""
      observedGeneration: 1
      reason: StageUpdatingSucceeded
      status: "True" # stage staging has completed successfully
      type: Succeeded
    endTime: ...
    stageName: staging
    startTime: ...
  - afterStageTaskStatus:
    - approvalRequestName: example-run-canary # ClusterApprovalRequest name for this stage
      type: Approval
    clusters:
    - clusterName: member3 # according the labelSelector and sortingLabelKey, member3 is selected first in this stage
      conditions:
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: ClusterUpdatingStarted
        status: "True"
        type: Started
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: ClusterUpdatingSucceeded
        status: "True" # member3 update is completed
        type: Succeeded
    - clusterName: member1 # member1 is selected after member3 because of order=2 label
      conditions:
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: ClusterUpdatingStarted
        status: "True" # member1 update has not finished yet
        type: Started
    conditions:
    - lastTransitionTime: ...
      message: ""
      observedGeneration: 1
      reason: StageUpdatingStarted
      status: "True" # stage canary is still executing
      type: Progressing
    stageName: canary
    startTime: ...
```
Wait a little bit more, and we can see stage `canary` finishes cluster update and is waiting for the Approval task.
We can check the `ClusterApprovalRequest` generated and not approved yet:
```bash
kubectl get clusterapprovalrequest
NAME                 UPDATE-RUN    STAGE    APPROVED   APPROVALACCEPTED   AGE
example-run-canary   example-run   canary                                 2m2s
```
We can approve the `ClusterApprovalRequest` by patching its status:
```bash
kubectl patch clusterapprovalrequests example-run-canary --type=merge -p {"status":{"conditions":[{"type":"Approved","status":"True","reason":"lgtm","message":"lgtm","lastTransitionTime":"'$(date --utc +%Y-%m-%dT%H:%M:%SZ)'","observedGeneration":1}]}} --subresource=status
clusterapprovalrequest.placement.kubernetes-fleet.io/example-run-canary patched
```
Then verify it's approved:
```bash
kubectl get clusterapprovalrequest
NAME                 UPDATE-RUN    STAGE    APPROVED   APPROVALACCEPTED   AGE
example-run-canary   example-run   canary   True       True               2m30s
```
The updateRun now is able to proceed and complete:
```bash
kubectl get crsur example-run
NAME          PLACEMENT           RESOURCE-SNAPSHOT   POLICY-SNAPSHOT   INITIALIZED   SUCCEEDED   AGE
example-run   example-placement   1                   0                 True          True        4m22s
```
The CRP also shows rollout has completed and resources are available on all member clusters:
```bash
kubectl get crp example-placement
NAME                GEN   SCHEDULED   SCHEDULED-GEN   AVAILABLE   AVAILABLE-GEN   AGE
example-placement   1     True        1               True        1               134m
```
The configmap `test-cm` should be deployed on all 3 member clusters, with latest data:
```yaml
data:
  key: value2
```

## Deploy a second ClusterStagedUpdateRun to rollback to a previous version

Now suppose the workload admin wants to rollback the configmap change, reverting the value `value2` back to `value1`.
Instead of manually updating the configmap from hub, they can create a new `ClusterStagedUpdateRun` with a previous resource snapshot index, "0" in our context and they can reuse the same strategy:
```bash
kubectl apply -f - << EOF
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterStagedUpdateRun
metadata:
  name: example-run-2
spec:
  placementName: example-placement
  resourceSnapshotIndex: "0"
  stagedRolloutStrategyName: example-strategy
EOF
```

Following the same step as [deploying the first updateRun](#deploy-a-clusterstagedupdaterun-to-rollout-latest-change), the second updateRun should succeed also. Complete status shown as below:
```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterStagedUpdateRun
metadata:
  ...
  name: example-run-2
  ...
spec:
  placementName: example-placement
  resourceSnapshotIndex: "0"
  stagedRolloutStrategyName: example-strategy
status:
  conditions:
  - lastTransitionTime: ...
    message: ClusterStagedUpdateRun initialized successfully
    observedGeneration: 1
    reason: UpdateRunInitializedSuccessfully
    status: "True"
    type: Initialized
  - lastTransitionTime: ...
    message: ""
    observedGeneration: 1
    reason: UpdateRunStarted
    status: "True"
    type: Progressing
  - lastTransitionTime: ...
    message: ""
    observedGeneration: 1
    reason: UpdateRunSucceeded # updateRun succeeded 
    status: "True"
    type: Succeeded
  deletionStageStatus:
    clusters: []
    conditions:
    - lastTransitionTime: ...
      message: ""
      observedGeneration: 1
      reason: StageUpdatingStarted
      status: "True"
      type: Progressing
    - lastTransitionTime: ...
      message: ""
      observedGeneration: 1
      reason: StageUpdatingSucceeded
      status: "True" # no clusters in the deletion stage, it completes directly
      type: Succeeded
    endTime: ...
    stageName: kubernetes-fleet.io/deleteStage
    startTime: ...
  policyObservedClusterCount: 3
  policySnapshotIndexUsed: "0"
  stagedUpdateStrategySnapshot:
    stages:
    - afterStageTasks:
      - type: TimedWait
        waitTime: 1m0s
      labelSelector:
        matchLabels:
          environment: staging
      name: staging
    - afterStageTasks:
      - type: Approval
        waitTime: 1h0m0s
      labelSelector:
        matchLabels:
          environment: canary
      name: canary
      sortingLabelKey: order
  stagesStatus:
  - afterStageTaskStatus:
    - conditions:
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: AfterStageTaskWaitTimeElapsed
        status: "True"
        type: WaitTimeElapsed
      type: TimedWait
    clusters:
    - clusterName: member2
      conditions:
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: ClusterUpdatingStarted
        status: "True"
        type: Started
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: ClusterUpdatingSucceeded
        status: "True"
        type: Succeeded
    conditions:
    - lastTransitionTime: ...
      message: ""
      observedGeneration: 1
      reason: StageUpdatingWaiting
      status: "False"
      type: Progressing
    - lastTransitionTime: ...
      message: ""
      observedGeneration: 1
      reason: StageUpdatingSucceeded
      status: "True"
      type: Succeeded
    endTime: ...
    stageName: staging
    startTime: ...
  - afterStageTaskStatus:
    - approvalRequestName: example-run-2-canary
      conditions:
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: AfterStageTaskApprovalRequestCreated
        status: "True"
        type: ApprovalRequestCreated
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: AfterStageTaskApprovalRequestApproved
        status: "True"
        type: ApprovalRequestApproved
      type: Approval
    clusters:
    - clusterName: member3
      conditions:
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: ClusterUpdatingStarted
        status: "True"
        type: Started
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: ClusterUpdatingSucceeded
        status: "True"
        type: Succeeded
    - clusterName: member1
      conditions:
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: ClusterUpdatingStarted
        status: "True"
        type: Started
      - lastTransitionTime: ...
        message: ""
        observedGeneration: 1
        reason: ClusterUpdatingSucceeded
        status: "True"
        type: Succeeded
    conditions:
    - lastTransitionTime: ...
      message: ""
      observedGeneration: 1
      reason: StageUpdatingWaiting
      status: "False"
      type: Progressing
    - lastTransitionTime: ...
      message: ""
      observedGeneration: 1
      reason: StageUpdatingSucceeded
      status: "True"
      type: Succeeded
    endTime: ...
    stageName: canary
    startTime: ...
```
The configmap `test-cm` should be updated on all 3 member clusters, with old data:
```yaml
data:
  key: value1
```