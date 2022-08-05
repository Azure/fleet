/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MemberCluster is a resource created in the hub cluster to represent a member cluster within a fleet.

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet},shortName=membercluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ConditionTypeMemberClusterJoin")].status`,name="Joined",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +kubebuilder:printcolumn:JSONPath=`.metadata.label[fleet.azure.com/clusterHealth]`,name="HealthStatus",type=string
type MemberCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MemberClusterSpec   `json:"spec"`
	Status MemberClusterStatus `json:"status,omitempty"`
}

// MemberClusterSpec defines the desired state of MemberCluster.
type MemberClusterSpec struct {
	// State indicates the desired state of the member cluster.

	// +kubebuilder:validation:Required,Enum=Join;Leave
	State ClusterState `json:"state"`

	// Identity used by the member cluster to contact the hub cluster.
	// The hub cluster will create the minimal required permission for this identity.

	// +required
	Identity rbacv1.Subject `json:"identity"`

	// HeartbeatPeriodSeconds indicates how often (in seconds) for the member cluster to send a heartbeat. Default to 60 seconds. Minimum value is 1.

	// +kubebuilder:default=60
	// +optional
	HeartbeatPeriodSeconds int32 `json:"leaseDurationSeconds,omitempty"`
}

// MemberClusterStatus defines the observed state of MemberCluster.
type MemberClusterStatus struct {
	// Conditions is an array of current observed conditions for this member cluster.
	// +required
	Conditions []metav1.Condition `json:"conditions"`

	// Resource usage collected from member cluster.
	// +optional
	ResourceUsage ResourceUsage `json:"resourceUsage,omitempty"`

	// AgentStatus field contains the status for each agent running in the member cluster.
	// +optional
	AgentStatus []AgentStatus `json:"agentStatus,omitempty"`
}

const (
	// ConditionTypeMemberClusterReadyToJoin is used to track the readiness of the hub cluster
	// controller to accept the new member cluster.
	// its conditionStatus can only be "True" == ReadyToJoin
	ConditionTypeMemberClusterReadyToJoin string = "ReadyToJoin"

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

//func (m *MemberCluster) SetConditionsWithType(agentType AgentType, conditions ...metav1.Condition) {
//	var desiredAgentStatus AgentStatus
//	for _, agentStatus := range m.Status.AgentStatus {
//		if agentType == agentStatus.Type {
//			desiredAgentStatus = agentStatus
//		}
//	}
//	if desiredAgentStatus.Type == agentType {
//		for _, c := range conditions {
//			meta.SetStatusCondition(&desiredAgentStatus.Conditions, c)
//		}
//	}
//}
//
//func (m *MemberCluster) GetConditionWithType(agentType AgentType, conditionType string) *metav1.Condition {
//	var desiredAgentStatus AgentStatus
//	for _, agentStatus := range m.Status.AgentStatus {
//		if agentType == agentStatus.Type {
//			desiredAgentStatus = agentStatus
//		}
//	}
//	if desiredAgentStatus.Type == agentType {
//		return meta.FindStatusCondition(desiredAgentStatus.Conditions, conditionType)
//	}
//	return nil
//}

func init() {
	SchemeBuilder.Register(&MemberCluster{}, &MemberClusterList{})
}
