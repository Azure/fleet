# ClusterResourcePlacement

## Overview

`ClusterResourcePlacement` concept is used to dynamically select cluster scoped resources (especially namespaces and all 
objects within it) and control how they are propagated to all or a subset of the member clusters.
A `ClusterResourcePlacement` mainly consists of three parts:
- **Resource selection**: select which cluster-scoped Kubernetes
resource objects need to be propagated from the hub cluster to selected member clusters. 
  
  It supports the following forms of resource selection:
  - Select resources by specifying just the <group, version, kind>. This selection propagates all resources with matching <group, version, kind>. 
  - Select resources by specifying the <group, version, kind> and name. This selection propagates only one resource that matches the <group, version, kind> and name. 
  - Select resources by specifying the <group, version, kind> and a set of labels using ClusterResourcePlacement -> LabelSelector. 
This selection propagates all resources that match the <group, version, kind> and label specified.

  **Note:** When a namespace is selected, all the namespace-scoped objects under this namespace are propagated to the 
selected member clusters along with this namespace.

- **Placement policy**: limit propagation of selected resources to a specific subset of member clusters.
  The following types of target cluster selection are supported:
    - **PickAll (Default)**: select any member clusters with matching cluster `Affinity` scheduling rules. If the `Affinity` 
is not specified, it will select all joined and healthy member clusters.
    - **PickFixed**: select a fixed list of member clusters defined in the `ClusterNames`.
    - **PickN**: select a `NumberOfClusters` of member clusters with optional matching cluster `Affinity` scheduling rules or topology spread constraints `TopologySpreadConstraints`.

- **Rollout strategy**: how to propagate new changes to the selected member clusters.

A simple `ClusterResourcePlacement` looks like this:

```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: crp-1
spec:
  policy:
    placementType: PickN
    numberOfClusters: 2
    topologySpreadConstraints:
      - maxSkew: 1
        topologyKey: "env"
        whenUnsatisfiable: DoNotSchedule
  resourceSelectors:
    - group: ""
      kind: Namespace
      name: test-deployment
      version: v1
  revisionHistoryLimit: 100
  strategy:
    rollingUpdate:
      maxSurge: 25%
      maxUnavailable: 25%
      unavailablePeriodSeconds: 5
    type: RollingUpdate
```

## When To Use `ClusterResourcePlacement`

`ClusterResourcePlacement` is useful when you want for a general way of managing and running workloads across multiple clusters. 
Some example scenarios include the following:
-  As a platform operator, I want to place my cluster-scoped resources (especially namespaces and all objects within it) 
to a cluster that resides in the us-east-1.
-  As a platform operator, I want to spread my cluster-scoped resources (especially namespaces and all objects within it) 
evenly across the different regions/zones.
- As a platform operator, I prefer to place my test resources into the staging AKS cluster.
- As a platform operator, I would like to separate the workloads for compliance or policy reasons.
- As a developer, I want to run my cluster-scoped resources (especially namespaces and all objects within it) on 3 clusters. 
In addition, each time I update my workloads, the updates take place with zero downtime by rolling out to these three clusters incrementally.

## Placement Workflow

![](placement-concept-overview.jpg)

The placement workflow will be divided into several stages:
* Creating `ClusterSchedulingPolicySnapshot` and `ClusterResourceSnapshot` snapshots: clusterResourcePlacement controller 
captures the selected resources and placement policy specified in the `ClusterResourcePlacement`.
* Creating `ClusterResourceBinding`: multi-cluster scheduler creates the `clusterResourceBinding` based on the scheduling 
decisions for each target cluster and rollout controller binds selected resources according to the rollout strategy.
* Creating `Work` on the member cluster reserved namespace of the hub cluster: work generator creates the work on the 
corresponding member cluster namespace. Each work contains the manifest workload to be deployed on the member clusters.
* Applying `Work` on the member clusters: apply work controller applies the manifest workload on the member clusters.

## Resource Selection

Resource selectors identify cluster-scoped objects to include based on standard Kubernetes identifiers - namely, the `group`, 
`kind`, `version`, and `name` of the object. Namespace-scoped objects are included automatically when the namespace they
are part of is selected. The example `ClusterResourcePlacement` above would include the `test-deployment` namespace and 
any objects that were created in that namespace.

The clusterResourcePlacement controller creates the `ClusterResourceSnapshot` to store a snapshot of selected resources
selected by the placement. The `ClusterResourceSnapshot` spec is immutable. Each time when the selected resources are updated,
the clusterResourcePlacement controller will detect the resource changes and create a new `ClusterResourceSnapshot`. It implies
that resources can change independently of any modifications to the `ClusterResourceSnapshot`. In other words, resource
changes can occur without directly affecting the `ClusterResourceSnapshot` itself.

The total amount of selected resources may exceed the 1MB limit for a single Kubernetes object. As a result, the controller 
may produce more than one `ClusterResourceSnapshot`s for all the selected resources.

