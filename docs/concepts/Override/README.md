# Override

## Overview
The `ClusterResourceOverride` and `ResourceOverride` provides a way to customize resource configurations before they are propagated 
to the target cluster by the `ClusterResourcePlacement`.

## Difference Between `ClusterResourceOverride` And `ResourceOverride`

`ClusterResourceOverride` represents the cluster-wide policy that overrides the cluster scoped resources to one or more
clusters while `ResourceOverride` will apply to resources in the same namespace as the namespace-wide policy.

> **Note:** If a namespace is selected by the `ClusterResourceOverride`, ALL the resources under the namespace are selected
automatically.

If the resource is selected by both `ClusterResourceOverride` and `ResourceOverride`, the `ResourceOverride` will win
when resolving the conflicts.

## When To Use Override
Overrides is useful when you want to customize the resources before they are propagated from the hub cluster to the target clusters.
Some example use cases are:
- As a platform operator, I want to propagate a clusterRoleBinding to cluster-us-east and cluster-us-west and would like to
grant the same role to different groups in each cluster.
- As a platform operator, I want to propagate a clusterRole to cluster-staging and cluster-production and would like to
grant more permissions to the cluster-staging cluster than the cluster-production cluster.
- As a platform operator, I want to propagate a namespace to all the clusters and would like to customize the labels for
each cluster.
- As an application developer, I would like to propagate a deployment to cluster-staging and cluster-production and would
like to always use the latest image in the staging cluster and a specific image in the production cluster.
- As an application developer, I would like to propagate a deployment to all the clusters and would like to use different
commands for my container in different regions.

## Limits
- Each resource can be only selected by one override simultaneously. In the case of namespace scoped resources, up to two
overrides will be allowed, considering the potential selection through both `ClusterResourceOverride` (select its namespace) 
and `ResourceOverride`.
- At most 100 `ClusterResourceOverride` can be created.
- At most 100 `ResourceOverride` can be created.

## Resource Selector
`ClusterResourceSelector` of `ClusterResourceOverride` selects which cluster-scoped resources need to be overridden before
applying to the selected clusters.

It supports the following forms of resource selection:
- Select resources by specifying the <group, version, kind> and name. This selection propagates only one resource that 
matches the <group, version, kind> and name.

> **Note:** Label selector of `ClusterResourceSelector` is not supported.

`ResourceSelector` of `ResourceOverride` selects which namespace-scoped resources need to be overridden before applying to
the selected clusters.

It supports the following forms of resource selection:
- Select resources by specifying the <group, version, kind> and name. This selection propagates only one resource that
matches the <group, version, kind> and name under the `ResourceOverride` namespace.

## Override Policy
Override policy defines how to override the selected resources on the target clusters.

It contains an array of override rules and its order determines the override order. For example, when there are two rules
selecting the same fields on the target cluster, the last one will win.

Each override rule contains the following fields:
- `ClusterSelector`: which cluster(s) the override rule applies to. It supports the following forms of cluster selection:
  - Select clusters by specifying the cluster labels.
  - An empty selector selects ALL the clusters.
  - A nil selector selects NO target cluster.
