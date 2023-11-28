# Troubleshooting guide

## Overview:

This TSG is meant to help you troubleshoot issues with the Fleet APIs.

## Cluster Resource Placement:

Internal Objects to keep in mind when troubleshooting CRP related errors on the hub cluster:
 - **ClusterResourceSnapshot**
 - **ClusterSchedulingPolicySnapshot**
 - **ClusterResourceBinding**
 - **Work** 

please read the API reference for more details about ech object https://github.com/Azure/fleet/blob/main/docs/api-references.md

### How can I debug when my CRP status is ClusterResourcePlacementScheduled condition status is set to "False"?

Some scenarios where we might see this condition,
- When we specify the placement policy to **PickFixed** but specify cluster names which don't match any joined member cluster name in the fleet.
- When we specify the placement policy to **PickN** and specify N clusters, but we have less than N clusters that have joined the fleet.
- When we specify the placement policy to **PickAll** and the specified Affinity and Topology constraints doesn't allow the scheduler to pick any cluster that has joined the fleet.

The output below is for a CRP with PickN Placement policy trying to propagate resources to two clusters with label env:prod,

**CRP spec:**
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

**CRP status:**

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

We can also take a look at the **ClusterSchedulingPolicySnapshot** status to figure out why the scheduler could not schedule the resource for the placement policy specified.

The corresponding **ClusterSchedulingPolicySnapshot's** spec and status gives us even more information why scheduling failed,

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

### How to verify the latest ClusterSchedulingPolicySnapshot for a CRP?

- We need to have ClusterResourcePlacement's name **{CRPName}**, replace **{CRPName}** in the command below,

```
kubectl get clusterschedulingpolicysnapshot -l kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP={CRPName}
```

- Compare ClusterSchedulingPolicySnapshot with the CRP's policy to ensure they match (excluding numberOfClusters field from CRP's spec)
- The ClusterSchedulingPolicySnapshot has a label called **number-of-clusters** check to see if it matches the number of clusters requested in CRP's **PickN** placement policy.

### How can I debug when my CRP status is ClusterResourcePlacementSynchronized condition status is set to "False"?

ClusterResourcePlacementSynchronized condition status is set to "False" if the following occurs,
- The work is not created/updated for a new ClusterResourceSnapshot, ClusterResourceBinding for a given cluster.

In the **ClusterResourcePlacement** status section check to see which **placementStatuses** also has WorkSynchronized status set to **false**.

From the **placementStatus** we can get the **clusterName** and then check the fleet-member-{clusterName} namespace to see if a work objects exists/updated in this case it won't as WorkSynchronized has failed.

We need to find the corresponding ClusterResourceBinding for our ClusterResourcePlacement which should have the status of **work** create/update. 

### How to find the latest ClusterResourceBinding resource?

We need to have ClusterResourcePlacement's name **{CRPName}**, replace **{CRPName}** in the command below. The command below lists all ClusterResourceBindings associated with ClusterResourcePlacement

```
kubectl get clusterresourcebinding -l kubernetes-fleet.io/parent-CRP={CRPName}
```

example, In this case we have ClusterResourcePlacement called test-crp,

```
kubectl get crp test-crp
NAME       GEN   SCHEDULED   SCHEDULEDGEN   APPLIED   APPLIEDGEN   AGE
test-crp   1     True        1              True      1            15s
```

the placementstatuses of the CRP above looks like, it has propagated resources to two member clusters and hence has two ClusterResourceBindings,

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

from the placementstatuses we can focus on which cluster we want to consider and note the clusterName,

```
kubectl get clusterresourcebinding -l kubernetes-fleet.io/parent-CRP=test-crp 
NAME                               WORKCREATED   RESOURCESAPPLIED   AGE
test-crp-kind-cluster-1-be990c3e   True          True               33s
test-crp-kind-cluster-2-ec4d953c   True          True               33s
```

The ClusterResourceBinding's name follow this format **{CRPName}-{clusterName}-{suffix}**, so once we have all ClusterResourceBindings listed find the ClusterResourceBinding for the target cluster you are looking for based on the clusterName.

### How to find the latest ClusterResourceSnapshot resource?

Replace **{CRPName}** in the command below with name of CRP,

```
kubectl get clusterresourcesnapshot -l kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP={CRPName} -o YAML
```

### How can I debug when my CRP ClusterResourcePlacementApplied condition is set to "False"?

In the **ClusterResourcePlacement** status section check to see which **placementStatuses** also has ResourceApplied status set to false.

From the **placementStatuses** we can get the **clusterName** and then use it to find the work object associated with the member cluster in the **fleet-member-{ClusterName}** namespace in the hub cluster and check its status to figure out what's wrong.

### How and where to find the correct Work resource?

We need to have the member cluster's namespace **fleet-member-{clusterName}**, ClusterResourceBinding's name **{CRBName}** and ClusterResourcePlacement's name **{CRPName}**.

```
kubectl get work -n fleet-member-{clusterName} -l kubernetes-fleet.io/parent-CRP={CRPName},kubernetes-fleet.io/parent-resource-binding={CRBName} -o YAML
```

### How can I debug when some clusters are not selected as expected?

Check the status of the **ClusterSchedulingPolicySnapshot** to determine which clusters were selected along with the reason

### How can I debug when a selected cluster does not have the expected resources on it?

Possible reasons as to selected cluster does not have expected resources,
- The latest ClusterResourceSnapshot resource doesn't exist
- The work objects for the selected resources are still being created/updated on the hub cluster in the target cluster's namespace, meaning the **placementStatus** section in CRP status has **WorkSynchronized** condition set to **false** which in turn means **ClusterResourcePlacementSynchronized** condition in CRP's status is also set to **false** (In this case the user has to wait for work to be created/updated on the target member cluster namespace)
- The selected resources are being applied by the work objects on the target cluster, meaning the **placementStatus** section in CRP status has **ResourceApplied** condition set to **Unknown** which in turn means **ClusterResourcePlacementApplied** condition in CRPs status is also set to **Unknown** (In this case, the user has to wait for the condition to either turn true/false)
- The selected resources have failed to be applied on the target cluster by the work objects, meaning the **placementStatus** section in CRP status has **ResourceApplied** condition set to **False** which in turn means **ClusterResourcePlacementApplied** conditions in CRPs status is also set to **False** (Take a look at the work object's status to figure out what went wrong on the apply)

We need to take a look at the **placementStatuses** section in CRP status for that particular cluster in ClusterResourcePlacement's status. In **placementStatuses** we would find **failedPlacements** which should have the reason.

### How can I debug when my CRP doesn't pick up the latest change?

Possible reason as to why CRP doesn't pick up the latest change,
- The latest ClusterResourceSnapshot has not been created
- The latest ClusterSchedulingPolicySnapshot has not been created
- The scheduler has not created/updated the cluster resource binding