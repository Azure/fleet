# How-to Guide: Using the Fleet `ClusterResourcePlacement` API

This guide provides an overview of how to use the Fleet `ClusterResourcePlacement` (CRP) API to orchestrate workload distribution across your fleet.

## Overview

The CRP API is a core Fleet API that facilitates the distribution of specific resources from the hub cluster to 
member clusters within a fleet. This API offers scheduling capabilities that allow you to target the most suitable 
group of clusters for a set of resources using a complex rule set. For example, you can distribute resources to
clusters in specific regions (North America, East Asia, Europe, etc.) and/or release stages (production, canary, etc.). 
You can even distribute resources according to certain topology spread constraints.

## API Components

The CRP API generally consists of the following components:

- **Resource Selectors**: These specify the set of resources selected for placement.
- **Scheduling Policy**: This determines the set of clusters where the resources will be placed.
- **Rollout Strategy**: This controls the behavior of resource placement when the resources themselves and/or the 
              scheduling policy are updated, minimizing interruptions caused by refreshes.

The following sections discuss these components in depth.

## Resource selectors

A `ClusterResourcePlacement` object may feature one or more resource selectors,
specifying which resources to select for placement. To add a resource selector, edit
the `resourceSelectors` field in the `ClusterResourcePlacement` spec:

```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: crp
spec:
  resourceSelectors:
    - group: "rbac.authorization.k8s.io"
      kind: ClusterRole
      version: v1          
      name: secretReader
```

The example above will pick a `ClusterRole` named `secretReader` for resource placement.

It is important to note that, as its name implies, `ClusterResourcePlacement` **selects only
cluster-scoped resources**. However, if you select a namespace, all the resources under the
namespace will also be placed.

### Different types of resource selectors

You can specify a resource selector in many different ways:

* To select **one specific resource**, such as a namespace, specify its API GVK (group, version, and
kind), and its name, in the resource selector:

    ```yaml
    # As mentioned earlier, all the resources under the namespace will also be selected.
    resourceSelectors:
      - group: ""
        kind: Namespace
        version: v1          
        name: work
    ```

