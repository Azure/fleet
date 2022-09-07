/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",shortName=crp,categories={fleet-workload}
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Scheduled")].status`,name="Scheduled",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Scheduled")].observedGeneration`,name="ScheduledGen",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Applied")].status`,name="Applied",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Applied")].observedGeneration`,name="AppliedGen",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterResourcePlacement is used to select cluster scoped resources and place them onto selected member clusters in a fleet.
// If a namespace is selected, ALL the resources under the namespace are placed to the target clusters.
// Note that you can't select the following resources:
// - reserved namespaces including: default, kube-* (reserved for Kubernetes system namespaces), fleet-* (reserved for fleet system namespaces).
// - reserved fleet resource types including: MemberCluster, InternalMemberCluster, ClusterResourcePlacement, MultiClusterService, ServiceImport, etc.
type ClusterResourcePlacement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ClusterResourcePlacement.
	// +optional
	Spec ClusterResourcePlacementSpec `json:"spec"`

	// The observed status of ClusterResourcePlacement.
	// +optional
	Status ClusterResourcePlacementStatus `json:"status,omitempty"`
}

// ClusterResourcePlacementSpec defines the desired state of ClusterResourcePlacement.
type ClusterResourcePlacementSpec struct {
	// kubebuilder:validation:MinItems=1
	// kubebuilder:validation:MaxItems=100

	// ResourceSelectors is an array of selectors used to select cluster scoped resources. The selectors are `ORed`.
	// You can have 1-100 selectors.
	// +required
	ResourceSelectors []ClusterResourceSelector `json:"resourceSelectors"`

	// Policy defines how to select member clusters to place the selected resources.
	// If unspecified, all the joined member clusters are selected.
	// +optional
	Policy *PlacementPolicy `json:"policy,omitempty"`
}

