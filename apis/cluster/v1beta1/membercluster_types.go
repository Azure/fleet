/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-cluster},shortName=cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Joined")].status`,name="Joined",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// MemberCluster is a resource created in the hub cluster to represent a member cluster within a fleet.
type MemberCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of MemberCluster.
	// +required
	Spec MemberClusterSpec `json:"spec"`

	// The observed status of MemberCluster.
	// +optional
	Status MemberClusterStatus `json:"status,omitempty"`
}

// MemberClusterSpec defines the desired state of MemberCluster.
type MemberClusterSpec struct {
	// +kubebuilder:validation:Required,Enum=Join;Leave

	// The identity used by the member cluster to access the hub cluster.
	// The hub agents deployed on the hub cluster will automatically grant the minimal required permissions to this identity for the member agents deployed on the member cluster to access the hub cluster.
	// +required
	Identity rbacv1.Subject `json:"identity"`

	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=600

	// How often (in seconds) for the member cluster to send a heartbeat to the hub cluster. Default: 60 seconds. Min: 1 second. Max: 10 minutes.
	// +optional
	HeartbeatPeriodSeconds int32 `json:"heartbeatPeriodSeconds,omitempty"`
}

// Metric is the observed value of a non-resource metric.
type Metric struct {
	// The name of the metric; it should be a valid Kubernetes label name.
	Name string `json:"name"`
	// The observed value of the metric; it should be a sortable numeric.
	Value string `json:"value"`
}

// MemberClusterStatus defines the observed status of MemberCluster.
type MemberClusterStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for the member cluster.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`

	// Metrics is an array of non-resource metrics observed for the member cluster.
	Metrics []Metric `json:"metrics,omitempty"`

	// The current observed resource usage of the member cluster. It is copied from the corresponding InternalMemberCluster object.
	// +optional
	ResourceUsage ResourceUsage `json:"resourceUsage,omitempty"`

	// AgentStatus is an array of current observed status, each corresponding to one member agent running in the member cluster.
	// +optional
	AgentStatus []AgentStatus `json:"agentStatus,omitempty"`
}

// MemberClusterConditionType defines a specific condition of a member cluster.
type MemberClusterConditionType string

const (
	// ConditionTypeMemberClusterReadyToJoin indicates the readiness condition of the given member cluster for joining the hub cluster.
	// Its condition status can be one of the following:
	// - "True" means the hub cluster is ready for the member cluster to join.
	// - "False" means the hub cluster is not ready for the member cluster to join.
	// - "Unknown" means it is unknown whether the hub cluster is ready for the member cluster to join.
	ConditionTypeMemberClusterReadyToJoin MemberClusterConditionType = "ReadyToJoin"

	// ConditionTypeMemberClusterJoined indicates the join condition of the given member cluster.
	// Its condition status can be one of the following:
	// - "True" means all the agents on the member cluster have joined.
	// - "False" means all the agents on the member cluster have left.
	// - "Unknown" means not all the agents have joined or left.
	ConditionTypeMemberClusterJoined MemberClusterConditionType = "Joined"

	// ConditionTypeMemberClusterHealthy indicates the health condition of the given member cluster.
	// Its condition status can be one of the following:
	// - "True" means the member cluster is healthy.
	// - "False" means the member cluster is unhealthy.
	// - "Unknown" means the member cluster has an unknown health status.
	// NOTE: This condition type is currently unused.
	ConditionTypeMemberClusterHealthy MemberClusterConditionType = "Healthy"
)

//+kubebuilder:object:root=true

// MemberClusterList contains a list of MemberCluster.
type MemberClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MemberCluster `json:"items"`
}

func (m *MemberCluster) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

func (m *MemberCluster) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

func (m *MemberCluster) RemoveCondition(conditionType string) {
	meta.RemoveStatusCondition(&m.Status.Conditions, conditionType)
}

// GetAgentStatus retrieves the status of a specific member agent from the MemberCluster object.
//
// If the specificed agent does not exist, or it has not updated its status with the hub cluster
// yet, this function returns nil.
func (m *MemberCluster) GetAgentStatus(agentType AgentType) *AgentStatus {
	for _, s := range m.Status.AgentStatus {
		if s.Type == agentType {
			return &s
		}
	}
	return nil
}

// GetAgentCondition queries the conditions in an agent status for a specific condition type.
func (m *MemberCluster) GetAgentCondition(agentType AgentType, conditionType AgentConditionType) *metav1.Condition {
	if s := m.GetAgentStatus(agentType); s != nil {
		return meta.FindStatusCondition(s.Conditions, string(conditionType))
	}
	return nil
}

func init() {
	SchemeBuilder.Register(&MemberCluster{}, &MemberClusterList{})
}
