/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
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

	Spec   InternalMemberClusterSpec   `json:"spec"`
	Status InternalMemberClusterStatus `json:"status,omitempty"`
}

// InternalMemberClusterSpec defines the desired state of InternalMemberCluster for the hub agent.
type InternalMemberClusterSpec struct {
	// State indicates the state of the member cluster.

	// +kubebuilder:validation:Required,Enum=Join;Leave
	State ClusterState `json:"state"`

	// HeartbeatPeriodSeconds indicates how often (in seconds) for the member cluster to send a heartbeat. Default to 60 seconds. Minimum value is 1.

	// +optional
	// +kubebuilder:default=60
	HeartbeatPeriodSeconds int32 `json:"leaseDurationSeconds,omitempty"`
}

// InternalMemberClusterConditionType identifies a specific condition on a InternalMemberCluster.
type InternalMemberClusterConditionType string

const (
	// ConditionTypeInternalMemberClusterJoin is used to track the join state of the InternalMemberCluster.
	// its conditionStatus can be "True" == Joined, "Unknown" == Joining/Leaving, "False" == Left
	ConditionTypeInternalMemberClusterJoin string = "Joined"

	// ConditionTypeInternalMemberClusterHeartbeat is used to track the Heartbeat state of the InternalMemberCluster.
	// Its conditionStatus can be "True" == Heartbeat is received, or "Unknown" == Heartbeat is not received yet. "False" is unused.
	ConditionTypeInternalMemberClusterHeartbeat string = "HeartbeatReceived"

	// ConditionTypeInternalMemberClusterHealth is used to track the Health state of the InternalMemberCluster.
	// its conditionStatus can be "True" == Healthy, "False" == UnHealthy. "Unknown" is unused.
	ConditionTypeInternalMemberClusterHealth string = "Healthy"
)

const (
	// MCSControllerJoin is used to track the MCS (Multi-Cluster Service) controller join state of the InternalMemberCluster.
	// Its conditionStatus can be "True" == Joined, "Unknown" == Joining/Leaving, "False" == Left.
	// When the condition becomes the false, the MCS controller could be safely uninstalled.
	MCSControllerJoin InternalMemberClusterConditionType = "MCSControllerJoined"

	// MCSControllerHeartbeat is used to track the MCS controller Heartbeat state of the InternalMemberCluster.
	// Its conditionStatus can be "True" == Heartbeat is received, or "Unknown" == Heartbeat is not received yet. "False" is unused.
	MCSControllerHeartbeat InternalMemberClusterConditionType = "MCSControllerHeartbeatReceived"

	// NetworkingControllerJoin is used to track the networking controller (excluding MCS controller) join state of the InternalMemberCluster.
	// Its conditionStatus can be "True" == Joined, "Unknown" == Joining/Leaving, "False" == Left.
	// When the condition becomes the false, the networking controller could be safely uninstalled.
	// Note, for now, networking controller (excluding MCS controller) does not need to cleanup the resource before leaving.
	// It's added to keep consistent with other controllers.
	NetworkingControllerJoin InternalMemberClusterConditionType = "NetworkingControllerJoined"

	// NetworkingControllerHeartbeat is used to track the networking controller (excluding MCS controller) Heartbeat
	// state of the InternalMemberCluster.
	// Its conditionStatus can be "True" == Heartbeat is received, or "Unknown" == Heartbeat is not received yet. "False" is unused.
	NetworkingControllerHeartbeat InternalMemberClusterConditionType = "NetworkingControllerHeartbeatReceived"
)

// InternalMemberClusterStatus defines the observed state of InternalMemberCluster.
type InternalMemberClusterStatus struct {
	// Conditions field contains the different condition statuses for this member cluster.

	// +required
	Conditions []metav1.Condition `json:"conditions"`

	// Capacity represents the total resource capacity from all nodeStatues on the member cluster.

	// +required
	Capacity v1.ResourceList `json:"capacity"`

	// Allocatable represents the total allocatable resources on the member cluster.

	// +required
	Allocatable v1.ResourceList `json:"allocatable"`
}

//+kubebuilder:object:root=true

// InternalMemberClusterList contains a list of InternalMemberCluster.
type InternalMemberClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InternalMemberCluster `json:"items"`
}

func (m *InternalMemberCluster) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

func (m *InternalMemberCluster) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

func init() {
	SchemeBuilder.Register(&InternalMemberCluster{}, &InternalMemberClusterList{})
}
