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
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster",categories={fleet,fleet-placement}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StageRollout defines a stage by stage rollout policy that is applied to the ClusterResourcePlacement of the same name.
type StageRollout struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of StageRollout.
	// +required
	Spec StageRolloutSpec `json:"spec"`

	// The observed status of StageRollout.
	// +optional
	Status StageRolloutStatus `json:"status,omitempty"`
}

// StageRolloutSpec defines the desired the rollout sequence stage by stage.
type StageRolloutSpec struct {
	// Stage rollout configurations for each rollout stage.
	// +required
	Stages []StageRolloutConfig `json:"stages"`

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

	// The maximum number of clusters that can be updated simultaneously within this stage.
	// Note that an unsuccessful update cluster counts as one of the parallel updates which means
	// the rollout will stuck if the number of failed clusters is equal to the MaxParallel.
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
	// CurrentStages list the name of the clusters in each stage.
	// The clusters in each stage are ordered following its order to be rolled out.
	// The rollout will restart from the first stage if the exact order of stages
	// and the clusters in each stage are changed.
	// +required
	CurrentStages [][]string `json:"currentStages"`

	// CurrentStageIndex is the index of the stage that is in the middle of the rollout.
	// The index starts from 0. The index is -1 if the rollout is not started.
	// The index is the size of the Stages if the rollout is completed.
	// +required
	CurrentStageIndex int `json:"currentStageIndex"`

	// ResourceSnapshotIndex is the resource index that is currently being rolled out.
	// Resource index logically represents the generation of the selected resources.
	// We take a new snapshot of the selected resources whenever the selection or their content change.
	// Each snapshot has a different resource index.
	// Each time the resource index is updated, the rollout will restart from the first stage.
	// +optional
	ResourceSnapshotIndex string `json:"resourceSnapshotIndex"`

	// the clusters that are actively updating. It can be empty if the rollout is stopped.
	// +optional
	CurrentUpdatingClusters []string `json:"currentUpdatingClusters,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	//
	// Conditions is an array of current observed conditions for StageRollout.
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
