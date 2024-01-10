/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

	// ResourceSnapshotTrackingLabel is the label that points to the cluster resource snapshot that this work is generated from.
	ResourceSnapshotTrackingLabel = fleetPrefix + "parent-resource-snapshot"

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

// ResourceSelector is used to select namespace scoped resources as the target resources to be placed.
// All the fields are `ANDed`. In other words, a resource must match all the fields to be selected.
type ResourceSelector struct {
	// Group name of the namespace-scoped resource.
	// Use an empty string to select resources under the core API group (e.g., services).
	// +required
	Group string `json:"group"`

	// Version of the namespace-scoped resource.
	// +required
	Version string `json:"version"`

	// Kind of the namespace-scoped resource.
	// +required
	Kind string `json:"kind"`

	// Namespace of the namespace-scoped resource.
	// +optional
	// Default is empty, which means inherit from the parent object scope.
	Namespace string `json:"namespace,omitempty"`

	// You can only specify at most one of the following two fields: Name and LabelSelector.
	// If none is specified, all the namespace-scoped resources with the given group, version, kind and namespace are selected.

	// Name of the namespace-scoped resource.
	// +optional
	Name string `json:"name,omitempty"`

	// A label query over all the namespace-scoped resources. Resources matching the query are selected.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// OverrideIdentifier defines the identity of an override.
type OverrideIdentifier struct {
	// Name is the name of the override.
	// +required
	Name string `json:"name"`

	// Namespace of the override.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Type of override. Can be "ClusterResourceOverrideType", or "ResourceOverrideType". Default is ClusterResourceOverrideType.
	// +kubebuilder:validation:Enum=ClusterResourceOverrideType;ResourceOverrideType
	// +kubebuilder:default=ClusterResourceOverrideType
	// +optional
	Type OverrideType `json:"type,omitempty"`
}

// OverrideType identifies the type of override.
// +enum
type OverrideType string

const (
	// ClusterResourceOverrideType is ClusterResourceOverride type.
	ClusterResourceOverrideType OverrideType = "ClusterResourceOverrideType"

	// ResourceOverrideType is ResourceOverride type.
	ResourceOverrideType OverrideType = "ResourceOverrideType"
)
