/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const AppliedWorkKind = "AppliedWork"

// AppliedWorkSpec represents the desired configuration of AppliedWork.
type AppliedWorkSpec struct {
	// WorkName represents the name of the related work on the hub.
	// +kubebuilder:validation:Required
	// +required
	WorkName string `json:"workName"`

	// WorkNamespace represents the namespace of the related work on the hub.
	// +kubebuilder:validation:Required
	// +required
	WorkNamespace string `json:"workNamespace"`
}

// AppliedWorkStatus represents the current status of AppliedWork.
type AppliedWorkStatus struct {
	// AppliedResources represents a list of resources defined within the Work that are applied.
	// Only resources with valid GroupVersionResource, namespace, and name are suitable.
	// An item in this slice is deleted when there is no mapped manifest in Work.Spec or by finalizer.
	// The resource relating to the item will also be removed from managed cluster.
	// The deleted resource may still be present until the finalizers for that resource are finished.
	// However, the resource will not be undeleted, so it can be removed from this list and eventual consistency is preserved.
	// +optional
	AppliedResources []AppliedResourceMeta `json:"appliedResources,omitempty"`
}

// AppliedResourceMeta represents the group, version, resource, name and namespace of a resource.
// Since these resources have been created, they must have valid group, version, resource, namespace, and name.
type AppliedResourceMeta struct {
	WorkResourceIdentifier `json:",inline"`

	// UID is set on successful deletion of the Kubernetes resource by controller. The
	// resource might be still visible on the managed cluster after this field is set.
	// It is not directly settable by a client.
	// +optional
	UID types.UID `json:"uid,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={fleet-placement}
// +kubebuilder:object:root=true

// AppliedWork represents an applied work on managed cluster that is placed
// on a managed cluster. An appliedwork links to a work on a hub recording resources
// deployed in the managed cluster.
// When the agent is removed from managed cluster, cluster-admin on managed cluster
// can delete appliedwork to remove resources deployed by the agent.
// The name of the appliedwork must be the same as {work name}
// The namespace of the appliedwork should be the same as the resource applied on
// the managed cluster.
type AppliedWork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec represents the desired configuration of AppliedWork.
	// +kubebuilder:validation:Required
	// +required
	Spec AppliedWorkSpec `json:"spec"`

	// Status represents the current status of AppliedWork.
	// +optional
	Status AppliedWorkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AppliedWorkList contains a list of AppliedWork.
type AppliedWorkList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of works.
	// +listType=set
	Items []AppliedWork `json:"items"`
}
