# How can I debug when my CRP status is ClusterResourcePlacementOverridden condition status is set to false?

The status of the `ClusterResourcePlacementOverridden` condition is set to `false` when there is an Override API related issue.
> Note: To get more information, look into the logs for the overrider controller (includes 
> controller for [ClusterResourceOverride](https://github.com/Azure/fleet/blob/main/pkg/controllers/overrider/clusterresource_controller.go) and 
> [ResourceOverride](https://github.com/Azure/fleet/blob/main/pkg/controllers/overrider/resource_controller.go)).

## Common scenarios:
Instances where this condition may arise:
- The `ClusterResourceOverride` or `ResourceOverride`  is created with an invalid field path for the resource.

## Case Study:
In the following example, an attempt is made to override the cluster role `secret-reader` that is being propagated by the `ClusterResourcePlacement` to the selected clusters.
However, the `ClusterResourceOverride` is created with an invalid path for the field within resource.

### ClusterRole:
```
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
    creationTimestamp: "2024-05-14T15:36:48Z"
    name: secret-reader
    resourceVersion: "81334"
    uid: 108e6312-3416-49be-aa3d-a665c5df58b4
rules:
- apiGroups:
  - ""
    resources:
  - secrets
    verbs:
  - get
  - watch
  - list
```
The `ClusterRole` `secret-reader` that is being propagated to the member clusters by the `ClusterResourcePlacement`.

### ClusterResourceOverride spec:
```
spec:
  clusterResourceSelectors:
  - group: rbac.authorization.k8s.io
    kind: ClusterRole
    name: secret-reader
    version: v1
  policy:
    overrideRules:
    - clusterSelector:
        clusterSelectorTerms:
        - labelSelector:
            matchLabels:
              env: canary
      jsonPatchOverrides:
      - op: add
        path: /metadata/labels/new-label
        value: new-value
```
The `ClusterResourceOverride` is created to override the `ClusterRole` `secret-reader` by adding a new label (`new-label`)
that has the value `new-value` for the clusters with the label `env: canary`.

### ClusterResourcePlacement Spec:
```
spec:
  resourceSelectors:
    - group: rbac.authorization.k8s.io
      kind: ClusterRole
      name: secret-reader
      version: v1
  policy:
    placementType: PickN
    numberOfClusters: 1
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  env: canary
  strategy:
    type: RollingUpdate
    applyStrategy:
      allowCoOwnership: true
```

### ClusterResourcePlacement Status:
```
status:
  conditions:
  - lastTransitionTime: "2024-05-14T16:16:18Z"
    message: found all cluster needed as specified by the scheduling policy, found
      1 cluster(s)
    observedGeneration: 1
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2024-05-14T16:16:18Z"
    message: All 1 cluster(s) start rolling out the latest resource
    observedGeneration: 1
    reason: RolloutStarted
    status: "True"
    type: ClusterResourcePlacementRolloutStarted
  - lastTransitionTime: "2024-05-14T16:16:18Z"
    message: Failed to override resources in 1 cluster(s)
    observedGeneration: 1
    reason: OverriddenFailed
    status: "False"
    type: ClusterResourcePlacementOverridden
  observedResourceIndex: "0"
  placementStatuses:
  - applicableClusterResourceOverrides:
    - cro-1-0
    clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2024-05-14T16:16:18Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 1
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-05-14T16:16:18Z"
      message: Detected the new changes on the resources and started the rollout process
      observedGeneration: 1
      reason: RolloutStarted
      status: "True"
      type: RolloutStarted
    - lastTransitionTime: "2024-05-14T16:16:18Z"
      message: 'Failed to apply the override rules on the resources: add operation
        does not apply: doc is missing path: "/metadata/labels/new-label": missing
        value'
      observedGeneration: 1
      reason: OverriddenFailed
      status: "False"
      type: Overridden
  selectedResources:
  - group: rbac.authorization.k8s.io
    kind: ClusterRole
    name: secret-reader
    version: v1
```
The CRP attempted to override a propagated resource utilizing an applicable `ClusterResourceOverrideSnapshot`.
However, as the `ClusterResourcePlacementOverridden` condition remains false, looking at the placement status for the cluster
where the condition `Overriden` failed will offer insights into the exact cause of the failure.

In this situation, the message indicates that the override failed because the path `/metadata/labels/new-label` and its corresponding value are missing.
Based on the previous example of the cluster role `secret-reader`, you can see that the path `/metadata/labels/` doesn't exist. This means that `labels` doesn't exist.
Therefore, a new label can't be added.

### Resolution:
To successfully override the cluster role `secret-reader`, correct the path and value in `ClusterResourceOverride`, as shown in the following code:
```
jsonPatchOverrides:
  - op: add
    path: /metadata/labels
    value: 
      newlabel: new-value
```
This will successfully add the new label `newlabel` with the value `new-value` to the `ClusterRole` `secret-reader`, as we are creating the `labels` field and adding a new value `newlabel: new-value` to it.