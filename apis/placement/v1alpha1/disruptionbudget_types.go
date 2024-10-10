/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=crpdb
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// PlacementDisruptionBudget is the policy applied to a ClusterResourcePlacement
// object that specifies its disruption budget, i.e., how many placements (clusters) can be
// down at the same time due to **voluntary** disruptions (e.g., evictions). Involuntary
// disruptions are not subject to this budget, but will still count against it.
//
// To apply a PlacementDisruptionBudget to a ClusterResourcePlacement, use the
// same name for the PlacementDisruptionBudget object as the ClusterResourcePlacement
// object. This guarantees a 1:1 link between the two objects.
type PlacementDisruptionBudget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the PlacementDisruptionBudget.
	// +required
	Spec PlacementDisruptionBudgetSpec `json:"spec"`

	// Status is the observed state of the PlacementDisruptionBudget.
	// +optional
	Status PlacementDisruptionBudgetStatus `json:"status,omitempty"`
}

// PlacementDisruptionBudgetSpec is the desired state of the
// PlacementDisruptionBudget.
type PlacementDisruptionBudgetSpec struct {
	// MaxUnavailable is the maximum number of placements that can be down at the same time
	// due to voluntary disruptions. For example, a setting of 1 would imply that
	// a voluntary disruption (e.g., an eviction) can only happen if all placements
	// from the linked ClusterResourcePlacement object are applied and available.
	//
	// This can be either an absolute value (e.g., 1) or a percentage (e.g., 10%).
	//
	// If a percentage is specified, Fleet will calculate the corresponding absolutate values
	// as follows:
	// * if the linked ClusterResourcePlacement object is of the PickFixed placement type,
	//   the percentage is against the number of clusters specified in the placement (i.e., the
	//   length of ClusterNames field in the placement policy);
	// * if the linked ClusterResourcePlacement object is of the PickAll placement type,
	//   the percentage is against the total number of clusters being selected by the scheduler
	//   at the time of the evaluation of the disruption budget;
	// * if the linked ClusterResourcePlacement object is of the PickN placement type,
	//   the percentage is against the number of clusters specified in the placement (i.e., the
	//   value of the NumberOfClusters fields in the placement policy).
	// The end result will be rounded up to the nearest integer if applicable.
	//
	// One may use a value of 0 for this field; in this case, no voluntary disruption would be
	// allowed.
	//
	// This field is mutually exclusive with the MinAvailable field in the spec; exactly one
	// of them can be set at a time.
	//
	// Defaults to 25%.
	// +kubebuilder:default="25%"
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern="^((100|[0-9]{1,2})%|[0-9]+)$"
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`

	// MinAvailable is the minimum number of placements that must be available at any time
	// despite voluntary disruptions. For example, a setting of 10 would imply that
	// a voluntary disruption (e.g., an eviction) can only happen if there are at least 11
	// placements from the linked ClusterResourcePlacement object are applied and available.
	//
	// This can be either an absolute value (e.g., 1) or a percentage (e.g., 10%).
	//
	// If a percentage is specified, Fleet will calculate the corresponding absolutate values
	// as follows:
	// * if the linked ClusterResourcePlacement object is of the PickFixed placement type,
	//   the percentage is against the number of clusters specified in the placement (i.e., the
	//   length of ClusterNames field in the placement policy);
	// * if the linked ClusterResourcePlacement object is of the PickAll placement type,
	//   the percentage is against the total number of clusters being selected by the scheduler
	//   at the time of the evaluation of the disruption budget;
	// * if the linked ClusterResourcePlacement object is of the PickN placement type,
	//   the percentage is against the number of clusters specified in the placement (i.e., the
	//   value of the NumberOfClusters fields in the placement policy).
	// The end result will be rounded up to the nearest integer if applicable.
	//
	// One may use a value of 0 for this field; in this case, voluntary disruption would be
	// allowed at any time.
	//
	// This field is mutually exclusive with the MaxUnavailable field in the spec; exactly one
	// of them can be set at a time.
	//
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern="^((100|[0-9]{1,2})%|[0-9]+)$"
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`
}

// PlacementDisruptionBudgetStatus is the observed state of the
// PlacementDisruptionBudget.
type PlacementDisruptionBudgetStatus struct {
	// Conditions is the list of currently observed conditions for the
	// PlacementDisruptionBudget object.
	//
	// Available condition types include:
	// * Applied: whether the disruption budget has been successfully applied to the linked
	//   ClusterResourcePlacement object.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// PlacementDisruptionBudgetConditionType identifies a specific condition of the
// PlacementDisruptionBudget.
type PlacementDisruptionBudgetConditionType string

const (
	// PlacementDisruptionBudgetApplied indicates whether the disruption budget
	// has been successfully applied to the linked ClusterResourcePlacement object.
	//
	// The following values are possible:
	// * True: the disruption budget has been successfully applied to the linked ClusterResourcePlacement.
	// * False: the disruption budget has not been applied to the linked ClusterResourcePlacement;
	//   the CRP object might be missing or has been marked for deletion.
	PlacementDisruptionBudgetApplied PlacementDisruptionBudgetConditionType = "Applied"
)

// PlacementDisruptionBudgetList contains a list of PlacementDisruptionBudget objects.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PlacementDisruptionBudgetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of PlacementDisruptionBudget objects.
	Items []PlacementDisruptionBudget `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&PlacementDisruptionBudget{},
		&PlacementDisruptionBudgetList{})
}
