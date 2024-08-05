/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",categories={fleet,fleet-placement}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StageRollout defines a group of override policies about how to override the selected cluster scope resources
// to target clusters.
type StageRollout struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of StageRollout.
	// +required
	Spec StageRolloutSpec `json:"spec"`

	// The observed status of StageRollout.
	// +required
	Status StageRolloutStatus `json:"status"`
}

// StageRolloutSpec defines the desired the rollout sequence stage by stage.
type StageRolloutSpec struct {
	// Stage rollout configurations for each rollout stage.
	// +required
	StageRollout []StageRolloutConfig `json:"stages"`

	// The wait time between each stage is completed.
	// +optional
	WaitTimeBetweenStage *metav1.Duration `json:"waitTimeBetweenStage,omitempty"`

	// TODO: Add alerting configuration.

	// TODO: Add health-check configuration.
}

// StageRolloutConfig describes a single rollout stage group configuration.
type StageRolloutConfig struct {
	// The name of the stage. This should be unique within the StageRollout.
	// +required
	Name string `json:"name"`

	// LabelSelector is a label query over all the joined member clusters. Clusters matching the query are selected.
	// We don't check if there are overlap between clusters selected by different stages.
	LabelSelector *metav1.LabelSelector `json:"labelSelector"`

	// The label key used to sort the selected clusters.
	// The clusters within the stage are updated following the rule below:
	//   - primary: Ascending order based on the value of the label key, interpreted as integers if present.
	//   - secondary: Ascending order based on the name of the cluster if the label key is absent.
	// +optional
	SortingLabelKey *string `json:"sortingLabelKey,omitempty"`

	// The maximum number of clusters that can be updated simultaneously within this stage
	// default is 1.
	// +optional
	MaxParallel *intstr.IntOrString `json:"maxParallel,omitempty"`

	// The wait time after all the clusters in this stage are updated.
	// This will override the global waitTimeBetweenStage for this specific stage
	// +optional
	WaitTime *metav1.Duration `json:"waitTime,omitempty"`
}

// StageRolloutStatus defines the observed state of the StageRollout.
type StageRolloutStatus struct {
	// The stage that is in the middle of the rollout.
	CurrentStage int `json:"stage"`

	// the clusters that are in the current stage.
	ClustersInCurrentStage []string `json:"clustersInCurrentStage"`

	// the clusters that are actively updating.
	// +optional
	CurrentUpdatingClusters []string `json:"currentUpdatingClusters,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	//
	// Conditions is an array of current observed conditions for ClusterResourceBinding.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`
}

// StageRolloutConditionType identifies a specific condition of the StageRollout.
type StageRolloutConditionType string

const (
	// StageRollingOut indicates whether the stage is rolling out normally.
	// Its condition status can be one of the following:
	// - "True" means the stage is rolling out.
	// - "False" means the stage rolling out is not progressing.
	// - "Unknown" means it is unknown.
	StageRollingOut StageRolloutConditionType = "StageRollingOut"

	// StageRolloutWaiting indicates whether the stage is waiting to be rolled out.
	// Its condition status can be one of the following:
	// - "True" means the staging is waiting to start to be rolled out.
	// - "False" means the staging is not waiting to start rollout.
	// - "Unknown" means it is unknown.
	StageRolloutWaiting StageRolloutConditionType = "RolloutWaiting"

	// TODO: add verification condition

	// TODO: add alerting condition
)

// StageRolloutList contains a list of StageRollout.
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type StageRolloutList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StageRollout `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&StageRollout{}, &StageRolloutList{},
	)
}
