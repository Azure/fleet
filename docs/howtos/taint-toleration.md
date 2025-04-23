# Adding/Removing taints on MemberCluster and Adding tolerations on ClusterResourcePlacement

This how-to guide discusses how to add/remove taints on `MemberCluster` and how to add tolerations on `ClusterResourcePlacement`.

## Adding taint to MemberCluster

In this example, we will add a taint to a `MemberCluster`. Then try to propagate resources to the `MemberCluster` using a `ClusterResourcePlacement`
with **PickAll** placement policy. The resources should not be propagated to the `MemberCluster` because of the taint.

We will first create a namespace that we will propagate to the member cluster,

```
kubectl create ns test-ns
```

Then apply the `MemberCluster` with a taint,

Example `MemberCluster` with taint:

```yaml
apiVersion: cluster.kubernetes-fleet.io/v1beta1
kind: MemberCluster
metadata:
  name: kind-cluster-1
spec:
  identity:
    name: fleet-member-agent-cluster-1
    kind: ServiceAccount
    namespace: fleet-system
    apiGroup: ""
  taints:
    - key: test-key1
      value: test-value1
      effect: NoSchedule
```

After applying the above `MemberCluster`, we will apply a `ClusterResourcePlacement` with the following spec:

```yaml
  resourceSelectors:
    - group: ""
      kind: Namespace
      version: v1          
      name: test-ns
  policy:
    placementType: PickAll
```

The `ClusterResourcePlacement` CR should not propagate the `test-ns` namespace to the member cluster because of the taint, 
looking at the status of the CR should show the following:

```yaml
status:
  conditions:
  - lastTransitionTime: "2024-04-16T19:03:17Z"
    message: found all the clusters needed as specified by the scheduling policy
    observedGeneration: 2
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2024-04-16T19:03:17Z"
    message: All 0 cluster(s) are synchronized to the latest resources on the hub
      cluster
    observedGeneration: 2
    reason: SynchronizeSucceeded
    status: "True"
    type: ClusterResourcePlacementSynchronized
  - lastTransitionTime: "2024-04-16T19:03:17Z"
    message: There are no clusters selected to place the resources
    observedGeneration: 2
    reason: ApplySucceeded
    status: "True"
    type: ClusterResourcePlacementApplied
  observedResourceIndex: "0"
  selectedResources:
  - kind: Namespace
    name: test-ns
    version: v1
```

Looking at the `ClusterResourcePlacementSynchronized`, `ClusterResourcePlacementApplied` conditions and reading the message fields 
we can see that no clusters were selected to place the resources.

## Removing taint from MemberCluster

In this example, we will remove the taint from the `MemberCluster` from the last section. This should automatically trigger the Fleet scheduler to propagate the resources to the `MemberCluster`.

After removing the taint from the `MemberCluster`. Let's take a look at the status of the `ClusterResourcePlacement`:

```yaml
status:
  conditions:
  - lastTransitionTime: "2024-04-16T20:00:03Z"
    message: found all the clusters needed as specified by the scheduling policy
    observedGeneration: 2
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2024-04-16T20:02:57Z"
    message: All 1 cluster(s) are synchronized to the latest resources on the hub
      cluster
    observedGeneration: 2
    reason: SynchronizeSucceeded
    status: "True"
    type: ClusterResourcePlacementSynchronized
  - lastTransitionTime: "2024-04-16T20:02:57Z"
    message: Successfully applied resources to 1 member clusters
    observedGeneration: 2
    reason: ApplySucceeded
    status: "True"
    type: ClusterResourcePlacementApplied
  observedResourceIndex: "0"
  placementStatuses:
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2024-04-16T20:02:52Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 2
      reason: ScheduleSucceeded
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-04-16T20:02:57Z"
      message: Successfully Synchronized work(s) for placement
      observedGeneration: 2
      reason: WorkSynchronizeSucceeded
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2024-04-16T20:02:57Z"
      message: Successfully applied resources
      observedGeneration: 2
      reason: ApplySucceeded
      status: "True"
      type: Applied
  selectedResources:
  - kind: Namespace
    name: test-ns
    version: v1
```

From the status we can clearly see that the resources were propagated to the member cluster after removing the taint.

## Adding toleration to ClusterResourcePlacement

Adding a toleration to a `ClusterResourcePlacement` CR allows the Fleet scheduler to tolerate specific taints on the `MemberClusters`.

For this section we will start from scratch, we will first create a namespace that we will propagate to the `MemberCluster`

```
kubectl create ns test-ns
```

Then apply the `MemberCluster` with a taint,

Example `MemberCluster` with taint:

