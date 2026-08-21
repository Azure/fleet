/*
Copyright 2026 The KubeFleet Authors.

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
)

// The condition types for the PlacementBinding and ClusterPlacementBinding APIs.
const (
	PlacementBindingCondTypeSynchronized = "Synchronized"
	PlacementBindingCondTypeAvailable    = "Available"
)

// The reasons for each condition type of the PlacementBinding and ClusterPlacementBinding APIs.
const (
	PlacementBindingSynchronizedCondReasonAllResourcesSynchronized         = "AllResourcesSynchronized"
	PlacementBindingSynchronizedCondReasonFailedToSynchronizeSomeResources = "FailedToSynchronizeSomeResources"

	PlacementBindingAvailableCondReasonAllResourcesAvailable    = "AllResourcesAvailable"
	PlacementBindingAvailableCondReasonSomeResourcesUnavailable = "SomeResourcesUnavailable"
)

// PlacementBinding is the KubeFleet API that binds the resources selected by a placement
// policy to a specific member cluster.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={kubefleet, kubefleet-placement}
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PlacementBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the binding.
	//
	// +kubebuilder:validation:Required
	Spec PlacementBindingSpec `json:"spec"`

	// The observed status of the binding.
	//
	// +kubebuilder:validation:Optional
	Status PlacementBindingStatus `json:"status,omitempty"`
}

// ClusterPlacementBinding is the KubeFleet API that binds the resources selected by a cluster placement
// policy to a specific member cluster.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={kubefleet, kubefleet-placement}
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterPlacementBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the binding.
	//
	// +kubebuilder:validation:Required
	Spec PlacementBindingSpec `json:"spec"`

	// The observed status of the binding.
	//
	// +kubebuilder:validation:Optional
	Status PlacementBindingStatus `json:"status,omitempty"`
}

type PlacementBindingSpec struct {
	// The name of the placement policy that this binding is associated with.
	//
	// +kubebuilder:validation:Required
	PlacementPolicyName string `json:"placementPolicyName"`

	// The cluster selectors associated with (fulfilled by) this binding.
	//
	// This field is added for informational purposes only. KubeFleet uses hashes of these cluster selectors
	// (kept in the annotations) to determine which cluster selectors the binding is associated with.
	//
	// +kubebuilder:validation:Optional
	ClusterSelectors []ClusterSelectorWithTermsOnly `json:"clusterSelectors,omitempty"`

	// The name of the member cluster that this binding is associated with.
	//
	// +kubebuilder:validation:Required
	ClusterName string `json:"clusterName"`

	// The name of the resource snapshot that this binding is associated with.
	//
	// If the resources being selected cannot fit within a single resource snapshot, this field tracks
	// the name of the primary resource snapshot that this binding is associated with.
	//
	// +kubebuilder:validation:Required
	ResourceSnapshotName string `json:"resourceSnapshotName"`

	// The strategy to synchronize the resources to the member cluster.
	//
	// +kubebuilder:validation:Optional
	SyncStrategy *SyncStrategy `json:"syncStrategy,omitempty"`

	// Whether the binding is suspended. If set to true, KubeFleet will remove resources from the associated cluster.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	Suspended bool `json:"suspended,omitempty"`
}

type ClusterSelectorWithTermsOnly struct {
	// The terms that describe the requirements for a target cluster in the cluster selector.
	//
	// +kubebuilder:validation:Optional
	Terms []ClusterLabelAndPropertySelectorTerm `json:"terms,omitempty"`
}

type PlacementBindingStatus struct {
	// A list of observed conditions about the binding.
	//
	// +kubebuilder:validation:Optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// The number of resources that are included in the currently associated resource snapshot(s).
	//
	// +kubebuilder:validation:Optional
	SelectedResources *int32 `json:"selectedResources,omitempty"`
	// The number of selected resources that have been synchronized to the target cluster.
	//
	// A resource is considered synchronized if it has been successfully created (applied) in the target cluster.
	//
	// +kubebuilder:validation:Optional
	SynchronizedResources *int32 `json:"synchronizedResources,omitempty"`
	// The number of selected resources that are available in the target cluster.
	//
	// A resource is considered available if it has been successfully created (applied) in the target cluster and
	// has passed KubeFleet's built-in availability checks (if applicable).
	//
	// +kubebuilder:validation:Optional
	AvailableResources *int32 `json:"availableResources,omitempty"`

	// A list of resources that have failed to be synchronized to the target cluster, or have failed to become
	// available in the target cluster.
	//
	// If there are more than 50 failed resources, only the first 50 will be included in this list.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=50
	FailedResources []FailedResource `json:"failedResources,omitempty"`
}

type FailedResource struct {
	// The object reference of the failed resource.
	//
	// +kubebuilder:validation:Required
	ObjectRef ObjectReference `json:"objectRef"`

	// A list of observed conditions about the failed resource.
	//
	// +kubebuilder:validation:Optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// The details about observed diffs between the resource on the hub cluster and on the member cluster side, if any.
	// This field is populated when drift detection is enabled and the resource is of a drifted state,
	// or when diff check upon takeovers is enabled and diffs have been detected on the resource to take over.
	//
	// +kubebuilder:validation:Optional
	DiffDetails *DiffDetails `json:"diffDetails,omitempty"`
}

// The list objects for the PlacementBinding and ClusterPlacementBinding APIs.

// PlacementBindingList contains a list of PlacementBinding.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PlacementBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []PlacementBinding `json:"items"`
}

// ClusterPlacementBindingList contains a list of ClusterPlacementBinding.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterPlacementBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClusterPlacementBinding `json:"items"`
}

// Set up the API types with the scheme builder.
func init() {
	SchemeBuilder.Register(&PlacementBinding{}, &PlacementBindingList{})
	SchemeBuilder.Register(&ClusterPlacementBinding{}, &ClusterPlacementBindingList{})
}
