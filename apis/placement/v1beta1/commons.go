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
	// ResourcePlacementKind represents the kind of ResourcePlacement.
	ResourcePlacementKind = "ResourcePlacement"
	// ResourcePlacementResource represents the resource name for ResourcePlacement.
	ResourcePlacementResource = "resourceplacements"
	// ClusterResourceBindingKind represents the kind of ClusterResourceBinding.
	ClusterResourceBindingKind = "ClusterResourceBinding"
	// ResourceBindingKind represents the kind of ResourceBinding.
	ResourceBindingKind = "ResourceBinding"
	// ClusterResourceSnapshotKind represents the kind of ClusterResourceSnapshot.
	ClusterResourceSnapshotKind = "ClusterResourceSnapshot"
	// ResourceSnapshotKind represents the kind of ResourceSnapshot.
	ResourceSnapshotKind = "ResourceSnapshot"
	// ClusterSchedulingPolicySnapshotKind represents the kind of ClusterSchedulingPolicySnapshot.
	ClusterSchedulingPolicySnapshotKind = "ClusterSchedulingPolicySnapshot"
	// SchedulingPolicySnapshotKind represents the kind of SchedulingPolicySnapshot.
	SchedulingPolicySnapshotKind = "SchedulingPolicySnapshot"
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
	// ResourceEnvelopeKind is the kind of the ResourceEnvelope.
	ResourceEnvelopeKind = "ResourceEnvelope"
	// ClusterResourceEnvelopeKind is the kind of the ClusterResourceEnvelope.
	ClusterResourceEnvelopeKind = "ClusterResourceEnvelope"
)

