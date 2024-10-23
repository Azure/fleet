# How can I debug when my CRP status is ClusterResourcePlacementWorkSynchronized condition status is set to false?

The `ClusterResourcePlacementWorkSynchronized` condition is false when the CRP has been recently updated but the associated work objects have not yet been synchronized with the changes.
> Note: In addition, it may be helpful to look into the logs for the [work generator controller](https://github.com/Azure/fleet/blob/main/pkg/controllers/workgenerator/controller.go) to get more information on why the work synchronization failed.

## Common Scenarios:
Instances where this condition may arise:
- The controller encounters an error while trying to generate the corresponding `work` object.
- The enveloped object is not well formatted.

### Case Study:
The CRP is attempting to propagate a resource to a selected cluster, but the work object has not been updated to reflect the latest changes due to the selected cluster has been terminated.

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
  strategy:
    type: RollingUpdate
 ```

### ClusterResourcePlacement Status:
```
spec:
  policy:
    numberOfClusters: 1
    placementType: PickN
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
  - lastTransitionTime: "2024-05-14T18:05:04Z"
    message: found all cluster needed as specified by the scheduling policy, found
      1 cluster(s)
    observedGeneration: 1
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2024-05-14T18:05:05Z"
    message: All 1 cluster(s) start rolling out the latest resource
    observedGeneration: 1
    reason: RolloutStarted
    status: "True"
    type: ClusterResourcePlacementRolloutStarted
  - lastTransitionTime: "2024-05-14T18:05:05Z"
    message: No override rules are configured for the selected resources
    observedGeneration: 1
    reason: NoOverrideSpecified
    status: "True"
    type: ClusterResourcePlacementOverridden
  - lastTransitionTime: "2024-05-14T18:05:05Z"
    message: There are 1 cluster(s) which have not finished creating or updating work(s)
      yet
    observedGeneration: 1
    reason: WorkNotSynchronizedYet
    status: "False"
    type: ClusterResourcePlacementWorkSynchronized
  observedResourceIndex: "0"
  placementStatuses:
  - clusterName: kind-cluster-1
    conditions:
    - lastTransitionTime: "2024-05-14T18:05:04Z"
      message: 'Successfully scheduled resources for placement in kind-cluster-1 (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 1
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-05-14T18:05:05Z"
      message: Detected the new changes on the resources and started the rollout process
      observedGeneration: 1
      reason: RolloutStarted
      status: "True"
      type: RolloutStarted
    - lastTransitionTime: "2024-05-14T18:05:05Z"
      message: No override rules are configured for the selected resources
      observedGeneration: 1
      reason: NoOverrideSpecified
      status: "True"
      type: Overridden
    - lastTransitionTime: "2024-05-14T18:05:05Z"
      message: 'Failed to synchronize the work to the latest: works.placement.kubernetes-fleet.io
        "crp1-work" is forbidden: unable to create new content in namespace fleet-member-kind-cluster-1
        because it is being terminated'
      observedGeneration: 1
      reason: SyncWorkFailed
      status: "False"
      type: WorkSynchronized
  selectedResources:
  - kind: Namespace
    name: test-ns
    version: v1
```
In the `ClusterResourcePlacement` status, the `ClusterResourcePlacementWorkSynchronized` condition status shows as `False`. 
The message for it indicates that the work object `crp1-work` is prohibited from generating new content within the namespace `fleet-member-kind-cluster-1` because it's currently terminating.

### Resolution:
To address the issue at hand, there are several potential solutions:
- Modify the `ClusterResourcePlacement` with a newly selected cluster. 
- Delete the `ClusterResourcePlacement` to remove work through garbage collection.
- Rejoin the member cluster. The namespace can only be regenerated after rejoining the cluster.

In other situations, you might opt to wait for the work to finish propagating.
