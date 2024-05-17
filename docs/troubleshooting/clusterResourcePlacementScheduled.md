# How can I debug when my CRP status is ClusterResourcePlacementScheduled condition status is set to false?
The `ClusterResourcePlacementScheduled` condition is set to `false` when the scheduler cannot find all the clusters needed as specified by the scheduling policy.
> Note: In addition, it may be helpful to look into the logs for the scheduler controller to get more information on why the scheduling failed.

### Common scenarios:

Instances where this condition may arise:

- When the placement policy is set to `PickFixed`, but the specified cluster names do not match any joined member cluster name in the fleet, or the specified cluster is no longer connected to the fleet.
- When the placement policy is set to `PickN`, and N clusters are specified, but there are fewer than N clusters that have joined the fleet or satisfy the placement policy.
- When the CRP resource selector selects a reserved namespace.

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