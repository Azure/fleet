# Using the `ClusterResourcePlacement` API to Place Resources

This how-to guide discusses how to use the Fleet `ClusterResourcePlacement` API to place
resources across the fleet as you see appropriate.

The `ClusterResourcePlacement` (CRP) API is a core Fleet API which allows workload orchestration.
Specifically, the API helps distribute specific resources, kept in the hub cluster, to the
member clusters joined in a fleet. The API features scheduling capabilities that allow you
to pinpoint the exact group of clusters most appropriate for a set of resources using complex rule
set, such as clusters in specific regions (north america, east asia, europe, etc.) and/or release
stages (production, canary, etc.); you can even distribute resources in accordance with some
topology spread constraints.

The API, generally speaking, consists of the following parts:

* one or more resource selectors, which specify the set of resources to select for placement; and
* a scheduling policy, which determines the set of clusters to place the resources at; and
* a rollout strategy, which controls the behavior of resource placement when the resources
themselves and/or the scheduling policy are updated, so as to minimize interruptions caused
by refreshes

The sections below discusses the components in depth.

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
        MatchLabels:
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
resourceSelectors:
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

Each scheduling policy is associated with a placement type, which determintes how Fleet will
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
    cluster meets, will set Fleet to prioritze it in scheduling.

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
            requiredDuringSchedulingIgnoredDuringExection:
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
    cluster meets, will set Fleet to prioritze it in scheduling.

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
    placementType: PickAll
    numberOfClusters: 3
    affinity:
        clusterAffinity:
            preferredDuringSchedulingIgnoredDuringExecution:
                weight: 20
                perference:
                - labelSelector:
                    matchLabels:
                        critial-level: 1
```

The `ClusterResourcePlacement` object above will pick first clusters with the `critial-level=1`
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

    This helps establishes deterministic scheduling behavior.

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

#### Upscaling and downscaling

You can edit the `numberOfClusters` field in the scheduling policy to pick more or less clusters.
When upscaling, Fleet will score all the clusters that have not been picked earlier, and find
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
* Add, update, or remove resource selectors in the `ClusterResourcePlacement`
* Add or update the scheduling policy in the `ClusterResourcePlacement`

These changes may trigger the following outcomes:

* New resources may need to placed on all picked clusters
* Resources already placed on a pick cluster may get updated or deleted
* Some clusters picked previously are now unpicked, and resources must be removed from such clusters
* Some clusters are newly picked, and resources must be added to them

Most of these outcomes may lead to service interruptions. Your apps running on the member clusters
may become unavailable temporarily, while Fleet sends out updated resources; clusters that are 
now unpicked lose all the placed resources, and traffic sent to these clusters will be lost;
if there are too many newly picked clusters and Fleet places resources on them at the same time,
your backend may get overloaded. The exact pattern of interruption may vary, 
depending on the set of resources you place using Fleet.

To help minimize the interruption, Fleet provides rollout strategy configuration to help you
transition between changes as smoothly as possible. Currently, Fleet supports only one rollout
strategy, rolling update; with this strategy, Fleet will apply changes, including the addition or
removal of picked clusters and resource refreshes, in an incremental manner with a number of phaes
at a pace most appropriate for you. This is the default option and will apply to all
changes you initiated.

This rollout strategy can be configured with the following parameters:

* `maxUnavailable` controls that, for the selected set of resources, how many clusters may become
unavailable during a change. It can be set as an absolute number or a percentage. Default is 25%,
and you should not use zero for this value.

    **The less value you set this parameter with, the less interruption you will experience during
    a change**; however, this would lead to slower rollouts.

    Note that Fleet considers a cluster as unavailable if resources have not been successfully
    applied to the cluster.

    <details><summary>How Fleet interprets this value</summary>
    <p></p>

    Fleet, in actuality, makes sure that at any time, there are **at least** N - `maxUnavailable`
    number of clusters available, where N is:

    * for scheduling policies of the `PickN` placement type, the `numberOfClusters` value given;
    * for scheduling policies of the `PickFixed` placement type, the number of cluster names given;
    * for scheduling policies of the `PickAll` placement type, the number of clusters Fleet picks.

    If you use a percentage for the `maxUnavailable` parameter, it is calculated against N as
    well.

    </details>

* `maxSurge` controls how many newly picked clusters will receive resource placements. It can
also be set as an absolute number or a percentage. Default is 25%, and you should not use zero for
this value.

    **The less value you set this parameter with, the less new resource placements Fleet will run
    at the same time**; however, this would lead to slower rollouts.

    <details><summary>How Fleet interprets this value</summary>
    <p></p>

    Fleet, in actuality, makes sure that at any time, there are **at most** N + `maxSurge`
    number of clusters available, where N is:

    * for scheduling policies of the `PickN` placement type, the `numberOfClusters` value given;
    * for scheduling policies of the `PickFixed` placement type, the number of cluster names given;
    * for scheduling policies of the `PickAll` placement type, the number of clusters Fleet picks.

    If you use a percentage for the `maxUnavailable` parameter, it is calculated against N as
    well.

    </details>

* `unavailablePeriodSeconds` controls the frequeny of rollout phases. Default is 60 seconds.

    **The less value you set this parameter with, the quicker rollout will become**. However, using
    a value that is too little may lead to unexpected service interruptions.

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
