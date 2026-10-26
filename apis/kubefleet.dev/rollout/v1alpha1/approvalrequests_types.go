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
	ApprovalRequestCondTypeApproved = "Approved"
)

// ApprovalRequest is the KubeFleet API that enables users to approve a rollout to a stage of clusters
// for a placement policy, when staged update is in use.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={kubefleet,kubefleet-rollout}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
type ApprovalRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the ApprovalRequest API object.
	//
	// +kubebuilder:validation:Required
	Spec ApprovalRequestSpec `json:"spec"`

	// The observed status of the ApprovalRequest API object.
	//
	// +kubebuilder:validation:Optional
	Status ApprovalRequestStatus `json:"status,omitempty"`
}

// ClusterApprovalRequest is the KubeFleet API that enables users to approve a rollout to a stage of clusters
// for a cluster placement policy, when staged update is in use.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={kubefleet,kubefleet-rollout}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
type ClusterApprovalRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the ClusterApprovalRequest API object.
	//
	// +kubebuilder:validation:Required
	Spec ApprovalRequestSpec `json:"spec"`

	// The observed status of the ClusterApprovalRequest API object.
	//
	// +kubebuilder:validation:Optional
	Status ApprovalRequestStatus `json:"status,omitempty"`
}

type ApprovalRequestSpec struct {
	// The name of the StagedUpdateRun API object that the approval request is associated with.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="stagedUpdateRunName is immutable"
	StagedUpdateRunName string `json:"stagedUpdateRunName"`

	// The name of the stage that the approval request is associated with in the staged update run.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[a-z0-9]+$"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="stageName is immutable"
	StageName string `json:"stageName"`
}

type ApprovalRequestStatus struct {
	// A list of observed conditions about the approval request.
	//
	// +kubebuilder:validation:Optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// The list objects for ApprovalRequest and ClusterApprovalRequest APIs.

// ApprovalRequestList contains a list of ApprovalRequest API objects.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ApprovalRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ApprovalRequest `json:"items"`
}

// ClusterApprovalRequestList contains a list of ClusterApprovalRequest API objects.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterApprovalRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClusterApprovalRequest `json:"items"`
}

// Set up the API types with the scheme builder.
func init() {
	SchemeBuilder.Register(&ApprovalRequest{}, &ApprovalRequestList{})
	SchemeBuilder.Register(&ClusterApprovalRequest{}, &ClusterApprovalRequestList{})
}