// ClusterResourceSelector is used to select cluster scoped resources as the target resources to be placed.
// If a namespace is selected, ALL the resources under the namespace are selected automatically.
// All the fields are `ANDed`. In other words, a resource must match all the fields to be selected.
type ClusterResourceSelector struct {
	// Group name of the cluster-scoped resource.
	// Use an empty string to select resources under the core API group (e.g., namespaces).
	// +required
	Group string `json:"group"`

	// Version of the cluster-scoped resource.
	// +required
	Version string `json:"version"`

	// Kind of the cluster-scoped resource.
	// Note: When `Kind` is `namespace`, ALL the resources under the selected namespaces are selected.
	// +required
	Kind string `json:"kind"`

	// You can only specify at most one of the following two fields: Name and LabelSelector.
	// If none is specified, all the cluster-scoped resources with the given group, version and kind are selected.

	// Name of the cluster-scoped resource.
	// +optional
	Name string `json:"name,omitempty"`

	// A label query over all the cluster-scoped resources. Resources matching the query are selected.
	// Note that namespace-scoped resources can't be selected even if they match the query.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// PlacementPolicy contains the rules to select target member clusters to place the selected resources to.
// Note that only clusters that are both joined and satisfying the rules will be selected.
//
// You can only specify at most one of the two fields: ClusterNames and Affinity.
// If none is specified, all the joined clusters are selected.
type PlacementPolicy struct {
	// kubebuilder:validation:MaxItems=100

	// ClusterNames contains a list of names of MemberCluster to place the selected resources to.
	// If the list is not empty, Affinity is ignored.
	// +optional
	ClusterNames []string `json:"clusterNames,omitempty"`

	// Affinity contains cluster affinity scheduling rules. Defines which member clusters to place the selected resources to.
	// +optional
	Affinity *Affinity `json:"affinity,omitempty"`
}

// Affinity is a group of cluster affinity scheduling rules. More to be added.
type Affinity struct {
	// ClusterAffinity contains cluster affinity scheduling rules for the selected resources.
	// +optional
	ClusterAffinity *ClusterAffinity `json:"clusterAffinity,omitempty"`
}

// ClusterAffinity contains cluster affinity scheduling rules for the selected resources.
type ClusterAffinity struct {
	// kubebuilder:validation:MaxItems=10

	// ClusterSelectorTerms is a list of cluster selector terms. The terms are `ORed`.
	// +optional
	ClusterSelectorTerms []ClusterSelectorTerm `json:"clusterSelectorTerms,omitempty"`
}

// ClusterSelectorTerm contains the requirements to select clusters.
type ClusterSelectorTerm struct {
	// LabelSelector is a label query over all the joined member clusters. Clusters matching the query are selected.
	// +required
	LabelSelector metav1.LabelSelector `json:"labelSelector"`
}

// ClusterResourcePlacementStatus defines the observed state of resource.
type ClusterResourcePlacementStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for ClusterResourcePlacement.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`

	// SelectedResources contains a list of resources selected by ResourceSelectors.
	// +optional
	SelectedResources []ResourceIdentifier `json:"selectedResources,omitempty"`

	// TargetClusters contains a list of names of member cluster selected by PlacementPolicy.
	// Note that the clusters must be both joined and meeting PlacementPolicy.
	// +optional
	TargetClusters []string `json:"targetClusters,omitempty"`

	// kubebuilder:validation:MaxItems=1000

	// FailedResourcePlacements is a list of all the resources failed to be placed to the given clusters.
	// Note that we only include 1000 failed resource placements even if there are more than 1000.
	// +optional
	FailedResourcePlacements []FailedResourcePlacement `json:"failedPlacements,omitempty"`
}

// ResourceIdentifier identifies one Kubernetes resource.
type ResourceIdentifier struct {
	// Group is the group name of the selected resource.
	// +required
	Group string `json:"group,omitempty"`

	// Version is the version of the selected resource.
	// +required
	Version string `json:"version,omitempty"`

	// Kind represents the Kind of the selected resources.
	// +required
	Kind string `json:"kind"`

	// Name of the target resource.
	// +required
	Name string `json:"name"`

	// Namespace is the namespace of the resource. Empty if the resource is cluster scoped.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// FailedResourcePlacement contains the failure details of a failed resource placement.
type FailedResourcePlacement struct {
	// The resource failed to be placed.
	// +required
	ResourceIdentifier `json:",inline"`

	// Name of the member cluster that the resource is placed to.
	// +required
	ClusterName string `json:"clusterName"`

	// The failed condition status.
	// +required
	Condition metav1.Condition `json:"condition"`
}

// ResourcePlacementConditionType defines a specific condition of a resource placement.
type ResourcePlacementConditionType string

const (
	// ResourcePlacementConditionTypeScheduled indicates whether we have selected >0 resources to be placed to >0 clusters and created work CRs under the corresponding per-cluster namespaces (i.e., fleet-member-<member-name>).
	// Its condition status can be one of the following:
	// - "True" means we have selected >0 resources and target >0 clusters and created the work CRs.
	// - "False" means we have selected 0 resources, 0 clusters, or failed to create the work CRs.
	// - "Unknown" otherwise.
	ResourcePlacementConditionTypeScheduled ResourcePlacementConditionType = "Scheduled"

	// ResourcePlacementStatusConditionTypeApplied indicates whether the selected member clusters have received the work CRs and applied the selected resources locally.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully applied to all the target clusters.
	// - "False" means some of them have failed.
	// - "Unknown" otherwise.
	ResourcePlacementStatusConditionTypeApplied ResourcePlacementConditionType = "Applied"
)

// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterResourcePlacementList contains a list of ClusterResourcePlacement.
type ClusterResourcePlacementList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourcePlacement `json:"items"`
}

func (m *ClusterResourcePlacement) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

func (m *ClusterResourcePlacement) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

func init() {
	SchemeBuilder.Register(&ClusterResourcePlacement{}, &ClusterResourcePlacementList{})
}
