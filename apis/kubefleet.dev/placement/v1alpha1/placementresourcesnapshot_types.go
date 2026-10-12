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

const (
	// When users create a placement policy to place resources across member clusters, KubeFleet will capture the
	// resources selected by the placement policy at a specific point in time in the form of placement resource
	// snapshots. This enables KubeFleet to roll out resources to member clusters in a consistent manner.
	//
	// As resources change over time, there might be a time series of placement resource snapshots associated
	// with a placement policy. KubeFleet assigns these snapshots with a monotonically increasing index based on
	// their creation timestamp, starting from 0 with a step of 1.
	//
	// Due to sizing limitations in Kubernetes, when there are too many resources being selected at a time, or
	// when some resources are too large, KubeFleet will capture them using multiple placement resource snapshots.
	// These snapshots share the same index (as they are snapshots from the same point in time), and KubeFleet
	// will further assign them each with a sub-index to tell them apart, also starting from 0 with a step of 1.
	// The snapshot of the sub-index 0 is considered the primary snapshot of the same index.

	// PlacementResourceSnapshotOwnedByLabelKey is a label key that denotes the owner placement policy of
	// a placement resource snapshot. Its value is the name of the owner placement policy. KubeFleet
	// might truncate the name and add a hash suffix as needed.
	//
	// This label is set on all placement resource snapshots.
	PlacementResourceSnapshotOwnedByLabelKey = "placement.kubefleet.dev/placement-resource-snapshot-owned-by"
	// PlacementResourceSnapshotIndexLabelKey is a label key that denotes the index of a placement resource snapshot.
	// Its value is the index integer formatted as a string.
	//
	// This label is set on all placement resource snapshots.
	PlacementResourceSnapshotIndexLabelKey = "placement.kubefleet.dev/placement-resource-snapshot-index"
	// PlacementResourceSnapshotSubIndexLabelKey is a label key that denotes the sub-index of a placement resource snapshot.
	// Its value is the sub-index integer formatted as a string.
	//
	// This label is set on all placement resource snapshots.
	PlacementResourceSnapshotSubIndexLabelKey = "placement.kubefleet.dev/placement-resource-snapshot-sub-index"
	// SubIndexedPlacementResourceSnapshotCountLabelKey is a label key that denotes the total number of sub-indexed
	// placement resource snapshots associated with the same index. Its value is the count integer
	// formatted as a string.
	//
	// This label is set only on resource placement snapshots with the sub-index of 0.
	SubIndexedPlacementResourceSnapshotCountLabelKey = "placement.kubefleet.dev/sub-indexed-placement-resource-snapshot-count"

	// PlacementResourceSnapshotContentsHashAnnotationKey is an annotation key that denotes the hash of the contents
	// of a placement resource snapshot. Its value is the hash string.
	//
	// This annotation is set on all placement resource snapshots.
	PlacementResourceSnapshotContentsHashAnnotationKey = "placement.kubefleet.dev/placement-resource-snapshot-contents-hash"
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
