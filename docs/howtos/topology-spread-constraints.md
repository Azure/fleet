# Using Topology Spread Constraints to Pick Clusters

This how-to guide discusses how to use topology spread constraints to fine-tune how Fleet picks
clusters for resource placement.

Topology spread constraints are features in the `ClusterResourcePlacement` API, specifically
the scheduling policy section. Generally speaking, these constraints can help you spread
resources evenly across different groups of clusters in your fleet; or in other words, it
assures that Fleet will not pick too many clusters from one group, and too little from another.
You can use topology spread constraints to, for example:

* achieve high-availability for your database backend by making sure that there is at least
one database replica in each region; or
* verify if your application can support clusters of different configurations; or
* eliminate resource utilization hotspots in your infrastructure through spreading jobs
evenly across sections.

## Specifying a topology spread constraint

A topology spread constraint consists of three fields:

* `topologyKey` is a label key which Fleet uses to split your clusters from a fleet into different
groups.

    Specifically, clusters are grouped by the label values they have. For example, if you have
    three clusters in a fleet:

    * cluster `bravelion` with the label `system=critical` and `region=east`; and
    * cluster `smartfish` with the label `system=critical` and `region=west`; and
    * cluster `jumpingcat` with the label `system=normal` and `region=east`,
    
    and you use `system` as the topology key, the clusters will be split into 2 groups:

    * group 1 with cluster `bravelion` and `smartfish`, as they both have the value `critical`
    for label `system`; and
    * group 2 with cluster `jumpingcat`, as it has the value `normal` for label `system`.

    Note that the splitting concerns only one label `system`; other labels,
    such as `region`, do not count. 

    If a cluster does not have the given topology key, it **does not** belong to any group.
    Fleet may still pick this cluster, as placing resources on it does not violate the
    associated topology spread constraint.

    This is a required field.

* `maxSkew` specifies how **unevenly** resource placements are spread in your fleet.

    The skew of a set of resource placements are defined as the difference in count of
    resource placements between the group with the most and the group with
    the least, as split by the topology key.

    For example, in the fleet described above (3 clusters, 2 groups):

    * if Fleet picks two clusters from group A, but none from group B, the skew would be
    `2 - 0 = 2`; however,
    * if Fleet picks one cluster from group A and one from group B, the skew would be
    `1 - 1 = 0`.

    The minimum value of `maxSkew` is 1. The less you set this value with, the more evenly
    resource placements are spread in your fleet.

    This is a required field.

    > Note
    >
    > Naturally, `maxSkew` only makes sense when there are no less than two groups. If you
    > set a topology key that will not split the Fleet at all (i.e., all clusters with
    > the given topology key has exactly the same value), the associated topology spread
    > constraint will take no effect.

* `whenUnsatisfiable` specifies what Fleet would do when it exhausts all options to satisfy the
topology spread constraint; that is, picking any cluster in the fleet would lead to a violation.

    Two options are available:

    * `DoNotSchedule`: with this option, Fleet would guarantee that the topology spread constraint
    will be enforced all time; scheduling may fail if there is simply no possible way to satisfy
    the topology spread constraint.

    * `ScheduleAnyway`: with this option, Fleet would enforce the topology spread constraint
    in a best-effort manner; Fleet may, however, pick clusters that would violate the topology
    spread constraint if there is no better option.

    This is an optional field; if you do not specify a value, Fleet will use `DoNotSchedule` by
    default.

Below is an example of topology spread constraint, which tells Fleet to pick clusters evenly
from different groups, split based on the value of the label `system`:

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
    topologySpreadConstraints:
      - maxSkew: 2
        topologyKey: system
        whenUnsatisfiable: DoNotSchedule
