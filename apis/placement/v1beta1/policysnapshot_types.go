/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// PolicyIndexLabel is the label that indicate the policy snapshot index of a cluster policy.
	PolicyIndexLabel = fleetPrefix + "policy-index"

	// PolicySnapshotNameFmt is clusterPolicySnapshot name format: {CRPName}-{PolicySnapshotIndex}.
	PolicySnapshotNameFmt = "%s-%d"

	// NumberOfClustersAnnotation is the annotation that indicates how many clusters should be selected for selectN placement type.
	NumberOfClustersAnnotation = fleetPrefix + "number-of-clusters"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",shortName=pss,categories={fleet,fleet-placement}
// +kubebuilder:subresource:status
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

// SchedulingPolicySnapshotStatus defines the observed state of SchedulingPolicySnapshot.
type SchedulingPolicySnapshotStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge

	// ObservedCRPGeneration is the generation of the CRP which the scheduler uses to perform
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

func init() {
	SchemeBuilder.Register(&ClusterSchedulingPolicySnapshot{}, &ClusterSchedulingPolicySnapshotList{})
}
