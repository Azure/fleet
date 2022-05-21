/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InternalMemberCluster is used by the hub agent to control the member cluster state.
// Member agent watches this CR and updates its status.

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet},shortName=internalcluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
type InternalMemberCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MemberClusterSpec   `json:"spec"`
	Status MemberClusterStatus `json:"status,omitempty"`
}

// InternalMemberClusterSpec defines the desired state of MemberCluster for the hub agent.
type InternalMemberClusterSpec struct {
	// State indicates the state of the member cluster.

	// +kubebuilder:validation:Enum=Join;Leave
	// +required
	State ClusterState `json:"state"`

	// HeartbeatPeriodSeconds indicates how often (in seconds) for the member cluster to send a heartbeat. Default to 60 seconds. Minimum value is 1.

	// +optional
	// +kubebuilder:default=60
	HeartbeatPeriodSeconds int32 `json:"leaseDurationSeconds,omitempty"`
}

const (

	// ConditionTypeMembershipHeartBeat is used to track the Heartbeat state of the membership.
	// Its conditionStatus can be "True" == Heartbeat is success, "Unknown" == Heartbeat is timeout, "False" == Heartbeat is Failed
	ConditionTypeMembershipHeartBeat string = "HeartbeatReceived"
)

// MemberClusterStatus defines the observed state of MemberCluster.
type InternalMemberClusterStatus struct {
	// Conditions field contains the different condition statuses for this member cluster.
	Conditions []metav1.Condition `json:"conditions"`

	// Capacity represents the total resource capacity from all nodeStatuses on the member cluster.
	Capacity v1.ResourceList `json:"capacity"`

	// Allocatable represents the total allocatable resources on the member cluster.
	Allocatable v1.ResourceList `json:"allocatable"`
}

//+kubebuilder:object:root=true

// InternalMemberClusterList contains a list of InternalMemberCluster.
type InternalMemberClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InternalMemberCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InternalMemberCluster{}, &InternalMemberClusterList{})
}