`ClusterResourceSnapshot` sample:
```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourceSnapshot
metadata:
  annotations:
    kubernetes-fleet.io/number-of-enveloped-object: "0"
    kubernetes-fleet.io/number-of-resource-snapshots: "1"
    kubernetes-fleet.io/resource-hash: e0927e7d75c7f52542a6d4299855995018f4a6de46edf0f814cfaa6e806543f3
  creationTimestamp: "2023-11-10T08:23:38Z"
  generation: 1
  labels:
    kubernetes-fleet.io/is-latest-snapshot: "true"
    kubernetes-fleet.io/parent-CRP: crp-1
    kubernetes-fleet.io/resource-index: "4"
  name: crp-1-4-snapshot
  ownerReferences:
  - apiVersion: placement.kubernetes-fleet.io/v1beta1
    blockOwnerDeletion: true
    controller: true
    kind: ClusterResourcePlacement
    name: crp-1
    uid: 757f2d2c-682f-433f-b85c-265b74c3090b
  resourceVersion: "1641940"
  uid: d6e2108b-882b-4f6c-bb5e-c5ec5491dd20
spec:
  selectedResources:
  - apiVersion: v1
    kind: Namespace
    metadata:
      labels:
        kubernetes.io/metadata.name: test
      name: test
    spec:
      finalizers:
      - kubernetes
  - apiVersion: v1
    data:
      key1: value1
      key2: value2
      key3: value3
    kind: ConfigMap
    metadata:
      name: test-1
      namespace: test
```

## Placement Policy

`ClusterResourcePlacement` supports three types of policy as mentioned above. `ClusterSchedulingPolicySnapshot` will be
generated whenever policy changes are made to the `ClusterResourcePlacement` that require a new scheduling. Similar to
`ClusterResourceSnapshot`, its spec is immutable.

`ClusterSchedulingPolicySnapshot` sample:
```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterSchedulingPolicySnapshot
metadata:
  annotations:
    kubernetes-fleet.io/CRP-generation: "5"
    kubernetes-fleet.io/number-of-clusters: "2"
  creationTimestamp: "2023-11-06T10:22:56Z"
  generation: 1
  labels:
    kubernetes-fleet.io/is-latest-snapshot: "true"
    kubernetes-fleet.io/parent-CRP: crp-1
    kubernetes-fleet.io/policy-index: "1"
  name: crp-1-1
  ownerReferences:
  - apiVersion: placement.kubernetes-fleet.io/v1beta1
    blockOwnerDeletion: true
    controller: true
    kind: ClusterResourcePlacement
    name: crp-1
    uid: 757f2d2c-682f-433f-b85c-265b74c3090b
  resourceVersion: "1639412"
  uid: 768606f2-aa5a-481a-aa12-6e01e6adbea2
spec:
  policy:
    placementType: PickN
  policyHash: NDc5ZjQwNWViNzgwOGNmYzU4MzY2YjI2NDg2ODBhM2E4MTVlZjkxNGZlNjc1NmFlOGRmMGQ2Zjc0ODg1NDE2YQ==
status:
  conditions:
  - lastTransitionTime: "2023-11-06T10:22:56Z"
    message: found all the clusters needed as specified by the scheduling policy
    observedGeneration: 1
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: Scheduled
  observedCRPGeneration: 5
  targetClusters:
  - clusterName: aks-member-1
    clusterScore:
      affinityScore: 0
      priorityScore: 0
    reason: picked by scheduling policy
    selected: true
  - clusterName: aks-member-2
    clusterScore:
      affinityScore: 0
      priorityScore: 0
    reason: picked by scheduling policy
    selected: true
```


![](scheduling.jpg)

Compared with the Kubernetes' original scheduler framework, the multi-cluster scheduling selects a cluster for the placement 
in a 5-step operations:
1. Batch & PostBatch
2. Filter 
3. Score
4. Sort
5. Bind

The _batch & postBatch_ step is to define the batch size according to the desired and current `ClusterResourceBinding`. 
The postBatch is to adjust the batch size if needed.

The _filter_ step finds the set of clusters where it's feasible to schedule the placement, for example, whether the cluster
is matching required `Affinity` scheduling rules specified in the `Policy`. It also filters out any clusters which are 
leaving the fleet or no longer connected to the fleet, for example, its heartbeat has been stopped for a prolonged period of time.

In the _score_ step (only applied to the pickN type), the scheduler assigns a score to each cluster that survived filtering.
Each cluster is given a topology spread score (how much a cluster would satisfy the topology spread
constraints specified by the user), and an affinity score (how much a cluster would satisfy the preferred affinity terms
specified by the user). 

In the _sort_ step (only applied to the pickN type), it sorts all eligible clusters by their scores, sorting first by topology 
spread score and breaking ties based on the affinity score.

The _bind_ step is to create/update/delete the `ClusterResourceBinding` based on the desired and current member cluster list.

## Rollout Strategy
Update strategy determines how changes to the `ClusterWorkloadPlacement` will be rolled out across member clusters. 
The only supported update strategy is `RollingUpdate` and it replaces the old placed resource using rolling update, i.e. 
gradually create the new one while replace the old ones.

