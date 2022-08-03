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

// AgentType defines agent/binary running in the member cluster.
type AgentType string

const (
	// MemberAgent (core) handles the join/unjoin and work orchestration of the multi-clusters.
	MemberAgent AgentType = "MemberAgent"
	// MultiClusterServiceAgent (networking) is responsible for exposing multi-cluster services via L4 load
	// balancer.
	MultiClusterServiceAgent AgentType = "MultiClusterServiceAgent"
	// ServiceExportImportAgent (networking) is responsible for export or import services across multi-clusters.
	ServiceExportImportAgent AgentType = "ServiceExportImportAgent"
)

// AgentStatus contains status information received for the particular agent type.
type AgentStatus struct {
	// Type of agent type.
	// +required
	Type AgentType `json:"type"`

	// Conditions field contains the different condition statuses for this member cluster, eg join and health status.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Last time we got the heartbeat.
	// +optional
	LastReceivedHeartbeat metav1.Time `json:"lastReceivedHeartbeat,omitempty"`
}

// AgentConditionType identifies a specific condition on the Agent.
type AgentConditionType string

const (
	// AgentJoined is used to track the join state of the agent.
	// Its conditionStatus can be "True" == Joined, "Unknown" == Joining/Leaving, "False" == Left.
	AgentJoined AgentConditionType = "Joined"
	// AgentHealthy is used to track the Health state of the agent.
	// Its conditionStatus can be "True" == Healthy, "False" == UnHealthy. "Unknown" is unused.
	AgentHealthy AgentConditionType = "Healthy"
)