- `JSONPatchOverrides`: a list of JSON path override rules applied to the selected resources following [RFC 6902](https://datatracker.ietf.org/doc/html/rfc6902).

> **Note:** Updating the fields in the TypeMeta (e.g., `apiVersion`, `kind`) is not allowed.

> **Note:** Updating the fields in the ObjectMeta (e.g., `name`, `namespace`) excluding annotations and labels is not allowed.

> **Note:** Updating the fields in the Status (e.g., `status`) is not allowed.

## When To Trigger Rollout

It will take the snapshot of each override change as a result of `ClusterResourceOverrideSnapshot` and
`ResourceOverrideSnapshot`. The snapshot will be used to determine whether the override change should be applied to the existing
`ClusterResourcePlacement` or not. If applicable, it will start rolling out the new resources to the target clusters by
respecting the rollout strategy defined in the `ClusterResourcePlacement`.

## Examples

### add annotations to the configmap by using clusterResourceOverride
Suppose we create a configmap named `app-config-1` under the namespace `application-1` in the hub cluster, and we want to 
add an annotation to it, which is applied to all the member clusters.

```yaml
apiVersion: v1
data:
  data: test
kind: ConfigMap
metadata:
  creationTimestamp: "2024-05-07T08:06:27Z"
  name: app-config-1
  namespace: application-1
  resourceVersion: "1434"
  uid: b4109de8-32f2-4ac8-9e1a-9cb715b3261d
```

Create a `ClusterResourceOverride` named `cro-1` to add an annotation to the namespace `application-1`.

```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ClusterResourceOverride
metadata:
  creationTimestamp: "2024-05-07T08:06:27Z"
  finalizers:
    - kubernetes-fleet.io/override-cleanup
  generation: 1
  name: cro-1
  resourceVersion: "1436"
  uid: 32237804-7eb2-4d5f-9996-ff4d8ce778e7
spec:
  clusterResourceSelectors:
    - group: ""
      kind: Namespace
      name: application-1
      version: v1
  policy:
    overrideRules:
      - clusterSelector:
          clusterSelectorTerms: []
        jsonPatchOverrides:
          - op: add
            path: /metadata/annotations
            value:
              cro-test-annotation: cro-test-annotation-val
```

Check the configmap on one of the member cluster by running `kubectl get configmap app-config-1 -n application-1 -o yaml` command:

```yaml
apiVersion: v1
data:
  data: test
kind: ConfigMap
metadata:
  annotations:
    cro-test-annotation: cro-test-annotation-val
    kubernetes-fleet.io/last-applied-configuration: '{"apiVersion":"v1","data":{"data":"test"},"kind":"ConfigMap","metadata":{"annotations":{"cro-test-annotation":"cro-test-annotation-val","kubernetes-fleet.io/spec-hash":"4dd5a08aed74884de455b03d3b9c48be8278a61841f3b219eca9ed5e8a0af472"},"name":"app-config-1","namespace":"application-1","ownerReferences":[{"apiVersion":"placement.kubernetes-fleet.io/v1beta1","blockOwnerDeletion":false,"kind":"AppliedWork","name":"crp-1-work","uid":"77d804f5-f2f1-440e-8d7e-e9abddacb80c"}]}}'
    kubernetes-fleet.io/spec-hash: 4dd5a08aed74884de455b03d3b9c48be8278a61841f3b219eca9ed5e8a0af472
  creationTimestamp: "2024-05-07T08:06:27Z"
  name: app-config-1
  namespace: application-1
  ownerReferences:
  - apiVersion: placement.kubernetes-fleet.io/v1beta1
    blockOwnerDeletion: false
    kind: AppliedWork
    name: crp-1-work
    uid: 77d804f5-f2f1-440e-8d7e-e9abddacb80c
  resourceVersion: "1449"
  uid: a8601007-1e6b-4b64-bc05-1057ea6bd21b
```

### add annotations to the configmap by using resourceOverride

You can use the `ResourceOverride` to add an annotation to the configmap `app-config-1` explicitly in the namespace `application-1`.

```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ResourceOverride
metadata:
  creationTimestamp: "2024-05-07T08:25:31Z"
  finalizers:
  - kubernetes-fleet.io/override-cleanup
  generation: 1
  name: ro-1
  namespace: application-1
  resourceVersion: "3859"
  uid: b4117925-bc3c-438d-a4f6-067bc4577364
spec:
  policy:
    overrideRules:
    - clusterSelector:
        clusterSelectorTerms: []
      jsonPatchOverrides:
      - op: add
        path: /metadata/annotations
        value:
          ro-test-annotation: ro-test-annotation-val
  resourceSelectors:
  - group: ""
    kind: ConfigMap
    name: app-config-1
    version: v1
```

## How To Validate If Overrides Are Applied

You can validate if the overrides are applied by checking the `ClusterResourcePlacement` status. The status output will 
indicate both placement conditions and individual placement statuses on each member cluster that was overridden.

Sample output:
```yaml
status:
  conditions:
  - lastTransitionTime: "2024-05-07T08:06:27Z"
    message: found all the clusters needed as specified by the scheduling policy
    observedGeneration: 1
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2024-05-07T08:06:27Z"
    message: All 3 cluster(s) start rolling out the latest resource
    observedGeneration: 1
    reason: RolloutStarted
    status: "True"
    type: ClusterResourcePlacementRolloutStarted
  - lastTransitionTime: "2024-05-07T08:06:27Z"
    message: The selected resources are successfully overridden in the 3 clusters
    observedGeneration: 1
    reason: OverriddenSucceeded
    status: "True"
    type: ClusterResourcePlacementOverridden
  - lastTransitionTime: "2024-05-07T08:06:27Z"
    message: Works(s) are succcesfully created or updated in the 3 target clusters'
      namespaces
    observedGeneration: 1
    reason: WorkSynchronized
    status: "True"
    type: ClusterResourcePlacementWorkSynchronized
  - lastTransitionTime: "2024-05-07T08:06:27Z"
    message: The selected resources are successfully applied to 3 clusters
    observedGeneration: 1
    reason: ApplySucceeded
    status: "True"
    type: ClusterResourcePlacementApplied
  - lastTransitionTime: "2024-05-07T08:06:27Z"
    message: The selected resources in 3 cluster are available now
    observedGeneration: 1
    reason: ResourceAvailable
    status: "True"
    type: ClusterResourcePlacementAvailable
  observedResourceIndex: "0"
  placementStatuses:
  - applicableClusterResourceOverrides:
    - cro-1-0
    clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2024-05-07T08:06:27Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 1
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-05-07T08:06:27Z"
      message: Detected the new changes on the resources and started the rollout process
      observedGeneration: 1
      reason: RolloutStarted
      status: "True"
      type: RolloutStarted
    - lastTransitionTime: "2024-05-07T08:06:27Z"
      message: Successfully applied the override rules on the resources
      observedGeneration: 1
      reason: OverriddenSucceeded
      status: "True"
      type: Overridden
    - lastTransitionTime: "2024-05-07T08:06:27Z"
      message: All of the works are synchronized to the latest
      observedGeneration: 1
      reason: AllWorkSynced
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2024-05-07T08:06:27Z"
      message: All corresponding work objects are applied
      observedGeneration: 1
      reason: AllWorkHaveBeenApplied
      status: "True"
      type: Applied
    - lastTransitionTime: "2024-05-07T08:06:27Z"
      message: The availability of work object crp-1-work is not trackable
      observedGeneration: 1
      reason: WorkNotTrackable
      status: "True"
      type: Available
...
```

`applicableClusterResourceOverrides` in `placementStatuses` indicates which `ClusterResourceOverrideSnapshot` that is applied
to the target cluster. Similarly, `applicableResourceOverrides` will be set if the `ResourceOverrideSnapshot` is applied.
