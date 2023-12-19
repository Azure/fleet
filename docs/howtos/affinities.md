# Using Affinity to Pick Clusters

This how-to guide discusses how to use affinity settings to fine-tune how Fleet picks clusters
for resource placement.

Affinities terms are featured in the `ClusterResourcePlacement` API, specifically the scheduling
policy section. Each affinity term is a particular requirement that Fleet will check against clusters;
and the fulfillment of this requirement (or the lack of) would have certain effect on whether
Fleet would pick a cluster for resource placement.

Fleet currently supports two types of affinity terms:

* `requiredDuringSchedulingIgnoredDuringExecution` affinity terms; and
* `perferredDuringSchedulingIgnoredDuringExecution` affinity terms

Most affinity terms deal with cluster labels. To manage member clusters, specifically
adding/removing labels from a member cluster, see [Managing Member Clusters](clusters.md) How-To
Guide.

## `requiredDuringSchedulingIgnoredDuringExecution` affinity terms

The `requiredDuringSchedulingIgnoredDuringExecution` type of affinity terms serves as a hard
constraint that **a cluster must satisfy** before it can be picked. Each term features a label
selector, which describes the set of labels a cluster must have or not have.

### `matchLabels`

The most straightforward way is to specify `matchLabels` in the label selector, as showcased below:

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

The example above includes a `requiredDuringSchedulingIgnoredDuringExecution` term which requires
that the label `system=critical` must be present on a cluster before Fleet can pick it for the
`ClusterResourcePlacement`.

You can add multiple labels to `matchLabels`; any cluster that satisfy this affinity term would
have **all** the labels present.

### `matchExpressions`

For more complex logic, consider using `matchExpressions`, which allow you to use operators to
set rules for validating labels on a member cluster. Each `matchExpressions` requirement
includes:

* a key, which is the key of the label; and
* a list of values, which are the possible values for the label key; and
* an operator, which represents the relationship between the key and the list of values.

    Supported operators include:

    * `In`: the cluster must have a label key with one of the listed values.
    * `NotIn`: the cluster must have a label key that is not associated with any of the listed values.
    * `Exists`: the cluster must have the label key present; any value is acceptable.
    * `NotExists`: the cluster must not have the label key.

    If you plan to use `Exists` and/or `NotExists`, you must leave the list of values empty.

Below is an example of `matchExpressions` affinity term using the `In` operator:

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
                    matchExpressions:
                    - key: system
                      operator: In
                      values:
                      - critical
                      - standard
```

Any cluster with the label `system=critical` or `system=standard` will be picked by Fleet.

Similarly, you can also specify multiple `matchExpressions` requirements; any cluster that
satisfy this affinity term would meet all the requirements.

### Using both `matchLabels` and `matchExpressions` in one affinity term

You can specify both `matchLabels` and `matchExpressions` in one `requiredDuringSchedulingIgnoredDuringExecution` affinity term, as showcased below:

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
                      region: east
                    matchExpressions:
                    - key: system
                    - operator: Exists

```

With this affinity term, any cluster picked must:

* have the label `region=east` present;
* have the label `system` present, any value would do.

### Using multiple affinity terms

You can also specify multiple `requiredDuringSchedulingIgnoredDuringExecution` affinity terms,
as showcased below; a cluster will be picked if it can satisfy **any** affinity term.

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
                      region: west
                - labelSelector:
                    matchExpressions:
                    - key: system
                    - operator: DoesNotExist
```

With these two affinity terms, any cluster picked must:

* have the label `region=west` present; **or**
* does not have the label `system`

## `preferredDuringSchedulingIgnoredDuringExecution` affinity terms

The `preferredDuringSchedulingIgnoredDuringExecution` type of affinity terms serves as a soft
constraint for clusters; any cluster that satisfy such terms would receive an affinity score,
which Fleet uses to rank clusters when processing `ClusterResourcePlacement` with scheduling
policy of the `PickN` placement type. 

Each term features:

* a weight, between -100 and 100, which is the affinity score that Fleet would assign to a
cluster if it satisfies this term; and
* a label selector.

Both are required for this type of affinity terms to function.

The label selector is of the same struct as the one used in
`requiredDuringSchedulingIgnoredDuringExecution` type of affinity terms; see
[the documentation above](#requiredduringschedulingignoredduringexecution-affinity-terms) for usage.

Below is an example with a `preferredDuringSchedulingIgnoredDuringExecution` affinity term:

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
    numberOfClusters: 10
    affinity:
        clusterAffinity:
            preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 20
              preference:
                labelSelector:
                  matchLabels:
                  region: west
```

Any cluster with the `region=west` label would receive an affinity score of 20.

### Using multiple affinity terms

Similarly, you can use multiple `preferredDuringSchedulingIgnoredDuringExection` affinity terms,
as showcased below:

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
    numberOfClusters: 10
    affinity:
        clusterAffinity:
            preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 20
              preference:
                labelSelector:
                  matchLabels:
                   region: west
            - weight: -20
              preference:
                labelSelector:
                  matchLabels:
                    environment: prod
```

Cluster will be validated against each affinity term individually; the affinity scores it
receives will be summed up. For example:

* if a cluster has only the `region=west` label, it would receive an affinity score of 20; however
* if a cluster has both the `region=west` and `environment=prod` labels, it would receive an
affinity score of `20 + (-20) = 0`.

## Use both types of affinity terms

You can, if necessary, add both `requiredDuringSchedulingIgnoredDuringExecution` and
`preferredDuringSchedulingIgnoredDuringExection` types of affinity terms. Fleet will
first run all clusters against all the `requiredDuringSchedulingIgnoredDuringExecution` type
of affinity terms, filter out any that does not meet the requirements, and then
assign the rest with affinity scores per `preferredDuringSchedulingIgnoredDuringExection` type of
affinity terms.

Below is an example with both types of affinity terms:

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
    numberOfClusters: 10
    affinity:
        clusterAffinity:
            requiredDuringSchedulingIgnoredDuringExecution:
              clusterSelectorTerms:
              - labelSelector:
                  matchExpressions:
                  - key: system
                  - operator: Exists
            preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 20
              preference:
                labelSelector:
                  matchLabels:
                   region: west
```

With these affinity terms, only clusters with the label `system` (any value would do) can be
picked; and among them, those with the `region=west` will be prioritized for resource placement
as they receive an affinity score of 20.