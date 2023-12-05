# Troubleshooting guide

## Overview:

This TSG is meant to help you troubleshoot issues with the Fleet APIs.

## Cluster Resource Placement:

Internal Objects to keep in mind when troubleshooting CRP related errors on the hub cluster:
 - `ClusterResourceSnapshot`
 - `ClusterSchedulingPolicySnapshot`
 - `ClusterResourceBinding`
 - `Work`

please read the API reference for more details about each object https://github.com/Azure/fleet/blob/main/docs/api-references.md

### How can I debug when my CRP status is ClusterResourcePlacementScheduled condition status is set to "False"?

#### Common scenarios:

Instances where this condition may arise:

- When the placement policy is set to `PickFixed`, but the specified cluster names do not match any joined member cluster name in the fleet, or the specified cluster is no longer connected to the fleet.
- When the placement policy is set to `PickN`, and N clusters are specified, but there are fewer than N clusters that have joined the fleet or satisfy the placement policy.

**Note**: When the placement policy is set to `PickAll`, and the specified Affinity does not allow the scheduler to pick any cluster that has joined the fleet, the `ClusterResourcePlacementScheduled` is set to `true`.

#### Example Scenario:

The example output below demonstrates a `ClusterResourcePlacement` with a `PickN` placement policy attempting to propagate resources to two clusters labeled `env:prod`. In this instance, two clusters, namely `kind-cluster-1` and `kind-cluster-2`, are joined to the fleet, with only one member cluster, `kind-cluster-1`, having the label `env:prod`.

#### CRP spec:
```
spec:
  policy:
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
          - labelSelector:
              matchLabels:
                env: prod
    numberOfClusters: 2
    placementType: PickN
  resourceSelectors:
  ...
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
```

#### CRP status:
```
status:
  conditions:
  - lastTransitionTime: "2023-11-27T20:25:19Z"
    message: could not find all the clusters needed as specified by the scheduling
      policy
    observedGeneration: 2
    reason: SchedulingPolicyUnfulfilled
    status: "False"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2023-11-27T20:25:24Z"
    message: All 1 cluster(s) are synchronized to the latest resources on the hub
      cluster
    observedGeneration: 2
    reason: SynchronizeSucceeded
    status: "True"
    type: ClusterResourcePlacementSynchronized
  - lastTransitionTime: "2023-11-27T20:25:24Z"
    message: Successfully applied resources to 1 member clusters
    observedGeneration: 2
    reason: ApplySucceeded
    status: "True"
    type: ClusterResourcePlacementApplied
  placementStatuses:
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2023-11-27T20:25:19Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 2
      reason: ScheduleSucceeded
      status: "True"
      type: ResourceScheduled
    - lastTransitionTime: "2023-11-27T20:25:24Z"
      message: Successfully Synchronized work(s) for placement
      observedGeneration: 2
      reason: WorkSynchronizeSucceeded
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2023-11-27T20:25:24Z"
      message: Successfully applied resources
      observedGeneration: 2
      reason: ApplySucceeded
      status: "True"
      type: ResourceApplied
  - conditions:
    - lastTransitionTime: "2023-11-27T20:25:40Z"
      message: 'kind-cluster-2 is not selected: ClusterUnschedulable, none of the
        nonempty required cluster affinity term (total number: 1) is matched'
      observedGeneration: 2
      reason: ScheduleFailed
      status: "False"
      type: ResourceScheduled
  selectedResources:
  ...
```

The `ClusterResourcePlacementScheduled` condition is set to `false`, the goal is to select two clusters with the label `env:prod`, but only one member cluster possesses the correct label as specified in `clusterAffinity`.

We can also take a look at the `ClusterSchedulingPolicySnapshot` status to figure out why the scheduler could not schedule the resource for the placement policy specified.

