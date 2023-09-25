/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// ClusterResourcePlacementCleanupFinalizer is a finalizer added by the CRP controller to all CRPs, to make sure
	// that the CRP controller can react to CRP deletions if necessary.
	ClusterResourcePlacementCleanupFinalizer = fleetPrefix + "crp-cleanup"

	// RevisionHistoryLimitDefaultValue is the default value of RevisionHistoryLimit.
	RevisionHistoryLimitDefaultValue = int32(10)

	// SchedulerCRPCleanupFinalizer is a finalizer addd by the scheduler to CRPs, to make sure
	// that all bindings derived from a CRP can be cleaned up after the CRP is deleted.
	SchedulerCRPCleanupFinalizer = fleetPrefix + "scheduler-cleanup"

	// DefaultMaxUnavailableValue is the default value of MaxUnavailable in the rolling update config.
	DefaultMaxUnavailableValue = "25%"

	// 	DefaultMaxSurgeValue is the default value of MaxSurge in the rolling update config.
	DefaultMaxSurgeValue = "25%"

	// DefaultUnavailablePeriodSeconds is the default period of time we consider a newly applied workload as unavailable.
	DefaultUnavailablePeriodSeconds = 60
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",shortName=crp,categories={fleet,fleet-placement}
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Scheduled")].status`,name="Scheduled",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Scheduled")].observedGeneration`,name="ScheduledGen",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Applied")].status`,name="Applied",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Applied")].observedGeneration`,name="AppliedGen",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterResourcePlacement is used to select cluster scoped resources, including built-in resources and custom resources,
// and placement them onto selected member clusters in a fleet.
//
// If a namespace is selected, ALL the resources under the namespace are placed to the target clusters.
// Note that you can't select the following resources:
//   - reserved namespaces including: default, kube-* (reserved for Kubernetes system namespaces),
//     fleet-* (reserved for fleet system namespaces).
//   - reserved fleet resource types including: MemberCluster, InternalMemberCluster, ClusterResourcePlacement,
//     ClusterSchedulingPolicySnapshot, ClusterResourceSnapshot, ClusterResourceBinding, etc.
//
// `ClusterSchedulingPolicySnapshot` and `ClusterResourceSnapshot` objects are created when there are changes in the
// system to keep the history of the changes affecting a `ClusterResourcePlacement`.
type ClusterResourcePlacement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ClusterResourcePlacement.
	// +required
	Spec ClusterResourcePlacementSpec `json:"spec"`

	// The observed status of ClusterResourcePlacement.
	// +optional
	Status ClusterResourcePlacementStatus `json:"status,omitempty"`
}

