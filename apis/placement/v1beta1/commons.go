/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

const (
	// ClusterResourcePlacementKind represents the kind of ClusterResourcePlacement.
	ClusterResourcePlacementKind = "ClusterResourcePlacement"
	// ClusterResourcePlacementResource represents the resource name for ClusterResourcePlacement.
	ClusterResourcePlacementResource = "clusterresourceplacements"
	// ClusterResourceBindingKind represents the kind of ClusterResourceBinding.
	ClusterResourceBindingKind = "ClusterResourceBinding"
	// ClusterResourceSnapshotKind represents the kind of ClusterResourceSnapshot.
	ClusterResourceSnapshotKind = "ClusterResourceSnapshot"
	// ClusterSchedulingPolicySnapshotKind represents the kind of ClusterSchedulingPolicySnapshot.
	ClusterSchedulingPolicySnapshotKind = "ClusterSchedulingPolicySnapshot"
	// WorkKind represents the kind of Work.
	WorkKind = "Work"
	// AppliedWorkKind represents the kind of AppliedWork.
	AppliedWorkKind = "AppliedWork"
	// ClusterStagedUpdateRunKind is the kind of the ClusterStagedUpdateRun.
	ClusterStagedUpdateRunKind = "ClusterStagedUpdateRun"
	// ClusterStagedUpdateStrategyKind is the kind of the ClusterStagedUpdateStrategy.
	ClusterStagedUpdateStrategyKind = "ClusterStagedUpdateStrategy"
	// ClusterApprovalRequestKind is the kind of the ClusterApprovalRequest.
	ClusterApprovalRequestKind = "ClusterApprovalRequest"
	// ClusterResourcePlacementEvictionKind is the kind of the ClusterResourcePlacementEviction.
	ClusterResourcePlacementEvictionKind = "ClusterResourcePlacementEviction"
	// ClusterResourcePlacementDisruptionBudgetKind is the kind of the ClusterResourcePlacementDisruptionBudget.
	ClusterResourcePlacementDisruptionBudgetKind = "ClusterResourcePlacementDisruptionBudget"
)

const (
	// fleetPrefix is the prefix used for official fleet labels/annotations.
	// Unprefixed labels/annotations are reserved for end-users
	// See https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#label-selector-and-annotation-conventions
	fleetPrefix = "kubernetes-fleet.io/"

	// MemberClusterFinalizer is used to make sure that we handle gc of all the member cluster resources on the hub cluster.
	MemberClusterFinalizer = fleetPrefix + "membercluster-finalizer"

	// WorkFinalizer is used by the work generator to make sure that the binding is not deleted until the work objects
	// it generates are all deleted, or used by the work controller to make sure the work has been deleted in the member
	// cluster.
	WorkFinalizer = fleetPrefix + "work-cleanup"

	// CRPTrackingLabel points to the cluster resource placement that creates this resource binding.
	CRPTrackingLabel = fleetPrefix + "parent-CRP"

	// IsLatestSnapshotLabel indicates if the snapshot is the latest one.
	IsLatestSnapshotLabel = fleetPrefix + "is-latest-snapshot"

	// FleetResourceLabelKey indicates that the resource is a fleet resource.
	FleetResourceLabelKey = fleetPrefix + "is-fleet-resource"

	// FirstWorkNameFmt is the format of the name of the work generated with the first resource snapshot.
	// The name of the first work is {crpName}-work.
	FirstWorkNameFmt = "%s-work"

	// WorkNameWithSubindexFmt is the format of the name of a work generated with a resource snapshot with a subindex.
	// The name of the first work is {crpName}-{subindex}.
	WorkNameWithSubindexFmt = "%s-%d"

	// WorkNameWithConfigEnvelopeFmt is the format of the name of a work generated with a config envelope.
	// The format is {workPrefix}-configMap-uuid.
	WorkNameWithConfigEnvelopeFmt = "%s-configmap-%s"

	// ParentClusterResourceOverrideSnapshotHashAnnotation is the annotation to work that contains the hash of the parent cluster resource override snapshot list.
	ParentClusterResourceOverrideSnapshotHashAnnotation = fleetPrefix + "parent-cluster-resource-override-snapshot-hash"

	// ParentResourceOverrideSnapshotHashAnnotation is the annotation to work that contains the hash of the parent resource override snapshot list.
	ParentResourceOverrideSnapshotHashAnnotation = fleetPrefix + "parent-resource-override-snapshot-hash"

	// ParentResourceSnapshotNameAnnotation is the annotation applied to work that contains the name of the master resource snapshot that generates the work.
	ParentResourceSnapshotNameAnnotation = fleetPrefix + "parent-resource-snapshot-name"

	// ParentResourceSnapshotIndexLabel is the label applied to work that contains the index of the resource snapshot that generates the work.
	ParentResourceSnapshotIndexLabel = fleetPrefix + "parent-resource-snapshot-index"

	// ParentBindingLabel is the label applied to work that contains the name of the binding that generates the work.
	ParentBindingLabel = fleetPrefix + "parent-resource-binding"

	// CRPGenerationAnnotation indicates the generation of the CRP from which an object is derived or last updated.
	CRPGenerationAnnotation = fleetPrefix + "CRP-generation"

	// EnvelopeConfigMapAnnotation indicates the configmap is an envelope configmap containing resources we need to apply to the member cluster instead of the configMap itself.
	EnvelopeConfigMapAnnotation = fleetPrefix + "envelope-configmap"

	// EnvelopeTypeLabel marks the work object as generated from an envelope object.
	// The value of the annotation is the type of the envelope object.
	EnvelopeTypeLabel = fleetPrefix + "envelope-work"

	// EnvelopeNamespaceLabel contains the namespace of the envelope object that the work is generated from.
	EnvelopeNamespaceLabel = fleetPrefix + "envelope-namespace"

	// EnvelopeNameLabel contains the name of the envelope object that the work is generated from.
	EnvelopeNameLabel = fleetPrefix + "envelope-name"

	// PreviousBindingStateAnnotation records the previous state of a binding.
	// This is used to remember if an "unscheduled" binding was moved from a "bound" state or a "scheduled" state.
	PreviousBindingStateAnnotation = fleetPrefix + "previous-binding-state"

	// ClusterStagedUpdateRunFinalizer is used by the ClusterStagedUpdateRun controller to make sure that the ClusterStagedUpdateRun
	// object is not deleted until all its dependent resources are deleted.
	ClusterStagedUpdateRunFinalizer = fleetPrefix + "stagedupdaterun-finalizer"

	// TargetUpdateRunLabel indicates the target update run on a staged run related object.
	TargetUpdateRunLabel = fleetPrefix + "targetupdaterun"

	// UpdateRunDeleteStageName is the name of delete stage in the staged update run.
	UpdateRunDeleteStageName = fleetPrefix + "deleteStage"

	// IsLatestUpdateRunApprovalLabel indicates if the approval is the latest approval on a staged run.
	IsLatestUpdateRunApprovalLabel = fleetPrefix + "isLatestUpdateRunApproval"

	// TargetUpdatingStageNameLabel indicates the updating stage name on a staged run related object.
	TargetUpdatingStageNameLabel = fleetPrefix + "targetUpdatingStage"

	// ApprovalTaskNameFmt is the format of the approval task name.
	ApprovalTaskNameFmt = "%s-%s"
)

// NamespacedName comprises a resource name, with a mandatory namespace.
type NamespacedName struct {
	// Name is the name of the namespaced scope resource.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is namespace of the namespaced scope resource.
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}
