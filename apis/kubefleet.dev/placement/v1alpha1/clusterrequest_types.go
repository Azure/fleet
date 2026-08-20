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
	ClusterRequestCondTypeCompleted = "Completed"
)

// ClusterRequest is a KubeFleet API that represents a request for a member cluster to be provisioned.
// It is created by KubeFleet when it fails to find a member cluster that can fulfill some scheduling
// requirements as specified in a PlacementPolicy or ClusterPlacementPolicy object.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={kubefleet, kubefleet-placement}
// +kubebuilder:storageversion
type ClusterRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the cluster request.
	// +kubebuilder:validation:Required
	Spec ClusterRequestSpec `json:"spec,omitempty"`

	// The observed status of the cluster request.
	// +kubebuilder:validation:Optional
	Status ClusterRequestStatus `json:"status,omitempty"`
}

type ClusterRequestSpec struct {
	// The reference to the placement policy that submits the cluster request.
	//
	// This field is immutable after creation.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="the placementPolicyRef field is immutable"
	PlacementPolicyRef *ObjectReference `json:"placementPolicyRef"`

	// The cluster selector terms that describe the requirements for a new member cluster.
	//
	// If not specified, any member cluster can satisfy the request.
	//
	// This field is immutable after creation.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="the clusterSelectorTerms field is immutable"
	ClusterSelectorTerms []ClusterLabelAndPropertySelectorTerm `json:"clusterSelectorTerms,omitempty"`
}

type ClusterRequestStatus struct {
	// A list of observed conditions of the cluster request.
	//
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// The name of the cluster that has been provisioned for this cluster request, if any.
	//
	// +kubebuilder:validation:Optional
	ProvisionedClusterName *string `json:"provisionedClusterName,omitempty"`

	// The last observed most recent creation timestamp across all the member clusters. This field is used
	// as an expedient solution to verify if a cluster request is still valid for consideration, i.e.,
	// if the currently observed most recent cluster creation timestamp is later than this timestamp in the
	// status, a new member cluster must have been created after the cluster request was created,
	// and thus the cluster request should be considered stale and can be ignored. The placement policy
	// that submits the cluster request is responsible for updating this field if the new cluster does not
	// meet the need of the associated cluster selector, so that the cluster request can be re-evaluated again;
	// it may instead withdraw the cluster request if the new cluster has fulfilled the associated cluster selector.
	//
	// +kubebuilder:validation:Optional
	LastObservedMostRecentClusterCreationTimestamp *metav1.Time `json:"lastObservedMostRecentClusterCreationTimestamp,omitempty"`
}

// ClusterRequestList contains a list of ClusterRequest.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClusterRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterRequest{}, &ClusterRequestList{})
}
