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

const (
	// ResourceIndexLabel is the label that indicate the resource snapshot index of a cluster policy.
	ResourceIndexLabel = labelPrefix + "resourceIndex"

	// ResourceGroupHashAnnotation is the label that contains the value of the sha-256 hash
	// value of all the snapshots belong to the same snapshot index.
	ResourceGroupHashAnnotation = labelPrefix + "resourceHash"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",shortName=rss,categories={fleet-workload}
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterResourceSnapshot is used to store a snapshot of selected resources by a resource placement policy.
// It is immutable. We may need to produce more than one resourceSnapshot for one ResourcePlacement to
// get around the 1MB size limit of k8s objects.
// Each snapshot must have `CRPTrackingLabel`, `ResourceIndexLabel` and `IsLatestSnapshotLabel`. If there are multiple resource snapshots for a resourcePlacement, the parent one whose name is {CRPName}-{resourceIndex} will have a label "NumberOfResourceSnapshots" to store the total number of resource snapshots.
// Each snapshot must have an annotation "fleet.azure.com/resourceHash" with value as the the sha-256 hash value of all the snapshots belong to the same snapshot index.
type ClusterResourceSnapshot struct {
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

	// SelectedResources contains a list of resources selected by ResourceSelectors.
	// +required
	SelectedResources []ResourceContent `json:"selectedResources"`

	// PolicySnapShotName is the name of the policy snapshot that this resource snapshot is pointing to.
	PolicySnapShotName string `json:"policySnapShotName"`
}

// ResourceContent contains the content of a resource
type ResourceContent struct {
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	runtime.RawExtension `json:"-,inline"`
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

// ClusterResourceSnapShotList contains a list of ClusterResourceSnapshot.
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterResourceSnapShotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourceSnapshot `json:"items"`
}

// SetConditions sets the conditions for a ClusterResourceSnapshot.
func (m *ClusterResourceSnapshot) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

// GetCondition gets the condition for a ClusterResourceSnapshot.
func (m *ClusterResourceSnapshot) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

func init() {
	SchemeBuilder.Register(&ClusterResourceSnapshot{}, &ClusterResourceSnapShotList{})
}
