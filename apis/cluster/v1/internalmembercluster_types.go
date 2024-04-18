/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet,fleet-cluster},shortName=imc
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// InternalMemberCluster is used by hub agent to notify the member agents about the member cluster state changes, and is used by the member agents to report their status.
type InternalMemberCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of InternalMemberCluster.
	// +required
	Spec InternalMemberClusterSpec `json:"spec"`

	// The observed status of InternalMemberCluster.
	// +optional
	Status InternalMemberClusterStatus `json:"status,omitempty"`
}

// InternalMemberClusterSpec defines the desired state of InternalMemberCluster. Set by the hub agent.
type InternalMemberClusterSpec struct {
	// +kubebuilder:validation:Required,Enum=Join;Leave

	// The desired state of the member cluster. Possible values: Join, Leave.
	// +required
	State ClusterState `json:"state"`

	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=600

	// How often (in seconds) for the member cluster to send a heartbeat to the hub cluster. Default: 60 seconds. Min: 1 second. Max: 10 minutes.
	// +optional
	HeartbeatPeriodSeconds int32 `json:"heartbeatPeriodSeconds,omitempty"`
}

// InternalMemberClusterStatus defines the observed state of InternalMemberCluster.
type InternalMemberClusterStatus struct {
	// Conditions is an array of current observed conditions for the member cluster.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`

	// Properties is an array of properties observed for the member cluster.
	//
	// This field is beta-level; it is for the property-based scheduling feature and is only
	// populated when a property provider is enabled in the deployment.
	// +optional
	Properties map[PropertyName]PropertyValue `json:"properties,omitempty"`

	// The current observed resource usage of the member cluster. It is populated by the member agent.
	// +optional
	ResourceUsage ResourceUsage `json:"resourceUsage,omitempty"`

	// AgentStatus is an array of current observed status, each corresponding to one member agent running in the member cluster.
	// +optional
	AgentStatus []AgentStatus `json:"agentStatus,omitempty"`
}

//+kubebuilder:object:root=true

// InternalMemberClusterList contains a list of InternalMemberCluster.
type InternalMemberClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InternalMemberCluster `json:"items"`
}

// SetConditionsWithType is used to add condition to AgentStatus for a given agentType.
func (m *InternalMemberCluster) SetConditionsWithType(agentType AgentType, conditions ...metav1.Condition) {
	desiredAgentStatus := m.GetAgentStatus(agentType)
	for _, c := range conditions {
		meta.SetStatusCondition(&desiredAgentStatus.Conditions, c)
	}
}

// GetConditionWithType is used to retrieve the desired condition from AgentStatus for given agentType
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

// GetAgentStatus is used to retrieve agent status from internal member cluster,
// if it doesn't exist it creates the expected agent status and returns it.
func (m *InternalMemberCluster) GetAgentStatus(agentType AgentType) *AgentStatus {
	for i := range m.Status.AgentStatus {
		if m.Status.AgentStatus[i].Type == agentType {
			return &m.Status.AgentStatus[i]
		}
	}
	agentStatus := AgentStatus{
		Type:       agentType,
		Conditions: []metav1.Condition{},
	}
	m.Status.AgentStatus = append(m.Status.AgentStatus, agentStatus)
	return &m.Status.AgentStatus[len(m.Status.AgentStatus)-1]
}

func init() {
	SchemeBuilder.Register(&InternalMemberCluster{}, &InternalMemberClusterList{})
}
