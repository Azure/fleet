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

The output below is for a **CRP** with **PickN** Placement policy trying to propagate resources to two clusters with label **env:prod**, In this case two clusters are joined to the fleet called **kind-cluster-1**, **kind-cluster-2** where one member cluster **kind-cluster-1** has label **env:prod** on it.

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

**ClusterResourcePlacementScheduled** is set to **false** because we want to pick two clusters with label **env:prod** but only one member cluster has the correct label mentioned in **clusterAffinity**

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

The solution here is to add the **env:prod** label to the member cluster resource for **kind-cluster-2** as well so that the scheduler can pick the cluster to propagate resources.

### How to verify the latest ClusterSchedulingPolicySnapshot for a CRP?

- We need to have ClusterResourcePlacement's name **{CRPName}**, replace **{CRPName}** in the command below,

```
kubectl get clusterschedulingpolicysnapshot -l kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP={CRPName}
```

- Compare ClusterSchedulingPolicySnapshot with the **CRP's** policy to ensure they match (excluding numberOfClusters field from **CRP's** spec)
- The ClusterSchedulingPolicySnapshot has a label called **number-of-clusters** check to see if it matches the number of clusters requested in **CRP's** **PickN** placement policy.

### How can I debug when my CRP status is ClusterResourcePlacementSynchronized condition status is set to "False"?

**ClusterResourcePlacementSynchronized** condition status is set to "False" if the following occurs, the work is not created/updated for a new **ClusterResourceSnapshot**, **ClusterResourceBinding** for a given cluster.

In the **ClusterResourcePlacement** status section check to see which **placementStatuses** also has WorkSynchronized status set to **false**.

From the **placementStatus** we can get the **clusterName** and then check the **fleet-member-{clusterName}** namespace to see if a work objects exists/updated in this case it won't as **WorkSynchronized** has failed.

We need to find the corresponding **ClusterResourceBinding** for our **ClusterResourcePlacement** which should have the status of **work** create/update.

A common case where this could happen is user input for the **rollingUpdate** config it too strict for rolling update strategy.

In the example below we try to propagate a namespace to 3 member clusters but initially when the **CRP** is created the namespace doesn't exist on the hub cluster and the fleet currently has two member clusters called **kind-cluster-1, kind-cluster-2** joined.

**CRP spec:**

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

**CRP status:**
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

Since the resource **test-ns** namespace never existed on the hub cluster **ClusterResourcePlacementApplied** is set to true but **ClusterResourcePlacementScheduled** is set to false since the spec wants to pick 3 clusters but the **scheduler** can only schedule to two available joined clusters.

Now we will go ahead and create the namespace **test-ns** on the hub cluster, ideally we expect the namespace to be propagated,

**CRP status after namespace test-ns is created on the hub cluster:**
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

we see that **ClusterResourcePlacementSynchronized** is set to false and the message reads **"Works need to be synchronized on the hub cluster or there are still manifests pending to be processed by the 2 member clusters"**. We have this situation cause **rollingUpdate** input was not specified by the user and hence by default,

**maxUnavailable** is set to 1 and **maxSurge** is set to 1.

Meaning after the CRP was created first two **ClusterResourceBindings** were created and since the namespace didn't exist on the hub cluster we did not have to create work and **ClusterResourcePlacementSynchronized** was set to **true**.

But once we create the **test-ns** namespace on the hub the rollout controller tries to pick the two **ClusterResourceBindings** to update, but we have **maxUnavailable** set to 1 which is already the case since we have one missing member cluster now if when the rollout controller tries to roll out the updated **ClusterResourceBindings** and even if one of them fails to apply we break the criteria of the **rollout config** since **maxUnavailable** is set 1.

The solution to this particular case is to manually set maxUnavailable to a higher value than 2 to avoid this scenario.

### How to find the latest ClusterResourceBinding resource?

We need to have ClusterResourcePlacement's name **{CRPName}**, replace **{CRPName}** in the command below. The command below lists all **ClusterResourceBindings** associated with **ClusterResourcePlacement**

```
kubectl get clusterresourcebinding -l kubernetes-fleet.io/parent-CRP={CRPName}
```

example, In this case we have **CRP** called test-crp,

