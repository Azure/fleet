# How-to Guide: Using the Fleet `ClusterResourceOverride` API

This guide provides an overview of how to use the Fleet `ClusterResourceOverride` API to override cluster resources.

## Overview
`ClusterResourceOverride` is a feature within Fleet that allows for the modification or override of specific attributes 
across cluster-wide resources. With ClusterResourceOverride, you can define rules based on cluster labels or other 
criteria, specifying changes to be applied to various cluster-wide resources such as namespaces, roles, role bindings, 
or custom resource definitions. These modifications may include updates to permissions, configurations, or other 
parameters, ensuring consistent management and enforcement of configurations across your Fleet-managed Kubernetes clusters.

## API Components
The ResourceOverride API consists of the following components:
- **Cluster Resource Selectors**: These specify the set of cluster resources selected for overriding.
- **Policy**: This specifies the policy to be applied to the selected resources.


The following sections discuss these components in depth.

## Cluster Resource Selectors
A `ClusterResourceOverride` object may feature one or more cluster resource selectors, specifying which resources to select to be overridden.

The `ClusterResourceSelector` object supports the following fields:
- `group`: The API group of the resource
- `version`: The API version of the resource
- `kind`: The kind of the resource
- `name`: The name of the resource
> Note: The resource can only be selected by name.


To add a resource selector, edit the `clusterResourceSelectors` field in the `ClusterResourceOverride` spec:
```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ClusterResourceOverride
metadata:
  name: example-cro
spec:
  clusterResourceSelectors:
    - group: rbac.authorization.k8s.io
      kind: ClusterRole
      version: v1
      name: secret-reader
```

The example above will pick a `ClusterRole` named `secret-reader` to be overridden.


## Policy
The `Policy` is made up of a set of rules (`OverrideRules`) that specify the changes to be applied to the selected
resources on selected clusters.

Each `OverrideRule` supports the following fields:
- **Cluster Selector**: This specifies the set of clusters to which the override applies.
- **JSON Patch Override**: This specifies the changes to be applied to the selected resources.

To add an override rule, edit the `policy` field in the `ClusterResourceOverride` spec:

```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ClusterResourceOverride
metadata:
  name: example-cro
spec:
  clusterResourceSelectors:
    - group: rbac.authorization.k8s.io
      kind: ClusterRole
      version: v1
      name: secret-reader
  policy:
    overrideRules:
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  env: prod
        jsonPatchOverrides:
          - op: remove
            path: /rules/0/verbs/2
```
The `ClusterResourceOverride` object above will remove the verb "list" in the `ClusterRole` named `secret-reader` on 
clusters with the label `env: prod`.

### Cluster Selector
To specify the clusters to which the override applies, you can use the `clusterSelector` field in the `OverrideRule` spec.
The `clusterSelector` field supports the following fields:
- `clusterSelectorTerms`: A list of terms that are used to select clusters.
    * Each term in the list is used to select clusters based on the label selector.

### JSON Patch Override
To specify the changes to be applied to the selected resources, you can use the `jsonPatchOverrides` field in the `OverrideRule` spec.
The `jsonPatchOverrides` field supports the following fields:
- `op`: The operation to be performed. The supported operations are `add`, `remove`, and `replace`.
    * `add`: Adds a new value to the specified path.
    * `remove`: Removes the value at the specified path.
    * `replace`: Replaces the value at the specified path.


- `path`: The path to the field to be modified.
    * Some guidelines for the path are as follows:
        * Must start with a `/` character.
        * Cannot be empty.
        * Cannot contain an empty string ("///").
        * Cannot be a TypeMeta Field ("/kind", "/apiVersion").
        * Cannot be a Metadata Field ("/metadata/name", "/metadata/namespace"), except the fields "/metadata/annotations" and "metadata/labels".
        * Cannot be any field in the status of the resource.
    * Some examples of valid paths are:
        * `/metadata/labels/new-label`
        * `/metadata/annotations/new-annotation`
        * `/spec/template/spec/containers/0/resources/limits/cpu`
        * `/spec/template/spec/containers/0/resources/requests/memory`


- `value`: The value to be set.
    * If the `op` is `remove`, the value cannot be set.


### Multiple Override Patches
You may add multiple `JSONPatchOverride` to an `OverrideRule` to apply multiple changes to the selected cluster resources.
```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ClusterResourceOverride
metadata:
  name: cro-1
spec:
  clusterResourceSelectors:
    - group: rbac.authorization.k8s.io
      kind: ClusterRole
      version: v1
      name: secret-reader
  policy:
    overrideRules:
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  env: prod
        jsonPatchOverrides:
          - op: remove
            path: /rules/0/verbs/2
          - op: remove
            path: /rules/0/verbs/1
```
The `ClusterResourceOverride` object above will remove the verbs "list" and "watch" in the `ClusterRole` named 
`secret-reader` on clusters with the label `env: prod`.

## Applying the ClusterResourceOverride
Create a ClusterResourcePlacement resource to specify the placement rules for distributing the cluster resource overrides across
the cluster infrastructure. Ensure that you select the appropriate resource.
```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: crp
spec:
  resourceSelectors:
    - group: rbac.authorization.k8s.io
      kind: ClusterRole
      version: v1
      name: secret-reader
  policy:
    placementType: PickAll
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  env: prod
```
The `ClusterResourcePlacement` configuration outlined above will disperse resources across all clusters labeled with `env: prod`. 
As the changes are implemented, the corresponding `ClusterResourceOverride` configurations will be applied to the 
designated clusters, triggered by the selection of matching cluster role resource `secret-reader`.

To ensure that the `ClusterResourceOverride` object is applied to the selected resources, verify the `ClusterResourcePlacement`
status:
```
Status:
  Conditions:
    ...
    Last Transition Time:   2024-04-27T04:18:00Z
    Message:                The selected resources are successfully overridden in the 10 clusters
    Observed Generation:    1
    Reason:                 OverriddenSucceeded
    Status:                 True
    Type:                   ClusterResourcePlacementOverridden
    ...
  Observed Resource Index:  0
  Placement Statuses:
    Applicable Cluster Resource Overrides:
      example-cro-0
    Cluster Name:  member-50
    Conditions:
      ...
      Message:               Successfully applied the override rules on the resources
      Observed Generation:   1
      Reason:                OverriddenSucceeded
      Status:                True
      Type:                  Overridden
     ...
```
The `ClusterResourcePlacementOverridden` condition indicates whether the resource override has been successfully applied
to the selected resources in the selected clusters.

Each cluster maintains its own `Applicable Cluster Resource Overrides` which contain the cluster resource override snapshot
if relevant. Additionally, individual status messages for each cluster indicates whether the override rules have been 
effectively applied.


