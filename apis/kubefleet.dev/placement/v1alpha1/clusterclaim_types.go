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

const (
	ClusterClaimCondTypeCompleted = "Completed"

	// ClusterClaimPlacementPolicyNameLabel and ClusterClaimPlacementPolicyNamespaceLabel record the placement
	// policy that added a cluster claim. Cluster claims are cluster-scoped and cannot carry an
	// owner reference to a namespaced PlacementPolicy, so these labels are how KubeFleet — and
	// anyone querying claims, such as a provisioner watching the claims of a specific
	// placement — associates a claim with its policy. The namespace label is empty for claims
	// added by a ClusterPlacementPolicy.
	//
	// Object names may be longer than the 63-byte limit on label values: when a policy's name
	// does not fit, the label carries a shortened, collision-resistant form of it instead, and
	// selecting on the label with the policy's own name then matches nothing. The authoritative
	// identity is always spec.placementPolicyRef, so a consumer that must handle policies with
	// names longer than 63 bytes should list cluster claims and match on that field rather than
	// select on this label.
	ClusterClaimPlacementPolicyNameLabel      = "placement.kubefleet.dev/placement-policy-name"
	ClusterClaimPlacementPolicyNamespaceLabel = "placement.kubefleet.dev/placement-policy-namespace"
)

// ClusterClaim is a KubeFleet API that represents a claim for a new member cluster.
// It is created by KubeFleet when it fails to find a member cluster that can fulfill some scheduling
// requirements as specified in a PlacementPolicy or ClusterPlacementPolicy object.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={kubefleet, kubefleet-placement}
// +kubebuilder:storageversion
type ClusterClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the cluster claim.
	// +kubebuilder:validation:Required
	Spec ClusterClaimSpec `json:"spec,omitempty"`

	// The observed status of the cluster claim.
	// +kubebuilder:validation:Optional
	Status ClusterClaimStatus `json:"status,omitempty"`
}

// ClusterClaimSpec is the specification of a cluster claim.
//
// +kubebuilder:validation:XValidation:rule="has(self.clusterSelectorTerms) == has(oldSelf.clusterSelectorTerms)",message="the clusterSelectorTerms field cannot be added or removed after creation"
type ClusterClaimSpec struct {
	// The reference to the placement policy that adds the cluster claim.
	//
	// This field is immutable after creation.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="the placementPolicyRef field is immutable"
	PlacementPolicyRef *ObjectReference `json:"placementPolicyRef"`

	// The cluster selector terms that describe the requirements for a new member cluster.
	//
	// If not specified, any member cluster can satisfy the claim.
	//
	// This field is immutable after creation.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="the clusterSelectorTerms field is immutable"
	ClusterSelectorTerms []ClusterLabelAndPropertySelectorTerm `json:"clusterSelectorTerms,omitempty"`
}

type ClusterClaimStatus struct {
	// A list of observed conditions of the cluster claim.
	//
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// The name of the cluster that has been provisioned for this cluster claim, if any.
	//
	// A cluster claim asks for a single cluster: a provisioner fulfills it by provisioning one
	// cluster, recording its name here, and marking the Completed condition. A placement that still
	// needs more clusters than one claim provides is served one claim at a time -- once the
	// provisioned cluster becomes eligible, the completed claim is withdrawn and a new claim is
	// created, under the same deterministic name, for the next cluster. A provisioner that retries a
	// conflicting status write must therefore key the retry on the claim's identity (UID or resource
	// version), not on its name alone, so that a write meant for a completed claim is not applied to
	// the different, freshly created claim that has taken its name.
	//
	// +kubebuilder:validation:Optional
	ProvisionedClusterName *string `json:"provisionedClusterName,omitempty"`

	// The last observed most recent creation timestamp across all the member clusters. This field is used
	// as an expedient solution to verify if a cluster claim is still valid for consideration, i.e.,
	// if the currently observed most recent cluster creation timestamp is later than this timestamp in the
	// status, a new member cluster must have been created after the cluster claim was created,
	// and thus the cluster claim should be considered stale and can be ignored. The placement policy
	// that adds the cluster claim is responsible for updating this field if the new cluster does not
	// meet the need of the associated cluster selector, so that the cluster claim can be re-evaluated again;
	// it may instead withdraw the cluster claim if the new cluster has fulfilled the associated cluster selector.
	//
	// +kubebuilder:validation:Optional
	LastObservedMostRecentClusterCreationTimestamp *metav1.Time `json:"lastObservedMostRecentClusterCreationTimestamp,omitempty"`
}

// ClusterClaimList contains a list of ClusterClaim.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClusterClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterClaim{}, &ClusterClaimList{})
}
