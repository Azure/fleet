/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Membership is a resource created in a member cluster to represent its membership within a given fleet.

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet},shortName=membership
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ConditionTypeMembershipJoin")].status`,name="Joined",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
type Membership struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MembershipSpec   `json:"spec"`
	Status MembershipStatus `json:"status,omitempty"`
}

// GetCondition implements Conditioned
func (m *Membership) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

// MembershipSpec defines the desired state of the member agent installed in the member cluster.
type MembershipSpec struct {
	// State indicates the desired state of the member cluster.

	// +kubebuilder:validation:Required,Enum=Join;Leave
	State ClusterState `json:"state"`
}

// MembershipStatus defines the observed state of Membership.
type MembershipStatus struct {
	// Conditions is an array of current observed conditions for this member cluster.
	Conditions []metav1.Condition `json:"conditions"`
}

const (
	// ConditionTypeMembershipJoin is used to track the join state of the membership.
	// Its conditionStatus can be "True" == Joined, "Unknown" == Joining/Leaving, "False" == Left
	ConditionTypeMembershipJoin string = "Joined"
)

// SetConditions implements Conditioned.
func (m *Membership) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

//+kubebuilder:object:root=true

// MembershipList contains a list of Membership.
type MembershipList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Membership `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Membership{}, &MembershipList{})
}
