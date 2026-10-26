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
	"k8s.io/apimachinery/pkg/util/intstr"
)

type StageTaskType string

const (
	StageTaskTypeTimedWait StageTaskType = "TimedWait"
	StageTaskTypeApproval  StageTaskType = "Approval"
)

// StagedUpdateStrategy is the KubeFleet API that defines how to perform a staged rollout of resource changes
// for a placement policy; the API dictates how clusters are grouped into stages, how the rollout is performed
// within each stage, and which tasks to execute before and after each stage.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={kubefleet,kubefleet-rollout}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
type StagedUpdateStrategy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the StagedUpdateStrategy API object.
	//
	// +kubebuilder:validation:Required
	Spec StagedUpdateStrategySpec `json:"spec"`
}

// ClusterStagedUpdateStrategy is the KubeFleet API that defines how to perform a staged rollout of resource changes
// for a cluster placement policy; the API dictates how clusters are grouped into stages, how the rollout is performed
// within each stage, and which tasks to execute before and after each stage.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={kubefleet,kubefleet-rollout}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
type ClusterStagedUpdateStrategy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the ClusterStagedUpdateStrategy API object.
	//
	// +kubebuilder:validation:Required
	Spec StagedUpdateStrategySpec `json:"spec"`
}

type StagedUpdateStrategySpec struct {
	// The stages that KubeFleet rolls out resource changes to, in the order they are listed.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=31
	// +kubebuilder:validation:XValidation:rule="self.all(s, self.exists_one(t, t.name == s.name))",message="stage names must be unique"
	Stages []Stage `json:"stages"`
}

type Stage struct {
	// The name of the stage. It must be unique within the staged update strategy and may only consist of
	// lowercase alphanumeric characters.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[a-z0-9]+$"
	Name string `json:"name"`

	// The label selector that selects clusters to be included in the stage.
	//
	// Note that if a cluster is selected by multiple stages, it will only be included in the first stage that selects it.
	//
	// If set to nil, the stage includes no clusters at all. If set to an empty selector, the stage includes all applicable clusters.
	//
	// +kubebuilder:validation:Optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// The label key used to sort clusters in the stage. KubeFleet rolls out resource changes to clusters in the stage
	// based on the sorted order.
	//
	// Specifically, KubeFleet will read the value of the label key for each cluster in the stage, and sort clusters in ascending
	// order based on the label value, interpreted as an integer. If the label is missing on any cluster, an error will be raised.
	//
	// If this field is not set, KubeFleet will sort clusters in the stage based on the ascending lexical order of their names.
	//
	// +kubebuilder:validation:Optional
	SortingLabelKey *string `json:"sortingLabelKey,omitempty"`

	// The maximum number of clusters that can be rolled out concurrently within this stage.
	//
	// This field accepts either an integer value (e.g., 5), or a percentage value (e.g., "50%"). For percentage values,
	// the concurrency number is calculated based on the total number of clusters in the stage, with fractional results rounded down.
	//
	// A minimum concurrency of 1 is enforced.
	//
	// Defaults to 1.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern="^(100|[1-9][0-9]?)%$"
	// +kubebuilder:validation:XValidation:rule="self == null || type(self) != int || self >= 1",message="maxConcurrency must be at least 1"
	MaxConcurrency *intstr.IntOrString `json:"maxConcurrency,omitempty"`

	// A list of tasks to execute after the stage rollout is completed. The tasks run in parallel; a staged update will only
	// proceed to the next stage after all tasks are completed.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:rule="self.filter(e, e.type == 'Approval').size() <= 1",message="afterStageTasks cannot have more than one Approval task"
	// +kubebuilder:validation:XValidation:rule="self.filter(e, e.type == 'TimedWait').size() <= 1",message="afterStageTasks cannot have more than one TimedWait task"
	// +kubebuilder:validation:XValidation:rule="!self.exists(e, e.type == 'Approval' && has(e.waitTime))",message="waitTime does not apply to an Approval task in afterStageTasks"
	// +kubebuilder:validation:XValidation:rule="!self.exists(e, e.type == 'TimedWait' && !has(e.waitTime))",message="waitTime is required for a TimedWait task in afterStageTasks"
	AfterStageTasks []StageTask `json:"afterStageTasks,omitempty"`

	// A list of tasks to execute before the stage rollout starts. The tasks run in parallel; the current stage will only start
	// after all tasks are completed.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:validation:XValidation:rule="!self.exists(e, e.type == 'Approval' && has(e.waitTime))",message="waitTime does not apply to an Approval task in beforeStageTasks"
	// +kubebuilder:validation:XValidation:rule="!self.exists(e, e.type == 'TimedWait')",message="beforeStageTasks cannot include a TimedWait task"
	BeforeStageTasks []StageTask `json:"beforeStageTasks,omitempty"`
}

type StageTask struct {
	// The type of the stage task. Currently, the supported task types are TimedWait and Approval.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=TimedWait;Approval
	Type StageTaskType `json:"type"`

	// The time to wait in a TimedWait task.
	//
	// Specify the time period using hours (h), minutes (m), and seconds (s) units, e.g., "1h30m" for 1 hour and 30 minutes.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^(?:(?:0|[1-9][0-9]*)(\\.[0-9]+)?(?:s|m|h))+$"
	WaitTime *metav1.Duration `json:"waitTime,omitempty"`
}

// The list objects for StagedUpdateStrategy and ClusterStagedUpdateStrategy APIs.

// StagedUpdateStrategyList contains a list of StagedUpdateStrategy API objects.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type StagedUpdateStrategyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []StagedUpdateStrategy `json:"items"`
}

// ClusterStagedUpdateStrategyList contains a list of ClusterStagedUpdateStrategy API objects.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterStagedUpdateStrategyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClusterStagedUpdateStrategy `json:"items"`
}

// Set up the API types with the scheme builder.
func init() {
	SchemeBuilder.Register(&StagedUpdateStrategy{}, &StagedUpdateStrategyList{})
	SchemeBuilder.Register(&ClusterStagedUpdateStrategy{}, &ClusterStagedUpdateStrategyList{})
}
