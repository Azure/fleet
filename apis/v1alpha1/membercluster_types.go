/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +enum
type ClusterState string

const (
	ClusterStateJoin    ClusterState = "Join"
	ClusterStateLeave   ClusterState = "Leave"
	ClusterStateUpgrade ClusterState = "Upgrade"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet},shortName=cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ConditionTypeJoined")].status`,name="Joined",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +kubebuilder:printcolumn:JSONPath=`.metadata.label[fleet.azure.com/clusterHealth]`,name="HealthStatus",type=string

type MemberCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MemberClusterSpec   `json:"spec"`
	Status MemberClusterStatus `json:"status,omitempty"`
}

// MemberClusterSpec defines the desired state of MemberCluster. This is updated through the hub fleet api.
type MemberClusterSpec struct {
	// State indicates the state of the member cluster
	// +kubebuilder:validation:Enum=Join;Leave;Upgrade
	// +required
	State ClusterState `json:"state"`

	// Identity used by the member cluster to contact the hub cluster
	// The hub cluster will create the minimal required permission for this identity
	// +required
	Identity rbacv1.Subject `json:"identity"`

	// HeartbeatPeriodSeconds indicates the lease update time for the member cluster agent.
	// This is also the base of the threshold used by the Hub agent to determine if a member cluster is out of sync.
	// If its value is zero, the member cluster agent will update its lease every 60 seconds by default
	// +optional
	// +kubebuilder:default=60
	HeartbeatPeriodSeconds int32 `json:"leaseDurationSeconds,omitempty"`
}

// MemberClusterStatus defines the observed state of MemberCluster. This is selectively copied from the
// InternalMemberClusterStatus by the hub agent`1
type MemberClusterStatus struct {
	// Conditions field contains the different condition statuses for this member cluster.
	// +required
	Conditions []metav1.Condition `json:"conditions"`

	// Capacity represents the total resource capacity from all nodeStatuses
	// on the member cluster.
	// +required
	Capacity v1.ResourceList `json:"capacity"`

	// Allocatable represents the total allocatable resources on the member cluster.
	// +required
	Allocatable v1.ResourceList `json:"allocatable"`
}

//+kubebuilder:object:root=true

// MemberClusterList contains a list of MemberCluster
type MemberClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MemberCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MemberCluster{}, &MemberClusterList{})
}
