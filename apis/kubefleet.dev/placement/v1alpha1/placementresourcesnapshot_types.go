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
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// PlacementResourceSnapshot is the KubeFleet API that captures the resources selected by a placement policy
// as seen on the hub cluster at a specific point in time. It is referenced by other KubeFleet APIs
// to enable consistent rollouts of resources across multiple member clusters in the fleet.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={kubefleet, kubefleet-placement}
// +kubebuilder:storageversion
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PlacementResourceSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The spec of a placement resource snapshot.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="the spec field is immutable"
	Spec PlacementResourceSnapshotSpec `json:"spec"`
}

type PlacementResourceSnapshotSpec struct {
	// The manifests of the resources selected by the owner placement policy at the time of snapshot creation.
	//
	// +kubebuilder:validation:Optional
	Resources []SnapshottedResource `json:"resources,omitempty"`
}

type SnapshottedResource struct {
	// The identifier of the resource.
	//
	// +kubebuilder:validation:Required
	Identifier ObjectReference `json:"identifier"`

	// The manifest of the resource. It should be a Kubernetes object in YAML or JSON format.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	Manifest runtime.RawExtension `json:"manifest"`

	// Additional information associated with the resource, if any.
	//
	// +kubebuilder:validation:Optional
	AdditionalInfo map[string][]byte `json:"additionalInfo,omitempty"`
}

// ClusterPlacementResourceSnapshot is the KubeFleet API that captures the resources selected by
// a cluster placement policy as seen on the hub cluster at a specific point in time. It is referenced
// by other KubeFleet APIs to enable consistent rollouts of resources across multiple member clusters in the fleet.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={kubefleet, kubefleet-placement}
// +kubebuilder:storageversion
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterPlacementResourceSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The spec of a cluster placement resource snapshot.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="the spec field is immutable"
	Spec PlacementResourceSnapshotSpec `json:"spec"`
}

// The list objects for the PlacementResourceSnapshot and ClusterPlacementResourceSnapshot APIs.

// PlacementResourceSnapshotList contains a list of PlacementResourceSnapshot objects.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PlacementResourceSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []PlacementResourceSnapshot `json:"items"`
}

// ClusterPlacementResourceSnapshotList contains a list of ClusterPlacementResourceSnapshot objects.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterPlacementResourceSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClusterPlacementResourceSnapshot `json:"items"`
}

// Set up the API types with the scheme builder.
func init() {
	SchemeBuilder.Register(&PlacementResourceSnapshot{}, &PlacementResourceSnapshotList{})
	SchemeBuilder.Register(&ClusterPlacementResourceSnapshot{}, &ClusterPlacementResourceSnapshotList{})
}
