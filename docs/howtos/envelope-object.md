# How-to Guide: To propagate resources using ClusterResourcePlacement API without unintended side effects on the hub clusters

This guide discusses how to propagate a set of resources from the hub cluster to joined member clusters within an envelope object.

Currently, we allow `ConfigMap` to act as an envelope object by using a fleet reserved annotation.

So any configmap that is an envelope object must have this annotation on it `kubernetes-fleet.io/envelope-configmap` which would be set to **true**

## Example of an envelope object:

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

## Propagating a envelope configmap from hub cluster to member cluster:

We will now apply the example envelope object above on our hub cluster, the envelope object belongs to a namespace called app hence make sure the namespace app exists on the hub cluster. Then we use a `ClusterResourcePlacement` object to propagate the resource from hub to a member cluster named `kind-cluster-1`.

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

**Note:**
In the `selectedResources` section within the `placementStatus` for `kind-cluster-1` we only show that the envelope object got propagated, we don't include all the resources within the envelope object in the status.

From the `selectedResources` section within the `placementStatus` for `kind-cluster-1` we see that the namespace `app` along with the configmap `envelope-configmap` got propagated. And the user can also verify with resources mentioned within in the `envelope-configmap` object are also propagated in this case a `ResourceQuota` object and `MutatingWebhookConfigurations` object.