# How-to Guide: Using the Fleet `ResourceOverride` API

This guide provides an overview of how to use the Fleet `ResourceOverride` API to override resources.

## Overview
`ResourceOverride` is a Fleet API that allows you to modify or override specific attributes of 
existing resources within your cluster. With ResourceOverride, you can define rules based on cluster 
labels or other criteria, specifying changes to be applied to resources such as Deployments, StatefulSets, ConfigMaps, or Secrets. 
These changes can include updates to container images, environment variables, resource limits, or any other configurable parameters. 

## API Components
The ResourceOverride API consists of the following components:
- **Resource Selectors**: These specify the set of resources selected for overriding.
- **Policy**: This specifies the policy to be applied to the selected resources.


The following sections discuss these components in depth.

## Resource Selectors
A `ResourceOverride` object may feature one or more resource selectors, specifying which resources to select to be overridden.

The `ResourceSelector` object supports the following fields:
- `group`: The API group of the resource
- `version`: The API version of the resource
- `kind`: The kind of the resource
- `name`: The name of the resource
> Note: The resource can only be selected by name.


To add a resource selector, edit the `resourceSelectors` field in the `ResourceOverride` spec:
```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ResourceOverride
metadata:
  name: example-resource-override
  namespace: test-namespace
spec:
  resourceSelectors:
    -  group: apps
       kind: Deployment
       version: v1
       name: test-nginx
```
> Note: The ResourceOverride needs to be in the same namespace as the resources it is overriding.

The example above will pick a `Deployment` named `my-deployment` from the namespace `test-namespace` to be overridden.


## Policy
The `Policy` is made up of a set of rules (`OverrideRules`) that specify the changes to be applied to the selected 
resources on selected clusters.

Each `OverrideRule` supports the following fields:
- **Cluster Selector**: This specifies the set of clusters to which the override applies.
- **JSON Patch Override**: This specifies the changes to be applied to the selected resources.

To add an override rule, edit the `policy` field in the `ResourceOverride` spec:

```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ResourceOverride
metadata:
  name: example-resource-override
  namespace: test-namespace
spec:
  resourceSelectors:
    -  group: apps
       kind: Deployment
       version: v1
       name: test-nginx
  policy:
    overrideRules:
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  env: prod
        jsonPatchOverrides:
          - op: replace
            path: /spec/template/spec/containers/0/image
            value: "nginx:1.20.0"
```
The `ResourceOverride` object above will replace the image of the container in the `Deployment` named `my-deployment` 
with the image `nginx:1.20.0` on all clusters with the label `env: prod`.

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


### Multiple Override Rules
You may add multiple `OverrideRules` to a `Policy` to apply multiple changes to the selected resources.
```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ResourceOverride
metadata:
  name: ro-1
  namespace: test
spec:
  resourceSelectors:
    -  group: apps
       kind: Deployment
       version: v1
       name: test-nginx
  policy:
    overrideRules:
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  env: prod
        jsonPatchOverrides:
          - op: replace
            path: /spec/template/spec/containers/0/image
            value: "nginx:1.20.0"
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  env: test
        jsonPatchOverrides:
          - op: replace
            path: /spec/template/spec/containers/0/image
            value: "nginx:latest"
```
The `ResourceOverride` object above will replace the image of the container in the `Deployment` named `test-nginx`
with the image `nginx:1.20.0` on all clusters with the label `env: prod` and the image `nginx:latest` on all clusters with the label `env: test`.

## Applying the ResourceOverride
Create a ClusterResourcePlacement resource to specify the placement rules for distributing the resource overrides across 
the cluster infrastructure. Ensure that you select the appropriate namespaces containing the matching resources.
```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: crp-example
spec:
  resourceSelectors:
    - group: ""
      kind: Namespace
      name: test-namespace
      version: v1
  policy:
    placementType: PickAll
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  env: prod
            - labelSelector:
                matchLabels:
                  env: test
```
The `ClusterResourcePlacement` configuration outlined above will disperse resources within `test-namespace` across all 
clusters labeled with `env: prod` and `env: test`. As the changes are implemented, the corresponding `ResourceOverride` 
configurations will be applied to the designated clusters, triggered by the selection of matching deployment resource 
`my-deployment`.

To ensure that the `ResourceOverride` object is applied to the selected resources, verify the `ClusterResourcePlacement`
status:
```
Status:
  Conditions:
    ...
    Message:                The selected resources are successfully overridden in the 10 clusters
    Observed Generation:    1
    Reason:                 OverriddenSucceeded
    Status:                 True
    Type:                   ClusterResourcePlacementOverridden
    ...
  Observed Resource Index:  0
  Placement Statuses:
    Applicable Resource Overrides:
      Name:        ro-1-0
      Namespace:   test-namespace
    Cluster Name:  member-50
    Conditions:
      ...
      Last Transition Time:  2024-04-26T22:57:14Z
      Message:               Successfully applied the override rules on the resources
      Observed Generation:   1
      Reason:                OverriddenSucceeded
      Status:                True
      Type:                  Overridden
     ...
```
The `ClusterResourcePlacementOverridden` condition indicates whether the resource override has been successfully applied
to the selected resources in the selected clusters.
```
Selected Resources:
    Kind:       Namespace
    Name:       test-namespace
    Version:    v1
    Group:      apps
    Kind:       Deployment
    Name:       test-nginx
    Namespace:  test-namespace
    Version:    v1
```
Each cluster maintains its own `Applicable Resource Overrides` which contain the resource override snapshot and
the resource override namespace if relevant. Additionally, individual status messages for each cluster indicates
whether the override rules have been effectively applied.


