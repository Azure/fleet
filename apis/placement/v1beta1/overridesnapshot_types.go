/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",shortName=rss,categories={fleet,fleet-placement}
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterResourceOverrideSnapshot is used to store a snapshot of overrides which will be applied to the resources selected
// by the clusterResourcePlacement.
// Its spec is immutable.
// We may need to produce more than one overrideSnapshot for all the resources a ResourcePlacement selected to get around the 1MB size limit of k8s objects.
// We assign an ever-increasing index for each such group of overrideSnapshots.
// The naming convention of a clusterResourceOverrideSnapshot is {CRPName}-{overrideIndex}-{subindex}
// where the name of the first snapshot of a group has no subindex part so its name is {CRPName}-{overrideIndex}-snapshot.
// overrideIndex will begin with 0.
// Each snapshot MUST have the following labels:
//   - `CRPTrackingLabel` which points to its owner CRP.
//   - `OverrideIndexLabel` which is the index  of the snapshot group.
//
// All the snapshots within the same index group must have the same OverrideIndexLabel.
//
// The first snapshot of the index group MUST have the following annotations:
//   - `NumberOfOverrideSnapshotsAnnotation` to store the total number of override snapshots in the index group.
//   - `OverrideGroupHashAnnotation` whose value is the sha-256 hash of all the snapshots belong to the same snapshot index.
//
// Each snapshot (excluding the first snapshot) MUST have the following annotations:
//   - `SubindexOfOverrideSnapshotAnnotation` to store the subindex of override snapshot in the group.
type ClusterResourceOverrideSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of OverrideSnapshot.
	// +required
	Spec OverrideSnapshotSpec `json:"spec"`
}

// OverrideSnapshotSpec	defines the desired state of OverrideSnapshot.
type OverrideSnapshotSpec struct {
	// ClusterResourceOverrides contains a list of overrides related to the CRP.
	// The list will be ordered by its priority.
	// The Override with the highest priority value will be the first.
	// If the multiple overrides have the same priority value, it will be sorted by name.
	// For example, "override-1" will be first and then "override-2".
	// +required
	ClusterResourceOverrides []ClusterResourceOverride `json:"clusterResourceOverrides"`

	// Later, we can introduce ResourceOverrides defined by the application developers.
}
