# How-to Guide: To evict resources from member clusters using ClusterResourcePlacementEviction and protect resources on member clusters from voluntary disruption using ClusterResourcePlacementDisruptionBudget

This how-to guide discusses how to create ClusterResourcePlacementEviction objects and ClusterResourcePlacementDisruptionBudget objects to evict resources from member clusters and protect resources on member clusters from voluntary disruption, respectively.

## Evicting Resources from Member Clusters using ClusterResourcePlacementEviction

The ClusterResourcePlacementEviction object is used to remove resources from a member cluster once the resources have already been propagated from the hub cluster.

To successfully evict resources from a cluster, the user needs to specify:
- The name of the ClusterResourcePlacement object which propagated resources to the target cluster
- The name of the target cluster from which we need to evict resources.

In this example, we will create a ClusterResourcePlacement object with PickAll placement policy to propagate resources to an existing MemberCluster, add a taint to the member cluster 
resource and then create a ClusterResourcePlacementEviction object to evict resources from the MemberCluster.

We will first create a namespace that we will propagate to the member cluster

```
kubectl create ns test-ns
```

Then we will apply a `ClusterResourcePlacement` with the following spec:

```yaml
  resourceSelectors:
    - group: ""
      kind: Namespace
      version: v1          
      name: test-ns
  policy:
    placementType: PickAll
```

The CRP status after applying should look something like this:

```yaml
status:
  conditions:
  - lastTransitionTime: "2025-01-19T11:43:31Z"
    message: found all cluster needed as specified by the scheduling policy, found
      1 cluster(s)
    observedGeneration: 2
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2025-01-19T11:43:31Z"
    message: All 1 cluster(s) start rolling out the latest resource
    observedGeneration: 2
    reason: RolloutStarted
    status: "True"
    type: ClusterResourcePlacementRolloutStarted
  - lastTransitionTime: "2025-01-19T11:43:31Z"
    message: No override rules are configured for the selected resources
    observedGeneration: 2
    reason: NoOverrideSpecified
    status: "True"
    type: ClusterResourcePlacementOverridden
  - lastTransitionTime: "2025-01-19T11:43:31Z"
    message: Works(s) are succcesfully created or updated in 1 target cluster(s)'
      namespaces
    observedGeneration: 2
    reason: WorkSynchronized
    status: "True"
    type: ClusterResourcePlacementWorkSynchronized
  - lastTransitionTime: "2025-01-19T11:43:31Z"
    message: The selected resources are successfully applied to 1 cluster(s)
    observedGeneration: 2
    reason: ApplySucceeded
    status: "True"
    type: ClusterResourcePlacementApplied
  - lastTransitionTime: "2025-01-19T11:43:31Z"
    message: The selected resources in 1 cluster(s) are available now
    observedGeneration: 2
    reason: ResourceAvailable
    status: "True"
    type: ClusterResourcePlacementAvailable
  observedResourceIndex: "0"
  placementStatuses:
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2025-01-19T11:43:31Z"
      message: 'Successfully scheduled resources for placement in "kind-cluster-1"
        (affinity score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 2
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2025-01-19T11:43:31Z"
      message: Detected the new changes on the resources and started the rollout process
      observedGeneration: 2
      reason: RolloutStarted
      status: "True"
      type: RolloutStarted
    - lastTransitionTime: "2025-01-19T11:43:31Z"
      message: No override rules are configured for the selected resources
      observedGeneration: 2
      reason: NoOverrideSpecified
      status: "True"
      type: Overridden
    - lastTransitionTime: "2025-01-19T11:43:31Z"
      message: All of the works are synchronized to the latest
      observedGeneration: 2
      reason: AllWorkSynced
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2025-01-19T11:43:31Z"
      message: All corresponding work objects are applied
      observedGeneration: 2
      reason: AllWorkHaveBeenApplied
      status: "True"
      type: Applied
    - lastTransitionTime: "2025-01-19T11:43:31Z"
      message: All corresponding work objects are available
      observedGeneration: 2
      reason: AllWorkAreAvailable
      status: "True"
      type: Available
  selectedResources:
  - kind: Namespace
    name: test-ns
    version: v1
```

let's now add a taint to the member cluster to ensure this cluster is not picked again one we evict resources from it.

Modify the cluster object to add a taint:

```yaml
spec:
  heartbeatPeriodSeconds: 60
  identity:
    kind: ServiceAccount
    name: fleet-member-agent-cluster-1
    namespace: fleet-system
  taints:
    - effect: NoSchedule
      key: test-key
      value: test-value
```

Now we will create a `ClusterResourcePlacementEviction` object to evict resources from the member cluster:

```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacementEviction
metadata:
  name: test-eviction
spec:
  placementName: test-crp
  clusterName: kind-cluster-1
```

the eviction status let's us know if the eviction was successful:

```yaml
status:
  conditions:
    - lastTransitionTime: "2025-01-19T12:10:01Z"
      message: Eviction is valid
      observedGeneration: 1
      reason: ClusterResourcePlacementEvictionValid
      status: "True"
      type: Valid
    - lastTransitionTime: "2025-01-19T12:10:01Z"
      message: Eviction is allowed, no ClusterResourcePlacementDisruptionBudget specified
      observedGeneration: 1
      reason: ClusterResourcePlacementEvictionExecuted
      status: "True"
      type: Executed
```

since the eviction is successful, the resources should be removed from the cluster let's take a look at the CRP object's status to confirm:

```yaml
status:
  conditions:
    - lastTransitionTime: "2025-01-19T11:43:31Z"
      message: found all cluster needed as specified by the scheduling policy, found
        0 cluster(s)
      observedGeneration: 2
      reason: SchedulingPolicyFulfilled
      status: "True"
      type: ClusterResourcePlacementScheduled
  observedResourceIndex: "0"
  selectedResources:
    - kind: Namespace
      name: test-ns
      version: v1
```

The status shows that the resources have been removed from the cluster and the only reason the scheduler doesn't re-pick the cluster is because of the taint we added.