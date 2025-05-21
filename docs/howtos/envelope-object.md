# How-to Guide: To propagate resources using ClusterResourcePlacement API without unintended side effects on the hub cluster

## Propagating Resources with Envelope Objects

This guide provides instructions on propagating a set of resources from the hub cluster to joined member clusters within an envelope object.

## Envelope Objects with CRDs

Fleet now supports two types of envelope Custom Resource Definitions (CRDs) for propagating resources:

1. **ClusterResourceEnvelope**: Used to wrap cluster-scoped resources for placement.
2. **ResourceEnvelope**: Used to wrap namespace-scoped resources for placement.

These CRDs provide a more structured and Kubernetes-native way to package resources for propagation to member clusters without causing unintended side effects on the hub cluster.

### ClusterResourceEnvelope Example

The `ClusterResourceEnvelope` is a cluster-scoped resource that can only wrap other cluster-scoped resources. For example:

```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourceEnvelope
metadata:
  name: example
data:
  "webhook.yaml":
    apiVersion: admissionregistration.k8s.io/v1
    kind: ValidatingWebhookConfiguration
    metadata:
      name: guard
    webhooks:
    - name: guard.example.com
      rules:
      - operations: ["CREATE"]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
      clientConfig:
        service:
          name: guard
          namespace: ops
      admissionReviewVersions: ["v1"]
      sideEffects: None
      timeoutSeconds: 10
  "clusterrole.yaml":
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      name: pod-reader
    rules:
    - apiGroups: [""]
      resources: ["pods"]
      verbs: ["get", "list", "watch"]
```

### ResourceEnvelope Example

The `ResourceEnvelope` is a namespace-scoped resource that can only wrap namespace-scoped resources. For example:

```yaml
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ResourceEnvelope
metadata:
  name: example
  namespace: app
data:
  "cm.yaml":
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: config
      namespace: app
    data:
      foo: bar
  "deploy.yaml":
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: ingress
      namespace: app
    spec:
      replicas: 1
      selector:
        matchLabels:
          app: nginx
      template:
        metadata:
          labels:
            app: nginx
        spec:
          containers:
          - name: web
            image: nginx
```

## Propagating Envelope Objects from Hub cluster to Member cluster

We apply our envelope objects on the hub cluster and then use a `ClusterResourcePlacement` object to propagate these resources from the hub to member clusters.

### Example CRP spec for propagating a ResourceEnvelope:

```yaml
spec:
  policy:
    clusterNames:
    - kind-cluster-1
    placementType: PickFixed
  resourceSelectors:
  - group: ""
    kind: Namespace
    name: app
    version: v1
  - group: placement.kubernetes-fleet.io
    kind: ResourceEnvelope
    name: example
    namespace: app
    version: v1beta1
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
```

### Example CRP spec for propagating a ClusterResourceEnvelope:

```yaml
spec:
  policy:
    clusterNames:
    - kind-cluster-1
    placementType: PickFixed
  resourceSelectors:
  - group: placement.kubernetes-fleet.io
    kind: ClusterResourceEnvelope
    name: example
    version: v1beta1
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
```

### CRP status for ResourceEnvelope:

```yaml
status:
  conditions:
  - lastTransitionTime: "2023-11-30T19:54:13Z"
    message: found all the clusters needed as specified by the scheduling policy
    observedGeneration: 2
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2023-11-30T19:54:18Z"
    message: All 1 cluster(s) are synchronized to the latest resources on the hub
      cluster
    observedGeneration: 2
    reason: SynchronizeSucceeded
    status: "True"
    type: ClusterResourcePlacementSynchronized
  - lastTransitionTime: "2023-11-30T19:54:18Z"
    message: Successfully applied resources to 1 member clusters
    observedGeneration: 2
    reason: ApplySucceeded
    status: "True"
    type: ClusterResourcePlacementApplied
  placementStatuses:
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2023-11-30T19:54:13Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1:
        picked by scheduling policy'
      observedGeneration: 2
      reason: ScheduleSucceeded
      status: "True"
      type: ResourceScheduled
    - lastTransitionTime: "2023-11-30T19:54:18Z"
      message: Successfully Synchronized work(s) for placement
      observedGeneration: 2
      reason: WorkSynchronizeSucceeded
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2023-11-30T19:54:18Z"
      message: Successfully applied resources
      observedGeneration: 2
      reason: ApplySucceeded
      status: "True"
      type: ResourceApplied
  selectedResources:
  - kind: Namespace
    name: app
    version: v1
  - group: placement.kubernetes-fleet.io
    kind: ResourceEnvelope
    name: example
    namespace: app
    version: v1beta1
```