```

## How Fleet enforces topology spread constraints: topology spread scores

When you specify some topology spread constraints in the scheduling policy of
a `ClusterResourcePlacement` object, Fleet will start picking clusters **one at a time**.
More specifically, Fleet will:

* for each cluster in the fleet, evaluate how skew would change if resources were placed on it.

    Depending on the current spread of resource placements, there are three possible outcomes:

    * placing resources on the cluster reduces the skew by 1; or
    * placing resources on the cluster has no effect on the skew; or
    * placing resources on the cluster increases the skew by 1.

    Fleet would then assign a topology spread score to the cluster:

    * if the provisional placement reduces the skew by 1, the cluster receives a topology spread
    score of 1; or
    * if the provisional placement has no effect on the skew, the cluster receives a topology
    spread score of 0; or
    * if the provisional placement increases the skew by 1, but does not yet exceed the max skew
    specified in the constraint, the cluster receives a topology spread score of -1; or
    * if the provisional placement increases the skew by 1, and has exceeded the max skew specified in the constraint, 
    
        * for topology spread constraints with the `ScheduleAnyway` effect, the cluster receives a topology spread score of -1000; and
        * for those with the `DoNotSchedule` effect, the cluster will be removed from
        resource placement consideration.

* rank the clusters based on the topology spread score and other factors (e.g., affinity),
pick the one that is most appropriate.
* repeat the process, until all the needed count of clusters are found.

Below is an example that illustrates the process:

Suppose you have a fleet of 4 clusters:

* cluster `bravelion`, with label `region=east` and `system=critical`; and
* cluster `smartfish`, with label `region=east`; and
* cluster `jumpingcat`, with label `region=west`, and `system=critical`; and
* cluster `flyingpenguin`, with label `region=west`,

And you have created a `ClusterResourcePlacement` as follows:

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
    numberOfClusters: 2
    topologySpreadConstraints:
      - maxSkew: 1
        topologyKey: region
        whenUnsatisfiable: DoNotSchedule
```

Fleet will first scan all the 4 clusters in the fleet; they all have the `region` label, with
two different values `east` and `west` (2 cluster in each of them). This divides the clusters
into two groups, the `east` and the `west`

At this stage, no cluster has been picked yet, so there is no resource placement at all. The
current skew is thus 0, and placing resources on any of them would increase the skew by 1. This
is still below the `maxSkew` threshold given, so all clusters would receive a topology spread
score of -1.

Fleet could not find the most appropriate cluster based on the topology spread score so far,
so it would resort to other measures for ranking clusters. This would lead Fleet to pick cluster
`smartfish`.

> Note
>
> See [Using `ClusterResourcePlacement` to Place Resources](crp.md) How-To Guide for more
> information on how Fleet picks clusters.

Now, one cluster has been picked, and one more is needed by the `ClusterResourcePlacement`
object (as the `numberOfClusters` field is set to 2). Fleet scans the left 3 clusters again,
and this time, since `smartfish` from group `east` has been picked, any more resource placement
on clusters from group `east` would increase the skew by 1 more, and would lead to violation
of the topology spread constraint; Fleet will then assign the topology spread score of -1000 to
cluster `bravelion`, which is in group `east`. On the contrary, picking a cluster from any
cluster in group `west` would reduce the skew by 1, so Fleet assigns the topology spread score
of 1 to cluster `jumpingcat` and `flyingpenguin`.

With the higher topology spread score, `jumpingcat` and `flyingpenguin` become the leading
candidate in ranking. They have the same topology spread score, and based on the rules Fleet
has for picking clusters, `jumpingcat` would be picked finally.

## Using multiple topology spread constraints

You can, if necessary, use multiple topology spread constraints. Fleet will evaluate each of them
separately, and add up topology spread scores for each cluster for the final ranking. A cluster
would be removed from resource placement consideration if placing resources on it would violate
any one of the `DoNotSchedule` topology spread constraints.

Below is an example where two topology spread constraints are used:

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
    numberOfClusters: 2
    topologySpreadConstraints:
      - maxSkew: 2
        topologyKey: region
        whenUnsatisfiable: DoNotSchedule
      - maxSkew: 3
        topologyKey: environment
        whenUnsatisfiable: ScheduleAnyway
```

> Note
>
> It might be very difficult to find candidate clusters when multiple topology spread constraints
> are added. Considering using the `ScheduleAnyway` effect to add some leeway to the scheduling,
> if applicable.
