# Using property-based scheduling

This how-to guide discusses how to use property-based scheduling to produce scheduling decisions
based on cluster properties.

> Note
>
> The availability of properties depend on which (and if) you have a property provider
> set up in your Fleet deployment. For more information, see the
> [Concept: Property Provider and Cluster Properties](../concepts/PropertyProviderAndClusterProperties/README.md)
> documentation.
>
> It is also recommended that you read the
> [How-To Guide: Using Affinity to Pick Clusters](./affinities.md) first before following
> instructions in this document.

Fleet allows users to pick clusters based on exposed cluster properties via the affinity
terms in the `ClusterResourcePlacement` API:

* for the `requiredDuringSchedulingIgnoredDuringExecution` affinity terms, you may specify
property selectors to filter clusters based on their properties;
* for the `preferredDuringSchedulingIgnoredDuringExecution` affinity terms, you may specify
property sorters to prefer clusters with a property that ranks higher or lower.

# Property selectors in `requiredDuringSchedulingIgnoredDuringExecution` affinity terms

A property selector is essentially an array of expression matchers against cluster properties.
In each matcher you will specify:

* A name, which is the name of the property.

    If the property is a non-resource one, you may refer to it directly here; however, if the
    property is a resource one, the name here should be of the following format:

    ```
    resources.kubernetes-fleet.io/[CAPACITY-TYPE]-[RESOURCE-NAME]
    ```

    where `[CAPACITY-TYPE]` is one of `total`, `allocatable`, or `available`, depending on
    which capacity (usage information) you would like to check against, and `[RESOURCE-NAME]` is
    the name of the resource.

    For example, if you would like to select clusters based on the available CPU capacity of
    a cluster, the name used in the property selector should be
    
    ```
    resources.kubernetes-fleet.io/available-cpu
    ```

    and for the allocatable memory capacity, use 
    
    ```
    resources.kubernetes-fleet.io/allocatable-memory
    ```

* A list of values, which are possible values of the property.
* An operator, which describes the relationship between a cluster's observed value of the given
property and the list of values in the matcher.

    Currently, available operators are

    * `Gt` (Greater than): a cluster's observed value of the given property must be greater than
    the value in the matcher before it can be picked for resource placement.
    * `Ge` (Greater than or equal to): a cluster's observed value of the given property must be
    greater than or equal to the value in the matcher before it can be picked for resource placement.
    * `Lt` (Less than): a cluster's observed value of the given property must be less than
    the value in the matcher before it can be picked for resource placement.
    * `Le` (Less than or equal to): a cluster's observed value of the given property must be
    less than or equal to the value in the matcher before it can be picked for resource placement.
    * `Eq` (Equal to): a cluster's observed value of the given property must be equal to
    the value in the matcher before it can be picked for resource placement.
    * `Ne` (Not equal to): a cluster's observed value of the given property must be
    not equal to the value in the matcher before it can be picked for resource placement.

    Note that if you use the operator `Gt`, `Ge`, `Lt`, `Le`, `Eq`, or `Ne`, the list of values
    in the matcher should have exactly one value.

Fleet will evaluate each cluster, specifically their exposed properties, against the matchers;
failure to satisfy **any** matcher in the selector will exclude the cluster from resource
placement.

Note that if a cluster does not have the specified property for a matcher, it will automatically
fail the matcher.

Below is an example that uses a property selector to select only clusters with a node count of
at least 5 for resource placement:

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
                - propertySelector:
                    matchExpressions:
                    - name: "kubernetes.azure.com/node-count"
                      operator: Ge
                      values:
                      - 5
```

You may use both label selector and property selector in a
`requiredDuringSchedulingIgnoredDuringExecution` affinity term. Both selectors must be satisfied
before a cluster can be picked for resource placement:

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
                      region: east
                  propertySelector:
                    matchExpressions:
                    - name: "kubernetes.azure.com/node-count"
                      operator: Ge
                      values:
                      - 5
```

In the example above, Fleet will only consider a cluster for resource placement if it has the
`region=east` label and a node count no less than 5.

# Property sorters in `preferredDuringSchedulingIgnoredDuringExecution` affinity terms

A property sorter ranks all the clusters in the Fleet based on their values of a specified
property in ascending or descending order, then yields weights for the clusters in proportion
to their ranks. The proportional weights are calculated based on the weight value given in the
`preferredDuringSchedulingIgnoredDuringExecution` term.

A property sorter consists of:

* A name, which is the name of the property; see the format in the previous section for more
information.
* A sort order, which is one of `Ascending` and `Descending`, for ranking in ascending and
descending order respectively.

    As a rule of thumb, when the `Ascending` order is used, Fleet will prefer clusters with lower
    observed values, and when the `Descending` order is used, clusters with higher observed values
    will be preferred.

When using the sort order `Descending`, the proportional weight is calculated using the formula:

```
((Observed Value - Minimum observed value) / (Maximum observed value - Minimum observed value)) * Weight
```

For example, suppose that you would like to rank clusters based on the property of available CPU
capacity in descending order and currently, you have a fleet of 3 clusters with the available CPU
capacities as follows:

| Cluster | Available CPU capacity |
| -------- | ------- |
| bravelion | 100 |
| smartfish | 20 |
| jumpingcat | 10 |

The sorter would yield the weights below:

| Cluster | Available CPU capacity | Weight |
| -------- | ------- | ------- | 
| bravelion | 100 | (100 - 10) / (100 - 10) = 100% of the weight |
| smartfish | 20 | (20 - 10) / (100 - 10) = 11.11% of the weight |
| jumpingcat | 10 | (10 - 10) / (100 - 10) = 0% of the weight |


And when using the sort order `Ascending`, the proportional weight is calculated using the formula:

```
(1 - ((Observed Value - Minimum observed value) / (Maximum observed value - Minimum observed value))) * Weight
```

For example, suppose that you would like to rank clusters based on their per CPU core cost
in ascending order and currently across the fleet, you have a fleet of 3 clusters with the
per CPU core costs as follows:

| Cluster | Per CPU core cost |
| -------- | ------- |
| bravelion | 1 |
| smartfish | 0.2 |
| jumpingcat | 0.1 |

The sorter would yield the weights below:

| Cluster | Per CPU core cost | Weight |
| -------- | ------- | ------- | 
| bravelion | 1 | 1 - ((1 - 0.1) / (1 - 0.1)) = 0% of the weight |
| smartfish | 0.2 | 1 - ((0.2 - 0.1) / (1 - 0.1)) = 88.89% of the weight |
| jumpingcat | 0.1 | 1 - (0.1 - 0.1) / (1 - 0.1) = 100% of the weight |

The example below showcases a property sorter using the `Descending` order:

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
                metricSorter:
                  name: kubernetes.azure.com/node-count
                  sortOrder: Descending
```

In this example, Fleet will prefer clusters with higher node counts. The cluster with the highest
node count would receive a weight of 20, and the cluster with the lowest would receive 0. Other
clusters receive proportional weights calculated using the formulas above.

You may use both label selector and property sorter in a
`preferredDuringSchedulingIgnoredDuringExecution` affinity term. A cluster that fails the label
selector would receive no weight, and clusters that pass the label selector receive proportional
weights under the property sorter.

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
                    env: prod
                metricSorter:
                  name: resources.kubernetes-fleet.io/total-cpu
                  sortOrder: Descending
```

In the example above, a cluster would only receive additional weight if it has the label
`env=prod`, and the more total CPU capacity it has, the more weight it will receive, up to the
limit of 20.
