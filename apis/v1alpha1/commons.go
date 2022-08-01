/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type ClusterState string

const (
	ClusterStateJoin  ClusterState = "Join"
	ClusterStateLeave ClusterState = "Leave"
)

const (
	MemberClusterKind                = "MemberCluster"
	MemberClusterResource            = "memberclusters"
	InternalMemberClusterKind        = "InternalMemberCluster"
	ClusterResourcePlacementKind     = "ClusterResourcePlacement"
	ClusterResourcePlacementResource = "clusterresourceplacements"
)

type ControllerManagerType string

const (
	// MemberAgent (core) handles the join/unjoin and work orchestration of the multi-clusters.
	MemberAgent ControllerManagerType = "MemberAgent"
	// MultiClusterServiceControllerManager (networking) is responsible for exposing multi-cluster services via L4 load
	// balancer.
	MultiClusterServiceControllerManager ControllerManagerType = "MultiClusterServiceControllerManager"
	// ServiceExportImportControllerManager (networking) is responsible for export or import services across
	// multi-clusters.
	ServiceExportImportControllerManager ControllerManagerType = "ServiceExportImportControllerManager"
)

// ControllerManagerCondition contains different condition status information received for the particular controller
// manager type .
type ControllerManagerCondition struct {
	// Type of controller manager type.
	// +required
	Type ControllerManagerType `json:"type"`

	// Conditions field contains the different condition statuses for this member cluster, eg join and health status.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Last time we got the heartbeat.
	// +optional
	LastReceivedHeartbeat metav1.Time `json:"lastReceivedHeartbeat,omitempty"`
}