// ClusterResourcePlacementSpec defines the desired state of ClusterResourcePlacement.
type ClusterResourcePlacementSpec struct {
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=100

	// ResourceSelectors is an array of selectors used to select cluster scoped resources. The selectors are `ORed`.
	// You can have 1-100 selectors.
	// +required
	ResourceSelectors []ClusterResourceSelector `json:"resourceSelectors"`

	// Policy defines how to select member clusters to place the selected resources.
	// If unspecified, all the joined member clusters are selected.
	// +optional
	Policy *PlacementPolicy `json:"policy,omitempty"`

	// The rollout strategy to use to replace existing placement with new ones.
	// +optional
	// +patchStrategy=retainKeys
	Strategy RolloutStrategy `json:"strategy,omitempty"`

	// The number of old ClusterSchedulingPolicySnapshot or ClusterResourceSnapshot resources to retain to allow rollback.
	// This is a pointer to distinguish between explicit zero and not specified.
	// Defaults to 10.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=10
	// +optional
	RevisionHistoryLimit *int32 `json:"revisionHistoryLimit,omitempty"`
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

// PlacementPolicy contains the rules to select target member clusters to place the selected resources.
// Note that only clusters that are both joined and satisfying the rules will be selected.
//
// You can only specify at most one of the two fields: ClusterNames and Affinity.
// If none is specified, all the joined clusters are selected.
type PlacementPolicy struct {
	// Type of placement. Can be "PickAll", "PickN" or "PickFixed". Default is PickAll.
	// +kubebuilder:validation:Enum=PickAll;PickN;PickFixed
	// +kubebuilder:default=PickAll
	// +optional
	PlacementType PlacementType `json:"placementType,omitempty"`

	// +kubebuilder:validation:MaxItems=100
	// ClusterNames contains a list of names of MemberCluster to place the selected resources.
	// Only valid if the placement type is "PickFixed"
	// +optional
	ClusterNames []string `json:"clusterNames,omitempty"`

	// NumberOfClusters of placement. Only valid if the placement type is "PickN".
	// +kubebuilder:validation:Minimum=0
	// +optional
	NumberOfClusters *int32 `json:"numberOfClusters,omitempty"`

	// Affinity contains cluster affinity scheduling rules. Defines which member clusters to place the selected resources.
	// Only valid if the placement type is "PickAll" or "PickN".
	// +optional
	Affinity *Affinity `json:"affinity,omitempty"`

	// TopologySpreadConstraints describes how a group of resources ought to spread across multiple topology
	// domains. Scheduler will schedule resources in a way which abides by the constraints.
	// All topologySpreadConstraints are ANDed.
	// Only valid if the placement type is "PickN".
	// +optional
	// +patchMergeKey=topologyKey
	// +patchStrategy=merge
	TopologySpreadConstraints []TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty" patchStrategy:"merge" patchMergeKey:"topologyKey"`
}

// Affinity is a group of cluster affinity scheduling rules. More to be added.
type Affinity struct {
	// ClusterAffinity contains cluster affinity scheduling rules for the selected resources.
	// +optional
	ClusterAffinity *ClusterAffinity `json:"clusterAffinity,omitempty"`
}

// ClusterAffinity contains cluster affinity scheduling rules for the selected resources.
type ClusterAffinity struct {
	// If the affinity requirements specified by this field are not met at
	// scheduling time, the resource will not be scheduled onto the cluster.
	// If the affinity requirements specified by this field cease to be met
	// at some point after the placement (e.g. due to an update), the system
	// may or may not try to eventually remove the resource from the cluster.
	// +optional
	RequiredDuringSchedulingIgnoredDuringExecution *ClusterSelector `json:"requiredDuringSchedulingIgnoredDuringExecution,omitempty"`

	// The scheduler computes a score for each cluster at schedule time by iterating
	// through the elements of this field and adding "weight" to the sum if the cluster
	// matches the corresponding matchExpression. The scheduler then chooses the first
	// `N` clusters with the highest sum to satisfy the placement.
	// This field is ignored if the placement type is "PickAll".
	// If the cluster score changes at some point after the placement (e.g. due to an update),
	// the system may or may not try to eventually move the resource from a cluster with a lower score
	// to a cluster with higher score.
	// +optional
	PreferredDuringSchedulingIgnoredDuringExecution []PreferredClusterSelector `json:"preferredDuringSchedulingIgnoredDuringExecution,omitempty"`
}

type ClusterSelector struct {
	// +kubebuilder:validation:MaxItems=10
	// ClusterSelectorTerms is a list of cluster selector terms. The terms are `ORed`.
	// +required
	ClusterSelectorTerms []ClusterSelectorTerm `json:"clusterSelectorTerms"`
}

type PreferredClusterSelector struct {
	// Weight associated with matching the corresponding clusterSelectorTerm, in the range [-100, 100].
	// +required
	// +kubebuilder:validation:Minimum=-100
	// +kubebuilder:validation:Maximum=100
	Weight int32 `json:"weight"`

	// A cluster selector term, associated with the corresponding weight.
	// +required
	Preference ClusterSelectorTerm `json:"preference"`
}

// ClusterSelectorTerm contains the requirements to select clusters.
type ClusterSelectorTerm struct {
	// LabelSelector is a label query over all the joined member clusters. Clusters matching the query are selected.
	// +required
	LabelSelector metav1.LabelSelector `json:"labelSelector"`
}

// TopologySpreadConstraint specifies how to spread resources among the given cluster topology.
type TopologySpreadConstraint struct {
	// MaxSkew describes the degree to which resources may be unevenly distributed.
	// When `whenUnsatisfiable=DoNotSchedule`, it is the maximum permitted difference
	// between the number of resource copies in the target topology and the global minimum.
	// The global minimum is the minimum number of resource copies in a domain.
	// When `whenUnsatisfiable=ScheduleAnyway`, it is used to give higher precedence
	// to topologies that satisfy it.
	// It's an optional field. Default value is 1 and 0 is not allowed.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxSkew *int32 `json:"maxSkew,omitempty"`

	// TopologyKey is the key of cluster labels. Clusters that have a label with this key
	// and identical values are considered to be in the same topology.
	// We consider each <key, value> as a "bucket", and try to put balanced number
	// of replicas of the resource into each bucket honor the `MaxSkew` value.
	// It's a required field.
	// +required
	TopologyKey string `json:"topologyKey"`

	// WhenUnsatisfiable indicates how to deal with the resource if it doesn't satisfy
	// the spread constraint.
	// - DoNotSchedule (default) tells the scheduler not to schedule it.
	// - ScheduleAnyway tells the scheduler to schedule the resource in any cluster,
	//   but giving higher precedence to topologies that would help reduce the skew.
	// It's an optional field.
	// +optional
	WhenUnsatisfiable UnsatisfiableConstraintAction `json:"whenUnsatisfiable,omitempty"`
}

// UnsatisfiableConstraintAction defines the type of actions that can be taken if a constraint is not satisfied.
// +enum
type UnsatisfiableConstraintAction string

const (
	// DoNotSchedule instructs the scheduler not to schedule the resource
	// onto the cluster when constraints are not satisfied.
	DoNotSchedule UnsatisfiableConstraintAction = "DoNotSchedule"

	// ScheduleAnyway instructs the scheduler to schedule the resource
	// even if constraints are not satisfied.
	ScheduleAnyway UnsatisfiableConstraintAction = "ScheduleAnyway"
)

// RolloutStrategy describes how to roll out a new change in selected resources to target clusters.
type RolloutStrategy struct {
	// Type of rollout. The only supported type is "RollingUpdate". Default is "RollingUpdate".
	// +optional
	// +kubebuilder:validation:Enum=RollingUpdate
	// +kubebuilder:default=RollingUpdate
	Type RolloutStrategyType `json:"type,omitempty"`

	// Rolling update config params. Present only if RolloutStrategyType = RollingUpdate.
	// +optional
	RollingUpdate *RollingUpdateConfig `json:"rollingUpdate,omitempty"`
}

// +enum
type RolloutStrategyType string

const (
	// RollingUpdateRolloutStrategyType replaces the old placed resource using rolling update
	// i.e. gradually create the new one while replace the old ones.
	RollingUpdateRolloutStrategyType RolloutStrategyType = "RollingUpdate"
)

// RollingUpdateConfig contains the config to control the desired behavior of rolling update.
type RollingUpdateConfig struct {
	// The maximum number of clusters that can be unavailable during the rolling update
	// comparing to the desired number of clusters.
	// The desired number equals to the `NumberOfClusters` field when the placement type is `PickN`.
	// The desired number equals to the number of clusters scheduler selected when the placement type is `PickAll`.
	// Value can be an absolute number (ex: 5) or a percentage of the desired number of clusters (ex: 10%).
	// Absolute number is calculated from percentage by rounding up.
	// We consider a resource unavailable when we either remove it from a cluster or in-place
	// upgrade the resources content on the same cluster.
	// This can not be 0 if MaxSurge is 0.
	// Defaults to 25%.
	// +kubebuilder:default="25%"
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern="^((100|[0-9]{1,2})%|[0-9]+)$"
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`

	// The maximum number of clusters that can be scheduled above the desired number of clusters.
	// The desired number equals to the `NumberOfClusters` field when the placement type is `PickN`.
	// The desired number equals to the number of clusters scheduler selected when the placement type is `PickAll`.
	// Value can be an absolute number (ex: 5) or a percentage of desire (ex: 10%).
	// Absolute number is calculated from percentage by rounding up.
	// This does not apply to the case that we do in-place upgrade of resources on the same cluster.
	// This can not be 0 if MaxUnavailable is 0.
	// Defaults to 25%.
	// +kubebuilder:default="25%"
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern="^((100|[0-9]{1,2})%|[0-9]+)$"
	// +optional
	MaxSurge *intstr.IntOrString `json:"maxSurge,omitempty"`

	// UnavailablePeriodSeconds is used to config the time to wait between rolling out phases.
	// A resource placement is considered available after `UnavailablePeriodSeconds` seconds
	// has passed after the resources are applied to the target cluster successfully.
	// Default is 60.
	// +kubebuilder:default=60
	// +optional
	UnavailablePeriodSeconds *int `json:"unavailablePeriodSeconds,omitempty"`
}

// ClusterResourcePlacementStatus defines the observed state of the ClusterResourcePlacement object.
type ClusterResourcePlacementStatus struct {
	// SelectedResources contains a list of resources selected by ResourceSelectors.
	// +optional
	SelectedResources []ResourceIdentifier `json:"selectedResources,omitempty"`

	// PlacementStatuses contains a list of placement status on the clusters that are selected by PlacementPolicy.
	// Each selected cluster according to the latest resource placement is guaranteed to have a corresponding placementStatuses.
	// In the pickN case, there are N placement statuses where N = NumberOfClusters; Or in the pickFixed case, there are
	// N placement statuses where N = ClusterNames.
	// In these cases, some of them may not have assigned clusters when we cannot fill the required number of clusters.
	// TODO, For pickAll type, considering providing unselected clusters info.
	// +optional
	PlacementStatuses []ResourcePlacementStatus `json:"placementStatuses,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for ClusterResourcePlacement.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ResourceIdentifier identifies one Kubernetes resource.
type ResourceIdentifier struct {
	// Group is the group name of the selected resource.
	// +optional
	Group string `json:"group,omitempty"`

	// Version is the version of the selected resource.
	// +required
	Version string `json:"version"`

	// Kind represents the Kind of the selected resources.
	// +required
	Kind string `json:"kind"`

	// Name of the target resource.
	// +required
	Name string `json:"name"`

	// Namespace is the namespace of the resource. Empty if the resource is cluster scoped.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Envelope identifies the envelope object that contains this resource.
	// +optional
	Envelope EnvelopeIdentifier `json:"envelope,omitempty"`
}

// EnvelopeIdentifier identifies the envelope object that contains the selected resource.
type EnvelopeIdentifier struct {
	// Name of the envelope object.
	// +required
	Name string `json:"name"`

	// Namespace is the namespace of the envelope object. Empty if the envelope object is cluster scoped.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Type of the envelope object.
	// +kubebuilder:validation:Enum=ConfigMap
	// +kubebuilder:default=ConfigMap
	// +optional
	Type EnvelopeType `json:"type"`
}

// EnvelopeType defines the type of the envelope object.
// +enum
type EnvelopeType string

const (
	// ConfigMapEnvelopeType means the envelope object is of type `ConfigMap`.
	ConfigMapEnvelopeType EnvelopeType = "ConfigMap"
)

// ResourcePlacementStatus represents the placement status of selected resources for one target cluster.
type ResourcePlacementStatus struct {
	// ClusterName is the name of the cluster this resource is assigned to.
	// If it is not empty, its value should be unique cross all placement decisions for the Placement.
	// +optional
	ClusterName string `json:"clusterName,omitempty"`

	// +kubebuilder:validation:MaxItems=100

	// FailedResourcePlacements is a list of all the resources failed to be placed to the given cluster.
	// Note that we only include 100 failed resource placements even if there are more than 100.
	// This field is only meaningful if the `ClusterName` is not empty.
	// +optional
	FailedResourcePlacements []FailedResourcePlacement `json:"failedPlacements,omitempty"`

	// Conditions is an array of current observed conditions for ResourcePlacementStatus.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// FailedResourcePlacement contains the failure details of a failed resource placement.
type FailedResourcePlacement struct {
	// The resource failed to be placed.
	// +required
	ResourceIdentifier `json:",inline"`

	// The failed condition status.
	// +required
	Condition metav1.Condition `json:"condition"`
}

// ClusterResourcePlacementConditionType defines a specific condition of a cluster resource placement.
// +enum
type ClusterResourcePlacementConditionType string

const (
	// ClusterResourcePlacementScheduledConditionType indicates whether we have successfully scheduled the
	// ClusterResourcePlacement.
	// Its condition status can be one of the following:
	// - "True" means we have successfully scheduled the resources to fully satisfy the placement requirement.
	// - "False" means we didn't fully satisfy the placement requirement. We will fill the Reason field.
	// - "Unknown" means we don't have a scheduling decision yet.
	ClusterResourcePlacementScheduledConditionType ClusterResourcePlacementConditionType = "ClusterResourcePlacementScheduled"

	// ClusterResourcePlacementSynchronizedConditionType indicates whether the selected resources are synchronized under
	// the per-cluster namespaces (i.e., fleet-member-<member-name>) on the hub cluster.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully synchronized under the per-cluster namespaces
	// (i.e., fleet-member-<member-name>) on the hub cluster.
	// - "False" means all the selected resources have not been synchronized under the per-cluster namespaces
	// (i.e., fleet-member-<member-name>) on the hub cluster yet.
	ClusterResourcePlacementSynchronizedConditionType ClusterResourcePlacementConditionType = "ClusterResourcePlacementSynchronized"

	// ClusterResourcePlacementAppliedConditionType indicates whether all the selected member clusters have applied
	// the selected resources locally.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully applied to all the target clusters or apply is not needed
	// if there are no cluster(s) selected by the scheduler.
	// - "False" means some of them have failed. We will place some of the detailed failure in the FailedResourcePlacement array.
	// - "Unknown" means we haven't finished the apply yet.
	ClusterResourcePlacementAppliedConditionType ClusterResourcePlacementConditionType = "ClusterResourcePlacementApplied"
)

// ResourcePlacementConditionType defines a specific condition of a resource placement.
// +enum
type ResourcePlacementConditionType string

const (
	// ResourceScheduledConditionType indicates whether we have successfully scheduled the selected resources.
	// Its condition status can be one of the following:
	// - "True" means we have successfully scheduled the resources to satisfy the placement requirement.
	// - "False" means we didn't fully satisfy the placement requirement. We will fill the Message field.
	ResourceScheduledConditionType ResourcePlacementConditionType = "ResourceScheduled"

	// ResourceWorkSynchronizedConditionType indicates whether we have created or updated the corresponding work object(s)
	// under the per-cluster namespaces (i.e., fleet-member-<member-name>) which have the latest resources selected by
	// the placement.
	// Its condition status can be one of the following:
	// - "True" means we have successfully created the latest corresponding work(s) or updated the existing work(s) to
	// the latest.
	// - "False" means we have not created the latest corresponding work(s) or updated the existing work(s) to the latest
	// yet.
	// There are few possibilities:
	// - In the processing state
	// - Rollout controller has decided not to create or update the resources in this cluster for now to honor the
	// rollout strategy configurations specified in the placement.
	// - Work is not created/updated because of the unknown reasons.
	ResourceWorkSynchronizedConditionType ResourcePlacementConditionType = "WorkSynchronized"

	// ResourcesAppliedConditionType indicates whether the selected member cluster has applied the selected resources locally.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully applied to the target cluster.
	// - "False" means some of them have failed.
	// - "Unknown" means we haven't finished the apply yet.
	ResourcesAppliedConditionType ResourcePlacementConditionType = "ResourceApplied"
)

// PlacementType identifies the type of placement.
// +enum
type PlacementType string

const (
	// PickAllPlacementType picks all clusters that satisfy the rules.
	PickAllPlacementType PlacementType = "PickAll"

	// PickNPlacementType picks N clusters that satisfy the rules.
	PickNPlacementType PlacementType = "PickN"

	// PickFixedPlacementType picks a fixed set of clusters.
	PickFixedPlacementType PlacementType = "PickFixed"
)

// ClusterResourcePlacementList contains a list of ClusterResourcePlacement.
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterResourcePlacementList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourcePlacement `json:"items"`
}

// SetConditions sets the conditions of the ClusterResourcePlacement.
func (m *ClusterResourcePlacement) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

// GetCondition returns the condition of the ClusterResourcePlacement objects.
func (m *ClusterResourcePlacement) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

func init() {
	SchemeBuilder.Register(&ClusterResourcePlacement{}, &ClusterResourcePlacementList{})
}
