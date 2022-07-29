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
	// WorkControllerManager (core) handles the join/unjoin and work orchestration of the multi-clusters.
	WorkControllerManager ControllerManagerType = "WorkControllerManager"
	// MultiClusterServiceControllerManager (networking) is responsible for exposing multi-cluster services via L4 load
	// balancer.
	MultiClusterServiceControllerManager ControllerManagerType = "MultiClusterServiceControllerManager"
	// ServiceExportImportControllerManager (networking) is responsible for export or import services across
	// multi-clusters.
	ServiceExportImportControllerManager ControllerManagerType = "ServiceExportImportControllerManager"
)

// Heartbeat contains heartbeat information received for the particular controller manager type .
type Heartbeat struct {
	// Type of controller manager type.
	Type ControllerManagerType `json:"type"`
	// Last time we got the heartbeat.
	LastReceivedTime metav1.Time `json:"lastReceivedTimeTime"`
}