```
kubectl get crp test-crp
NAME       GEN   SCHEDULED   SCHEDULEDGEN   APPLIED   APPLIEDGEN   AGE
test-crp   1     True        1              True      1            15s
```

the **placementStatuses** of the **CRP** above looks like, it has propagated resources to two member clusters and hence has two **ClusterResourceBindings**,

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

from the **placementStatuses** we can focus on which cluster we want to consider and note the **clusterName**,

```
kubectl get clusterresourcebinding -l kubernetes-fleet.io/parent-CRP=test-crp 
NAME                               WORKCREATED   RESOURCESAPPLIED   AGE
test-crp-kind-cluster-1-be990c3e   True          True               33s
test-crp-kind-cluster-2-ec4d953c   True          True               33s
```

The ClusterResourceBinding's name follow this format **{CRPName}-{clusterName}-{suffix}**, so once we have all ClusterResourceBindings listed find the ClusterResourceBinding for the target cluster you are looking for based on the clusterName.

### How to find the latest ClusterResourceSnapshot resource?

Replace **{CRPName}** in the command below with name of **CRP**,

```
kubectl get clusterresourcesnapshot -l kubernetes-fleet.io/is-latest-snapshot=true,kubernetes-fleet.io/parent-CRP={CRPName} -o YAML
```

### How can I debug when my CRP ClusterResourcePlacementApplied condition is set to "False"?

In the **ClusterResourcePlacement** status section check to see which **placementStatuses** also has ResourceApplied status set to false.

From the **placementStatuses** we can get the **clusterName** and then use it to find the work object associated with the member cluster in the **fleet-member-{ClusterName}** namespace in the hub cluster and check its status to figure out what's wrong.

example, in this case the **CRP** is trying to propagate a namespace which contains a deployment to two member clusters, but the namespace already exists on one member cluster called **kind-cluster-1**

**CRP spec:**

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

**CRP status:**

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

in the **placementStatuses** section of the **CRP status** for kind-cluster-1 in **failedPlacements** we get a clear message as to why the resource failed to apply on the member cluster.

At times, we might need more information in that case please take a look at the work object

**work status:**
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

from looking at the **work status** and specifically the **manifestConditions** section we could see that the namespace could not be applied but the deployment within the namespace got propagated from hub to the member cluster correctly. In this case to solve this issue maybe delete the existing namespace on the member cluster but that's upto the user to decide since the namespace could already contain resources within it.

### How and where to find the correct Work resource?

We need to have the member cluster's namespace **fleet-member-{clusterName}**, ClusterResourceBinding's name **{CRBName}** and ClusterResourcePlacement's name **{CRPName}**.

```
kubectl get work -n fleet-member-{clusterName} -l kubernetes-fleet.io/parent-CRP={CRPName},kubernetes-fleet.io/parent-resource-binding={CRBName} -o YAML
```

### How can I debug when some clusters are not selected as expected?

Check the status of the **ClusterSchedulingPolicySnapshot** to determine which clusters were selected along with the reason.

### How can I debug when a selected cluster does not have the expected resources on it/ if CRP doesn't pick up the latest changes?

Please check the following cases,
- check to see if **ClusterResourcePlacementSynchronized** condition in CRP status is set to **true** or **false**
- If it's set to **false** check the question above '**_How can I debug when my CRP status is ClusterResourcePlacementSynchronized condition status is set to "False"_**'
- If it's set to **true**,
  - check to see if **ClusterResourcePlacementApplied** condition is set to **unknown**, **false** or **true**
  - if it's set to **unknown** please wait as the resources are still being applied to the member clusters (if it's stuck in unknown state please raise a github issue as it's an unexpected behavior)
  - if it's set to **false** check the question above '**_How can I debug when my CRP ClusterResourcePlacementApplied condition is set to "False"_**'
  - if it's set to **true** check to see if the resource exists on the hub cluster, the ClusterResourcePlacementApplied condition is set to true if the resource doesn't exist on the hub

We can also take a look at the **placementStatuses** section in CRP status for that particular cluster in **ClusterResourcePlacement's** status. In **placementStatuses** we would find **failedPlacements** which should have the reasons as to why they failed to apply.