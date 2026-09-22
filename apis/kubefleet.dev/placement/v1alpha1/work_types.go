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
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// The label key that denotes the namespace of a work object's owner placement policy and placement binding
	// objects. For cluster-scoped owners, the label has an empty value.
	WorkOwnerNamespaceLabelKey = "placement.kubefleet.dev/owner-namespace"
	// The label key that denotes the name of a work object's owner placement policy object.
	//
	// KubeFleet might truncate the name value and append a hash to satisfy Kubernetes' label value length limit
	// (63 characters).
	WorkOwnedByPlacementPolicyLabelKey = "placement.kubefleet.dev/owned-by-placement-policy"
	// The label key that denotes the name of a work object's owner placement binding object.
	//
	// KubeFleet might truncate the name value and append a hash to satisfy Kubernetes' label value length limit
	// (63 characters).
	WorkOwnedByPlacementBindingLabelKey = "placement.kubefleet.dev/owned-by-placement-binding"

	// The annotation key that denotes the name of a work object's owner placement policy object.
	//
	// The name will saved as the annotation value as it is.
	WorkOwnedByPlacementPolicyAnnotationKey = "placement.kubefleet.dev/owned-by-placement-policy"
	// The annotation key that denotes the name of a work object's owner placement binding object.
	//
	// The name will saved as the annotation value as it is.
	WorkOwnedByPlacementBindingAnnotationKey = "placement.kubefleet.dev/owned-by-placement-binding"
	// The annotation key that denotes the name of a work object's primary placement resource snapshot.
	//
	// The name will saved as the annotation value as it is.
	WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey = "placement.kubefleet.dev/linked-to-primary-placement-resource-snapshot"
	// The annotation key that denotes the number of linked work objects.
	LinkedWorkCountAnnotationKey = "placement.kubefleet.dev/linked-work-count"
	// The annotation key that denotes the source from which the work object is derived.
	WorkDerivedFromSourceAnnotationKey = "placement.kubefleet.dev/derived-from"
)

const (
	// The condition types for the Work API.
	WorkCondTypeApplied   = "Applied"
	WorkCondTypeAvailable = "Available"

	// The condition types for each manifest in the Work API.
	ManifestCondTypeApplied   = "Applied"
	ManifestCondTypeAvailable = "Available"
)

// Work is the KubeFleet API used for synchronizing resources to place between
// the hub cluster and a member cluster in the member cluster reserved namespace.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={kubefleet, kubefleet-placement}
// +kubebuilder:storageversion
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Work struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the work object.
	//
	// +kubebuilder:validation:Required
	Spec WorkSpec `json:"spec"`

	// The observed status of the work object.
	//
	// +kubebuilder:validation:Optional
	Status WorkStatus `json:"status,omitempty"`
}

type WorkSpec struct {
	// The manifests of the resources to be synchronized to the member cluster.
	//
	// +kubebuilder:validation:Optional
	Manifests []Manifest `json:"manifests,omitempty"`

	// The strategy to synchronize the resources to the member cluster.
	//
	// +kubebuilder:validation:Optional
	SyncStrategy *SyncStrategy `json:"syncStrategy,omitempty"`
}

type Manifest struct {
	// The manifest data.
	//
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	runtime.RawExtension `json:",inline"`
}

type WorkStatus struct {
	// A list of observed conditions of the work object.
	//
	// +kubebuilder:validation:Optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// The observed status of each manifest in the work object.
	//
	// +kubebuilder:validation:Optional
	Manifests []PerManifestStatus `json:"manifests,omitempty"`
}

type PerManifestStatus struct {
	// The identifier of the resource represented by the manifest.
	//
	// +kubebuilder:validation:Required
	Identifier ManifestIdentifier `json:"identifier,omitempty"`

	// A list of observed conditions of the manifest.
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

type DiffDetails struct {
	// The generation of the resource, as seen on the member cluster side.
	//
	// If set to nil, the resource has not been created yet on the member cluster.
	//
	// +kubebuilder:validation:Optional
	ObservedInMemberClusterGeneration *int64 `json:"observedInMemberClusterGeneration,omitempty"`

	// The timestamp when the diffs are first detected.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	FirstDiffedObservedTimestamp metav1.Time `json:"firstDiffedObservedTimestamp"`

	// The diffs observed between the state of the resource on the hub cluster and on the member cluster
	// side. A diff is reported as a JSON path and the values at the path on both sides.
	//
	// +kubebuilder:validation:Optional
	ObservedDiffs []PatchDetail `json:"observedDiffs,omitempty"`
}

type PatchDetail struct {
	// The JSON path that points to a field that has diffed.
	// +kubebuilder:validation:Required
	Path string `json:"path"`

	// The value at the JSON path from the member cluster side.
	//
	// If empty, the JSON path does not exist on the member cluster side.
	//
	// +kubebuilder:validation:Optional
	ValueInMember string `json:"valueInMember,omitempty"`

	// The value at the JSON path from the hub cluster side.
	//
	// If empty, the JSON path does not exist on the hub cluster side.
	//
	// +kubebuilder:validation:Optional
	ValueInHub string `json:"valueInHub,omitempty"`
}

type ManifestIdentifier struct {
	// The ordinal of the manifest.
	//
	// +kubebuilder:validation:Required
	Ordinal int `json:"ordinal"`

	// The namespace of the manifest.
	//
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`

	// The name of the manifest.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// The API group, version, kind, and resource of the manifest.

	// +kubebuilder:validation:Optional
	APIGroup string `json:"apiGroup,omitempty"`

	// +kubebuilder:validation:Required
	APIVersion string `json:"apiVersion,omitempty"`

	// +kubebuilder:validation:Required
	Kind string `json:"kind,omitempty"`

	// +kubebuilder:validation:Required
	Resource string `json:"resource,omitempty"`
}

// WorkList contains a list of Work.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type WorkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Work `json:"items"`
}

// Set up the API types with the scheme builder.
func init() {
	SchemeBuilder.Register(&Work{}, &WorkList{})
}
