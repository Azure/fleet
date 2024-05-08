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

## How can I debug when my CRP status is ClusterResourcePlacementScheduled condition status is set to false?

### Common scenarios:

Instances where this condition may arise:

- When the placement policy is set to `PickFixed`, but the specified cluster names do not match any joined member cluster name in the fleet, or the specified cluster is no longer connected to the fleet.
- When the placement policy is set to `PickN`, and N clusters are specified, but there are fewer than N clusters that have joined the fleet or satisfy the placement policy.

>>Note: When the placement policy is set to `PickAll`, the `ClusterResourcePlacementScheduled` condition is always set to `true`.

### Example Scenario:

The example output below demonstrates a `ClusterResourcePlacement` with a `PickN` placement policy attempting to propagate resources to two clusters labeled `env:prod`. In this instance, two clusters, namely `kind-cluster-1` and `kind-cluster-2`, are joined to the fleet, with only one member cluster, `kind-cluster-1`, having the label `env:prod`.

### CRP spec:
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

### CRP status:
```
status:
  conditions:
  - lastTransitionTime: "2024-05-07T22:36:33Z"
    message: could not find all the clusters needed as specified by the scheduling
      policy
    observedGeneration: 1
    reason: SchedulingPolicyUnfulfilled
    status: "False"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2024-05-07T22:36:33Z"
    message: All 1 cluster(s) start rolling out the latest resource
    observedGeneration: 1
    reason: RolloutStarted
    status: "True"
    type: ClusterResourcePlacementRolloutStarted
  - lastTransitionTime: "2024-05-07T22:36:33Z"
    message: No override rules are configured for the selected resources
    observedGeneration: 1
    reason: NoOverrideSpecified
    status: "True"
    type: ClusterResourcePlacementOverridden
  - lastTransitionTime: "2024-05-07T22:36:33Z"
    message: Works(s) are succcesfully created or updated in the 1 target clusters'
      namespaces
    observedGeneration: 1
    reason: WorkSynchronized
    status: "True"
    type: ClusterResourcePlacementWorkSynchronized
  - lastTransitionTime: "2024-05-07T22:36:33Z"
    message: The selected resources are successfully applied to 1 clusters
    observedGeneration: 1
    reason: ApplySucceeded
    status: "True"
    type: ClusterResourcePlacementApplied
  - lastTransitionTime: "2024-05-07T22:36:33Z"
    message: The selected resources in 1 cluster are available now
    observedGeneration: 1
    reason: ResourceAvailable
    status: "True"
    type: ClusterResourcePlacementAvailable
  observedResourceIndex: "0"
  placementStatuses:
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2024-05-07T22:36:33Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 1
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-05-07T22:36:33Z"
      message: Detected the new changes on the resources and started the rollout process
      observedGeneration: 1
      reason: RolloutStarted
      status: "True"
      type: RolloutStarted
    - lastTransitionTime: "2024-05-07T22:36:33Z"
      message: No override rules are configured for the selected resources
      observedGeneration: 1
      reason: NoOverrideSpecified
      status: "True"
      type: Overridden
    - lastTransitionTime: "2024-05-07T22:36:33Z"
      message: All of the works are synchronized to the latest
      observedGeneration: 1
      reason: AllWorkSynced
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2024-05-07T22:36:33Z"
      message: All corresponding work objects are applied
      observedGeneration: 1
      reason: AllWorkHaveBeenApplied
      status: "True"
      type: Applied
    - lastTransitionTime: "2024-05-07T22:36:33Z"
      message: All corresponding work objects are available
      observedGeneration: 1
      reason: AllWorkAreAvailable
      status: "True"
      type: Available
  - conditions:
    - lastTransitionTime: "2024-05-07T22:36:33Z"
      message: 'kind-cluster-2 is not selected: ClusterUnschedulable, cluster does not
        match with any of the required cluster affinity terms'
      observedGeneration: 1
      reason: ScheduleFailed
      status: "False"
      type: Scheduled
  selectedResources:
  ...
```

The `ClusterResourcePlacementScheduled` condition is set to `false`, the goal is to select two clusters with the label `env:prod`, but only one member cluster possesses the correct label as specified in `clusterAffinity`.

We can also take a look at the `ClusterSchedulingPolicySnapshot` status to figure out why the scheduler could not schedule the resource for the placement policy specified.

