/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",shortName=rss,categories={fleet-workload}
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ResourceSnapShot is used to store a snapshot of selected resources by a resource placement policy.
// It is immutable. We may need to produce more than one resourceSnapshot for one ResourcePlacement to
// get around the 1MB size limit of k8s objects.
// Each snapshot must have a label "fleet.azure.com/snapshotGroup" with value as the snapshot index.
// Each must have an annotation "fleet.azure.com/resource-hash" with value as the sha-256 hash
// value of all the snapshots belong to the same snapshot index.
type ResourceSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ResourceSnapShot.
	// +required
	Spec ResourceSnapShotSpec `json:"spec"`

	// The observed status of ResourceSnapShot.
	// +optional
	Status ResourceSnapShotStatus `json:"status,omitempty"`
}

// ResourceSnapShotSpec	defines the desired state of ResourceSnapShot.
type ResourceSnapShotSpec struct {
	// Index is the parent index of this resource snapshot. Each index can have multiple snapshots.
	// All the snapshots with the same index have the same label "fleet.azure.com/snapshotGroup" that
	// points to the index.
	// +required
	Index int32 `json:"index"`

	// SelectedResources contains a list of resources selected by ResourceSelectors.
	// +required
	SelectedResources []ResourceContent `json:"selectedResources"`

	// IsLatest indicates if the resourceSnapshot is the latest or not.
	// +required
	IsLatest bool `json:"isLatest"`

	// PolicySnapShotName is the name of the policy snapshot that this resource snapshot is pointing to.
	PolicySnapShotName string `json:"policySnapShotName"`
}

// ResourceContent contains the content of a resource
type ResourceContent struct {
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	runtime.Unknown `json:",inline"`
}

type ResourceSnapShotStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for ResourceSnapShot.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`
}

// ClusterResourcePlacementList contains a list of ClusterResourcePlacement.
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ResourceSnapShotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceSnapShot `json:"items"`
}

func (m *ResourceSnapShot) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

func (m *ResourceSnapShot) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

func init() {
	SchemeBuilder.Register(&ResourceSnapShot{}, &ResourceSnapShot{})
}
