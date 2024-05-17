# How can I debug when my CRP status is ClusterResourcePlacementOverridden condition status is set to false?

The status of the `ClusterResourcePlacementOverridden` condition is set to `false` when there is an Override API related issue.
> Note: In addition, it may be helpful to look into the logs for the overrider controller to get more information on why the override did not succeed.

## Common scenarios:

- The `ClusterResourceOverride` or `ResourceOverride`  is created with an invalid field path for the resource.

## Example Scenario:
In the following example, an attempt is made to override the cluster role `secret-reader` that is being propagated by the `ClusterResourcePlacement` to the selected clusters.
However, the `ClusterResourceOverride` is created with an invalid path for the resource.

### ClusterRole:
```
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
annotations:
kubectl.kubernetes.io/last-applied-configuration: |
{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"annotations":{},"name":"secret-reader"},"rules":[{"apiGroups":[""],"resources":["secrets"],"verbs":["get","watch","list"]}]}
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
The `ClusterResourceOverride` is created to override the `ClusterRole` `secret-reader` by adding a new label `new-label`
and value `new-value` for the clusters with the label `env: canary`.

### CRP Spec:
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

### CRP Status:
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
The accompanying message highlights that the override failed due to the absence of the path `/metadata/labels/new-label` and its corresponding value.
Based on the previous example of the cluster role `secret-reader`, it's evident that no labels were initially present.
Therefore, the specified path for adding a new label is incorrect.

### Resolution:
The solution here is to correct the path and value in the `ClusterResourceOverride` to successfully override the `ClusterRole` `secret-reader` as shown below:
```
jsonPatchOverrides:
  - op: add
    path: /metadata/labels
    value: 
      newlabel: new-value
```
