/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=crpe
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

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
	Spec ClusterResourcePlacementEvictionSpec `json:"spec"`

	// Status is the observed state of the ClusterResourcePlacementEviction.
	// +optional
	Status ClusterResourcePlacementEvictionStatus `json:"status,omitempty"`
}

// ClusterResourcePlacementEvictionSpec is the desired state of the ClusterResourcePlacementEviction.
type ClusterResourcePlacementEvictionSpec struct {
	// PlacementReference is the name of the ClusterResourcePlacement object which
	// the Eviction object targets.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="The PlacementReference field is immutable"
	// +kubebuilder:validation:MaxLength=255
	PlacementReference string `json:"placementReference"`

	// ClusterName is the name of the cluster that the Eviction object targets.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="The ClusterName field is immutable"
	// +kubebuilder:validation:MaxLength=255
	ClusterName string `json:"clusterName"`
}

// ClusterResourcePlacementEvictionStatus is the observed state of the ClusterResourcePlacementEviction.
type ClusterResourcePlacementEvictionStatus struct {
	// Conditions is the list of currently observed conditions for the
	// ClusterResourcePlacementEviction object.
	//
	// Available condition types include:
	// * Valid: whether the Eviction object is valid, i.e., it targets at a valid placement.
	// * Executed: whether the Eviction object has been executed.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ClusterResourcePlacementEvictionConditionType identifies a specific condition of the
// ClusterResourcePlacementEviction.
type ClusterResourcePlacementEvictionConditionType string

const (
	// ClusterResourcePlacementEvictionConditionTypeValid indicates whether the Eviction object is valid.
	//
	// The following values are possible:
	// * True: the Eviction object is valid.
	// * False: the Eviction object is invalid; it might be targeting a CRP object or a placement
	//   that does not exist yet.
	//   Note that this is a terminal state; once an Eviction object is deemed invalid, it will
	//   not be evaluated again, even if the target appears later.
	ClusterResourcePlacementEvictionConditionTypeValid ClusterResourcePlacementEvictionConditionType = "Valid"

	// ClusterResourcePlacementEvictionConditionTypeExecuted indicates whether the Eviction object has been executed.
	//
	// The following values are possible:
	// * True: the Eviction object has been executed.
	//   Note that this is a terminal state; once an Eviction object is executed, it will not be
	//   executed again.
	// * False: the Eviction object has not been executed yet.
	ClusterResourcePlacementEvictionConditionTypeExecuted ClusterResourcePlacementEvictionConditionType = "Executed"
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

func init() {
	SchemeBuilder.Register(&ClusterResourcePlacementEviction{}, &ClusterResourcePlacementEvictionList{})
}
