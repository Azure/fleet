# How-to Guide: To propagate resources using ClusterResourcePlacement API without unintended side effects on the hub clusters

# Propagating Resources with Envelope Objects

This guide provides instructions on propagating a set of resources from the hub cluster to joined member clusters within an envelope object.

## Envelope Object with ConfigMap

Currently, we support using a `ConfigMap` as an envelope object by leveraging a fleet-reserved annotation.

To designate a `ConfigMap` as an envelope object, ensure that it contains the following annotation:

```yaml
metadata:
  annotations:
    kubernetes-fleet.io/envelope-configmap: "true"
```

## Example:

### Configmap Envelope Object:
```
apiVersion: v1
kind: ConfigMap
metadata:
  name: envelope-configmap
  namespace: app
  annotations:
    kubernetes-fleet.io/envelope-configmap: "true"
data:
  resourceQuota.yaml: |
    apiVersion: v1
    kind: ResourceQuota
    metadata:
      name: mem-cpu-demo
      namespace: app
    spec:
      hard:
        requests.cpu: "1"
        requests.memory: 1Gi
        limits.cpu: "2"
        limits.memory: 2Gi
  webhook.yaml: |
    apiVersion: admissionregistration.k8s.io/v1
    kind: MutatingWebhookConfiguration
    metadata:
      creationTimestamp: null
      labels:
        azure-workload-identity.io/system: "true"
      name: azure-wi-webhook-mutating-webhook-configuration
    webhooks:
    - admissionReviewVersions:
      - v1
      - v1beta1
      clientConfig:
        service:
          name: azure-wi-webhook-webhook-service
          namespace: app
          path: /mutate-v1-pod
      failurePolicy: Fail
      matchPolicy: Equivalent
      name: mutation.azure-workload-identity.io
      rules:
      - apiGroups:
        - ""
        apiVersions:
        - v1
        operations:
        - CREATE
        - UPDATE
        resources:
        - pods
      sideEffects: None
```

## Propagating an envelope configmap from hub cluster to member cluster:

We will now apply the example envelope object above on our hub cluster, the envelope object belongs to a namespace called `app` hence make sure the namespace `app` exists on the hub cluster. Then we use a `ClusterResourcePlacement` object to propagate the resource from hub to a member cluster named `kind-cluster-1`.

### CRP spec:
```
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
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
```

### CRP status:

```
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
  - kind: ConfigMap
    name: envelope-configmap
    namespace: app
    version: v1
```

# Note:
In the `selectedResources` section within the `placementStatus` for `kind-cluster-1`, we specifically display the propagation of the envelope object. Please note that we do not individually list all the resources contained within the envelope object in this status.

Upon inspection of the `selectedResources` section for `kind-cluster-1`, it indicates that the namespace `app` and the configmap `envelope-configmap` have been successfully propagated. Users can further verify the successful propagation of resources mentioned within the `envelope-configmap` object by ensuring that the `failedPlacements` section in the `placementStatus` for `kind-cluster-1` does not appear in the status.

## Example of using an Envelope object where resource failed to apply:

To try this scenario, ensure the CRP created in the section above are deleted on the hub cluster.

In this example we will make a small change to the envelope object we used above by changing the namespace to `test-ns`, so we ensure that we have namespace `test-ns` exists on the hub cluster.

### Configmap Envelope Object:
```
apiVersion: v1
kind: ConfigMap
metadata:
  name: envelop-configmap
  namespace: test-ns
  annotations:
    kubernetes-fleet.io/envelope-configmap: "true"
data:
  resourceQuota.yaml: |
    apiVersion: v1
    kind: ResourceQuota
    metadata:
      name: mem-cpu-demo
      namespace: app
    spec:
      hard:
        requests.cpu: "1"
        requests.memory: 1Gi
        limits.cpu: "2"
        limits.memory: 2Gi
  webhook.yaml: |
    apiVersion: admissionregistration.k8s.io/v1
    kind: MutatingWebhookConfiguration
    metadata:
      creationTimestamp: null
      labels:
        azure-workload-identity.io/system: "true"
      name: azure-wi-webhook-mutating-webhook-configuration
    webhooks:
    - admissionReviewVersions:
      - v1
      - v1beta1
      clientConfig:
        service:
          name: azure-wi-webhook-webhook-service
          namespace: app
          path: /mutate-v1-pod
      failurePolicy: Fail
      matchPolicy: Equivalent
      name: mutation.azure-workload-identity.io
      rules:
      - apiGroups:
        - ""
        apiVersions:
        - v1
        operations:
        - CREATE
        - UPDATE
        resources:
        - pods
      sideEffects: None
```

### Propagate configmap envelope object from hub cluster to member cluster:

We use a `ClusterResourcePlacement` object to propagate the resource from hub to a member cluster named `kind-cluster-1`.

### CRP spec:
```
spec:
  policy:
    clusterNames:
    - kind-cluster-1
    placementType: PickFixed
  resourceSelectors:
  - group: ""
    kind: Namespace
    name: test-ns
    version: v1
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
```

### CRP status:
```
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"placement.kubernetes-fleet.io/v1beta1","kind":"ClusterResourcePlacement","metadata":{"annotations":{},"name":"test-crp"},"spec":{"policy":{"clusterNames":["kind-cluster-1"],"placementType":"PickFixed"},"resourceSelectors":[{"group":"","kind":"Namespace","name":"test-ns","version":"v1"}]}}
  creationTimestamp: "2023-12-06T00:09:53Z"
  finalizers:
  - kubernetes-fleet.io/crp-cleanup
  - kubernetes-fleet.io/scheduler-cleanup
  generation: 2
  name: test-crp
  resourceVersion: "2610"
  uid: 33421ee0-9b69-4c2e-835f-31e086999940
spec:
  policy:
    clusterNames:
    - kind-cluster-1
    placementType: PickFixed
  resourceSelectors:
  - group: ""
    kind: Namespace
    name: test-ns
    version: v1
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
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
        name: envelop-configmap
        namespace: test-ns
        type: ConfigMap
      kind: ResourceQuota
      name: mem-cpu-demo
      namespace: app
      version: v1
  selectedResources:
  - kind: Namespace
    name: test-ns
    version: v1
  - kind: ConfigMap
    name: envelop-configmap
    namespace: test-ns
    version: v1
```

In `ClusterResourcePlacement` status, `placementStatuses` section for `kind-cluster-1` in the `failedPlacements` section we see that the ResourceQuota object had failed to apply with the following error message "**Failed to apply manifest: namespaces "app" not found**", we also see the envelope object `envelope-configmap` which tried to propagate this resource mentioned in the `failedPlacements` section.

We see this message because we tried to propagate the namespace `test-ns` which contains the envelope object. But the envelope object contains resources belonging to another namespace called `app` which doesn't exist on the member cluster.

### Resolution:
- Create the namespace `app` on the member cluster.
- Propagate the namespace called `app` using another `ClusterResourcePlacement` before we apply the current `ClusterResourcePlacement` from the example, doing this ensures that namespace app exists on the member cluster before we propagate resources to it.
- Propagate a namespace called `app` along with namespace `test-ns` in the same `ClusterResourcePlacement` object, doing this can lead to `ClusterResourcePlacementApplied` condition showing up as false for some time because we cannot **guarantee the order in which the namespaces are propagated** this eventually gets resolved once the `ClusterResourcePlacement` object is reconciled again.
