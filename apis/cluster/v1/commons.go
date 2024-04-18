/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterState string

const (
	ClusterStateJoin  ClusterState = "Join"
	ClusterStateLeave ClusterState = "Leave"
)

// ResourceUsage contains the observed resource usage of a member cluster.
type ResourceUsage struct {
	// Capacity represents the total resource capacity of all the nodes on a member cluster.
	//
	// A node's total capacity is the amount of resource installed on the node.
	// +optional
	Capacity corev1.ResourceList `json:"capacity,omitempty"`

	// Allocatable represents the total allocatable resources of all the nodes on a member cluster.
	//
	// A node's allocatable capacity is the amount of resource that can actually be used
	// for user workloads, i.e.,
	// allocatable capacity = total capacity - capacities reserved for the OS, kubelet, etc.
	//
	// For more information, see
	// https://kubernetes.io/docs/tasks/administer-cluster/reserve-compute-resources/.
	// +optional
	Allocatable corev1.ResourceList `json:"allocatable,omitempty"`

	// Available represents the total available resources of all the nodes on a member cluster.
	//
	// A node's available capacity is the amount of resource that has not been used yet, i.e.,
	// available capacity = allocatable capacity - capacity that has been requested by workloads.
	//
	// This field is beta-level; it is for the property-based scheduling feature and is only
	// populated when a property provider is enabled in the deployment.
	// +optional
	Available corev1.ResourceList `json:"available,omitempty"`

	// When the resource usage is observed.
	// +optional
	ObservationTime metav1.Time `json:"observationTime,omitempty"`
}

// AgentType defines a type of agent/binary running in a member cluster.
type AgentType string

const (
	// MemberAgent (core) handles member cluster joining/leaving as well as k8s object placement from hub to member clusters.
	MemberAgent AgentType = "MemberAgent"
	// MultiClusterServiceAgent (networking) is responsible for exposing multi-cluster services via L4 load
	// balancer.
	MultiClusterServiceAgent AgentType = "MultiClusterServiceAgent"
	// ServiceExportImportAgent (networking) is responsible for export or import services across multi-clusters.
	ServiceExportImportAgent AgentType = "ServiceExportImportAgent"
)

// AgentStatus defines the observed status of the member agent of the given type.
type AgentStatus struct {
	// Type of the member agent.
	// +required
	Type AgentType `json:"type"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for the member agent.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Last time we received a heartbeat from the member agent.
	// +optional
	LastReceivedHeartbeat metav1.Time `json:"lastReceivedHeartbeat,omitempty"`
}

// AgentConditionType identifies a specific condition on the Agent.
type AgentConditionType string

const (
	// AgentJoined indicates the join condition of the given member agent.
	// Its condition status can be one of the following:
	// - "True" means the member agent has joined.
	// - "False" means the member agent has left.
	// - "Unknown" means the member agent is joining or leaving or in an unknown status.
	AgentJoined AgentConditionType = "Joined"
	// AgentHealthy indicates the health condition of the given member agent.
	// Its condition status can be one of the following:
	// - "True" means the member agent is healthy.
	// - "False" means the member agent is unhealthy.
	// - "Unknown" means the member agent has an unknown health status.
	AgentHealthy AgentConditionType = "Healthy"
)

const (
	MemberClusterKind                = "MemberCluster"
	MemberClusterResource            = "memberclusters"
	InternalMemberClusterKind        = "InternalMemberCluster"
	ClusterResourcePlacementResource = "clusterresourceplacements"
)

// A ConditionedWithType may have conditions set or retrieved based on agent type. Conditions typically
// indicate the status of both a resource and its reconciliation process.
// +kubebuilder:object:generate=false
type ConditionedWithType interface {
	SetConditionsWithType(AgentType, ...metav1.Condition)
	GetConditionWithType(AgentType, string) *metav1.Condition
}

// A ConditionedAgentObj is for kubernetes resources where multiple agents can set and update conditions within AgentStatus.
// +kubebuilder:object:generate=false
type ConditionedAgentObj interface {
	client.Object
	ConditionedWithType
}
