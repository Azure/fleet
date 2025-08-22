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
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubefleet-dev/kubefleet/apis"
)

const (
	// ResourceIndexLabel is the label that indicate the resource snapshot index of a cluster resource snapshot.
	ResourceIndexLabel = FleetPrefix + "resource-index"

	// ResourceGroupHashAnnotation is the annotation that contains the value of the sha-256 hash
	// value of all the snapshots belong to the same snapshot index.
	ResourceGroupHashAnnotation = FleetPrefix + "resource-hash"

	// NumberOfEnvelopedObjectsAnnotation is the annotation that contains the number of the enveloped objects in the resource snapshot group.
	NumberOfEnvelopedObjectsAnnotation = FleetPrefix + "number-of-enveloped-object"

	// NumberOfResourceSnapshotsAnnotation is the annotation that contains the total number of resource snapshots.
	NumberOfResourceSnapshotsAnnotation = FleetPrefix + "number-of-resource-snapshots"

	// SubindexOfResourceSnapshotAnnotation is the annotation to store the subindex of resource snapshot in the group.
	SubindexOfResourceSnapshotAnnotation = FleetPrefix + "subindex-of-resource-snapshot"

	// NextResourceSnapshotCandidateDetectionTimeAnnotation is the annotation to store the time of next resourceSnapshot candidate detected by the controller.
	NextResourceSnapshotCandidateDetectionTimeAnnotation = FleetPrefix + "next-resource-snapshot-candidate-detection-time"

	// ResourceSnapshotNameFmt is resourcePolicySnapshot name format: {CRPName}-{resourceIndex}-snapshot.
	ResourceSnapshotNameFmt = "%s-%d-snapshot"

	// ResourceSnapshotNameWithSubindexFmt is resourcePolicySnapshot name with subindex format: {CRPName}-{resourceIndex}-{subindex}.
	ResourceSnapshotNameWithSubindexFmt = "%s-%d-%d"
)

// make sure the ResourceSnapshotObj and ResourceSnapshotList interfaces are implemented by the
// ClusterResourceSnapshot and ResourceSnapshot types.
var _ ResourceSnapshotObj = &ClusterResourceSnapshot{}
var _ ResourceSnapshotObj = &ResourceSnapshot{}
var _ ResourceSnapshotObjList = &ClusterResourceSnapshotList{}
var _ ResourceSnapshotObjList = &ResourceSnapshotList{}

// A ResourceSnapshotSpecGetterSetter offers methods to get and set the resource snapshot spec.
// +kubebuilder:object:generate=false
type ResourceSnapshotSpecGetterSetter interface {
	GetResourceSnapshotSpec() *ResourceSnapshotSpec
	SetResourceSnapshotSpec(ResourceSnapshotSpec)
}

// A ResourceSnapshotStatusGetterSetter offers methods to get and set the resource snapshot status.
// +kubebuilder:object:generate=false
type ResourceSnapshotStatusGetterSetter interface {
	GetResourceSnapshotStatus() *ResourceSnapshotStatus
	SetResourceSnapshotStatus(ResourceSnapshotStatus)
}

// A ResourceSnapshotObj offers an abstract way to work with a resource snapshot object.
// +kubebuilder:object:generate=false
type ResourceSnapshotObj interface {
	apis.ConditionedObj
	ResourceSnapshotSpecGetterSetter
	ResourceSnapshotStatusGetterSetter
}

// A ResourceSnapshotSpec offers methods to get and set the resource snapshot spec.
// +kubebuilder:object:generate=false
type ResourceSnapshotListItemGetter interface {
	GetResourceSnapshotObjs() []ResourceSnapshotObj
}

// A ResourceSnapshotObjList offers an abstract way to work with a list of resource snapshot objects.
// +kubebuilder:object:generate=false
type ResourceSnapshotObjList interface {
	client.ObjectList
	ResourceSnapshotListItemGetter
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",shortName=crs,categories={fleet,fleet-placement}
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterResourceSnapshot is used to store a snapshot of selected resources by a resource placement policy.
// Its spec is immutable.
// We may need to produce more than one resourceSnapshot for all the resources a ResourcePlacement selected to get around the 1MB size limit of k8s objects.
// We assign an ever-increasing index for each such group of resourceSnapshots.
// The naming convention of a clusterResourceSnapshot is {CRPName}-{resourceIndex}-{subindex}
// where the name of the first snapshot of a group has no subindex part so its name is {CRPName}-{resourceIndex}-snapshot.
// resourceIndex will begin with 0.
// Each snapshot MUST have the following labels:
//   - `CRPTrackingLabel` which points to its owner CRP.
//   - `ResourceIndexLabel` which is the index  of the snapshot group.
//   - `IsLatestSnapshotLabel` which indicates whether the snapshot is the latest one.
//
// All the snapshots within the same index group must have the same ResourceIndexLabel.
//
// The first snapshot of the index group MUST have the following annotations:
//   - `NumberOfResourceSnapshotsAnnotation` to store the total number of resource snapshots in the index group.
//   - `ResourceGroupHashAnnotation` whose value is the sha-256 hash of all the snapshots belong to the same snapshot index.
//
// Each snapshot (excluding the first snapshot) MUST have the following annotations:
//   - `SubindexOfResourceSnapshotAnnotation` to store the subindex of resource snapshot in the group.
//
// Snapshot may have the following annotations to indicate the time of next resourceSnapshot candidate detected by the controller:
//   - `NextResourceSnapshotCandidateDetectionTimeAnnotation` to store the time of next resourceSnapshot candidate detected by the controller.
type ClusterResourceSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ResourceSnapshot.
	// +required
	Spec ResourceSnapshotSpec `json:"spec"`

	// The observed status of ResourceSnapshot.
	// +optional
	Status ResourceSnapshotStatus `json:"status,omitempty"`
}

