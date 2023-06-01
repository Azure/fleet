/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// PolicyIndexLabel is the label that indicate the policy snapshot index of a cluster policy.
	PolicyIndexLabel = labelPrefix + "policyIndex"

	// IsLatestSnapshotLabel tells if the policy snapshot is the latest one.
	IsLatestSnapshotLabel = labelPrefix + "isLatestSnapshot"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",shortName=pss,categories={fleet-workload}
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterPolicySnapshot is used to store a snapshot of cluster placement policy.
// It is immutable.
// It must have `CRPTrackingLabel`, `PolicyIndexLabel` and `IsLatestSnapshotLabel` labels.
type ClusterPolicySnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of PolicySnapShot.
	// +required
	Spec PolicySnapShotSpec `json:"spec"`

	// The observed status of PolicySnapShot.
	// +optional
	Status PolicySnapShotStatus `json:"status,omitempty"`
}

// PolicySnapShotSpec defines the desired state of PolicySnapShot.
type PolicySnapShotSpec struct {
	// Policy defines how to select member clusters to place the selected resources.
	// If unspecified, all the joined member clusters are selected.
	// +optional
	Policy *PlacementPolicy `json:"policy,omitempty"`

	// PolicyHash is the sha-256 hash value of the Policy field.
	// +required
	PolicyHash []byte `json:"policyHash"`
}

// PolicySnapShotStatus defines the observed state of PolicySnapShot.
type PolicySnapShotStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for PolicySnapShot.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`

	// +kubebuilder:validation:MaxItems=100
	// ClusterDecisions contains a list of names of member clusters considered by the scheduler.
	// Note that all the selected clusters must present in the list while not all the
	// member clusters are guaranteed to be listed.
	// +optional
	ClusterDecisions []ClusterDecision `json:"targetClusters,omitempty"`
}

// PolicySnapShotConditionType identifies a specific condition of the PolicySnapShot.
type PolicySnapShotConditionType string

const (
	// 	Scheduled indicates the scheduled condition of the given policySnapShot.
	// Its condition status can be one of the following:
	// - "True" means the corresponding policySnapShot is fully scheduled.
	// - "False" means the corresponding policySnapShot is not scheduled yet.
	// - "Unknown" means this policy does not have a full schedule yet.
	PolicySnapshotScheduled PolicySnapShotConditionType = "Scheduled"
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

// ClusterPolicySnapShotList contains a list of ClusterPolicySnapShot.
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterPolicySnapShotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterPolicySnapshot `json:"items"`
}

// SetConditions sets the given conditions on the ClusterPolicySnapshot.
func (m *ClusterPolicySnapshot) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

// GetCondition returns the condition of the given type if exists.
func (m *ClusterPolicySnapshot) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

func init() {
	SchemeBuilder.Register(&ClusterPolicySnapshot{}, &ClusterPolicySnapShotList{})
}