The corresponding `ClusterSchedulingPolicySnapshot` spec and status gives us even more information on why scheduling failed, refer to this [section](#how-to-find--verify-the-latest-clusterschedulingpolicysnapshot-for-a-crp),

#### Latest ClusterSchedulingPolicySnapshot:
```
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterSchedulingPolicySnapshot
metadata:
  annotations:
    kubernetes-fleet.io/CRP-generation: "2"
    kubernetes-fleet.io/number-of-clusters: "2"
  creationTimestamp: "2023-11-27T21:33:01Z"
  generation: 1
  labels:
    kubernetes-fleet.io/is-latest-snapshot: "true"
    kubernetes-fleet.io/parent-CRP: crp-4
    kubernetes-fleet.io/policy-index: "0"
  name: ...
  ownerReferences:
  - apiVersion: placement.kubernetes-fleet.io/v1beta1
    blockOwnerDeletion: true
    controller: true
    kind: ClusterResourcePlacement
    name: ...
    uid: 37e83327-26e0-4c48-8276-e62cc6aa067f
  resourceVersion: "10085"
  uid: f2a3d0ea-c9fa-455d-be09-51b5d090e5d6
spec:
  policy:
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
          - labelSelector:
              matchLabels:
                env: prod
    placementType: PickN
  policyHash: ZjE0Yjk4YjYyMTVjY2U3NzQ1MTZkNWRhZjRiNjQ1NzQ4NjllNTUyMzZkODBkYzkyYmRkMGU3OTI3MWEwOTkyNQ==
status:
  conditions:
  - lastTransitionTime: "2023-11-27T21:33:01Z"
    message: could not find all the clusters needed as specified by the scheduling
      policy
    observedGeneration: 1
    reason: SchedulingPolicyUnfulfilled
    status: "False"
    type: Scheduled
  observedCRPGeneration: 2
  targetClusters:
  - clusterName: kind-cluster-1
    clusterScore:
      affinityScore: 0
      priorityScore: 0
    reason: picked by scheduling policy
    selected: true
  - clusterName: kind-cluster-2
    reason: 'ClusterUnschedulable, none of the nonempty required cluster affinity
      term (total number: 1) is matched'
    selected: false
```

#### Resolution:
The solution here is to add the `env:prod` label to the member cluster resource for `kind-cluster-2` as well, so that the scheduler can select the cluster to propagate resources.

### How can I debug when my CRP status is ClusterResourcePlacementSynchronized condition status is set to "False"?

The **ClusterResourcePlacementSynchronized** condition status is set to **"False"** under the following circumstances: when the work is not created or updated for a new **ClusterResourceSnapshot**.

#### **Investigation Steps:**

- In the **ClusterResourcePlacement** status section, examine the **placementStatuses** to identify clusters with the `WorkSynchronized` status set to **"False"**.
- Locate the corresponding **ClusterResourceBinding** for the identified cluster. This resource should indicate the status of the `Work`, whether it was created or updated.
- A common scenario leading to this issue is the user input for the **rollingUpdate** configuration being too strict. Verify the values for `maxUnavailable` and `maxSurge` to ensure they align with your expectations.

#### **Example Scenario:**

In the following example, an attempt is made to propagate a namespace to three member clusters. However, during the initial creation of the `ClusterResourcePlacement`, the namespace doesn't exist on the hub cluster, and the fleet currently comprises two member clusters named **kind-cluster-1** and **kind-cluster-2**.

#### CRP spec:
```
spec:
  policy:
    numberOfClusters: 3
    placementType: PickN
  resourceSelectors:
  - group: ""
    kind: Namespace
    name: test-ns
    version: v1
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
```

#### CRP status:
```
status:
  conditions:
  - lastTransitionTime: "2023-11-29T21:36:49Z"
    message: could not find all the clusters needed as specified by the scheduling
      policy
    observedGeneration: 2
    reason: SchedulingPolicyUnfulfilled
    status: "False"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2023-11-29T21:36:54Z"
    message: All 2 cluster(s) are synchronized to the latest resources on the hub
      cluster
    observedGeneration: 2
    reason: SynchronizeSucceeded
    status: "True"
    type: ClusterResourcePlacementSynchronized
  - lastTransitionTime: "2023-11-29T21:36:54Z"
    message: Successfully applied resources to 2 member clusters
    observedGeneration: 2
    reason: ApplySucceeded
    status: "True"
    type: ClusterResourcePlacementApplied
  placementStatuses:
  - clusterName: kind-cluster-2
    conditions:
    - lastTransitionTime: "2023-11-29T21:36:49Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-2 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 2
      reason: ScheduleSucceeded
      status: "True"
      type: ResourceScheduled
    - lastTransitionTime: "2023-11-29T21:36:49Z"
      message: Successfully Synchronized work(s) for placement
      observedGeneration: 2
      reason: WorkSynchronizeSucceeded
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2023-11-29T21:36:54Z"
      message: Successfully applied resources
      observedGeneration: 2
      reason: ApplySucceeded
      status: "True"
      type: ResourceApplied
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2023-11-29T21:36:49Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 2
      reason: ScheduleSucceeded
      status: "True"
      type: ResourceScheduled
    - lastTransitionTime: "2023-11-29T21:36:54Z"
      message: Successfully Synchronized work(s) for placement
      observedGeneration: 2
      reason: WorkSynchronizeSucceeded
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2023-11-29T21:36:54Z"
      message: Successfully applied resources
      observedGeneration: 2
      reason: ApplySucceeded
      status: "True"
      type: ResourceApplied
```

Given that the resource **test-ns** namespace never existed on the hub cluster, the `ClusterResourcePlacement` status reflects the following:
- `ClusterResourcePlacementApplied` is set to `true`
- `ClusterResourcePlacementScheduled` is set to `false`, as the specified policy aims to pick three clusters, but the scheduler can only accommodate placement in two currently available and joined clusters.

Let's check the latest `ClusterResourceSnapshot`, please check this [section](#how-to-find-the-latest-clusterresourcesnapshot-resource) for more details,

#### Latest ClusterResourceSnapshot:
```
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourceSnapshot
metadata:
  annotations:
    kubernetes-fleet.io/number-of-enveloped-object: "0"
    kubernetes-fleet.io/number-of-resource-snapshots: "1"
    kubernetes-fleet.io/resource-hash: 83ff749c5d8eb5a0b62d714175bcbaef1409b371fc5a229a002db6fcc1f144e1
  creationTimestamp: "2023-12-05T04:17:49Z"
  generation: 1
  labels:
    kubernetes-fleet.io/is-latest-snapshot: "true"
    kubernetes-fleet.io/parent-CRP: test-crp
    kubernetes-fleet.io/resource-index: "0"
  name: test-crp-0-snapshot
  ownerReferences:
  - apiVersion: placement.kubernetes-fleet.io/v1beta1
    blockOwnerDeletion: true
    controller: true
    kind: ClusterResourcePlacement
    name: test-crp
    uid: 1c474983-cda0-49cb-bf60-3d2a42f122ba
  resourceVersion: "2548"
  uid: dde6ec98-af99-4c4f-aabc-329a2862709a
spec:
  selectedResources: []
```

We observe that the `selectedResources` field in the spec is empty because the namespace `test-ns` doesn't exist on the hub cluster.

Let's check the `ClusterResourceBinding` for `kind-cluster-1`, please check this [section](#how-to-find-the-latest-clusterresourcebinding-resource) for more details,

#### ClusterResourceBinding for kind-cluster-1:
```
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourceBinding
metadata:
  creationTimestamp: "2023-12-05T04:17:49Z"
  finalizers:
  - kubernetes-fleet.io/work-cleanup
  generation: 2
  labels:
    kubernetes-fleet.io/parent-CRP: test-crp
  name: test-crp-kind-cluster-1-4e5c873b
  resourceVersion: "2572"
  uid: 8ae9741d-e95c-44f8-b36a-29d73f6b833c
spec:
  clusterDecision:
    clusterName: kind-cluster-1
    clusterScore:
      affinityScore: 0
      priorityScore: 0
    reason: picked by scheduling policy
    selected: true
  resourceSnapshotName: test-crp-0-snapshot
  schedulingPolicySnapshotName: test-crp-0
  state: Bound
  targetCluster: kind-cluster-1
status:
  conditions:
  - lastTransitionTime: "2023-12-05T04:17:50Z"
    message: ""
    observedGeneration: 2
    reason: AllWorkSynced
    status: "True"
    type: Bound
  - lastTransitionTime: "2023-12-05T04:17:50Z"
    message: ""
    observedGeneration: 2
    reason: AllWorkHasBeenApplied
    status: "True"
    type: Applied
```

Upon inspecting the `ClusterResourceBinding` spec, we observe that the `resourceSnapshotName` matches the latest `ClusterResourceSnapshot` name. Additionally, both the `Bound` and `Applied` conditions are set to True.

Subsequently, we proceed to create the `test-ns` namespace on the hub cluster. We anticipate the seamless propagation of the namespace across the relevant clusters.

#### CRP status after namespace test-ns is created on the hub cluster:
```
status:
  conditions:
  - lastTransitionTime: "2023-11-29T21:36:49Z"
    message: could not find all the clusters needed as specified by the scheduling
      policy
    observedGeneration: 2
    reason: SchedulingPolicyUnfulfilled
    status: "False"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2023-11-29T21:49:43Z"
    message: There are still 2 cluster(s) pending to be sychronized on the hub cluster
    observedGeneration: 2
    reason: SynchronizePending
    status: "False"
    type: ClusterResourcePlacementSynchronized
  - lastTransitionTime: "2023-11-29T21:49:43Z"
    message: 'Works need to be synchronized on the hub cluster or there are still
      manifests pending to be processed by the 2 member clusters '
    observedGeneration: 2
    reason: ApplyPending
    status: Unknown
    type: ClusterResourcePlacementApplied
  placementStatuses:
  - clusterName: kind-cluster-2
    conditions:
    - lastTransitionTime: "2023-11-29T21:36:49Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-2 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 2
      reason: ScheduleSucceeded
      status: "True"
      type: ResourceScheduled
    - lastTransitionTime: "2023-11-29T21:49:43Z"
      message: 'In the process of synchronizing or operation is blocked by the rollout
        strategy '
      observedGeneration: 2
      reason: WorkSynchronizePending
      status: "False"
      type: WorkSynchronized
    - lastTransitionTime: "2023-11-29T21:49:43Z"
      message: Works need to be synchronized on the hub cluster or there are still
        manifests pending to be processed by the member cluster
      observedGeneration: 2
      reason: ApplyPending
      status: Unknown
      type: ResourceApplied
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2023-11-29T21:36:49Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 2
      reason: ScheduleSucceeded
      status: "True"
      type: ResourceScheduled
    - lastTransitionTime: "2023-11-29T21:49:43Z"
      message: 'In the process of synchronizing or operation is blocked by the rollout
        strategy '
      observedGeneration: 2
      reason: WorkSynchronizePending
      status: "False"
      type: WorkSynchronized
    - lastTransitionTime: "2023-11-29T21:49:43Z"
      message: Works need to be synchronized on the hub cluster or there are still
        manifests pending to be processed by the member cluster
      observedGeneration: 2
      reason: ApplyPending
      status: Unknown
      type: ResourceApplied
  selectedResources:
  - kind: Namespace
    name: test-ns
    version: v1
```

Upon examination, the `ClusterResourcePlacementSynchronized` status is found to be `false`, accompanied by a message indicating that "**Works need to be synchronized on the hub cluster, or there are still manifests pending to be processed by the 2 member clusters.**"

Let's check the latest `ClusterResourceSnapshot`, please check this [section](#how-to-find-the-latest-clusterresourcesnapshot-resource) for more details,

#### Latest ClusterResourceSnapshot:
```
metadata:
  annotations:
    kubernetes-fleet.io/number-of-enveloped-object: "0"
    kubernetes-fleet.io/number-of-resource-snapshots: "1"
    kubernetes-fleet.io/resource-hash: 72344be6e268bc7af29d75b7f0aad588d341c228801aab50d6f9f5fc33dd9c7c
  creationTimestamp: "2023-12-05T04:36:24Z"
  generation: 1
  labels:
    kubernetes-fleet.io/is-latest-snapshot: "true"
    kubernetes-fleet.io/parent-CRP: test-crp
    kubernetes-fleet.io/resource-index: "1"
  name: test-crp-1-snapshot
  ownerReferences:
  - apiVersion: placement.kubernetes-fleet.io/v1beta1
    blockOwnerDeletion: true
    controller: true
    kind: ClusterResourcePlacement
    name: test-crp
    uid: 1c474983-cda0-49cb-bf60-3d2a42f122ba
  resourceVersion: "4489"
  uid: a520f775-14cc-4bf5-b8cd-c4efc0e2be34
spec:
  selectedResources:
  - apiVersion: v1
    kind: Namespace
    metadata:
      labels:
        kubernetes.io/metadata.name: test-ns
      name: test-ns
    spec:
      finalizers:
      - kubernetes
```

Upon inspecting `ClusterResourceSnapshot` spec, we observe that the `selectedResources` section now has the namespace `test-ns`.

Let's check the `ClusterResourceBinding` for `kind-cluster-1` to see if it got updated after the namespace `test-ns` was created, please check this [section](#how-to-find-the-latest-clusterresourcebinding-resource) for more details,

#### ClusterResourceBinding for kind-cluster-1:
```
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourceBinding
metadata:
  creationTimestamp: "2023-12-05T04:17:49Z"
  finalizers:
  - kubernetes-fleet.io/work-cleanup
  generation: 2
  labels:
    kubernetes-fleet.io/parent-CRP: test-crp
  name: test-crp-kind-cluster-1-4e5c873b
  resourceVersion: "2572"
  uid: 8ae9741d-e95c-44f8-b36a-29d73f6b833c
spec:
  clusterDecision:
    clusterName: kind-cluster-1
    clusterScore:
      affinityScore: 0
      priorityScore: 0
    reason: picked by scheduling policy
    selected: true
  resourceSnapshotName: test-crp-0-snapshot
  schedulingPolicySnapshotName: test-crp-0
  state: Bound
  targetCluster: kind-cluster-1
status:
  conditions:
  - lastTransitionTime: "2023-12-05T04:17:50Z"
    message: ""
    observedGeneration: 2
    reason: AllWorkSynced
    status: "True"
    type: Bound
  - lastTransitionTime: "2023-12-05T04:17:50Z"
    message: ""
    observedGeneration: 2
    reason: AllWorkHasBeenApplied
    status: "True"
    type: Applied
```

Upon inspection, it is observed that the `ClusterResourceBinding` remains unchanged. Notably, in the spec, the `resourceSnapshotName` still references the old `ClusterResourceSnapshot` name.

This scenario arises due to the absence of explicit `rollingUpdate` input from the user. Consequently, the default values are applied:

- `maxUnavailable` is configured to **25% * 3 (desired number), rounded to 1**
- `maxSurge` is configured to **25% * 3 (desired number), rounded to 1**

#### Summary of Events:
1. Initially, when the CRP was created, two `ClusterResourceBindings` were generated. However, since the `test-ns` namespace did not exist on the hub cluster, the `Work` object was created with an empty list of manifests, and `ClusterResourcePlacementSynchronized` was set to `true`.
2. Upon creating the `test-ns` namespace on the hub, the rollout controller attempted to update the two existing `ClusterResourceBindings`. However, the `rollingUpdate` configuration was too strict: `maxUnavailable` was set to 1, which was already the case due to a missing member cluster. If, during the update, even one of the bindings failed to apply, it would violate the `rollingUpdate` configuration since `maxUnavailable` was set to 1.

#### Resolution:
- To address this specific issue, consider manually setting `maxUnavailable` to a value greater than 2 to relax the `rollingUpdate` configuration.
- Alternatively, you can also join a third member cluster.

### How can I debug when my CRP ClusterResourcePlacementApplied condition is set to "False"?

#### Investigation steps:

1. Check `placementStatuses`: In the `ClusterResourcePlacement` status section, inspect the `placementStatuses` to identify which clusters have the `ResourceApplied` condition set to `false` and note down their `clusterName`.
2. Locate `Work` Object in Hub Cluster: Use the identified `clusterName` to locate the `Work` object associated with the member cluster.
3. Check `Work` object status: Inspect the status of the `Work` object to understand the specific issues preventing successful resource application.

#### Example Scenario:
In this example, the `ClusterResourcePlacement` is attempting to propagate a namespace containing a deployment to two member clusters. However, the namespace already exists on one member cluster, specifically named `kind-cluster-1`.

#### CRP spec:
```
  policy:
    clusterNames:
    - kind-cluster-1
    - kind-cluster-2
    placementType: PickFixed
  resourceSelectors:
  - group: ""
    kind: Namespace
    name: test-ns
    version: v1
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
```

#### CRP status:
```
 conditions:
  - lastTransitionTime: "2023-11-28T20:56:15Z"
    message: found all the clusters needed as specified by the scheduling policy
    observedGeneration: 2
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2023-11-28T20:56:21Z"
    message: All 2 cluster(s) are synchronized to the latest resources on the hub
      cluster
    observedGeneration: 2
    reason: SynchronizeSucceeded
    status: "True"
    type: ClusterResourcePlacementSynchronized
  - lastTransitionTime: "2023-11-28T20:56:21Z"
    message: Failed to apply manifests to 1 clusters, please check the `failedResourcePlacements`
      status
    observedGeneration: 2
    reason: ApplyFailed
    status: "False"
    type: ClusterResourcePlacementApplied
  placementStatuses:
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2023-11-28T20:56:15Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1:
        picked by scheduling policy'
      observedGeneration: 2
      reason: ScheduleSucceeded
      status: "True"
      type: ResourceScheduled
    - lastTransitionTime: "2023-11-28T20:56:21Z"
      message: Successfully Synchronized work(s) for placement
      observedGeneration: 2
      reason: WorkSynchronizeSucceeded
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2023-11-28T20:56:21Z"
      message: Failed to apply manifests, please check the `failedResourcePlacements`
        status
      observedGeneration: 2
      reason: ApplyFailed
      status: "False"
      type: ResourceApplied
    failedPlacements:
    - condition:
        lastTransitionTime: "2023-11-28T20:56:16Z"
        message: 'Failed to apply manifest: resource is not managed by the work controller'
        reason: AppliedManifestFailedReason
        status: "False"
        type: Applied
      kind: Namespace
      name: test-ns
      version: v1
  - clusterName: kind-cluster-2
    conditions:
    - lastTransitionTime: "2023-11-28T20:56:15Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-2:
        picked by scheduling policy'
      observedGeneration: 2
      reason: ScheduleSucceeded
      status: "True"
      type: ResourceScheduled
    - lastTransitionTime: "2023-11-28T20:56:15Z"
      message: Successfully Synchronized work(s) for placement
      observedGeneration: 2
      reason: WorkSynchronizeSucceeded
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2023-11-28T20:56:21Z"
      message: Successfully applied resources
      observedGeneration: 2
      reason: ApplySucceeded
      status: "True"
      type: ResourceApplied
  selectedResources:
  - group: apps
    kind: Deployment
    name: test-nginx
    namespace: test-ns
    version: v1
  - kind: Namespace
    name: test-ns
    version: v1
```

In the `ClusterResourcePlacement` status, `placementStatuses` for `kind-cluster-1` in the `failedPlacements` section, we get a clear message as to why the resource failed to apply on the member cluster.

To gain more insights also take a look at the `work` object, please check this [section](#how-and-where-to-find-the-correct-work-resource) for more details,

#### Work status:
```
 status:
    conditions:
    - lastTransitionTime: "2023-11-28T21:07:15Z"
      message: Failed to apply work
      observedGeneration: 1
      reason: AppliedWorkFailed
      status: "False"
      type: Applied
    manifestConditions:
    - conditions:
      - lastTransitionTime: "2023-11-28T20:56:16Z"
        message: ManifestNoChange
        observedGeneration: 1
        reason: ManifestNoChange
        status: "True"
        type: Applied
      identifier:
        group: apps
        kind: Deployment
        name: test-nginx
        namespace: test-ns
        ordinal: 0
        resource: deployments
        version: v1
    - conditions:
      - lastTransitionTime: "2023-11-28T20:56:16Z"
        message: 'Failed to apply manifest: resource is not managed by the work controller'
        reason: AppliedManifestFailedReason
        status: "False"
        type: Applied
      identifier:
        kind: Namespace
        name: test-ns
        ordinal: 1
        resource: namespaces
        version: v1
```

From looking at the `Work` status and specifically the `manifestConditions` section, we could see that the namespace could not be applied but the deployment within the namespace got propagated from hub to the member cluster.

#### Resolution:
In this scenario, a potential solution is to delete the existing namespace on the member cluster. However, it's essential to note that this decision rests with the user, as the namespace might already contain resources.

### How can I debug when some clusters are not selected as expected?

Check the status of the `ClusterSchedulingPolicySnapshot` to determine which clusters were selected along with the reason.

### How can I debug when a selected cluster does not have the expected resources on it or if CRP doesn't pick up the latest changes?

Please check the following cases,
- Check to see if `ClusterResourcePlacementSynchronized` condition in CRP status is set to `true` or `false`.
- If it's set to `false` check this [question](#how-can-i-debug-when-my-crp-status-is-clusterresourceplacementsynchronized-condition-status-is-set-to--false--).
- If it's set to `true`,
  - Check to see if `ClusterResourcePlacementApplied` condition is set to `unknown`, `false` or `true`.
  - If it's set to `unknown`, please wait as the resources are still being applied to the member cluster (if it's stuck in unknown state for a while, please raise a github issue as it's an unexpected behavior).
  - If it's set to `false`, check this [question](#how-can-i-debug-when-my-crp-clusterresourceplacementapplied-condition-is-set-to--false--).
  - If it's set to `true`, check to see if the resource exists on the hub cluster. The `ClusterResourcePlacementApplied` condition is set to `true` if the resource doesn't exist on the hub.

We can also take a look at the `placementStatuses` section in `ClusterResourcePlacement` status for that particular cluster. In `placementStatuses` we would find `failedPlacements` section which should have the reasons as to why resources failed to apply.

### How to find & verify the latest ClusterSchedulingPolicySnapshot for a CRP?

We need to have `ClusterResourcePlacement` name `{CRPName}`, replace `{CRPName}` in the command below,

```
kubectl get clusterschedulingpolicysnapshot -l kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP={CRPName}
```

- Compare `ClusterSchedulingPolicySnapshot` with the `ClusterResourcePlacement` policy to ensure they match (excluding `numberOfClusters` field from `ClusterResourcePlacement` spec).
- If placement type is `PickN`, check if the number of clusters requested in `ClusterResourcePlacment` placement policy matches the value for the label called `number-of-clusters`.

### How to find the latest ClusterResourceBinding resource?

We need to have `ClusterResourcePlacement` name `{CRPName}`, replace `{CRPName} `in the command below. The command below lists all `ClusterResourceBindings` associated with `ClusterResourcePlacement`,

```
kubectl get clusterresourcebinding -l kubernetes-fleet.io/parent-CRP={CRPName}
```

#### Example:

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

### How to find the latest ClusterResourceSnapshot resource?

Replace `{CRPName}` in the command below with name of `ClusterResourcePlacement`,

```
kubectl get clusterresourcesnapshot -l kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP={CRPName} -o YAML
```

### How and where to find the correct Work resource?

We need to have the member cluster's namespace which follow this format `fleet-member-{clusterName}`, `ClusterResourceBinding` name `{CRBName}` and `ClusterResourcePlacement` name `{CRPName}`.

```
kubectl get work -n fleet-member-{clusterName} -l kubernetes-fleet.io/parent-CRP={CRPName},kubernetes-fleet.io/parent-resource-binding={CRBName} -o YAML
```