// ResourceSnapshotSpec	defines the desired state of ResourceSnapshot.
type ResourceSnapshotSpec struct {
	// SelectedResources contains a list of resources selected by ResourceSelectors.
	// +required
	SelectedResources []ResourceContent `json:"selectedResources"`
}

// ResourceContent contains the content of a resource
type ResourceContent struct {
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	runtime.RawExtension `json:"-,inline"`
}

type ResourceSnapshotStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for ResourceSnapshot.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`
}

// ClusterResourceSnapshotList contains a list of ClusterResourceSnapshot.
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterResourceSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourceSnapshot `json:"items"`
}

// SetConditions sets the conditions for a ClusterResourceSnapshot.
func (m *ClusterResourceSnapshot) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

// GetCondition gets the condition for a ClusterResourceSnapshot.
func (m *ClusterResourceSnapshot) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

// GetResourceSnapshotSpec returns the resource snapshot spec.
func (m *ClusterResourceSnapshot) GetResourceSnapshotSpec() *ResourceSnapshotSpec {
	return &m.Spec
}

// SetResourceSnapshotSpec sets the resource snapshot spec.
func (m *ClusterResourceSnapshot) SetResourceSnapshotSpec(spec ResourceSnapshotSpec) {
	spec.DeepCopyInto(&m.Spec)
}

// GetResourceSnapshotStatus returns the resource snapshot status.
func (m *ClusterResourceSnapshot) GetResourceSnapshotStatus() *ResourceSnapshotStatus {
	return &m.Status
}

// SetResourceSnapshotStatus sets the resource snapshot status.
func (m *ClusterResourceSnapshot) SetResourceSnapshotStatus(status ResourceSnapshotStatus) {
	status.DeepCopyInto(&m.Status)
}

// ClusterResourceSnapshotList returns the list of ResourceSnapshotObj from the ResourceSnapshotList.
func (c *ClusterResourceSnapshotList) GetResourceSnapshotObjs() []ResourceSnapshotObj {
	objs := make([]ResourceSnapshotObj, 0, len(c.Items))
	for i := range c.Items {
		objs = append(objs, &c.Items[i])
	}
	return objs
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced",shortName=rs,categories={fleet,fleet-placement}
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ResourceSnapshot is used to store a snapshot of selected resources by a resource placement policy.
// Its spec is immutable.
// We may need to produce more than one resourceSnapshot for all the resources a ResourcePlacement selected to get around the 1MB size limit of k8s objects.
// We assign an ever-increasing index for each such group of resourceSnapshots.
// The naming convention of a resourceSnapshot is {RPName}-{resourceIndex}-{subindex}
// where the name of the first snapshot of a group has no subindex part so its name is {RPName}-{resourceIndex}-snapshot.
// resourceIndex will begin with 0.
// Each snapshot MUST have the following labels:
//   - `CRPTrackingLabel` which points to its owner resource placement.
//   - `ResourceIndexLabel` which is the index  of the snapshot group.
//
// The first snapshot of the index group MAY have the following labels:
//   - `IsLatestSnapshotLabel` which indicates whether the snapshot is the latest one.
//
// All the snapshots within the same index group must have the same ResourceIndexLabel.
//
// The first snapshot of the index group MUST have the following annotations:
//   - `NumberOfResourceSnapshotsAnnotation` to store the total number of resource snapshots in the index group.
//   - `ResourceGroupHashAnnotation` whose value is the sha-256 hash of all the snapshots belong to the same snapshot index.
//
// Each snapshot (excluding the first snapshot) MUST have the following annotations:
//   - `SubindexOfResourceSnapshotAnnotation` to store the subindex of resource snapshot in the group.
type ResourceSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ResourceSnapshot.
	// +required
	Spec ResourceSnapshotSpec `json:"spec"`

	// The observed status of ResourceSnapshot.
	// +optional
	Status ResourceSnapshotStatus `json:"status,omitempty"`
}

// ResourceSnapshotList contains a list of ResourceSnapshot.
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ResourceSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceSnapshot `json:"items"`
}

// SetConditions sets the conditions for a ResourceSnapshot.
func (m *ResourceSnapshot) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

// GetCondition gets the condition for a ResourceSnapshot.
func (m *ResourceSnapshot) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

// GetResourceSnapshotSpec returns the resource snapshot spec.
func (m *ResourceSnapshot) GetResourceSnapshotSpec() *ResourceSnapshotSpec {
	return &m.Spec
}

// SetResourceSnapshotSpec sets the resource snapshot spec.
func (m *ResourceSnapshot) SetResourceSnapshotSpec(spec ResourceSnapshotSpec) {
	spec.DeepCopyInto(&m.Spec)
}

// GetResourceSnapshotStatus returns the resource snapshot status.
func (m *ResourceSnapshot) GetResourceSnapshotStatus() *ResourceSnapshotStatus {
	return &m.Status
}

// SetResourceSnapshotStatus sets the resource snapshot status.
func (m *ResourceSnapshot) SetResourceSnapshotStatus(status ResourceSnapshotStatus) {
	status.DeepCopyInto(&m.Status)
}

// GetResourceSnapshotObjs returns the list of ResourceSnapshotObj from the ResourceSnapshotList.
func (c *ResourceSnapshotList) GetResourceSnapshotObjs() []ResourceSnapshotObj {
	objs := make([]ResourceSnapshotObj, 0, len(c.Items))
	for i := range c.Items {
		objs = append(objs, &c.Items[i])
	}
	return objs
}

func init() {
	SchemeBuilder.Register(&ClusterResourceSnapshot{}, &ClusterResourceSnapshotList{})
	SchemeBuilder.Register(&ResourceSnapshot{}, &ResourceSnapshotList{})
}
