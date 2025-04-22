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

package v1beta1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=crpe
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Valid")].status`,name="Valid",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Executed")].status`,name="Executed",type=string

// ClusterResourcePlacementEviction is an eviction attempt on a specific placement from
// a ClusterResourcePlacement object; one may use this API to force the removal of specific
// resources from a cluster.
//
// An eviction is a voluntary disruption; its execution is subject to the disruption budget
// linked with the target ClusterResourcePlacement object (if present).
//
// Beware that an eviction alone does not guarantee that a placement will not re-appear; i.e.,
// after an eviction, the Fleet scheduler might still pick the previous target cluster for
// placement. To prevent this, considering adding proper taints to the target cluster before running
// an eviction that will exclude it from future placements; this is especially true in scenarios
// where one would like to perform a cluster replacement.
//
// For safety reasons, Fleet will only execute an eviction once; the spec in this object is immutable,
// and once executed, the object will be ignored after. To trigger another eviction attempt on the
// same placement from the same ClusterResourcePlacement object, one must re-create (delete and
// create) the same Eviction object. Note also that an Eviction object will be
// ignored once it is deemed invalid (e.g., such an object might be targeting a CRP object or
// a placement that does not exist yet), even if it does become valid later
// (e.g., the CRP object or the placement appears later). To fix the situation, re-create the
// Eviction object.
//
// Note: Eviction of resources from a cluster propagated by a PickFixed CRP is not allowed.
// If the user wants to remove resources from a cluster propagated by a PickFixed CRP simply
// remove the cluster name from cluster names field from the CRP spec.
//
// Executed evictions might be kept around for a while for auditing purposes; the Fleet controllers might
// have a TTL set up for such objects and will garbage collect them automatically. For further
// information, see the Fleet documentation.
type ClusterResourcePlacementEviction struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the ClusterResourcePlacementEviction.
	//
	// Note that all fields in the spec are immutable.
	// +required
	Spec PlacementEvictionSpec `json:"spec"`

	// Status is the observed state of the ClusterResourcePlacementEviction.
	// +optional
	Status PlacementEvictionStatus `json:"status,omitempty"`
}

// PlacementEvictionSpec is the desired state of the parent PlacementEviction.
type PlacementEvictionSpec struct {
	// PlacementName is the name of the Placement object which
	// the Eviction object targets.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="The PlacementName field is immutable"
	// +kubebuilder:validation:MaxLength=255
	PlacementName string `json:"placementName"`

	// ClusterName is the name of the cluster that the Eviction object targets.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="The ClusterName field is immutable"
	// +kubebuilder:validation:MaxLength=255
	ClusterName string `json:"clusterName"`
}

// PlacementEvictionStatus is the observed state of the parent PlacementEviction.
type PlacementEvictionStatus struct {
	// Conditions is the list of currently observed conditions for the
	// PlacementEviction object.
	//
	// Available condition types include:
	// * Valid: whether the Eviction object is valid, i.e., it targets at a valid placement.
	// * Executed: whether the Eviction object has been executed.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// PlacementEvictionConditionType identifies a specific condition of the
// PlacementEviction.
type PlacementEvictionConditionType string

const (
	// PlacementEvictionConditionTypeValid indicates whether the Eviction object is valid.
	//
	// The following values are possible:
	// * True: the Eviction object is valid.
	// * False: the Eviction object is invalid; it might be targeting a CRP object or a placement
	//   that does not exist yet.
	//   Note that this is a terminal state; once an Eviction object is deemed invalid, it will
	//   not be evaluated again, even if the target appears later.
	PlacementEvictionConditionTypeValid PlacementEvictionConditionType = "Valid"

	// PlacementEvictionConditionTypeExecuted indicates whether the Eviction object has been executed.
	//
	// The following values are possible:
	// * True: the Eviction object has been executed.
	//   Note that this is a terminal state; once an Eviction object is executed, it will not be
	//   executed again.
	// * False: the Eviction object has not been executed yet.
	PlacementEvictionConditionTypeExecuted PlacementEvictionConditionType = "Executed"
)

// ClusterResourcePlacementEvictionList contains a list of ClusterResourcePlacementEviction objects.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterResourcePlacementEvictionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of ClusterResourcePlacementEviction objects.
	Items []ClusterResourcePlacementEviction `json:"items"`
}

// SetConditions set the given conditions on the ClusterResourcePlacementEviction.
func (e *ClusterResourcePlacementEviction) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&e.Status.Conditions, c)
	}
}

// GetCondition returns the condition of the given ClusterResourcePlacementEviction.
func (e *ClusterResourcePlacementEviction) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(e.Status.Conditions, conditionType)
}

func init() {
	SchemeBuilder.Register(
		&ClusterResourcePlacementEviction{},
		&ClusterResourcePlacementEvictionList{})
}