* Alternately, you may also select a set of resources of the same API GVK using a label selector;
it also requires that you specify the API GVK and the filtering label(s):

    ```yaml
    # As mentioned earlier, all the resources under the namespaces will also be selected.
    resourceSelectors:
      - group: ""
        kind: Namespace
        version: v1          
        labelSelector:
          matchLabels:
            system: critical
    ```

    In the example above, all the namespaces in the hub cluster with the label `system=critical`
    will be selected (along with the resources under them). 

    Fleet uses standard Kubernetes label selectors; for its specification and usage, see the
    [Kubernetes API reference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#labelselector-v1-meta).

* Very occasionally, you may need to select all the resources under a specific GVK; to achieve
this, use a resource selector with only the API GVK added:

    ```yaml
    resourceSelectors:
      - group: "rbac.authorization.k8s.io"
        kind: ClusterRole
        version: v1          
    ```

    In the example above, all the cluster roles in the hub cluster will be picked.

### Multiple resource selectors

You may specify up to 100 different resource selectors; Fleet will pick a resource if it matches
any of the resource selectors specified (i.e., all selectors are OR'd).

```yaml
# As mentioned earlier, all the resources under the namespace will also be selected.
resourceSelectors:
  - group: ""
    kind: Namespace
    version: v1          
    name: work
  - group: "rbac.authorization.k8s.io"
    kind: ClusterRole
    version: v1
    name: secretReader      
```

In the example above, Fleet will pick the namespace `work` (along with all the resources
under it) and the cluster role `secretReader`.

> Note
>
> You can find the GVKs of built-in Kubernetes API objects in the
> [Kubernetes API reference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/).

## Scheduling policy

Each scheduling policy is associated with a placement type, which determines how Fleet will
pick clusters. The `ClusterResourcePlacement` API supports the following placement types:

| Placement type | Description |
|----------------|-------------|
| `PickFixed`      | Pick a specific set of clusters by their names. |
| `PickAll`        | Pick all the clusters in the fleet, per some standard. |
| `PickN`          | Pick a count of N clusters in the fleet, per some standard. |

> Note
>
> Scheduling policy itself is optional. If you do not specify a scheduling policy,
> Fleet will assume that you would like to use
> a scheduling of the `PickAll` placement type; it effectively sets Fleet to pick
> all the clusters in the fleet.

Fleet does not support switching between different placement types; if you need to do
so, re-create a new `ClusterResourcePlacement` object.

### `PickFixed` placement type

`PickFixed` is the most straightforward placement type, through which you directly tell Fleet
which clusters to place resources at. To use this placement type, specify the target cluster
names in the `clusterNames` field, such as

```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: crp
spec:
  resourceSelectors:
    - ...
  policy:
    placementType: PickFixed
    clusterNames: 
      - bravelion
      - smartfish 
```

The example above will place resources to two clusters, `bravelion` and `smartfish`.

### `PickAll` placement type

`PickAll` placement type allows you to pick all clusters in the fleet per some standard. With
this placement type, you may use affinity terms to fine-tune which clusters you would like
for Fleet to pick:

* An affinity term specifies a requirement that a cluster needs to meet, usually the presence
of a label.

    There are two types of affinity terms:

    * `requiredDuringSchedulingIgnoredDuringExecution` terms are requirements that a cluster
    must meet before it can be picked; and
    * `preferredDuringSchedulingIgnoredDuringExecution` terms are requirements that, if a 
    cluster meets, will set Fleet to prioritize it in scheduling.

    In the scheduling policy of the `PickAll` placement type, you may only use the
    `requiredDuringSchedulingIgnoredDuringExecution` terms.

> Note
>
> You can learn more about affinities in [Using Affinities to Pick Clusters](affinities.md) How-To
> Guide.

```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: crp
spec:
  resourceSelectors:
    - ...
  policy:
    placementType: PickAll
    affinity:
        clusterAffinity:
            requiredDuringSchedulingIgnoredDuringExecution:
                clusterSelectorTerms:
                    - labelSelector:
                        matchLabels:
                            system: critical
```

The `ClusterResourcePlacement` object above will pick all the clusters with the label
`system:critical` on them; clusters without the label will be ignored.

Fleet is forward-looking with the `PickAll` placement type: any cluster that satisfies the
affinity terms of a `ClusterResourcePlacement` object, even if it joins after the
 `ClsuterResourcePlacement` object is created, will be picked.

> Note
>
> You may specify a scheduling policy of the `PickAll` placement with no affinity; this will set
> Fleet to select all clusters currently present in the fleet.

### `PickN` placement type

`PickN` placement type allows you to pick a specific number of clusters in the fleet for resource
placement; with this placement type, you may use affinity terms and topology spread constraints
to fine-tune which clusters you would like Fleet to pick.

* An affinity term specifies a requirement that a cluster needs to meet, usually the presence
of a label.

    There are two types of affinity terms:

    * `requiredDuringSchedulingIgnoredDuringExecution` terms are requirements that a cluster
    must meet before it can be picked; and
    * `preferredDuringSchedulingIgnoredDuringExecution` terms are requirements that, if a 
    cluster meets, will set Fleet to prioritize it in scheduling.

* A topology spread constraint can help you spread resources evenly across different groups
of clusters. For example, you may want to have a database replica deployed in each region
to enable high-availability. 

> Note
>
> You can learn more about affinities in [Using Affinities to Pick Clusters](affinities.md)
> How-To Guide, and more about topology spread constraints in
> [Using Topology Spread Constraints to Pick Clusters](topology-spread-constraints.md) How-To Guide.

```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: crp
spec:
  resourceSelectors:
    - ...
  policy:
    placementType: PickN
    numberOfClusters: 3
    affinity:
        clusterAffinity:
            preferredDuringSchedulingIgnoredDuringExecution:
                - weight: 20
                  preference:
                    - labelSelector:
                        matchLabels:
                            critical-level: 1
```

The `ClusterResourcePlacement` object above will pick first clusters with the `critical-level=1`
on it; if only there are not enough (less than 3) such clusters, will Fleet pick clusters with no
such label.
  
To be more precise, with this placement type, Fleet scores clusters on how well it satisfies the
affinity terms and the topology spread constraints; Fleet will assign:

* an affinity score, for how well the cluster satisfies the affinity terms; and
* a topology spread score, for how well the cluster satisfies the topology spread constraints.

> Note
>
> For more information on the scoring specifics, see 
> [Using Affinities to Pick Clusters](affinities.md) How-To Guide (for affinity score) and
> [Using Topology Spread Constraints to Pick Clusters](topology-spread-constraints.md) How-To
> Guide (for topology spread score).

After scoring, Fleet ranks the clusters using the rule below and picks the top N clusters:

* the cluster with the highest topology spread score ranks the highest;
* if there are multiple clusters with the same topology spread score, the one with the highest
affinity score ranks the highest;
* if there are multiple clusters with same topology spread score and affinity score, sort their
names by alphanumeric order; the one with the most significant name ranks the highest.

    This helps establish deterministic scheduling behavior.

Both affinity terms and topology spread constraints are optional. If you do not specify
affinity terms or topology spread constraints, all clusters will be assigned 0 in
affinity score or topology spread score respectively. When neither is added in the scheduling
policy, Fleet will simply rank clusters by their names, and pick N out of them, with
most significant names in alphanumeric order.

#### When there are not enough clusters to pick

It may happen that Fleet cannot find enough clusters to pick. In this situation, Fleet will 
keep looking until all N clusters are found.

Note that Fleet will stop looking once all N clusters are found, even if there appears a
cluster that scores higher.

#### Up-scaling and downscaling

You can edit the `numberOfClusters` field in the scheduling policy to pick more or less clusters.
When up-scaling, Fleet will score all the clusters that have not been picked earlier, and find
the most appropriate ones; for downscaling, Fleet will unpick the clusters that ranks lower
first.

> Note
>
> For downscaling, the ranking Fleet uses for unpicking clusters is composed when the scheduling
> is performed, i.e., it may not reflect the latest setup in the Fleet.

### A few more points about scheduling policies

#### Responding to changes in the fleet

Generally speaking, once a cluster is picked by Fleet for a `ClusterResourcePlacement` object,
it will not be unpicked even if you modify the cluster in a way that renders it unfit for
the scheduling policy, e.g., you have removed a label for the cluster, which is required for
some affinity term. Fleet will also not remove resources from the cluster even if the cluster
becomes unhealthy, e.g., it gets disconnected from the hub cluster. This helps reduce service
interruption.

However, Fleet will unpick a cluster if it leaves the fleet. If you are using a scheduling
policy of the `PickN` placement type, Fleet will attempt to find a new cluster as replacement.

#### Finding the scheduling decisions Fleet makes

You can find out why Fleet picks a cluster in the status of a `ClusterResourcePlacement` object.
For more information, see the
[Understanding the Status of a `ClusterResourcePlacement`](crp-status.md) How-To Guide.

#### Available fields for each placement type

The table below summarizes the available scheduling policy fields for each placement type:

|                             | `PickFixed` | `PickAll` | `PickN` |
|-----------------------------|-------------|-----------|---------|
| `placementType`             | ✅ | ✅ | ✅ |
| `numberOfClusters`          | ❌ | ❌ | ✅ |
| `clusterNames`              | ✅ | ❌ | ❌ |
| `affinity`                  | ❌ | ✅ | ✅ |
| `topologySpreadConstraints` | ❌ | ❌ | ✅ |

## Rollout strategy

After a `ClusterResourcePlacement` is created, you may want to

* Add, update, or remove the resources that have been selected by the
`ClusterResourcePlacement` in the hub cluster
* Update the resource selectors in the `ClusterResourcePlacement`
* Update the scheduling policy in the `ClusterResourcePlacement`

These changes may trigger the following outcomes:

* New resources may need to be placed on all picked clusters
* Resources already placed on a picked cluster may get updated or deleted
* Some clusters picked previously are now unpicked, and resources must be removed from such clusters
* Some clusters are newly picked, and resources must be added to them

Most outcomes can lead to service interruptions. Apps running on member clusters may temporarily become 
unavailable as Fleet dispatches updated resources. Clusters that are no longer selected will lose all placed resources,
resulting in lost traffic. If too many new clusters are selected and Fleet places resources on them simultaneously, 
your backend may become overloaded. The exact interruption pattern may vary depending on the resources you place using Fleet.
To minimize interruption, Fleet allows users to configure the rollout strategy. There are two types of rollout strategies we currently support.

### Default rollout strategy: Rolling Update

The default strategy is rolling update, and it applies to all changes you initiate.
This strategy ensures changes, including the addition or removal of selected clusters and resource refreshes, 
are applied incrementally in a phased manner at a pace suitable for you, similar to native Kubernetes deployments.

This rollout strategy can be configured with the following parameters:

* `maxUnavailable` determines how many clusters may become unavailable during a change for the selected set of resources. 
It can be set as an absolute number or a percentage. The default is 25%, and zero should not be used for this value.
    
    - Setting this parameter to a lower value will result in less interruption during a change but will lead to slower rollouts.

    - Fleet considers a cluster as unavailable if resources have not been successfully applied to the cluster.

    - <details><summary>How Fleet interprets this value</summary>
      Fleet, in actuality, makes sure that at any time, there are **at least** N - `maxUnavailable`
      number of clusters available, where N is:
  
      * for scheduling policies of the `PickN` placement type, the `numberOfClusters` value given;
      * for scheduling policies of the `PickFixed` placement type, the number of cluster names given;
      * for scheduling policies of the `PickAll` placement type, the number of clusters Fleet picks.
  
      If you use a percentage for the `maxUnavailable` parameter, it is calculated against N as
      well.
  
      </details>

* `maxSurge` determines the number of additional clusters, beyond the required number, that will receive resource placements.
It can also be set as an absolute number or a percentage. The default is 25%, and zero should not be used for this value.

    - Setting this parameter to a lower value will result in fewer resource placements on additional 
        clusters by Fleet, which may slow down the rollout process.

    -  <details><summary>How Fleet interprets this value</summary>
        Fleet, in actuality, makes sure that at any time, there are **at most** N + `maxSurge`
             number of clusters available, where N is:

        * for scheduling policies of the `PickN` placement type, the `numberOfClusters` value given;
        * for scheduling policies of the `PickFixed` placement type, the number of cluster names given;
        * for scheduling policies of the `PickAll` placement type, the number of clusters Fleet picks.
  
        If you use a percentage for the `maxUnavailable` parameter, it is calculated against N as well.
  
        </details>

* `unavailablePeriodSeconds` allows users to inform the fleet when the resources are deemed "ready".
     The default value is 60 seconds.
    
    - Fleet only considers newly applied resources on a cluster as "ready" once `unavailablePeriodSeconds` seconds 
       have passed **after** the resources have been **successfully** applied to that cluster.
    - Setting a lower value for this parameter will result in faster rollouts. However, we **strongly** 
       recommend that users set it to a value that all the initialization/preparation tasks can be completed within
       that time frame. This ensures that the resources are typically ready after the `unavailablePeriodSeconds` have passed.
    - We are currently designing a generic "ready gate" for resources being applied to clusters. Please feel free to raise 
       issues or provide feedback if you have any thoughts on this.

> Note
>
> Fleet will round numbers up if you use a percentage for `maxUnavailable` and/or `maxSurge`.

For example, if you have a `ClusterResourcePlacement` with a scheduling policy of the `PickN`
placement type and a target number of clusters of 10, with the default rollout strategy, as
shown in the example below,

```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: crp
spec:
  resourceSelectors:
    - ...
  policy:
    ...
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 25%
      maxSurge: 25%
      unavailablePeriodSeconds: 60
```

Every time you initiate a change on selected resources, Fleet will:

* Find `10 * 25% = 2.5, rounded up to 3` clusters, which will receive the resource refresh;
* Wait for 60 seconds (`unavailablePeriodSeconds`), and repeat the process;
* Stop when all the clusters have received the latest version of resources.

The exact period of time it takes for Fleet to complete a rollout depends not only on the
`unavailablePeriodSeconds`, but also the actual condition of a resource placement; that is,
if it takes longer for a cluster to get the resources applied successfully, Fleet will wait
longer to complete the rollout, in accordance with the rolling update strategy you specified.

> Note
>
> In very extreme circumstances, rollout may get stuck, if Fleet just cannot apply resources
> to some clusters. You can identify this behavior if CRP status; for more information, see
> [Understanding the Status of a `ClusterResourcePlacement`](crp-status.md) How-To Guide.

### External rollout strategy and staged update run

Fleet supports flexible rollout patterns through an `External` rollout strategy, which allows you to implement custom rollout controllers. 
When configured, Fleet delegates the responsibility of resource placement to your external controller instead of using Fleet's built-in rolling update mechanism.

One implementation of an external rollout strategy is the **Staged Update Run**. 
This approach enables a controlled, stage-by-stage placement of workload resources defined in a `ClusterResourcePlacement`.

To utilize this strategy: 
1. Set `spec.strategy.type` as `External` in the `ClusterResourcePlacement` object.
2. Define your rollout process using two custom resources:
    - `ClusterStagedUpdateStrategy`: A reusable template defining the rollout pattern
    - `ClusterStagedUpdateRun`: The resource that triggers and manages the actual rollout process.

For comprehensive guidance on implementing staged updates, please refer to the [Staged Update Run Concepts](../concepts/StagedUpdateRun/README.md) 
and [Staged Update Run How-To Guide](updaterun.md).

## Snapshots and revisions

Internally, Fleet keeps a history of all the scheduling policies you have used with a
`ClusterResourcePlacement`, and all the resource versions (snapshots) the
`ClusterResourcePlacement` has selected. These are kept as `ClusterSchedulingPolicySnapshot`
and `ClusterResourceSnapshot` objects respectively.

You can list and view such objects for reference, but you should not modify their contents
(in a typical setup, such requests will be rejected automatically). To control the length
of the history (i.e., how many snapshot objects Fleet will keep for a `ClusterResourcePlacement`),
configure the `revisionHistoryLimit` field:

```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: crp
spec:
  resourceSelectors:
    - ...
  policy:
    ...
  strategy:
    ...
  revisionHistoryLimit: 10
```

The default value is 10.

> Note
>
> In this early stage, the history is kept for reference purposes only; in the future, Fleet
> may add features to allow rolling back to a specific scheduling policy and/or resource version.