## Placement status

After a `ClusterResourcePlacement` is created, details on current status can be seen by performing a `kubectl describe crp <name>`.
The status output will indicate both placement conditions and individual placement statuses on each member cluster that was selected.
The list of resources that are selected for placement will also be included in the describe output. 

Sample output:

```yaml
Name:         crp-1
Namespace:
Labels:       <none>
Annotations:  <none>
API Version:  placement.kubernetes-fleet.io/v1beta1
Kind:         ClusterResourcePlacement
Metadata:
  ...
Spec:
  Policy:
    Number Of Clusters:  2
    Placement Type:      PickN
  Resource Selectors:
    Group:
    Kind:                  Namespace
    Name:                  test
    Version:               v1
  Revision History Limit:  10
Status:
  Conditions:
    Last Transition Time:  2023-11-10T08:14:52Z
    Message:               found all the clusters needed as specified by the scheduling policy
    Observed Generation:   5
    Reason:                SchedulingPolicyFulfilled
    Status:                True
    Type:                  ClusterResourcePlacementScheduled
    Last Transition Time:  2023-11-10T08:23:43Z
    Message:               All 2 cluster(s) are synchronized to the latest resources on the hub cluster
    Observed Generation:   5
    Reason:                SynchronizeSucceeded
    Status:                True
    Type:                  ClusterResourcePlacementSynchronized
    Last Transition Time:  2023-11-10T08:23:43Z
    Message:               Successfully applied resources to 2 member clusters
    Observed Generation:   5
    Reason:                ApplySucceeded
    Status:                True
    Type:                  ClusterResourcePlacementApplied
  Placement Statuses:
    Cluster Name:  aks-member-1
    Conditions:
      Last Transition Time:  2023-11-10T08:14:52Z
      Message:               Successfully scheduled resources for placement in aks-member-1 (affinity score: 0, topology spread score: 0): picked by scheduling policy
      Observed Generation:   5
      Reason:                ScheduleSucceeded
      Status:                True
      Type:                  ResourceScheduled
      Last Transition Time:  2023-11-10T08:23:43Z
      Message:               Successfully Synchronized work(s) for placement
      Observed Generation:   5
      Reason:                WorkSynchronizeSucceeded
      Status:                True
      Type:                  WorkSynchronized
      Last Transition Time:  2023-11-10T08:23:43Z
      Message:               Successfully applied resources
      Observed Generation:   5
      Reason:                ApplySucceeded
      Status:                True
      Type:                  ResourceApplied
    Cluster Name:            aks-member-2
    Conditions:
      Last Transition Time:  2023-11-10T08:14:52Z
      Message:               Successfully scheduled resources for placement in aks-member-2 (affinity score: 0, topology spread score: 0): picked by scheduling policy
      Observed Generation:   5
      Reason:                ScheduleSucceeded
      Status:                True
      Type:                  ResourceScheduled
      Last Transition Time:  2023-11-10T08:23:43Z
      Message:               Successfully Synchronized work(s) for placement
      Observed Generation:   5
      Reason:                WorkSynchronizeSucceeded
      Status:                True
      Type:                  WorkSynchronized
      Last Transition Time:  2023-11-10T08:23:43Z
      Message:               Successfully applied resources
      Observed Generation:   5
      Reason:                ApplySucceeded
      Status:                True
      Type:                  ResourceApplied
  Selected Resources:
    Kind:       Namespace
    Name:       test
    Version:    v1
    Kind:       ConfigMap
    Name:       test-1
    Namespace:  test
    Version:    v1
Events:
  Type    Reason                     Age                    From                                   Message
  ----    ------                     ----                   ----                                   -------
  Normal  PlacementScheduleSuccess   12m (x5 over 3d22h)    cluster-resource-placement-controller  Successfully scheduled the placement
  Normal  PlacementSyncSuccess       3m28s (x7 over 3d22h)  cluster-resource-placement-controller  Successfully synchronized the placement
  Normal  PlacementRolloutCompleted  3m28s (x7 over 3d22h)  cluster-resource-placement-controller  Resources have been applied to the selected clusters
```

## Envelope Object

The `ClusterResourcePlacement` leverages the fleet hub cluster as a staging environment for customer resources. These resources are then propagated to member clusters that are part of the fleet, based on the `ClusterResourcePlacement` spec.

In essence, the objective is not to apply or create resources on the hub cluster for local use but to propagate these resources to other member clusters within the fleet.

Certain resources, when created or applied on the hub cluster, may lead to unintended side effects. These include:

- Validating/Mutating Webhook Configurations
- Cluster Role Bindings
- Resource Quotas
- Storage Classes
- Flow Schemas
- Priority Classes
- Ingress Classes
- Ingresses
- Network Policies

To address this, we support the use of `ConfigMap` with a fleet-reserved annotation. This allows users to encapsulate resources that might have side effects on the hub cluster within the `ConfigMap`. For detailed instructions, please refer to this [document](../../howtos/envelope-object.md).