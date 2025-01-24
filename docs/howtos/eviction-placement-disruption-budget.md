# Using ClusterResourcePlacementEviction and ClusterResourcePlacementDisruptionBudget

This how-to guide discusses how to create `ClusterResourcePlacementEviction` objects and `ClusterResourcePlacementDisruptionBudget` objects to evict resources from member clusters and protect resources on member clusters from voluntary disruption, respectively.

## Evicting Resources from Member Clusters using ClusterResourcePlacementEviction

The `ClusterResourcePlacementEviction` object is used to remove resources from a member cluster once the resources have already been propagated from the hub cluster.

To successfully evict resources from a cluster, the user needs to specify:
- The name of the `ClusterResourcePlacement` object which propagated resources to the target cluster.
- The name of the target cluster from which we need to evict resources.

In this example, we will create a `ClusterResourcePlacement` object with PickAll placement policy to propagate resources to an existing `MemberCluster`, add a taint to the member cluster 
resource and then create a `ClusterResourcePlacementEviction` object to evict resources from the `MemberCluster`.

We will first create a namespace that we will propagate to the member cluster.

```
kubectl create ns test-ns
```

Then we will apply a `ClusterResourcePlacement` with the following spec:

```yaml
spec:
  resourceSelectors:
    - group: ""
      kind: Namespace
      version: v1          
      name: test-ns
  policy:
    placementType: PickAll
```

The `CRP` status after applying should look something like this:

```yaml
kubectl get crp test-crp
NAME       GEN   SCHEDULED   SCHEDULED-GEN   AVAILABLE   AVAILABLE-GEN   AGE
test-crp   2     True        2               True        2               5m49s
```

let's now add a taint to the member cluster to ensure this cluster is not picked again by the scheduler once we evict resources from it.

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

the eviction object should look like this, if the eviction was successful:

```yaml
kubectl get crpe test-eviction
NAME            VALID   EXECUTED
test-eviction   True    True
```

since the eviction is successful, the resources should be removed from the cluster, let's take a look at the `CRP` object status to verify:

```yaml
kubectl get crp test-crp
NAME       GEN   SCHEDULED   SCHEDULED-GEN   AVAILABLE   AVAILABLE-GEN   AGE
test-crp   2     True        2                                           15m
```

from the object we can clearly tell that the resources were evicted since the `AVAILABLE` column is empty. If the user needs more information `ClusterResourcePlacement` object's status can be checked.

## Protecting resources from voluntary disruptions using ClusterResourcePlacementDisruptionBudget

In this example, we will create a `ClusterResourcePlacement` object with PickN placement policy to propagate resources to an existing MemberCluster,
then create a `ClusterResourcePlacementDisruptionBudget` object to protect resources on the MemberCluster from voluntary disruption and 
then try to evict resources from the MemberCluster using `ClusterResourcePlacementEviction`.

We will first create a namespace that we will propagate to the member cluster.

```
kubectl create ns test-ns
```

Then we will apply a `ClusterResourcePlacement` with the following spec:

```yaml
spec:
  resourceSelectors:
    - group: ""
      kind: Namespace
      version: v1
      name: test-ns
  policy:
    placementType: PickN
    numberOfClusters: 1
```

The `CRP` object after applying should look something like this:

```yaml
kubectl get crp test-crp
NAME       GEN   SCHEDULED   SCHEDULED-GEN   AVAILABLE   AVAILABLE-GEN   AGE
test-crp   2     True        2               True        2               8s
```

Now we will create a `ClusterResourcePlacementDisruptionBudget` object to protect resources on the member cluster from voluntary disruption:

```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacementDisruptionBudget
metadata:
  name: test-crp
spec:
  minAvailable: 1
```

> **Note:** An eviction object is only reconciled once, after which it reaches a terminal state, if the user desires to create/apply the same eviction object again they need to delete the existing eviction object and re-create the object for the eviction to occur again.

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

> **Note:** The eviction controller will try to get the corresponding `ClusterResourcePlacementDisruptionBudget` object when a `ClusterResourcePlacementEviction` object is reconciled to check if the specified MaxAvailable or MinAvailable allows the eviction to be executed.

let's take a look at the eviction object to see if the eviction was executed,

```yaml
kubectl get crpe test-eviction
NAME            VALID   EXECUTED
test-eviction   True    False
```

from the eviction object we can see the eviction was not executed.

let's take a look at the `ClusterResourcePlacementEviction` object status to verify why the eviction was not executed:

```yaml
status:
  conditions:
  - lastTransitionTime: "2025-01-21T15:52:29Z"
    message: Eviction is valid
    observedGeneration: 1
    reason: ClusterResourcePlacementEvictionValid
    status: "True"
    type: Valid
  - lastTransitionTime: "2025-01-21T15:52:29Z"
    message: 'Eviction is blocked by specified ClusterResourcePlacementDisruptionBudget,
      availablePlacements: 1, totalPlacements: 1'
    observedGeneration: 1
    reason: ClusterResourcePlacementEvictionNotExecuted
    status: "False"
    type: Executed
```

the eviction status clearly mentions that the eviction was blocked by the specified `ClusterResourcePlacementDisruptionBudget`.