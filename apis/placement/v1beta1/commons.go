/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

const (
	ClusterResourcePlacementKind        = "ClusterResourcePlacement"
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
	CRPTrackingLabel = fleetPrefix + "parentCRP"

	// IsLatestSnapshotLabel tells if the snapshot is the latest one.
	IsLatestSnapshotLabel = fleetPrefix + "isLatestSnapshot"

	// FleetResourceLabelKey is that label that indicates the resource is a fleet resource.
	FleetResourceLabelKey = fleetPrefix + "isFleetResource"

	// FirstWorkNameFmt is the format of the name of the first work.
	FirstWorkNameFmt = "%s-work"

	// WorkNameWithSubindexFmt is the format of the name of a work with subindex.
	WorkNameWithSubindexFmt = "%s-%d"

	// ParentResourceSnapshotIndexLabel is the label applied to work that contains the index of the resource snapshot that generates the work.
	ParentResourceSnapshotIndexLabel = fleetPrefix + "parent-resource-snapshot-index"

	// ParentBindingLabel is the label applied to work that contains the name of the binding that generates the work.
	ParentBindingLabel = fleetPrefix + "parent-resource-binding"

	// CRPGenerationAnnotation is the annotation that indicates the generation of the CRP from
	// which an object is derived or last updated.
	CRPGenerationAnnotation = fleetPrefix + "CRPGeneration"
)
