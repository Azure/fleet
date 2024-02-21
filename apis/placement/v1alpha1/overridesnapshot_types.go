/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",categories={fleet,fleet-placement}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterResourceOverrideSnapshot is used to store a snapshot of ClusterResourceOverride.
// Its spec is immutable.
// We assign an ever-increasing index for snapshots.
// The naming convention of a ClusterResourceOverrideSnapshot is {ClusterResourceOverride}-{resourceIndex}.
// resourceIndex will begin with 0.
// Each snapshot MUST have the following labels:
//   - `OverrideTrackingLabel` which points to its owner ClusterResourceOverride.
//   - `IsLatestSnapshotLabel` which indicates whether the snapshot is the latest one.
type ClusterResourceOverrideSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ClusterResourceOverrideSnapshotSpec.
	// +required
	Spec ClusterResourceOverrideSnapshotSpec `json:"spec"`
}

// ClusterResourceOverrideSnapshotSpec defines the desired state of ClusterResourceOverride.
type ClusterResourceOverrideSnapshotSpec struct {
	// OverrideSpec stores the spec of ClusterResourceOverride.
	OverrideSpec ClusterResourceOverrideSpec `json:"overrideSpec"`

	// OverrideHash is the sha-256 hash value of the OverrideSpec field.
	// +required
	OverrideHash []byte `json:"overrideHash"`
}

// +genclient
// +genclient:Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced",categories={fleet,fleet-placement}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ResourceOverrideSnapshot is used to store a snapshot of ResourceOverride.
// Its spec is immutable.
// We assign an ever-increasing index for snapshots.
// The naming convention of a ResourceOverrideSnapshot is {ResourceOverride}-{resourceIndex}.
// resourceIndex will begin with 0.
// Each snapshot MUST have the following labels:
//   - `OverrideTrackingLabel` which points to its owner ResourceOverride.
//   - `IsLatestSnapshotLabel` which indicates whether the snapshot is the latest one.
type ResourceOverrideSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ResourceOverrideSnapshot.
	// +required
	Spec ResourceOverrideSnapshotSpec `json:"spec"`
}

// ResourceOverrideSnapshotSpec defines the desired state of ResourceOverride.
type ResourceOverrideSnapshotSpec struct {
	// OverrideSpec stores the spec of ResourceOverride.
	OverrideSpec ResourceOverrideSpec `json:"overrideSpec"`

	// OverrideHash is the sha-256 hash value of the OverrideSpec field.
	// +required
	OverrideHash []byte `json:"overrideHash"`
}

// ClusterResourceOverrideSnapshotList contains a list of ClusterResourceOverrideSnapshot.
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterResourceOverrideSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourceOverrideSnapshot `json:"items"`
}

// ResourceOverrideSnapshotList contains a list of ResourceOverrideSnapshot.
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ResourceOverrideSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceOverrideSnapshot `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&ClusterResourceOverrideSnapshot{}, &ClusterResourceOverrideSnapshotList{},
		&ResourceOverrideSnapshot{}, &ResourceOverrideSnapshotList{},
	)
}
