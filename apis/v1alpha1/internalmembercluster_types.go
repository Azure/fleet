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

// InternalMemberClusterStatus defines the observed state of InternalMemberCluster.
type InternalMemberClusterStatus struct {
	// Resource usage collected from member cluster.
	// +optional
	ResourceUsage ResourceUsage `json:"resourceUsage,omitempty"`

	// AgentStatus field contains the status for each agent running in the member cluster.
	// +optional
	AgentStatus []AgentStatus `json:"agentStatus,omitempty"`
}

// ResourceUsage represents the resource usage collected from the member cluster and its observation time.
type ResourceUsage struct {
	// Capacity represents the total resource capacity from all nodeStatues on the member cluster.
	// +optional
	Capacity v1.ResourceList `json:"capacity,omitempty"`

	// Allocatable represents the total allocatable resources on the member cluster.
	// +optional
	Allocatable v1.ResourceList `json:"allocatable,omitempty"`

	// The time we observe the member cluster resource usage, including capacity and allocatable.
	// +optional
	ObservationTime metav1.Time `json:"observationTime,omitempty"`
}

//+kubebuilder:object:root=true

// InternalMemberClusterList contains a list of InternalMemberCluster.
type InternalMemberClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InternalMemberCluster `json:"items"`
}

func (m *InternalMemberCluster) SetConditionsWithType(agentType AgentType, conditions ...metav1.Condition) {
	var desiredAgentStatus AgentStatus
	for _, agentStatus := range m.Status.AgentStatus {
		if agentType == agentStatus.Type {
			desiredAgentStatus = agentStatus
		}
	}

	if desiredAgentStatus.Type == "" {
		desiredAgentStatus = AgentStatus{
			Type:       MemberAgent,
			Conditions: []metav1.Condition{},
		}
		m.Status.AgentStatus = append(m.Status.AgentStatus, desiredAgentStatus)
	}

	if desiredAgentStatus.Type == agentType {
		for _, c := range conditions {
			meta.SetStatusCondition(&desiredAgentStatus.Conditions, c)
		}
	}
}

func (m *InternalMemberCluster) GetConditionWithType(agentType AgentType, conditionType string) *metav1.Condition {
	var desiredAgentStatus AgentStatus
	for _, agentStatus := range m.Status.AgentStatus {
		if agentType == agentStatus.Type {
			desiredAgentStatus = agentStatus
		}
	}
	if desiredAgentStatus.Type == agentType {
		return meta.FindStatusCondition(desiredAgentStatus.Conditions, conditionType)
	}
	return nil
}

func init() {
	SchemeBuilder.Register(&InternalMemberCluster{}, &InternalMemberClusterList{})
}
