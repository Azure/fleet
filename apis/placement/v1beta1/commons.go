/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

const (
	ClusterResourcePlacementKind        = "ClusterResourcePlacement"
	ClusterResourcePlacementResource    = "clusterresourceplacements"
	ClusterResourceBindingKind          = "ClusterResourceBinding"
	ClusterResourceSnapshotKind         = "ClusterResourceSnapshot"
	ClusterSchedulingPolicySnapshotKind = "ClusterSchedulingPolicySnapshot"
	WorkKind                            = "Work"
	AppliedWorkKind                     = "AppliedWork"
)

const (
	// Unprefixed labels/annotations are reserved for end-users
	// we will add a kubernetes-fleet.io to designate these labels/annotations as official fleet labels/annotations.
	// See https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#label-selector-and-annotation-conventions
	fleetPrefix = "kubernetes-fleet.io/"

	// MemberClusterFinalizer is used to make sure that we handle gc of all the member cluster resources on the hub cluster.
	MemberClusterFinalizer = fleetPrefix + "membercluster-finalizer"

	// WorkFinalizer is used by the work generator to make sure that the binding is not deleted until the work objects
	// it generates are all deleted, or used by the work controller to make sure the work has been deleted in the member
	// cluster.
	WorkFinalizer = fleetPrefix + "work-cleanup"

	// CRPTrackingLabel is the label that points to the cluster resource policy that creates a resource binding.
	CRPTrackingLabel = fleetPrefix + "parent-CRP"

	// IsLatestSnapshotLabel tells if the snapshot is the latest one.
	IsLatestSnapshotLabel = fleetPrefix + "is-latest-snapshot"

	// FleetResourceLabelKey is that label that indicates the resource is a fleet resource.
	FleetResourceLabelKey = fleetPrefix + "is-fleet-resource"

	// FirstWorkNameFmt is the format of the name of the work generated with first resource snapshot .
	// The name of the first work is {crpName}-work.
	FirstWorkNameFmt = "%s-work"

	// WorkNameWithSubindexFmt is the format of the name of a work generated with resource snapshot with subindex.
	// The name of the first work is {crpName}-{subindex}.
	WorkNameWithSubindexFmt = "%s-%d"

	// WorkNameWithConfigEnvelopeFmt is the format of the name of a work generated with config envelop.
	// The format is {workPrefix}-configMap-uuid
	WorkNameWithConfigEnvelopeFmt = "%s-configmap-%s"

	// ParentResourceSnapshotIndexLabel is the label applied to work that contains the index of the resource snapshot that generates the work.
	ParentResourceSnapshotIndexLabel = fleetPrefix + "parent-resource-snapshot-index"

	// ParentBindingLabel is the label applied to work that contains the name of the binding that generates the work.
	ParentBindingLabel = fleetPrefix + "parent-resource-binding"

	// CRPGenerationAnnotation is the annotation that indicates the generation of the CRP from
	// which an object is derived or last updated.
	CRPGenerationAnnotation = fleetPrefix + "CRP-generation"

	// EnvelopeConfigMapAnnotation is the annotation that indicates the configmap is an envelope configmap that contains resources
	// we need to apply to the member cluster instead of the configMap itself.
	EnvelopeConfigMapAnnotation = fleetPrefix + "envelope-configmap"

	// EnvelopeTypeLabel is the label that marks the work object as generated from an envelope object.
	// The value of the annotation is the type of the envelope object.
	EnvelopeTypeLabel = fleetPrefix + "envelope-work"

	// EnvelopeNamespaceLabel is the label that contains the namespace of the envelope object that the work is generated from.
	EnvelopeNamespaceLabel = fleetPrefix + "envelope-namespace"

	// EnvelopeNameLabel is the label that contains the name of the envelope object that the work is generated from.
	EnvelopeNameLabel = fleetPrefix + "envelope-name"

	// PreviousBindingStateAnnotation is the annotation that records the previous state of a binding.
	// This is used to remember if an "unscheduled" binding was moved from a "bound" state or a "scheduled" state.
	PreviousBindingStateAnnotation = fleetPrefix + "previous-binding-state"
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