```yaml
spec:
  heartbeatPeriodSeconds: 60
  identity:
    apiGroup: ""
    kind: ServiceAccount
    name: fleet-member-agent-cluster-1
    namespace: fleet-system
  taints:
    - effect: NoSchedule
      key: test-key1
      value: test-value1
```

The `ClusterResourcePlacement` CR will not propagate the `test-ns` namespace to the member cluster because of the taint.

Now we will add a toleration to a `ClusterResourcePlacement` CR as part of the placement policy, which will use the Exists operator to tolerate the taint.

Example `ClusterResourcePlacement` spec with tolerations after adding new toleration:

```yaml
spec:
  policy:
    placementType: PickAll
    tolerations:
      - key: test-key1
        operator: Exists
  resourceSelectors:
    - group: ""
      kind: Namespace
      name: test-ns
      version: v1
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
```

Let's take a look at the status of the `ClusterResourcePlacement` CR after adding the toleration:

```yaml
status:
  conditions:
    - lastTransitionTime: "2024-04-16T20:16:10Z"
      message: found all the clusters needed as specified by the scheduling policy
      observedGeneration: 3
      reason: SchedulingPolicyFulfilled
      status: "True"
      type: ClusterResourcePlacementScheduled
    - lastTransitionTime: "2024-04-16T20:16:15Z"
      message: All 1 cluster(s) are synchronized to the latest resources on the hub
        cluster
      observedGeneration: 3
      reason: SynchronizeSucceeded
      status: "True"
      type: ClusterResourcePlacementSynchronized
    - lastTransitionTime: "2024-04-16T20:16:15Z"
      message: Successfully applied resources to 1 member clusters
      observedGeneration: 3
      reason: ApplySucceeded
      status: "True"
      type: ClusterResourcePlacementApplied
  observedResourceIndex: "0"
  placementStatuses:
    - clusterName: kind-cluster-1
      conditions:
        - lastTransitionTime: "2024-04-16T20:16:10Z"
          message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
          observedGeneration: 3
          reason: ScheduleSucceeded
          status: "True"
          type: Scheduled
        - lastTransitionTime: "2024-04-16T20:16:15Z"
          message: Successfully Synchronized work(s) for placement
          observedGeneration: 3
          reason: WorkSynchronizeSucceeded
          status: "True"
          type: WorkSynchronized
        - lastTransitionTime: "2024-04-16T20:16:15Z"
          message: Successfully applied resources
          observedGeneration: 3
          reason: ApplySucceeded
          status: "True"
          type: Applied
  selectedResources:
    - kind: Namespace
      name: test-ns
      version: v1
```

From the status we can see that the resources were propagated to the `MemberCluster` after adding the toleration.

Now let's try adding a new taint to the member cluster CR and see if the resources are still propagated to the `MemberCluster`,

Example `MemberCluster` CR with new taint:

```yaml
  heartbeatPeriodSeconds: 60
  identity:
    apiGroup: ""
    kind: ServiceAccount
    name: fleet-member-agent-cluster-1
    namespace: fleet-system
  taints:
  - effect: NoSchedule
    key: test-key1
    value: test-value1
  - effect: NoSchedule
    key: test-key2
    value: test-value2
```

Let's take a look at the `ClusterResourcePlacement` CR status after adding the new taint:

```yaml
status:
  conditions:
  - lastTransitionTime: "2024-04-16T20:27:44Z"
    message: found all the clusters needed as specified by the scheduling policy
    observedGeneration: 2
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2024-04-16T20:27:49Z"
    message: All 1 cluster(s) are synchronized to the latest resources on the hub
      cluster
    observedGeneration: 2
    reason: SynchronizeSucceeded
    status: "True"
    type: ClusterResourcePlacementSynchronized
  - lastTransitionTime: "2024-04-16T20:27:49Z"
    message: Successfully applied resources to 1 member clusters
    observedGeneration: 2
    reason: ApplySucceeded
    status: "True"
    type: ClusterResourcePlacementApplied
  observedResourceIndex: "0"
  placementStatuses:
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2024-04-16T20:27:44Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 2
      reason: ScheduleSucceeded
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-04-16T20:27:49Z"
      message: Successfully Synchronized work(s) for placement
      observedGeneration: 2
      reason: WorkSynchronizeSucceeded
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2024-04-16T20:27:49Z"
      message: Successfully applied resources
      observedGeneration: 2
      reason: ApplySucceeded
      status: "True"
      type: Applied
  selectedResources:
  - kind: Namespace
    name: test-ns
    version: v1
```

Nothing changes in the status because even if the new taint is not tolerated, the existing resources on the `MemberCluster`
will continue to run because the taint effect is `NoSchedule` and the cluster was already selected for resource propagation in a 
previous scheduling cycle.