> **Note:** In the `selectedResources` section, we specifically display the propagated envelope object. We do not individually list all the resources contained within the envelope object in the status.

Upon inspection of the `selectedResources`, it indicates that the namespace `app` and the ResourceEnvelope `example` have been successfully propagated. Users can further verify the successful propagation of resources contained within the envelope object by ensuring that the `failedPlacements` section in the `placementStatus` for the target cluster does not appear in the status.

## Example CRP status where resources within an envelope object failed to apply

### CRP status with failed ResourceEnvelope resource:

In the example below, within the `placementStatus` section for `kind-cluster-1`, the `failedPlacements` section provides details on a resource that failed to apply along with information about the envelope object which contained the resource.

```yaml
status:
  conditions:
  - lastTransitionTime: "2023-12-06T00:09:53Z"
    message: found all the clusters needed as specified by the scheduling policy
    observedGeneration: 2
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2023-12-06T00:09:58Z"
    message: All 1 cluster(s) are synchronized to the latest resources on the hub
      cluster
    observedGeneration: 2
    reason: SynchronizeSucceeded
    status: "True"
    type: ClusterResourcePlacementSynchronized
  - lastTransitionTime: "2023-12-06T00:09:58Z"
    message: Failed to apply manifests to 1 clusters, please check the `failedPlacements`
      status
    observedGeneration: 2
    reason: ApplyFailed
    status: "False"
    type: ClusterResourcePlacementApplied
  placementStatuses:
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2023-12-06T00:09:53Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1:
        picked by scheduling policy'
      observedGeneration: 2
      reason: ScheduleSucceeded
      status: "True"
      type: ResourceScheduled
    - lastTransitionTime: "2023-12-06T00:09:58Z"
      message: Successfully Synchronized work(s) for placement
      observedGeneration: 2
      reason: WorkSynchronizeSucceeded
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2023-12-06T00:09:58Z"
      message: Failed to apply manifests, please check the `failedPlacements` status
      observedGeneration: 2
      reason: ApplyFailed
      status: "False"
      type: ResourceApplied
    failedPlacements:
    - condition:
        lastTransitionTime: "2023-12-06T00:09:53Z"
        message: 'Failed to apply manifest: namespaces "app" not found'
        reason: AppliedManifestFailedReason
        status: "False"
        type: Applied
      envelope:
        name: example
        namespace: app
        type: ResourceEnvelope
      kind: Deployment
      name: ingress
      namespace: app
      version: apps/v1
  selectedResources:
  - kind: Namespace
    name: app
    version: v1
  - group: placement.kubernetes-fleet.io
    kind: ResourceEnvelope
    name: example
    namespace: app
    version: v1beta1
```

### CRP status with failed ClusterResourceEnvelope resource:

Similar to namespace-scoped resources, cluster-scoped resources within a ClusterResourceEnvelope can also fail to apply:

```yaml
status:
  conditions:
  - lastTransitionTime: "2023-12-06T00:09:53Z"
    message: found all the clusters needed as specified by the scheduling policy
    observedGeneration: 2
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2023-12-06T00:09:58Z"
    message: Failed to apply manifests to 1 clusters, please check the `failedPlacements`
      status
    observedGeneration: 2
    reason: ApplyFailed
    status: "False"
    type: ClusterResourcePlacementApplied
  placementStatuses:
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2023-12-06T00:09:58Z"
      message: Failed to apply manifests, please check the `failedPlacements` status
      observedGeneration: 2
      reason: ApplyFailed
      status: "False"
      type: ResourceApplied
    failedPlacements:
    - condition:
        lastTransitionTime: "2023-12-06T00:09:53Z"
        message: 'Failed to apply manifest: service "guard" not found in namespace "ops"'
        reason: AppliedManifestFailedReason
        status: "False"
        type: Applied
      envelope:
        name: example
        type: ClusterResourceEnvelope
      kind: ValidatingWebhookConfiguration
      name: guard
      group: admissionregistration.k8s.io
      version: v1
  selectedResources:
  - group: placement.kubernetes-fleet.io
    kind: ClusterResourceEnvelope
    name: example
    version: v1beta1
```