const (
	// FleetPrefix is the prefix used for official fleet labels/annotations.
	// Unprefixed labels/annotations are reserved for end-users
	// See https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#label-selector-and-annotation-conventions
	FleetPrefix = "kubernetes-fleet.io/"

	// MemberClusterFinalizer is used to make sure that we handle gc of all the member cluster resources on the hub cluster.
	MemberClusterFinalizer = FleetPrefix + "membercluster-finalizer"

	// WorkFinalizer is used by the work generator to make sure that the binding is not deleted until the work objects
	// it generates are all deleted, or used by the work controller to make sure the work has been deleted in the member
	// cluster.
	WorkFinalizer = FleetPrefix + "work-cleanup"

	// ClusterResourcePlacementStatusCleanupFinalizer is a finalizer added by the controller to all ClusterResourcePlacementStatus objects, to make sure
	// that the controller can react to ClusterResourcePlacementStatus deletions if necessary.
	ClusterResourcePlacementStatusCleanupFinalizer = FleetPrefix + "cluster-resource-placement-status-cleanup"

	// PlacementTrackingLabel points to the placement that creates this resource binding.
	// TODO: migrate the label content to "parent-placement" to work with both the PR and CRP
	PlacementTrackingLabel = FleetPrefix + "parent-CRP"

	// IsLatestSnapshotLabel indicates if the snapshot is the latest one.
	IsLatestSnapshotLabel = FleetPrefix + "is-latest-snapshot"

	// FleetResourceLabelKey indicates that the resource is a fleet resource.
	FleetResourceLabelKey = FleetPrefix + "is-fleet-resource"

	// FirstWorkNameFmt is the format of the name of the work generated with the first resource snapshot.
	// The name of the first work is {crpName}-work.
	FirstWorkNameFmt = "%s-work"

	// WorkNameWithSubindexFmt is the format of the name of a work generated with a resource snapshot with a subindex.
	// The name of the first work is {crpName}-{subindex}.
	WorkNameWithSubindexFmt = "%s-%d"

	// WorkNameBaseFmt is the format of the base name of the work. It's formatted as {namespace}.{placementName}.
	WorkNameBaseFmt = "%s.%s"

	// WorkNameWithConfigEnvelopeFmt is the format of the name of a work generated with a config envelope.
	// The format is {workPrefix}-configMap-uuid.
	WorkNameWithConfigEnvelopeFmt = "%s-configmap-%s"

	// WorkNameWithEnvelopeCRFmt is the format of the name of a work generated with an envelope CR.
	// The format is [WORK-PREFIX]-envelope-[UUID].
	WorkNameWithEnvelopeCRFmt = "%s-envelope-%s"

	// ParentClusterResourceOverrideSnapshotHashAnnotation is the annotation to work that contains the hash of the parent cluster resource override snapshot list.
	ParentClusterResourceOverrideSnapshotHashAnnotation = FleetPrefix + "parent-cluster-resource-override-snapshot-hash"

	// ParentResourceOverrideSnapshotHashAnnotation is the annotation to work that contains the hash of the parent resource override snapshot list.
	ParentResourceOverrideSnapshotHashAnnotation = FleetPrefix + "parent-resource-override-snapshot-hash"

	// ParentResourceSnapshotNameAnnotation is the annotation applied to work that contains the name of the master resource snapshot that generates the work.
	ParentResourceSnapshotNameAnnotation = FleetPrefix + "parent-resource-snapshot-name"

	// ParentResourceSnapshotIndexLabel is the label applied to work that contains the index of the resource snapshot that generates the work.
	ParentResourceSnapshotIndexLabel = FleetPrefix + "parent-resource-snapshot-index"

	// ParentBindingLabel is the label applied to work that contains the name of the binding that generates the work.
	ParentBindingLabel = FleetPrefix + "parent-resource-binding"

	// ParentNamespaceLabel is the label applied to work that contains the namespace of the binding that generates the work.
	ParentNamespaceLabel = FleetPrefix + "parent-placement-namespace"

	// CRPGenerationAnnotation indicates the generation of the placement from which an object is derived or last updated.
	// TODO: rename this variable
	CRPGenerationAnnotation = FleetPrefix + "CRP-generation"

	// EnvelopeConfigMapAnnotation indicates the configmap is an envelope configmap containing resources we need to apply to the member cluster instead of the configMap itself.
	EnvelopeConfigMapAnnotation = FleetPrefix + "envelope-configmap"

	// EnvelopeTypeLabel marks the work object as generated from an envelope object.
	// The value of the annotation is the type of the envelope object.
	EnvelopeTypeLabel = FleetPrefix + "envelope-work"

	// EnvelopeNamespaceLabel contains the namespace of the envelope object that the work is generated from.
	EnvelopeNamespaceLabel = FleetPrefix + "envelope-namespace"

	// EnvelopeNameLabel contains the name of the envelope object that the work is generated from.
	EnvelopeNameLabel = FleetPrefix + "envelope-name"

	// PreviousBindingStateAnnotation records the previous state of a binding.
	// This is used to remember if an "unscheduled" binding was moved from a "bound" state or a "scheduled" state.
	PreviousBindingStateAnnotation = FleetPrefix + "previous-binding-state"

	// ClusterStagedUpdateRunFinalizer is used by the ClusterStagedUpdateRun controller to make sure that the ClusterStagedUpdateRun
	// object is not deleted until all its dependent resources are deleted.
	ClusterStagedUpdateRunFinalizer = FleetPrefix + "stagedupdaterun-finalizer"

	// TargetUpdateRunLabel indicates the target update run on a staged run related object.
	TargetUpdateRunLabel = FleetPrefix + "targetupdaterun"

	// UpdateRunDeleteStageName is the name of delete stage in the staged update run.
	UpdateRunDeleteStageName = FleetPrefix + "deleteStage"

	// IsLatestUpdateRunApprovalLabel indicates if the approval is the latest approval on a staged run.
	IsLatestUpdateRunApprovalLabel = FleetPrefix + "isLatestUpdateRunApproval"

	// TargetUpdatingStageNameLabel indicates the updating stage name on a staged run related object.
	TargetUpdatingStageNameLabel = FleetPrefix + "targetUpdatingStage"

	// ApprovalTaskNameFmt is the format of the approval task name.
	ApprovalTaskNameFmt = "%s-%s"
)

var (
	// ClusterResourceOverrideKind is the kind of the ClusterResourceOverride.
	ClusterResourceOverrideKind = "ClusterResourceOverride"

	// ClusterResourceOverrideSnapshotKind is the kind of the ClusterResourceOverrideSnapshot.
	ClusterResourceOverrideSnapshotKind = "ClusterResourceOverrideSnapshot"

	// ResourceOverrideKind is the kind of the ResourceOverride.
	ResourceOverrideKind = "ResourceOverride"

	// ResourceOverrideSnapshotKind is the kind of the ResourceOverrideSnapshot.
	ResourceOverrideSnapshotKind = "ResourceOverrideSnapshot"

	// OverrideClusterNameVariable is the reserved variable in the override value that will be replaced by the actual cluster name.
	OverrideClusterNameVariable = "${MEMBER-CLUSTER-NAME}"

	// OverrideClusterLabelKeyVariablePrefix is a reserved variable in the override expression.
	// We use this variable to find the associated key following the prefix.
	// The key name ends with a "}" character (but not include it).
	// The key name must be a valid Kubernetes label name and case-sensitive.
	// The content of the string containing this variable will be replaced by the actual label value on the member cluster.
	// For example, if the string is "${MEMBER-CLUSTER-LABEL-KEY-kube-fleet.io/region}" then the key name is "kube-fleet.io/region".
	// If there is a label "kube-fleet.io/region": "us-west-1" on the member cluster, this string will be replaced by "us-west-1".
	OverrideClusterLabelKeyVariablePrefix = "${MEMBER-CLUSTER-LABEL-KEY-"
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
