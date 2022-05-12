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

// MemberCluster is a resource created in the hub cluster to represent a member cluster within a fleet.

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

type ClusterState string

// MemberClusterSpec defines the desired state of MemberCluster.
type MemberClusterSpec struct {
	// State indicates the desired state of the member cluster.

	// +kubebuilder:validation:Enum=Join;Leave;Upgrade
	// +required
	State ClusterState `json:"state"`

	// Identity used by the member cluster to contact the hub cluster.
	// The hub cluster will create the minimal required permission for this identity.
	// +required
	Identity rbacv1.Subject `json:"identity"`

	// HeartbeatPeriodSeconds indicates how often (in seconds) for the member cluster to send a heartbeat. Default to 60 seconds. Minimum value is 1.

	// +optional
	// +kubebuilder:default=60
	HeartbeatPeriodSeconds int32 `json:"leaseDurationSeconds,omitempty"`
}

// MemberClusterStatus defines the observed state of MemberCluster.
type MemberClusterStatus struct {
	// Conditions is an array of current observed conditions for this member cluster.

	// +required
	Conditions []metav1.Condition `json:"conditions"`

	// Capacity represents the total resources of all the nodes within the member cluster.

	// +required
	Capacity v1.ResourceList `json:"capacity"`

	// Allocatable represents the total resources of all the nodes within the member cluster that are available for scheduling.

	// +required
	Allocatable v1.ResourceList `json:"allocatable"`
}

const (
	// ConditionTypeMemberClusterJoin is used to track the join state of the memberCluster.
	// its conditionStatus can be "True" == Joined, "Unknown" == Joining/Leaving, "False" == Left
	ConditionTypeMemberClusterJoin string = "Joined"

	// ConditionTypeMemberClusterHealthy is used to track the Health state of the MemberCluster.
	// its conditionStatus can be "True" == Healthy, "Unknown" == Health degraded, "False" == UnHealthy
	ConditionTypeMemberClusterHealth string = "Healthy"
)

//+kubebuilder:object:root=true

// MemberClusterList contains a list of MemberCluster.
type MemberClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MemberCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MemberCluster{}, &MemberClusterList{})
}
