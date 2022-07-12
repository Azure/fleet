/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",shortName=crp,categories={fleet-workload}
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterResourcePlacement is used to place cluster scoped resources or ALL the resources in a namespace
// onto one or more member clusters in a fleet. Users cannot select resources in a system reserved namespace.
// System reserved namespaces are: kube-system, fleet-system, fleet-work-*.
type ClusterResourcePlacement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec represents the desired behavior of ClusterResourcePlacement.
	// +required
	Spec ClusterResourcePlacementSpec `json:"spec"`

	// Most recently observed status of the ClusterResourcePlacement.
	// +optional
	Status ClusterResourcePlacementStatus `json:"status,omitempty"`
}

// ClusterResourcePlacementSpec represents the desired behavior of a ClusterResourcePlacement object.
type ClusterResourcePlacementSpec struct {
	// ResourceSelectors is used to select cluster scoped resources. The selectors are `ORed`.
	// kubebuilder:validation:MaxItems=100
	// +required
	ResourceSelectors []ClusterResourceSelector `json:"resourceSelectors"`

	// Policy represents the placement policy to select clusters to place all the selected resources.
	// Default is place to the entire fleet if this field is omitted.
	// +optional
	Policy PlacementPolicy `json:"policy,omitempty"`
}

// ClusterResourceSelector is used to specify cluster scoped resources to be selected.
// Note: When the cluster resource is of type `namespace`, ALL the resources in this namespace are selected.
// All the fields present in this structure are `ANDed`.
type ClusterResourceSelector struct {
	// Group is the group name of the target resource.
	// +required
	Group string `json:"group"`

	// Version is the version of the target resource.
	// +required
	Version string `json:"version"`

	// Kind is the kind of the target resources.
	// Note: When the `kind` field is `namespace` then all the resources inside that namespace is selected.
	// +required
	Kind string `json:"kind"`

	// Name is the name of the target resource.
	// Default is empty, which means selecting all resources.
	// This field is mutually exclusive with the `labelSelector` field.
	// +optional
	Name string `json:"name,omitempty"`

	// labelSelector spells out a label query over a set of resources.
	// This field is mutually exclusive with the `name` field.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// PlacementPolicy represents the rule for select clusters.
type PlacementPolicy struct {
	// ClusterNames is a request to schedule the selected resource to a list of member clusters.
	// If exists, we only place the resources within the clusters in this list.
	// kubebuilder:validation:MaxItems=100
	// +optional
	ClusterNames []string `json:"clusterNames,omitempty"`

	// Affinity represents the selected resources' scheduling constraints.
	// If not set, the entire fleet can be scheduling candidate.
	// +optional
	Affinity *Affinity `json:"Affinity,omitempty"`
}

// Affinity represents the filter to select clusters.
// The selectors in this struct are `ANDed`.
type Affinity struct {
	// ClusterAffinity describes cluster affinity scheduling rules for the resources.
	// +optional
	ClusterAffinity *ClusterAffinity `json:"clusterAffinity,omitempty"`
}

// ClusterAffinity represents the filter to select clusters.
type ClusterAffinity struct {
	// ClusterSelectorTerms is a list of cluster selector terms. The terms are `ORed`.
	// kubebuilder:validation:MaxItems=10
	// +required
	ClusterSelectorTerms []ClusterSelectorTerm `json:"clusterSelectorTerms"`
}

// ClusterSelectorTerm represents the requirements to selected clusters.
type ClusterSelectorTerm struct {
	// LabelSelector is a list of cluster requirements by cluster's labels.
	// kubebuilder:validation:MaxItems=10
	// +optional
	LabelSelector metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// ClusterResourcePlacementStatus defines the observed state of resource.
type ClusterResourcePlacementStatus struct {
	// Conditions field contains the overall condition statuses for this resource.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions"`

	// SelectedResources is a list of the resources the resource selector selects.
	// +optional
	SelectedResources []ResourceIdentifier `json:"selectedResources,omitempty"`

	// TargetClusters is a list of cluster names that this resource should run on.
	// +optional
	TargetClusters []string `json:"targetClusters,omitempty"`

	// FailedResourcePlacements is a list of all failed to place resources status.
	// kubebuilder:validation:MaxItems=1000
	// +optional
	FailedResourcePlacements []FailedResourcePlacement `json:"failedPlacements,omitempty"`
}

// ResourceIdentifier points to one resource we selected
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

	// Namespace is the namespace of the resource, the resource is cluster scoped if the value is empty.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// FailedResourcePlacement shows the failure details of a failed resource placement.
type FailedResourcePlacement struct {
	ResourceIdentifier `json:",inline"`

	// ClusterName is the name of the cluster that this resource is placed on.
	// +required
	ClusterName string `json:"clusterName"`

	// Condition contains the failed condition status for this failed to place resource.
	// +required
	Condition metav1.Condition `json:"condition"`
}

// ResourcePlacementConditionType identifies a specific condition on a workload.
type ResourcePlacementConditionType string

const (
	// ResourcePlacementConditionTypeScheduled indicates if we have successfully identified the set of clusters on which
	// the selected resources should run and created work CRs in the per-cluster namespace.
	// its conditionStatus can be "True" == Scheduled, "False" == Failed to Schedule
	ResourcePlacementConditionTypeScheduled ResourcePlacementConditionType = "Scheduled"

	// ResourcePlacementStatusConditionTypeApplied indicates if the referenced workload is applied on the selected member cluster.
	// its conditionStatus can be "True" == All referenced workloads are applied, "False" == Not all are applied
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

func init() {
	SchemeBuilder.Register(&ClusterResourcePlacement{}, &ClusterResourcePlacementList{})
}
