/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement}
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// ClusterResourcePlacementRebalance is a rebalancing attempt on a ClusterResourcePlacement
// object; one may use this API to ask Fleet to produce a new set of scheduling decisions
// for the ClusterResourcePlacement object. This is most helpful in cases where certain
// clusters in the fleet have become hotspots and the developers would like move select
// workloads to less resource strained clusters to improve the system utilization.
//
// The execution of the rebalacing operation is subject to the rollout strategy defined
// in the ClusterResourcePlacement object. This behavior is akin to editing the placement policy
// of the ClusterResourcePlacement object; disruption budget would not apply to this operation.
//
// To trigger a rebalancing operation, create a ClusterResourcePlacementRebalance object with
// the same name as the target ClusterResourcePlacement object. This guarantees a 1:1 link
// between the two objects.
//
// For safety reasons, Fleet will only execute a rebalancing attempt once; this object has no
// spec and once executed, the object will be ignored afterwards. To trigger another rebalancing
// attempt on the same ClusterResourcePlacement object, re-create (delete and create) the
// same ClusterResourcePlacementRebalance object. Note also that a ClusterResourcePlacementRebalance
// object will be ignored once it is deemed invalid (e.g., the target ClusterResourcePlacement object
// does not exist); even if the ClusterResourcePlacement object later appears, Fleet will not
// process the ClusterResourcePlacementRebalance object again.
//
// Rebalacing attempts work only with ClusterResourcePlacement objects of the PickN placement type.
//
// Executed rebalancing attempts might be kept around for a while for auditing purposes; the
// Fleet controllers might have a TTL set up for such objects and will garbage collect them
// automatically. For further information, see the Fleet documentation.
type ClusterResourcePlacementRebalance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Status is the observed state of the ClusterResourcePlacementRebalance object.
	// +optional
	Status ClusterResourcePlacementRebalanceStatus `json:"status,omitempty"`
}

// ClusterResourcePlacementRebalanceStatus is the observed state of the ClusterResourcePlacementRebalance object.
type ClusterResourcePlacementRebalanceStatus struct {
	// Conditions is the list of currently observed conditions for the
	// ClusterResourcePlacementEviction object.
	//
	// Available condition types include:
	// * Valid: whether the Eviction object is valid, i.e., it targets at a valid placement.
	// * Executed: whether the Eviction object has been executed.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ClusterResourcePlacementRebalanceConditionType identifies a specific condition of a
// ClusterResourcePlacementRebalance object.
type ClusterResourcePlacementRebalanceConditionType string

const (
	// ClusterResourcePlacementRebalanceValid indicates whether the rebalancing attempt is valid.
	//
	// The following values are possible:
	// * True: the rebalancing attempt is valid.
	// * False: the rebalancing attempt is invalid; it might be targeting a CRP that does not
	//   exist yet, or has been marked for deletion.
	//   Note thate this ia a terminal state; once the rebalancing attempt is deemed invalid,
	//   it will not be retried.
	ClusterResourcePlacementRebalanceValid ClusterResourcePlacementRebalanceConditionType = "Valid"

	// ClusterResourcePlacementRebalanceExecuted indicates whether the rebalancing attempt has been executed.
	//
	// The following values are possible:
	// * True: the rebalancing attempt has been executed.
	//   Note that this is a terminal state; once the rebalancing attempt has been executed,
	//   it will not be retried.
	// * False: the rebalancing attempt has not been executed yet.
	ClusterResourcePlacementRebalanceExecuted ClusterResourcePlacementRebalanceConditionType = "Executed"
)

// ClusterResourcePlacementRebalanceList is a collection of ClusterResourcePlacementRebalance objects.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterResourcePlacementRebalanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of ClusterResourcePlacementRebalance objects.
	Items []ClusterResourcePlacementRebalance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterResourcePlacementRebalance{}, &ClusterResourcePlacementRebalanceList{})
}
