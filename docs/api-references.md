# API Reference

## Packages
- [cluster.kubernetes-fleet.io/v1beta1](#clusterkubernetes-fleetiov1beta1)
- [placement.kubernetes-fleet.io/v1beta1](#placementkubernetes-fleetiov1beta1)


## cluster.kubernetes-fleet.io/v1beta1



### Resource Types
- [MemberCluster](#membercluster)
- [MemberClusterList](#memberclusterlist)





#### AgentStatus



AgentStatus defines the observed status of the member agent of the given type.

_Appears in:_
- [MemberClusterStatus](#memberclusterstatus)

| Field | Description |
| --- | --- |
| `type` _[AgentType](#agenttype)_ | Type of the member agent. |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#condition-v1-meta) array_ | Conditions is an array of current observed conditions for the member agent. |
| `lastReceivedHeartbeat` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#time-v1-meta)_ | Last time we received a heartbeat from the member agent. |


#### AgentType

_Underlying type:_ _string_

AgentType defines a type of agent/binary running in a member cluster.

_Appears in:_
- [AgentStatus](#agentstatus)









#### MemberCluster



MemberCluster is a resource created in the hub cluster to represent a member cluster within a fleet.

_Appears in:_
- [MemberClusterList](#memberclusterlist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `cluster.kubernetes-fleet.io/v1beta1`
| `kind` _string_ | `MemberCluster`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[MemberClusterSpec](#memberclusterspec)_ | The desired state of MemberCluster. |
| `status` _[MemberClusterStatus](#memberclusterstatus)_ | The observed status of MemberCluster. |




#### MemberClusterList



MemberClusterList contains a list of MemberCluster.



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `cluster.kubernetes-fleet.io/v1beta1`
| `kind` _string_ | `MemberClusterList`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `items` _[MemberCluster](#membercluster) array_ |  |


#### MemberClusterSpec



MemberClusterSpec defines the desired state of MemberCluster.

_Appears in:_
- [MemberCluster](#membercluster)

| Field | Description |
| --- | --- |
| `identity` _[Subject](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#subject-v1-rbac)_ | The identity used by the member cluster to access the hub cluster. The hub agents deployed on the hub cluster will automatically grant the minimal required permissions to this identity for the member agents deployed on the member cluster to access the hub cluster. |
| `heartbeatPeriodSeconds` _integer_ | How often (in seconds) for the member cluster to send a heartbeat to the hub cluster. Default: 60 seconds. Min: 1 second. Max: 10 minutes. |


#### MemberClusterStatus



MemberClusterStatus defines the observed status of MemberCluster.

_Appears in:_
- [MemberCluster](#membercluster)

| Field | Description |
| --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#condition-v1-meta) array_ | Conditions is an array of current observed conditions for the member cluster. |
| `resourceUsage` _[ResourceUsage](#resourceusage)_ | The current observed resource usage of the member cluster. It is copied from the corresponding InternalMemberCluster object. |
| `agentStatus` _[AgentStatus](#agentstatus) array_ | AgentStatus is an array of current observed status, each corresponding to one member agent running in the member cluster. |


#### ResourceUsage



ResourceUsage contains the observed resource usage of a member cluster.

_Appears in:_
- [MemberClusterStatus](#memberclusterstatus)

| Field | Description |
| --- | --- |
| `capacity` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#resourcelist-v1-core)_ | Capacity represents the total resource capacity of all the nodes on a member cluster. |
| `allocatable` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#resourcelist-v1-core)_ | Allocatable represents the total resources of all the nodes on a member cluster that are available for scheduling. |
| `observationTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#time-v1-meta)_ | When the resource usage is observed. |



## placement.kubernetes-fleet.io/v1beta1



### Resource Types
- [ClusterResourceBinding](#clusterresourcebinding)
- [ClusterResourcePlacement](#clusterresourceplacement)
- [ClusterResourceSnapshot](#clusterresourcesnapshot)
- [ClusterSchedulingPolicySnapshot](#clusterschedulingpolicysnapshot)
- [Work](#work)
- [WorkList](#worklist)



#### Affinity



Affinity is a group of cluster affinity scheduling rules. More to be added.

_Appears in:_
- [PlacementPolicy](#placementpolicy)

| Field | Description |
| --- | --- |
| `clusterAffinity` _[ClusterAffinity](#clusteraffinity)_ | ClusterAffinity contains cluster affinity scheduling rules for the selected resources. |






#### BindingState

_Underlying type:_ _string_

BindingState is the state of the binding.

_Appears in:_
- [ResourceBindingSpec](#resourcebindingspec)





#### ClusterDecision



ClusterDecision represents a decision from a placement An empty ClusterDecision indicates it is not scheduled yet.

_Appears in:_
- [ResourceBindingSpec](#resourcebindingspec)
- [SchedulingPolicySnapshotStatus](#schedulingpolicysnapshotstatus)

| Field | Description |
| --- | --- |
| `clusterName` _string_ | ClusterName is the name of the ManagedCluster. If it is not empty, its value should be unique cross all placement decisions for the Placement. |
| `selected` _boolean_ | Selected indicates if this cluster is selected by the scheduler. |
| `clusterScore` _[ClusterScore](#clusterscore)_ | ClusterScore represents the score of the cluster calculated by the scheduler. |
| `reason` _string_ | Reason represents the reason why the cluster is selected or not. |


#### ClusterResourceBinding



ClusterResourceBinding represents a scheduling decision that binds a group of resources to a cluster. It MUST have a label named `CRPTrackingLabel` that points to the cluster resource policy that creates it.

_Appears in:_
- [ClusterResourceBindingList](#clusterresourcebindinglist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `placement.kubernetes-fleet.io/v1beta1`
| `kind` _string_ | `ClusterResourceBinding`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[ResourceBindingSpec](#resourcebindingspec)_ | The desired state of ClusterResourceBinding. |
| `status` _[ResourceBindingStatus](#resourcebindingstatus)_ | The observed status of ClusterResourceBinding. |




#### ClusterResourcePlacement



ClusterResourcePlacement is used to select cluster scoped resources, including built-in resources and custom resources, and placement them onto selected member clusters in a fleet. 
 If a namespace is selected, ALL the resources under the namespace are placed to the target clusters. Note that you can't select the following resources: - reserved namespaces including: default, kube-* (reserved for Kubernetes system namespaces), fleet-* (reserved for fleet system namespaces). - reserved fleet resource types including: MemberCluster, InternalMemberCluster, ClusterResourcePlacement, ClusterSchedulingPolicySnapshot, ClusterResourceSnapshot, ClusterResourceBinding, etc. 
 `ClusterSchedulingPolicySnapshot` and `ClusterResourceSnapshot` objects are created when there are changes in the system to keep the history of the changes affecting a `ClusterResourcePlacement`.

_Appears in:_
- [ClusterResourcePlacementList](#clusterresourceplacementlist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `placement.kubernetes-fleet.io/v1beta1`
| `kind` _string_ | `ClusterResourcePlacement`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[ClusterResourcePlacementSpec](#clusterresourceplacementspec)_ | The desired state of ClusterResourcePlacement. |
| `status` _[ClusterResourcePlacementStatus](#clusterresourceplacementstatus)_ | The observed status of ClusterResourcePlacement. |






#### ClusterResourcePlacementSpec



ClusterResourcePlacementSpec defines the desired state of ClusterResourcePlacement.

_Appears in:_
- [ClusterResourcePlacement](#clusterresourceplacement)

| Field | Description |
| --- | --- |
| `resourceSelectors` _[ClusterResourceSelector](#clusterresourceselector) array_ | ResourceSelectors is an array of selectors used to select cluster scoped resources. The selectors are `ORed`. You can have 1-100 selectors. |
| `policy` _[PlacementPolicy](#placementpolicy)_ | Policy defines how to select member clusters to place the selected resources. If unspecified, all the joined member clusters are selected. |
| `strategy` _[RolloutStrategy](#rolloutstrategy)_ | The rollout strategy to use to replace existing placement with new ones. |
| `revisionHistoryLimit` _integer_ | The number of old ClusterSchedulingPolicySnapshot or ClusterResourceSnapshot resources to retain to allow rollback. This is a pointer to distinguish between explicit zero and not specified. Defaults to 10. |


#### ClusterResourcePlacementStatus



ClusterResourcePlacementStatus defines the observed state of the ClusterResourcePlacement object.

_Appears in:_
- [ClusterResourcePlacement](#clusterresourceplacement)

| Field | Description |
| --- | --- |
| `selectedResources` _[ResourceIdentifier](#resourceidentifier) array_ | SelectedResources contains a list of resources selected by ResourceSelectors. |
| `observedResourceIndex` _string_ | Resource index logically represents the generation of the selected resources. We take a new snapshot of the selected resources whenever the selection or their content change. Each snapshot has a different resource index. One resource snapshot can contain multiple clusterResourceSnapshots CRs in order to store large amount of resources. To get clusterResourceSnapshot of a given resource index, use the following command: `kubectl get ClusterResourceSnapshot --selector=kubernetes-fleet.io/resource-index=$ObservedResourceIndex ` ObservedResourceIndex is the resource index that the conditions in the ClusterResourcePlacementStatus observe. For example, a condition of `ClusterResourcePlacementSynchronized` type is observing the synchronization status of the resource snapshot with the resource index $ObservedResourceIndex. |
| `placementStatuses` _[ResourcePlacementStatus](#resourceplacementstatus) array_ | PlacementStatuses contains a list of placement status on the clusters that are selected by PlacementPolicy. Each selected cluster according to the latest resource placement is guaranteed to have a corresponding placementStatuses. In the pickN case, there are N placement statuses where N = NumberOfClusters; Or in the pickFixed case, there are N placement statuses where N = ClusterNames. In these cases, some of them may not have assigned clusters when we cannot fill the required number of clusters. TODO, For pickAll type, considering providing unselected clusters info. |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#condition-v1-meta) array_ | Conditions is an array of current observed conditions for ClusterResourcePlacement. |


#### ClusterResourceSelector



ClusterResourceSelector is used to select cluster scoped resources as the target resources to be placed. If a namespace is selected, ALL the resources under the namespace are selected automatically. All the fields are `ANDed`. In other words, a resource must match all the fields to be selected.

_Appears in:_
- [ClusterResourcePlacementSpec](#clusterresourceplacementspec)

| Field | Description |
| --- | --- |
| `group` _string_ | Group name of the cluster-scoped resource. Use an empty string to select resources under the core API group (e.g., namespaces). |
| `version` _string_ | Version of the cluster-scoped resource. |
| `kind` _string_ | Kind of the cluster-scoped resource. Note: When `Kind` is `namespace`, ALL the resources under the selected namespaces are selected. |
| `name` _string_ | Name of the cluster-scoped resource. |
| `labelSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#labelselector-v1-meta)_ | A label query over all the cluster-scoped resources. Resources matching the query are selected. Note that namespace-scoped resources can't be selected even if they match the query. |


#### ClusterResourceSnapshot



ClusterResourceSnapshot is used to store a snapshot of selected resources by a resource placement policy. Its spec is immutable. We may need to produce more than one resourceSnapshot for all the resources a ResourcePlacement selected to get around the 1MB size limit of k8s objects. We assign an ever-increasing index for each such group of resourceSnapshots. The naming convention of a clusterResourceSnapshot is {CRPName}-{resourceIndex}-{subindex} where the name of the first snapshot of a group has no subindex part so its name is {CRPName}-{resourceIndex}-snapshot. resourceIndex will begin with 0. Each snapshot MUST have the following labels: - `CRPTrackingLabel` which points to its owner CRP. - `ResourceIndexLabel` which is the index  of the snapshot group. - `IsLatestSnapshotLabel` which indicates whether the snapshot is the latest one. 
 All the snapshots within the same index group must have the same ResourceIndexLabel. 
 The first snapshot of the index group MUST have the following annotations: - `NumberOfResourceSnapshotsAnnotation` to store the total number of resource snapshots in the index group. - `ResourceGroupHashAnnotation` whose value is the sha-256 hash of all the snapshots belong to the same snapshot index. 
 Each snapshot (excluding the first snapshot) MUST have the following annotations: - `SubindexOfResourceSnapshotAnnotation` to store the subindex of resource snapshot in the group.

_Appears in:_
- [ClusterResourceSnapshotList](#clusterresourcesnapshotlist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `placement.kubernetes-fleet.io/v1beta1`
| `kind` _string_ | `ClusterResourceSnapshot`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[ResourceSnapshotSpec](#resourcesnapshotspec)_ | The desired state of ResourceSnapshot. |
| `status` _[ResourceSnapshotStatus](#resourcesnapshotstatus)_ | The observed status of ResourceSnapshot. |




#### ClusterSchedulingPolicySnapshot



ClusterSchedulingPolicySnapshot is used to store a snapshot of cluster placement policy. Its spec is immutable. The naming convention of a ClusterSchedulingPolicySnapshot is {CRPName}-{PolicySnapshotIndex}. PolicySnapshotIndex will begin with 0. Each snapshot must have the following labels: - `CRPTrackingLabel` which points to its owner CRP. - `PolicyIndexLabel` which is the index of the policy snapshot. - `IsLatestSnapshotLabel` which indicates whether the snapshot is the latest one.

_Appears in:_
- [ClusterSchedulingPolicySnapshotList](#clusterschedulingpolicysnapshotlist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `placement.kubernetes-fleet.io/v1beta1`
| `kind` _string_ | `ClusterSchedulingPolicySnapshot`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[SchedulingPolicySnapshotSpec](#schedulingpolicysnapshotspec)_ | The desired state of SchedulingPolicySnapshot. |
| `status` _[SchedulingPolicySnapshotStatus](#schedulingpolicysnapshotstatus)_ | The observed status of SchedulingPolicySnapshot. |




#### ClusterScore



ClusterScore represents the score of the cluster calculated by the scheduler.

_Appears in:_
- [ClusterDecision](#clusterdecision)

| Field | Description |
| --- | --- |
| `affinityScore` _integer_ | AffinityScore represents the affinity score of the cluster calculated by the last scheduling decision based on the preferred affinity selector. An affinity score may not present if the cluster does not meet the required affinity. |
| `priorityScore` _integer_ | TopologySpreadScore represents the priority score of the cluster calculated by the last scheduling decision based on the topology spread applied to the cluster. A priority score may not present if the cluster does not meet the topology spread. |


#### ClusterSelector





_Appears in:_
- [ClusterAffinity](#clusteraffinity)

| Field | Description |
| --- | --- |
| `clusterSelectorTerms` _[ClusterSelectorTerm](#clusterselectorterm) array_ | ClusterSelectorTerms is a list of cluster selector terms. The terms are `ORed`. |


#### ClusterSelectorTerm



ClusterSelectorTerm contains the requirements to select clusters.

_Appears in:_
- [ClusterSelector](#clusterselector)
- [PreferredClusterSelector](#preferredclusterselector)

| Field | Description |
| --- | --- |
| `labelSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#labelselector-v1-meta)_ | LabelSelector is a label query over all the joined member clusters. Clusters matching the query are selected. |


#### EnvelopeIdentifier



EnvelopeIdentifier identifies the envelope object that contains the selected resource.

_Appears in:_
- [FailedResourcePlacement](#failedresourceplacement)
- [ResourceIdentifier](#resourceidentifier)

| Field | Description |
| --- | --- |
| `name` _string_ | Name of the envelope object. |
| `namespace` _string_ | Namespace is the namespace of the envelope object. Empty if the envelope object is cluster scoped. |
| `type` _[EnvelopeType](#envelopetype)_ | Type of the envelope object. |


#### EnvelopeType

_Underlying type:_ _string_

EnvelopeType defines the type of the envelope object.

_Appears in:_
- [EnvelopeIdentifier](#envelopeidentifier)



#### FailedResourcePlacement



FailedResourcePlacement contains the failure details of a failed resource placement.

_Appears in:_
- [ResourcePlacementStatus](#resourceplacementstatus)

| Field | Description |
| --- | --- |
| `group` _string_ | Group is the group name of the selected resource. |
| `version` _string_ | Version is the version of the selected resource. |
| `kind` _string_ | Kind represents the Kind of the selected resources. |
| `name` _string_ | Name of the target resource. |
| `namespace` _string_ | Namespace is the namespace of the resource. Empty if the resource is cluster scoped. |
| `envelope` _[EnvelopeIdentifier](#envelopeidentifier)_ | Envelope identifies the envelope object that contains this resource. |
| `condition` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#condition-v1-meta)_ | The failed condition status. |


#### Manifest



Manifest represents a resource to be deployed on spoke cluster.

_Appears in:_
- [WorkloadTemplate](#workloadtemplate)



#### ManifestCondition



ManifestCondition represents the conditions of the resources deployed on spoke cluster.

_Appears in:_
- [WorkStatus](#workstatus)

| Field | Description |
| --- | --- |
| `identifier` _[WorkResourceIdentifier](#workresourceidentifier)_ | resourceId represents a identity of a resource linking to manifests in spec. |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#condition-v1-meta) array_ | Conditions represents the conditions of this resource on spoke cluster |


#### PlacementPolicy



PlacementPolicy contains the rules to select target member clusters to place the selected resources. Note that only clusters that are both joined and satisfying the rules will be selected. 
 You can only specify at most one of the two fields: ClusterNames and Affinity. If none is specified, all the joined clusters are selected.

_Appears in:_
- [ClusterResourcePlacementSpec](#clusterresourceplacementspec)
- [SchedulingPolicySnapshotSpec](#schedulingpolicysnapshotspec)

| Field | Description |
| --- | --- |
| `placementType` _[PlacementType](#placementtype)_ | Type of placement. Can be "PickAll", "PickN" or "PickFixed". Default is PickAll. |
| `clusterNames` _string array_ | ClusterNames contains a list of names of MemberCluster to place the selected resources. Only valid if the placement type is "PickFixed" |
| `numberOfClusters` _integer_ | NumberOfClusters of placement. Only valid if the placement type is "PickN". |
| `affinity` _[Affinity](#affinity)_ | Affinity contains cluster affinity scheduling rules. Defines which member clusters to place the selected resources. Only valid if the placement type is "PickAll" or "PickN". |
| `topologySpreadConstraints` _[TopologySpreadConstraint](#topologyspreadconstraint) array_ | TopologySpreadConstraints describes how a group of resources ought to spread across multiple topology domains. Scheduler will schedule resources in a way which abides by the constraints. All topologySpreadConstraints are ANDed. Only valid if the placement type is "PickN". |


#### PlacementType

_Underlying type:_ _string_

PlacementType identifies the type of placement.

_Appears in:_
- [PlacementPolicy](#placementpolicy)



#### PreferredClusterSelector





_Appears in:_
- [ClusterAffinity](#clusteraffinity)

| Field | Description |
| --- | --- |
| `weight` _integer_ | Weight associated with matching the corresponding clusterSelectorTerm, in the range [-100, 100]. |
| `preference` _[ClusterSelectorTerm](#clusterselectorterm)_ | A cluster selector term, associated with the corresponding weight. |




#### ResourceBindingSpec



ResourceBindingSpec defines the desired state of ClusterResourceBinding.

_Appears in:_
- [ClusterResourceBinding](#clusterresourcebinding)

| Field | Description |
| --- | --- |
| `state` _[BindingState](#bindingstate)_ | The desired state of the binding. Possible values: Scheduled, Bound, Unscheduled. |
| `resourceSnapshotName` _string_ | ResourceSnapshotName is the name of the resource snapshot that this resource binding points to. If the resources are divided into multiple snapshots because of the resource size limit, it points to the name of the leading snapshot of the index group. |
| `schedulingPolicySnapshotName` _string_ | SchedulingPolicySnapshotName is the name of the scheduling policy snapshot that this resource binding points to; more specifically, the scheduler creates this bindings in accordance with this scheduling policy snapshot. |
| `targetCluster` _string_ | TargetCluster is the name of the cluster that the scheduler assigns the resources to. |
| `clusterDecision` _[ClusterDecision](#clusterdecision)_ | ClusterDecision explains why the scheduler selected this cluster. |


#### ResourceBindingStatus



ResourceBindingStatus represents the current status of a ClusterResourceBinding.

_Appears in:_
- [ClusterResourceBinding](#clusterresourcebinding)

| Field | Description |
| --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#condition-v1-meta) array_ | Conditions is an array of current observed conditions for ClusterResourceBinding. |


#### ResourceContent



ResourceContent contains the content of a resource

_Appears in:_
- [ResourceSnapshotSpec](#resourcesnapshotspec)



#### ResourceIdentifier



ResourceIdentifier identifies one Kubernetes resource.

_Appears in:_
- [ClusterResourcePlacementStatus](#clusterresourceplacementstatus)
- [FailedResourcePlacement](#failedresourceplacement)

| Field | Description |
| --- | --- |
| `group` _string_ | Group is the group name of the selected resource. |
| `version` _string_ | Version is the version of the selected resource. |
| `kind` _string_ | Kind represents the Kind of the selected resources. |
| `name` _string_ | Name of the target resource. |
| `namespace` _string_ | Namespace is the namespace of the resource. Empty if the resource is cluster scoped. |
| `envelope` _[EnvelopeIdentifier](#envelopeidentifier)_ | Envelope identifies the envelope object that contains this resource. |




#### ResourcePlacementStatus



ResourcePlacementStatus represents the placement status of selected resources for one target cluster.

_Appears in:_
- [ClusterResourcePlacementStatus](#clusterresourceplacementstatus)

| Field | Description |
| --- | --- |
| `clusterName` _string_ | ClusterName is the name of the cluster this resource is assigned to. If it is not empty, its value should be unique cross all placement decisions for the Placement. |
| `failedPlacements` _[FailedResourcePlacement](#failedresourceplacement) array_ | FailedResourcePlacements is a list of all the resources failed to be placed to the given cluster. Note that we only include 100 failed resource placements even if there are more than 100. This field is only meaningful if the `ClusterName` is not empty. |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#condition-v1-meta) array_ | Conditions is an array of current observed conditions for ResourcePlacementStatus. |


#### ResourceSnapshotSpec



ResourceSnapshotSpec	defines the desired state of ResourceSnapshot.

_Appears in:_
- [ClusterResourceSnapshot](#clusterresourcesnapshot)

| Field | Description |
| --- | --- |
| `selectedResources` _[ResourceContent](#resourcecontent) array_ | SelectedResources contains a list of resources selected by ResourceSelectors. |


#### ResourceSnapshotStatus





_Appears in:_
- [ClusterResourceSnapshot](#clusterresourcesnapshot)

| Field | Description |
| --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#condition-v1-meta) array_ | Conditions is an array of current observed conditions for ResourceSnapshot. |


#### RollingUpdateConfig



RollingUpdateConfig contains the config to control the desired behavior of rolling update.

_Appears in:_
- [RolloutStrategy](#rolloutstrategy)

| Field | Description |
| --- | --- |
| `maxUnavailable` _[IntOrString](#intorstring)_ | The maximum number of clusters that can be unavailable during the rolling update comparing to the desired number of clusters. The desired number equals to the `NumberOfClusters` field when the placement type is `PickN`. The desired number equals to the number of clusters scheduler selected when the placement type is `PickAll`. Value can be an absolute number (ex: 5) or a percentage of the desired number of clusters (ex: 10%). Absolute number is calculated from percentage by rounding up. We consider a resource unavailable when we either remove it from a cluster or in-place upgrade the resources content on the same cluster. This can not be 0 if MaxSurge is 0. Defaults to 25%. |
| `maxSurge` _[IntOrString](#intorstring)_ | The maximum number of clusters that can be scheduled above the desired number of clusters. The desired number equals to the `NumberOfClusters` field when the placement type is `PickN`. The desired number equals to the number of clusters scheduler selected when the placement type is `PickAll`. Value can be an absolute number (ex: 5) or a percentage of desire (ex: 10%). Absolute number is calculated from percentage by rounding up. This does not apply to the case that we do in-place upgrade of resources on the same cluster. This can not be 0 if MaxUnavailable is 0. Defaults to 25%. |
| `unavailablePeriodSeconds` _integer_ | UnavailablePeriodSeconds is used to config the time to wait between rolling out phases. A resource placement is considered available after `UnavailablePeriodSeconds` seconds has passed after the resources are applied to the target cluster successfully. Default is 60. |


#### RolloutStrategy



RolloutStrategy describes how to roll out a new change in selected resources to target clusters.

_Appears in:_
- [ClusterResourcePlacementSpec](#clusterresourceplacementspec)

| Field | Description |
| --- | --- |
| `type` _[RolloutStrategyType](#rolloutstrategytype)_ | Type of rollout. The only supported type is "RollingUpdate". Default is "RollingUpdate". |
| `rollingUpdate` _[RollingUpdateConfig](#rollingupdateconfig)_ | Rolling update config params. Present only if RolloutStrategyType = RollingUpdate. |


#### RolloutStrategyType

_Underlying type:_ _string_



_Appears in:_
- [RolloutStrategy](#rolloutstrategy)





#### SchedulingPolicySnapshotSpec



SchedulingPolicySnapshotSpec defines the desired state of SchedulingPolicySnapshot.

_Appears in:_
- [ClusterSchedulingPolicySnapshot](#clusterschedulingpolicysnapshot)

| Field | Description |
| --- | --- |
| `policy` _[PlacementPolicy](#placementpolicy)_ | Policy defines how to select member clusters to place the selected resources. If unspecified, all the joined member clusters are selected. |
| `policyHash` _[byte](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#byte-v1-meta) array_ | PolicyHash is the sha-256 hash value of the Policy field. |


#### SchedulingPolicySnapshotStatus



SchedulingPolicySnapshotStatus defines the observed state of SchedulingPolicySnapshot.

_Appears in:_
- [ClusterSchedulingPolicySnapshot](#clusterschedulingpolicysnapshot)

| Field | Description |
| --- | --- |
| `observedCRPGeneration` _integer_ | ObservedCRPGeneration is the generation of the CRP which the scheduler uses to perform the scheduling cycle and prepare the scheduling status. |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#condition-v1-meta) array_ | Conditions is an array of current observed conditions for SchedulingPolicySnapshot. |
| `targetClusters` _[ClusterDecision](#clusterdecision) array_ | ClusterDecisions contains a list of names of member clusters considered by the scheduler. Note that all the selected clusters must present in the list while not all the member clusters are guaranteed to be listed due to the size limit. We will try to add the clusters that can provide the most insight to the list first. |


#### TopologySpreadConstraint



TopologySpreadConstraint specifies how to spread resources among the given cluster topology.

_Appears in:_
- [PlacementPolicy](#placementpolicy)

| Field | Description |
| --- | --- |
| `maxSkew` _integer_ | MaxSkew describes the degree to which resources may be unevenly distributed. When `whenUnsatisfiable=DoNotSchedule`, it is the maximum permitted difference between the number of resource copies in the target topology and the global minimum. The global minimum is the minimum number of resource copies in a domain. When `whenUnsatisfiable=ScheduleAnyway`, it is used to give higher precedence to topologies that satisfy it. It's an optional field. Default value is 1 and 0 is not allowed. |
| `topologyKey` _string_ | TopologyKey is the key of cluster labels. Clusters that have a label with this key and identical values are considered to be in the same topology. We consider each <key, value> as a "bucket", and try to put balanced number of replicas of the resource into each bucket honor the `MaxSkew` value. It's a required field. |
| `whenUnsatisfiable` _[UnsatisfiableConstraintAction](#unsatisfiableconstraintaction)_ | WhenUnsatisfiable indicates how to deal with the resource if it doesn't satisfy the spread constraint. - DoNotSchedule (default) tells the scheduler not to schedule it. - ScheduleAnyway tells the scheduler to schedule the resource in any cluster, but giving higher precedence to topologies that would help reduce the skew. It's an optional field. |


#### UnsatisfiableConstraintAction

_Underlying type:_ _string_

UnsatisfiableConstraintAction defines the type of actions that can be taken if a constraint is not satisfied.

_Appears in:_
- [TopologySpreadConstraint](#topologyspreadconstraint)



#### Work



Work is the Schema for the works API.

_Appears in:_
- [WorkList](#worklist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `placement.kubernetes-fleet.io/v1beta1`
| `kind` _string_ | `Work`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[WorkSpec](#workspec)_ | spec defines the workload of a work. |
| `status` _[WorkStatus](#workstatus)_ | status defines the status of each applied manifest on the spoke cluster. |


#### WorkList



WorkList contains a list of Work.



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `placement.kubernetes-fleet.io/v1beta1`
| `kind` _string_ | `WorkList`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `items` _[Work](#work) array_ | List of works. |


#### WorkResourceIdentifier



WorkResourceIdentifier provides the identifiers needed to interact with any arbitrary object. Renamed original "ResourceIdentifier" so that it won't conflict with ResourceIdentifier defined in the clusterresourceplacement_types.go.

_Appears in:_
- [AppliedResourceMeta](#appliedresourcemeta)
- [ManifestCondition](#manifestcondition)

| Field | Description |
| --- | --- |
| `ordinal` _integer_ | Ordinal represents an index in manifests list, so the condition can still be linked to a manifest even thougth manifest cannot be parsed successfully. |
| `group` _string_ | Group is the group of the resource. |
| `version` _string_ | Version is the version of the resource. |
| `kind` _string_ | Kind is the kind of the resource. |
| `resource` _string_ | Resource is the resource type of the resource |
| `namespace` _string_ | Namespace is the namespace of the resource, the resource is cluster scoped if the value is empty |
| `name` _string_ | Name is the name of the resource |


#### WorkSpec



WorkSpec defines the desired state of Work.

_Appears in:_
- [Work](#work)

| Field | Description |
| --- | --- |
| `workload` _[WorkloadTemplate](#workloadtemplate)_ | Workload represents the manifest workload to be deployed on spoke cluster |


#### WorkStatus



WorkStatus defines the observed state of Work.

_Appears in:_
- [Work](#work)

| Field | Description |
| --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#condition-v1-meta) array_ | Conditions contains the different condition statuses for this work. Valid condition types are: 1. Applied represents workload in Work is applied successfully on the spoke cluster. 2. Progressing represents workload in Work in the trasitioning from one state to another the on the spoke cluster. 3. Available represents workload in Work exists on the spoke cluster. 4. Degraded represents the current state of workload does not match the desired state for a certain period. |
| `manifestConditions` _[ManifestCondition](#manifestcondition) array_ | ManifestConditions represents the conditions of each resource in work deployed on spoke cluster. |


#### WorkloadTemplate



WorkloadTemplate represents the manifest workload to be deployed on spoke cluster

_Appears in:_
- [WorkSpec](#workspec)

| Field | Description |
| --- | --- |
| `manifests` _[Manifest](#manifest) array_ | Manifests represents a list of kuberenetes resources to be deployed on the spoke cluster. |


