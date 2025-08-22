/*
Copyright 2025 The KubeFleet Authors.

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

package v1beta1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubefleet-dev/kubefleet/apis"
)

const (
	// PolicyIndexLabel is the label that indicate the policy snapshot index of a cluster policy.
	PolicyIndexLabel = FleetPrefix + "policy-index"

	// PolicySnapshotNameFmt is clusterPolicySnapshot name format: {CRPName}-{PolicySnapshotIndex}.
	PolicySnapshotNameFmt = "%s-%d"

	// NumberOfClustersAnnotation is the annotation that indicates how many clusters should be selected for selectN placement type.
	NumberOfClustersAnnotation = FleetPrefix + "number-of-clusters"
)

// make sure the PolicySnapshotObj and PolicySnapshotList interfaces are implemented by the
// ClusterSchedulingPolicySnapshot and SchedulingPolicySnapshot types.
var _ PolicySnapshotObj = &ClusterSchedulingPolicySnapshot{}
var _ PolicySnapshotObj = &SchedulingPolicySnapshot{}
var _ PolicySnapshotList = &ClusterSchedulingPolicySnapshotList{}
var _ PolicySnapshotList = &SchedulingPolicySnapshotList{}

// A PolicySnapshotSpecGetterSetter offers methods to get and set the policy snapshot spec.
// +kubebuilder:object:generate=false
type PolicySnapshotSpecGetterSetter interface {
	GetPolicySnapshotSpec() *SchedulingPolicySnapshotSpec
	SetPolicySnapshotSpec(SchedulingPolicySnapshotSpec)
}

// A PolicySnapshotStatusGetterSetter offers methods to get and set the policy snapshot status.
// +kubebuilder:object:generate=false
type PolicySnapshotStatusGetterSetter interface {
	GetPolicySnapshotStatus() *SchedulingPolicySnapshotStatus
	SetPolicySnapshotStatus(SchedulingPolicySnapshotStatus)
}

// A PolicySnapshotObj offers an abstract way to work with a fleet policy snapshot object.
// +kubebuilder:object:generate=false
type PolicySnapshotObj interface {
	apis.ConditionedObj
	PolicySnapshotSpecGetterSetter
	PolicySnapshotStatusGetterSetter
}

// PolicySnapshotListItemGetter offers a method to get the list of PolicySnapshotObj.
// +kubebuilder:object:generate=false
type PolicySnapshotListItemGetter interface {
	GetPolicySnapshotObjs() []PolicySnapshotObj
}

// A PolicySnapshotList offers an abstract way to work with a list of fleet policy snapshot objects.
// +kubebuilder:object:generate=false
type PolicySnapshotList interface {
	client.ObjectList
	PolicySnapshotListItemGetter
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",shortName=csps,categories={fleet,fleet-placement}
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterSchedulingPolicySnapshot is used to store a snapshot of cluster placement policy.
// Its spec is immutable.
// The naming convention of a ClusterSchedulingPolicySnapshot is {CRPName}-{PolicySnapshotIndex}.
// PolicySnapshotIndex will begin with 0.
// Each snapshot must have the following labels:
//   - `CRPTrackingLabel` which points to its owner CRP.
//   - `PolicyIndexLabel` which is the index of the policy snapshot.
//   - `IsLatestSnapshotLabel` which indicates whether the snapshot is the latest one.
type ClusterSchedulingPolicySnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of SchedulingPolicySnapshot.
	// +required
	Spec SchedulingPolicySnapshotSpec `json:"spec"`

	// The observed status of SchedulingPolicySnapshot.
	// +optional
	Status SchedulingPolicySnapshotStatus `json:"status,omitempty"`
}

// GetPolicySnapshotSpec returns the policy snapshot spec.
func (m *ClusterSchedulingPolicySnapshot) GetPolicySnapshotSpec() *SchedulingPolicySnapshotSpec {
	return &m.Spec
}

// SetPolicySnapshotSpec sets the policy snapshot spec.
func (m *ClusterSchedulingPolicySnapshot) SetPolicySnapshotSpec(spec SchedulingPolicySnapshotSpec) {
	spec.DeepCopyInto(&m.Spec)
}

// GetPolicySnapshotStatus returns the policy snapshot status.
func (m *ClusterSchedulingPolicySnapshot) GetPolicySnapshotStatus() *SchedulingPolicySnapshotStatus {
	return &m.Status
}

// SetPolicySnapshotStatus sets the policy snapshot status.
func (m *ClusterSchedulingPolicySnapshot) SetPolicySnapshotStatus(status SchedulingPolicySnapshotStatus) {
	status.DeepCopyInto(&m.Status)
}

// SchedulingPolicySnapshotSpec defines the desired state of SchedulingPolicySnapshot.
type SchedulingPolicySnapshotSpec struct {
	// Policy defines how to select member clusters to place the selected resources.
	// If unspecified, all the joined member clusters are selected.
	// +optional
	Policy *PlacementPolicy `json:"policy,omitempty"`

	// PolicyHash is the sha-256 hash value of the Policy field.
	// +required
	PolicyHash []byte `json:"policyHash"`
}

// Tolerations returns tolerations for SchedulingPolicySnapshotSpec to handle nil policy case.
func (s *SchedulingPolicySnapshotSpec) Tolerations() []Toleration {
	if s.Policy != nil {
		return s.Policy.Tolerations
	}
	return nil
}

// SchedulingPolicySnapshotStatus defines the observed state of SchedulingPolicySnapshot.
type SchedulingPolicySnapshotStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge

	// ObservedCRPGeneration is the generation of the resource placement which the scheduler uses to perform
	// the scheduling cycle and prepare the scheduling status.
	// +required
	ObservedCRPGeneration int64 `json:"observedCRPGeneration"`

	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for SchedulingPolicySnapshot.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`

	// +kubebuilder:validation:MaxItems=1000
	// ClusterDecisions contains a list of names of member clusters considered by the scheduler.
	// Note that all the selected clusters must present in the list while not all the
	// member clusters are guaranteed to be listed due to the size limit. We will try to
	// add the clusters that can provide the most insight to the list first.
	// +optional
	ClusterDecisions []ClusterDecision `json:"targetClusters,omitempty"`
}

// SchedulingPolicySnapshotConditionType identifies a specific condition of the SchedulingPolicySnapshot.
type SchedulingPolicySnapshotConditionType string

const (
	// 	Scheduled indicates the scheduled condition of the given SchedulingPolicySnapshot.
	// Its condition status can be one of the following:
	// - "True" means we have successfully scheduled corresponding SchedulingPolicySnapshot to fully satisfy the
	// placement requirement.
	// - "False" means we did not fully satisfy the placement requirement of the corresponding SchedulingPolicySnapshot.
	// - "Unknown" means the status of the scheduling is unknown.
	PolicySnapshotScheduled SchedulingPolicySnapshotConditionType = "Scheduled"
)

// ClusterDecision represents a decision from a placement
// An empty ClusterDecision indicates it is not scheduled yet.
type ClusterDecision struct {
	// ClusterName is the name of the ManagedCluster. If it is not empty, its value should be unique cross all
	// placement decisions for the Placement.
	// +kubebuilder:validation:Required
	// +required
	ClusterName string `json:"clusterName"`

	// Selected indicates if this cluster is selected by the scheduler.
	// +required
	Selected bool `json:"selected"`

	// ClusterScore represents the score of the cluster calculated by the scheduler.
	// +optional
	ClusterScore *ClusterScore `json:"clusterScore"`

	// Reason represents the reason why the cluster is selected or not.
	// +required
	Reason string `json:"reason"`
}

// ClusterScore represents the score of the cluster calculated by the scheduler.
type ClusterScore struct {
	// AffinityScore represents the affinity score of the cluster calculated by the last
	// scheduling decision based on the preferred affinity selector.
	// An affinity score may not present if the cluster does not meet the required affinity.
	// +optional
	AffinityScore *int32 `json:"affinityScore,omitempty"`

	// TopologySpreadScore represents the priority score of the cluster calculated by the last
	// scheduling decision based on the topology spread applied to the cluster.
	// A priority score may not present if the cluster does not meet the topology spread.
	// +optional
	TopologySpreadScore *int32 `json:"priorityScore,omitempty"`
}

// ClusterSchedulingPolicySnapshotList contains a list of ClusterSchedulingPolicySnapshot.
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterSchedulingPolicySnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterSchedulingPolicySnapshot `json:"items"`
}

// SetConditions sets the given conditions on the ClusterSchedulingPolicySnapshot.
func (m *ClusterSchedulingPolicySnapshot) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

// GetCondition returns the condition of the given type if exists.
func (m *ClusterSchedulingPolicySnapshot) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

// GetPolicySnapshotObjs returns the list of PolicySnapshotObj from the ClusterSchedulingPolicySnapshotList.
func (c *ClusterSchedulingPolicySnapshotList) GetPolicySnapshotObjs() []PolicySnapshotObj {
	objs := make([]PolicySnapshotObj, 0, len(c.Items))
	for i := range c.Items {
		objs = append(objs, &c.Items[i])
	}
	return objs
}

// +genclient
// +genclient:Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced",shortName=sps,categories={fleet,fleet-placement}
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SchedulingPolicySnapshot is used to store a snapshot of cluster placement policy.
// Its spec is immutable.
// The naming convention of a SchedulingPolicySnapshot is {RPName}-{PolicySnapshotIndex}.
// PolicySnapshotIndex will begin with 0.
// Each snapshot must have the following labels:
//   - `CRPTrackingLabel` which points to its placement owner.
//   - `PolicyIndexLabel` which is the index of the policy snapshot.
//   - `IsLatestSnapshotLabel` which indicates whether the snapshot is the latest one.
type SchedulingPolicySnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of SchedulingPolicySnapshot.
	// +required
	Spec SchedulingPolicySnapshotSpec `json:"spec"`

	// The observed status of SchedulingPolicySnapshot.
	// +optional
	Status SchedulingPolicySnapshotStatus `json:"status,omitempty"`
}

// GetPolicySnapshotSpec returns the policy snapshot spec.
func (m *SchedulingPolicySnapshot) GetPolicySnapshotSpec() *SchedulingPolicySnapshotSpec {
	return &m.Spec
}

// SetPolicySnapshotSpec sets the policy snapshot spec.
func (m *SchedulingPolicySnapshot) SetPolicySnapshotSpec(spec SchedulingPolicySnapshotSpec) {
	spec.DeepCopyInto(&m.Spec)
}

// GetPolicySnapshotStatus returns the policy snapshot status.
func (m *SchedulingPolicySnapshot) GetPolicySnapshotStatus() *SchedulingPolicySnapshotStatus {
	return &m.Status
}

// SetPolicySnapshotStatus sets the policy snapshot status.
func (m *SchedulingPolicySnapshot) SetPolicySnapshotStatus(status SchedulingPolicySnapshotStatus) {
	status.DeepCopyInto(&m.Status)
}

// SchedulingPolicySnapshotList contains a list of SchedulingPolicySnapshotList.
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type SchedulingPolicySnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SchedulingPolicySnapshot `json:"items"`
}

// SetConditions sets the given conditions on the ClusterSchedulingPolicySnapshot.
func (m *SchedulingPolicySnapshot) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

// GetCondition returns the condition of the given type if exists.
func (m *SchedulingPolicySnapshot) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

// GetPolicySnapshotObjs returns the list of PolicySnapshotObj from the SchedulingPolicySnapshotList.
func (c *SchedulingPolicySnapshotList) GetPolicySnapshotObjs() []PolicySnapshotObj {
	objs := make([]PolicySnapshotObj, 0, len(c.Items))
	for i := range c.Items {
		objs = append(objs, &c.Items[i])
	}
	return objs
}

func init() {
	SchemeBuilder.Register(&ClusterSchedulingPolicySnapshot{}, &ClusterSchedulingPolicySnapshotList{}, &SchedulingPolicySnapshot{}, &SchedulingPolicySnapshotList{})
}