The corresponding `ClusterSchedulingPolicySnapshot` spec and status gives us even more information on why scheduling failed. Please refer to this [section](#how-to-find--verify-the-latest-clusterschedulingpolicysnapshot-for-a-crp) to learn how to get the latest `ClusterSchedulingPolicySnapshot`.

### Latest ClusterSchedulingPolicySnapshot:
```
apiVersion: placement.kubernetes-fleet.io/v1
kind: ClusterSchedulingPolicySnapshot
metadata:
  annotations:
    kubernetes-fleet.io/CRP-generation: "1"
    kubernetes-fleet.io/number-of-clusters: "2"
  creationTimestamp: "2024-05-07T22:36:33Z"
  generation: 1
  labels:
    kubernetes-fleet.io/is-latest-snapshot: "true"
    kubernetes-fleet.io/parent-CRP: crp-2
    kubernetes-fleet.io/policy-index: "0"
  name: crp-2-0
  ownerReferences:
  - apiVersion: placement.kubernetes-fleet.io/v1beta1
    blockOwnerDeletion: true
    controller: true
    kind: ClusterResourcePlacement
    name: crp-2
    uid: 48bc1e92-a8b9-4450-a2d5-c6905df2cbf0
  resourceVersion: "10090"
  uid: 2137887e-45fd-4f52-bbb7-b96f39854625
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
  - lastTransitionTime: "2024-05-07T22:36:33Z"
    message: could not find all the clusters needed as specified by the scheduling
      policy
    observedGeneration: 1
    reason: SchedulingPolicyUnfulfilled
    status: "False"
    type: Scheduled
  observedCRPGeneration: 1
  targetClusters:
  - clusterName: kind-cluster-1
    clusterScore:
      affinityScore: 0
      priorityScore: 0
    reason: picked by scheduling policy
    selected: true
  - clusterName: kind-cluster-2
    reason: ClusterUnschedulable, cluster does not match with any of the required
      cluster affinity terms
    selected: false
```

### Resolution:
The solution here is to add the `env:prod` label to the member cluster resource for `kind-cluster-2` as well, so that the scheduler can select the cluster to propagate resources.

## How can I debug when my CRP status is ClusterResourcePlacementRolloutStarted condition status is set to false?

The `ClusterResourcePlacementRolloutStarted` condition status is set to `false` under the following circumstances: the selected resources have not been rolled out in all scheduled clusters yet.

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
  - clusterName: cluster-2
    conditions:
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: 'Successfully scheduled resources for placement in cluster-2 (affinity
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
  - clusterName: cluster-1
    conditions:
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: 'Successfully scheduled resources for placement in cluster-1 (affinity
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
  - clusterName: cluster-2
    conditions:
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: 'Successfully scheduled resources for placement in cluster-2 (affinity
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
  - clusterName: cluster-1
    conditions:
    - lastTransitionTime: "2024-05-07T23:08:53Z"
      message: 'Successfully scheduled resources for placement in cluster-1 (affinity
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

## How can I debug when my CRP ClusterResourcePlacementApplied condition is set to false?

### Investigation steps:

1. Check `placementStatuses`: In the `ClusterResourcePlacement` status section, inspect the `placementStatuses` to identify which clusters have the `ResourceApplied` condition set to `false` and note down their `clusterName`.
2. Locate `Work` Object in Hub Cluster: Use the identified `clusterName` to locate the `Work` object associated with the member cluster. Please refer to this [section](#how-and-where-to-find-the-correct-work-resource) to learn how to get the correct `Work` resource.
3. Check `Work` object status: Inspect the status of the `Work` object to understand the specific issues preventing successful resource application.

### Example Scenario:
In this example, the `ClusterResourcePlacement` is attempting to propagate a namespace containing a deployment to two member clusters. However, the namespace already exists on one member cluster, specifically named `kind-cluster-1`.

### CRP spec:
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

### CRP status:
```
status:
  conditions:
  - lastTransitionTime: "2024-05-07T23:32:40Z"
    message: could not find all the clusters needed as specified by the scheduling
      policy
    observedGeneration: 1
    reason: SchedulingPolicyUnfulfilled
    status: "False"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2024-05-07T23:32:40Z"
    message: All 2 cluster(s) start rolling out the latest resource
    observedGeneration: 1
    reason: RolloutStarted
    status: "True"
    type: ClusterResourcePlacementRolloutStarted
  - lastTransitionTime: "2024-05-07T23:32:40Z"
    message: No override rules are configured for the selected resources
    observedGeneration: 1
    reason: NoOverrideSpecified
    status: "True"
    type: ClusterResourcePlacementOverridden
  - lastTransitionTime: "2024-05-07T23:32:40Z"
    message: Works(s) are succcesfully created or updated in the 2 target clusters'
      namespaces
    observedGeneration: 1
    reason: WorkSynchronized
    status: "True"
    type: ClusterResourcePlacementWorkSynchronized
  - lastTransitionTime: "2024-05-07T23:32:40Z"
    message: Failed to apply resources to 1 clusters, please check the `failedPlacements`
      status
    observedGeneration: 1
    reason: ApplyFailed
    status: "False"
    type: ClusterResourcePlacementApplied
  observedResourceIndex: "0"
  placementStatuses:
  - clusterName: kind-cluster-2
    conditions:
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-2 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 1
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: Detected the new changes on the resources and started the rollout process
      observedGeneration: 1
      reason: RolloutStarted
      status: "True"
      type: RolloutStarted
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: No override rules are configured for the selected resources
      observedGeneration: 1
      reason: NoOverrideSpecified
      status: "True"
      type: Overridden
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: All of the works are synchronized to the latest
      observedGeneration: 1
      reason: AllWorkSynced
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: All corresponding work objects are applied
      observedGeneration: 1
      reason: AllWorkHaveBeenApplied
      status: "True"
      type: Applied
    - lastTransitionTime: "2024-05-07T23:32:49Z"
      message: The availability of work object crp-4-work is not trackable
      observedGeneration: 1
      reason: WorkNotTrackable
      status: "True"
      type: Available
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 1
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: Detected the new changes on the resources and started the rollout process
      observedGeneration: 1
      reason: RolloutStarted
      status: "True"
      type: RolloutStarted
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: No override rules are configured for the selected resources
      observedGeneration: 1
      reason: NoOverrideSpecified
      status: "True"
      type: Overridden
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: All of the works are synchronized to the latest
      observedGeneration: 1
      reason: AllWorkSynced
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: Work object crp-4-work is not applied
      observedGeneration: 1
      reason: NotAllWorkHaveBeenApplied
      status: "False"
      type: Applied
    failedPlacements:
    - condition:
        lastTransitionTime: "2024-05-07T23:32:40Z"
        message: 'Failed to apply manifest: failed to process the request due to a
          client error: resource exists and is not managed by the fleet controller
          and co-ownernship is disallowed'
        reason: ManifestsAlreadyOwnedByOthers
        status: "False"
        type: Applied
      kind: Namespace
      name: test-ns
      version: v1
  selectedResources:
  - kind: Namespace
    name: test-ns
    version: v1
  - group: apps
    kind: Deployment
    name: test-nginx
    namespace: test-ns
    version: v1
```

In the `ClusterResourcePlacement` status, `placementStatuses` for `kind-cluster-1` in the `failedPlacements` section, we get a clear message as to why the resource failed to apply on the member cluster.

To gain more insights also take a look at the `work` object, please check this [section](#how-and-where-to-find-the-correct-work-resource) for more details,

### Work status of kind-cluster-1:
```
 status:
  conditions:
  - lastTransitionTime: "2024-05-07T23:32:40Z"
    message: 'Apply manifest {Ordinal:0 Group: Version:v1 Kind:Namespace Resource:namespaces
      Namespace: Name:test-ns} failed'
    observedGeneration: 1
    reason: WorkAppliedFailed
    status: "False"
    type: Applied
  - lastTransitionTime: "2024-05-07T23:32:40Z"
    message: ""
    observedGeneration: 1
    reason: WorkAppliedFailed
    status: Unknown
    type: Available
  manifestConditions:
  - conditions:
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: 'Failed to apply manifest: failed to process the request due to a client
        error: resource exists and is not managed by the fleet controller and co-ownernship
        is disallowed'
      reason: ManifestsAlreadyOwnedByOthers
      status: "False"
      type: Applied
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: Manifest is not applied yet
      reason: ManifestApplyFailed
      status: Unknown
      type: Available
    identifier:
      kind: Namespace
      name: test-ns
      ordinal: 0
      resource: namespaces
      version: v1
  - conditions:
    - lastTransitionTime: "2024-05-07T23:32:40Z"
      message: Manifest is already up to date
      observedGeneration: 1
      reason: ManifestAlreadyUpToDate
      status: "True"
      type: Applied
    - lastTransitionTime: "2024-05-07T23:32:51Z"
      message: Manifest is trackable and available now
      observedGeneration: 1
      reason: ManifestAvailable
      status: "True"
      type: Available
    identifier:
      group: apps
      kind: Deployment
      name: test-nginx
      namespace: test-ns
      ordinal: 1
      resource: deployments
      version: v1
```

From looking at the `Work` status and specifically the `manifestConditions` section, we could see that the namespace could not be applied but the deployment within the namespace got propagated from hub to the member cluster.

### Resolution:
In this scenario, a potential solution is to delete the existing namespace on the member cluster. However, it's essential to note that this decision rests with the user, as the namespace might already contain resources.

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
