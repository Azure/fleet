/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=crpdb

// ClusterResourcePlacementDisruptionBudget is the policy applied to a ClusterResourcePlacement
// object that specifies its disruption budget, i.e., how many placements (clusters) can be
// down at the same time due to voluntary disruptions (e.g., evictions). Involuntary
// disruptions are not subject to this budget, but will still count against it.
//
// To apply a ClusterResourcePlacementDisruptionBudget to a ClusterResourcePlacement, use the
// same name for the ClusterResourcePlacementDisruptionBudget object as the ClusterResourcePlacement
// object. This guarantees a 1:1 link between the two objects.
type ClusterResourcePlacementDisruptionBudget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the ClusterResourcePlacementDisruptionBudget.
	// +kubebuilder:validation:XValidation:rule="!(has(self.maxUnavailable) && has(self.minAvailable))",message="Both MaxUnavailable and MinAvailable cannot be specified"
	// +required
	Spec PlacementDisruptionBudgetSpec `json:"spec"`
}

// PlacementDisruptionBudgetSpec is the desired state of the PlacementDisruptionBudget.
type PlacementDisruptionBudgetSpec struct {
	// MaxUnavailable is the maximum number of placements (clusters) that can be down at the
	// same time due to voluntary disruptions. For example, a setting of 1 would imply that
	// a voluntary disruption (e.g., an eviction) can only happen if all placements (clusters)
	// from the linked Placement object are applied and available.
	//
	// This can be either an absolute value (e.g., 1) or a percentage (e.g., 10%).
	//
	// If a percentage is specified, Fleet will calculate the corresponding absolute values
	// as follows:
	// * if the linked Placement object is of the PickFixed placement type,
	//   we don't perform any calculation because eviction is not allowed for PickFixed CRP.
	// * if the linked Placement object is of the PickAll placement type, MaxUnavailable cannot
	//   be specified since we cannot derive the total number of clusters selected.
	// * if the linked Placement object is of the PickN placement type,
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
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:XValidation:rule="type(self) == string ? self.matches('^(100|[0-9]{1,2})%$') : self >= 0",message="If supplied value is String should match regex '^(100|[0-9]{1,2})%$' or If supplied value is Integer must be greater than or equal to 0"
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`

	// MinAvailable is the minimum number of placements (clusters) that must be available at any
	// time despite voluntary disruptions. For example, a setting of 10 would imply that
	// a voluntary disruption (e.g., an eviction) can only happen if there are at least 11
	// placements (clusters) from the linked Placement object are applied and available.
	//
	// This can be either an absolute value (e.g., 1) or a percentage (e.g., 10%).
	//
	// If a percentage is specified, Fleet will calculate the corresponding absolute values
	// as follows:
	// * if the linked Placement object is of the PickFixed placement type,
	//   we don't perform any calculation because eviction is not allowed for PickFixed CRP.
	// * if the linked Placement object is of the PickAll placement type, MinAvailable can be
	//   specified but only as an integer since we cannot derive the total number of clusters selected.
	// * if the linked Placement object is of the PickN placement type,
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
	// +kubebuilder:validation:XValidation:rule="type(self) == string ? self.matches('^(100|[0-9]{1,2})%$') : self >= 0",message="If supplied value is String should match regex '^(100|[0-9]{1,2})%$' or If supplied value is Integer must be greater than or equal to 0"
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`
}

// ClusterResourcePlacementDisruptionBudgetList contains a list of ClusterResourcePlacementDisruptionBudget objects.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterResourcePlacementDisruptionBudgetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of PlacementDisruptionBudget objects.
	Items []ClusterResourcePlacementDisruptionBudget `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&ClusterResourcePlacementDisruptionBudget{},
		&ClusterResourcePlacementDisruptionBudgetList{})
}
