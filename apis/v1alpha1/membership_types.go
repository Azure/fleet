/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Membership is a resource created in a member cluster to represent its membership within a given fleet.

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories={fleet}
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ConditionTypeJoined")].status`,name="Joined",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
type Membership struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MembershipSpec   `json:"spec"`
	Status MembershipStatus `json:"status,omitempty"`
}

// MembershipSpec defines the desired state of the member agent installed in the member cluster.
type MembershipSpec struct {
	// MemberClusterName is the name of the MemberCluster custom resource, which can be found in the hub cluster.

	// +required
	MemberClusterName string `json:"memberClusterName"`

	// HubURL is the url of apiserver endpoint of the hub cluster for the member agent to contact
	// +required
	HubURL string `json:"hubUrl"`

	// State indicates the desired state of the member cluster.

	// +kubebuilder:validation:Enum=Join;Leave
	// +required
	State ClusterState `json:"state"`
}

// MembershipStatus defines the observed state of Membership.
type MembershipStatus struct {
	// Conditions is an array of current observed conditions for this member cluster.
	Conditions []metav1.Condition `json:"conditions"`
}

const (
	// ConditionTypeMembershipJoin is used to track the join state of the membership.
	// Its conditionStatus can be "True" == Joined, "Unknown" == Joining/Leaving, "False" == Leave
	ConditionTypeMembershipJoin string = "Joined"
)

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
