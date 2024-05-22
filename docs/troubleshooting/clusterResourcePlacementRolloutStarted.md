# How can I debug when my CRP status is ClusterResourcePlacementRolloutStarted condition status is set to false?

The `ClusterResourcePlacementRolloutStarted` condition status is set to `false` under the following circumstances: the selected resources have not been rolled out in all scheduled clusters yet.
> Note: In addition, it may be helpful to look into the logs for the [rollout controller](https://github.com/Azure/fleet/blob/main/pkg/controllers/rollout/controller.go) to get more information on why the rollout did not start.

## Common scenarios:

- The CRP rollout strategy is blocked due to the `rollingUpdate` configuration being too strict.

### Investigation Steps:

- In the `ClusterResourcePlacement` status section, examine the `placementStatuses` to identify clusters with the `RolloutStarted` status set to `false`.
- Locate the corresponding `ClusterResourceBinding` for the identified cluster. Please check this [section](#how-to-find-the-latest-clusterresourcebinding-resource) to learn how to get the latest `ClusterResourceBinding`. This resource should indicate the status of the `Work` whether it was created or updated.
- A common scenario leading to this issue is the user input for the `rollingUpdate` configuration being too strict. Verify the values for `maxUnavailable` and `maxSurge` to ensure they align with your expectations.

### Example Scenario:

In the following example, an attempt is made to propagate a namespace to three member clusters. However, during the initial creation of the `ClusterResourcePlacement`, the namespace doesn't exist on the hub cluster, and the fleet currently comprises two member clusters named `kind-cluster-1` and `kind-cluster-2`.

### CRP spec:
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

### CRP status:
```
status:
  conditions:
  - lastTransitionTime: "2024-05-07T23:08:53Z"
    message: could not find all the clusters needed as specified by the scheduling
      policy
    observedGeneration: 1
    reason: SchedulingPolicyUnfulfilled
    status: "False"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2024-05-07T23:08:53Z"
    message: All 2 cluster(s) start rolling out the latest resource
    observedGeneration: 1
    reason: RolloutStarted
    status: "True"
    type: ClusterResourcePlacementRolloutStarted
  - lastTransitionTime: "2024-05-07T23:08:53Z"
    message: No override rules are configured for the selected resources
    observedGeneration: 1
    reason: NoOverrideSpecified
    status: "True"
    type: ClusterResourcePlacementOverridden
  - lastTransitionTime: "2024-05-07T23:08:53Z"
    message: Works(s) are succcesfully created or updated in the 2 target clusters'
      namespaces
    observedGeneration: 1
    reason: WorkSynchronized
    status: "True"
    type: ClusterResourcePlacementWorkSynchronized
  - lastTransitionTime: "2024-05-07T23:08:53Z"
    message: The selected resources are successfully applied to 2 clusters
    observedGeneration: 1
    reason: ApplySucceeded
    status: "True"
    type: ClusterResourcePlacementApplied
  - lastTransitionTime: "2024-05-07T23:08:53Z"
    message: The selected resources in 2 cluster are available now
    observedGeneration: 1
    reason: ResourceAvailable
    status: "True"
    type: ClusterResourcePlacementAvailable
  observedResourceIndex: "0"
  placementStatuses:
  - clusterName: kind-cluster-2
    conditions:
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-2 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 1
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: Detected the new changes on the resources and started the rollout process
      observedGeneration: 1
      reason: RolloutStarted
      status: "True"
      type: RolloutStarted
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: No override rules are configured for the selected resources
      observedGeneration: 1
      reason: NoOverrideSpecified
      status: "True"
      type: Overridden
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: All of the works are synchronized to the latest
      observedGeneration: 1
      reason: AllWorkSynced
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: All corresponding work objects are applied
      observedGeneration: 1
      reason: AllWorkHaveBeenApplied
      status: "True"
      type: Applied
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: All corresponding work objects are available
      observedGeneration: 1
      reason: AllWorkAreAvailable
      status: "True"
      type: Available
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 1
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: Detected the new changes on the resources and started the rollout process
      observedGeneration: 1
      reason: RolloutStarted
      status: "True"
      type: RolloutStarted
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: No override rules are configured for the selected resources
      observedGeneration: 1
      reason: NoOverrideSpecified
      status: "True"
      type: Overridden
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: All of the works are synchronized to the latest
      observedGeneration: 1
      reason: AllWorkSynced
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: All corresponding work objects are applied
      observedGeneration: 1
      reason: AllWorkHaveBeenApplied
      status: "True"
      type: Applied
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: All corresponding work objects are available
      observedGeneration: 1
      reason: AllWorkAreAvailable
      status: "True"
      type: Available
```

Given that the resource `test-ns` namespace never existed on the hub cluster, the `ClusterResourcePlacement` status reflects the following:
- `ClusterResourcePlacementScheduled` is set to `false`, as the specified policy aims to pick three clusters, but the scheduler can only accommodate placement in two currently available and joined clusters.
- `ClusterResourcePlacementRolloutStarted` is set to `true`, as the rollout process has commenced with 2 clusters being selected.
- `ClusterResourcePlacementOverridden` is set to `true`, as no override rules are configured for the selected resources.
- `ClusterResourcePlacementWorkSynchronized` is set to `true`.
- `ClusterResourcePlacementApplied` is set to `true`.
- `ClusterResourcePlacementAvailable` is set to `true`.

Subsequently, we proceed to create the `test-ns` namespace on the hub cluster. We anticipate the seamless propagation of the namespace across the relevant clusters.

### CRP status after namespace test-ns is created on the hub cluster:
```
status:
  conditions:
  - lastTransitionTime: "2024-05-07T23:08:53Z"
    message: could not find all the clusters needed as specified by the scheduling
      policy
    observedGeneration: 1
    reason: SchedulingPolicyUnfulfilled
    status: "False"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2024-05-07T23:13:51Z"
    message: The rollout is being blocked by the rollout strategy in 2 cluster(s)
    observedGeneration: 1
    reason: RolloutNotStartedYet
    status: "False"
    type: ClusterResourcePlacementRolloutStarted
  observedResourceIndex: "1"
  placementStatuses:
  - clusterName: kind-cluster-2
    conditions:
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-2 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 1
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-05-07T23:13:51Z"
      message: The rollout is being blocked by the rollout strategy
      observedGeneration: 1
      reason: RolloutNotStartedYet
      status: "False"
      type: RolloutStarted
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 1
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-05-07T23:13:51Z"
      message: The rollout is being blocked by the rollout strategy
      observedGeneration: 1
      reason: RolloutNotStartedYet
      status: "False"
      type: RolloutStarted
  selectedResources:
  - kind: Namespace
    name: test-ns
    version: v1
```

Upon examination, the `ClusterResourcePlacementScheduled` status is found to be `false`, and for the
`ClusterResourcePlacementRolloutStarted` status condition we see a message indicating that `The rollout is being blocked by the rollout strategy in 2 cluster(s)`

Let's check the latest `ClusterResourceSnapshot`. Please refer to this [section](#how-to-find-the-latest-clusterresourcesnapshot-resource) to learn how to get the latest `ClusterResourceSnapshot`.

### Latest ClusterResourceSnapshot:
```
apiVersion: placement.kubernetes-fleet.io/v1
kind: ClusterResourceSnapshot
metadata:
  annotations:
    kubernetes-fleet.io/number-of-enveloped-object: "0"
    kubernetes-fleet.io/number-of-resource-snapshots: "1"
    kubernetes-fleet.io/resource-hash: 72344be6e268bc7af29d75b7f0aad588d341c228801aab50d6f9f5fc33dd9c7c
  creationTimestamp: "2024-05-07T23:13:51Z"
  generation: 1
  labels:
    kubernetes-fleet.io/is-latest-snapshot: "true"
    kubernetes-fleet.io/parent-CRP: crp-3
    kubernetes-fleet.io/resource-index: "1"
  name: crp-3-1-snapshot
  ownerReferences:
  - apiVersion: placement.kubernetes-fleet.io/v1beta1
    blockOwnerDeletion: true
    controller: true
    kind: ClusterResourcePlacement
    name: crp-3
    uid: b4f31b9a-971a-480d-93ac-93f093ee661f
  resourceVersion: "14434"
  uid: 85ee0e81-92c9-4362-932b-b0bf57d78e3f
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

Let's check the `ClusterResourceBinding` for `kind-cluster-1` to see if it got updated after the namespace `test-ns` was created. Please check this [section](#how-to-find-the-latest-clusterresourcebinding-resource) to learn how to get the latest `ClusterResourceBinding`.

### ClusterResourceBinding for kind-cluster-1:
```
apiVersion: placement.kubernetes-fleet.io/v1
kind: ClusterResourceBinding
metadata:
  creationTimestamp: "2024-05-07T23:08:53Z"
  finalizers:
  - kubernetes-fleet.io/work-cleanup
  generation: 2
  labels:
    kubernetes-fleet.io/parent-CRP: crp-3
  name: crp-3-kind-cluster-1-7114c253
  resourceVersion: "14438"
  uid: 0db4e480-8599-4b40-a1cc-f33bcb24b1a7
spec:
  applyStrategy:
    type: ClientSideApply
  clusterDecision:
    clusterName: kind-cluster-1
    clusterScore:
      affinityScore: 0
      priorityScore: 0
    reason: picked by scheduling policy
    selected: true
  resourceSnapshotName: crp-3-0-snapshot
  schedulingPolicySnapshotName: crp-3-0
  state: Bound
  targetCluster: kind-cluster-1
status:
  conditions:
  - lastTransitionTime: "2024-05-07T23:13:51Z"
    message: The resources cannot be updated to the latest because of the rollout
      strategy
    observedGeneration: 2
    reason: RolloutNotStartedYet
    status: "False"
    type: RolloutStarted
  - lastTransitionTime: "2024-05-07T23:08:53Z"
    message: No override rules are configured for the selected resources
    observedGeneration: 2
    reason: NoOverrideSpecified
    status: "True"
    type: Overridden
  - lastTransitionTime: "2024-05-07T23:08:53Z"
    message: All of the works are synchronized to the latest
    observedGeneration: 2
    reason: AllWorkSynced
    status: "True"
    type: WorkSynchronized
  - lastTransitionTime: "2024-05-07T23:08:53Z"
    message: All corresponding work objects are applied
    observedGeneration: 2
    reason: AllWorkHaveBeenApplied
    status: "True"
    type: Applied
  - lastTransitionTime: "2024-05-07T23:08:53Z"
    message: All corresponding work objects are available
    observedGeneration: 2
    reason: AllWorkAreAvailable
    status: "True"
    type: Available
```

Upon inspection, it is observed that the `ClusterResourceBinding` remains unchanged. Notably, in the spec, the `resourceSnapshotName` still references the old `ClusterResourceSnapshot` name.

This scenario arises due to the absence of explicit `rollingUpdate` input from the user. Consequently, the default values are applied:

- `maxUnavailable` is configured to `25% * 3 (desired number), rounded to 1`
- `maxSurge` is configured to `25% * 3 (desired number), rounded to 1`

### Summary of Events:
1. Initially, when the CRP was created, two `ClusterResourceBindings` were generated. However, since the `test-ns`
   namespace did not exist on the hub cluster, rollout could be started on the selected clusters, and `ClusterResourcePlacementRolloutStarted` was set to `true`.
2. Upon creating the `test-ns` namespace on the hub, the rollout controller attempted to update the two existing `ClusterResourceBindings`.
   However, the `rollingUpdate` configuration was too strict: `maxUnavailable` was set to 1, which was already the case due to a missing member cluster.
   If, during the update, even one of the bindings failed to apply, it would violate the `rollingUpdate` configuration since `maxUnavailable` was set to 1.

### Resolution:
- To address this specific issue, consider manually setting `maxUnavailable` to a value greater than 2 to relax the `rollingUpdate` configuration.
- Alternatively, you can also join a third member cluster.
