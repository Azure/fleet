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

The output below is for a CRP with PickN Placement policy trying to propagate resources to clusters with label env:prod, 

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
  - group: apps
    kind: Deployment
    name: test-nginx
    namespace: test-ns
    version: v1
  - kind: Namespace
    name: test-ns
    version: v1
```

We can also take a look at the **ClusterSchedulingPolicySnapshot** status to figure out why the scheduler could not schedule the resource for the placement policy specified.

The corresponding **ClusterSchedulingPolicySnapshot's** spec and status gives us even more information why scheduling failed,

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
    placementType: PickN
  policyHash: ZjE0Yjk4YjYyMTVjY2U3NzQ1MTZkNWRhZjRiNjQ1NzQ4NjllNTUyMzZkODBkYzkyYmRkMGU3OTI3MWEwOTkyNQ==
status:
  conditions:
  - lastTransitionTime: "2023-11-27T20:25:19Z"
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

### How to find the latest ClusterSchedulingSnapshot resource?

We need to have ClusterResourcePlacement's name **{CRPName}**, replace **{CRPName}** in the command below,

```
$ kubectl get clusterschedulingpolicysnapshot -l kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP={CRPName}
```

### How can I debug when my CRP status is ClusterResourcePlacementSynchronized condition status is set to "False"?

In the **ClusterResourcePlacement** status section check to see which **placementStatuses** also has WorkSynchronized status set to **false**.

From the **placementStatus** we can get the **clusterName** and then check the fleet-member-{clusterName} namespace to see if a work objects exists/updated in this case it won't as WorkSynchronized has failed.

We need to find the corresponding ClusterResourceBinding for our ClusterResourcePlacement which should have the status of **work** create/update. 

### How to find the latest ClusterResourceBinding resource?

We need to have ClusterResourcePlacement's name **{CRPName}**, replace **{CRPName}** in the command below. The command below lists all ClusterResourceBindings associated with ClusterResourcePlacement

```
$ kubectl get clusterresourcebinding -l kubernetes-fleet.io/parent-CRP={CRPName}
```

example, In this case we have ClusterResourcePlacement called test-crp,

```
$ kubectl get crp test-crp
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
$ kubectl get clusterresourcebinding -l kubernetes-fleet.io/parent-CRP=test-crp 
NAME                               WORKCREATED   RESOURCESAPPLIED   AGE
test-crp-kind-cluster-1-be990c3e   True          True               33s
test-crp-kind-cluster-2-ec4d953c   True          True               33s
```

The ClusterResourceBinding's name follow this format **{CRPName}-{clusterName}-{suffix}**, so once we have all ClusterResourceBindings listed find the ClusterResourceBinding for the target cluster you are looking for based on the clusterName.

### How can I debug when my CRP ClusterResourcePlacementApplied condition is set to "False"?

In the **ClusterResourcePlacement** status section check to see which **placementStatuses** also has ResourceApplied status set to false.

From the **placementStatuses** we can get the **clusterName** and then use it to find the work object associated with the member cluster in the **fleet-member-{ClusterName}** namespace in the hub cluster and check its status to figure out what's wrong.

### How and where to find the correct Work resource?

We need to have the member cluster's namespace **fleet-member-{clusterName}**, ClusterResourceBinding's name **{CRBName}** and ClusterResourcePlacement's name **{CRPName}**.

```
$ kubectl get work -n fleet-member-{clusterName} -l kubernetes-fleet.io/parent-CRP={CRPName},kubernetes-fleet.io/parent-resource-binding={CRBName} -o YAML
```

### How can I debug when some clusters are not selected as expected?

Check the status of the **ClusterSchedulingPolicySnapshot** to determine which clusters were selected along with the reason

### How can I debug when a selected cluster does not have the expected resources on it?

We need to take a look at the **placementStatuses** section in CRP status for that particular cluster in ClusterResourcePlacement's status. In **placementStatuses** we would find **failedPlacements** which should have the reason

### How to find the latest ClusterResourceSnapshot resource?

Replace **{CRPName}** in the command below with name of CRP

```
$ kubectl get clusterresourcesnapshot -l kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP={CRPName} -o YAML
```

### How can I debug when my CRP doesn't pick up the latest change?