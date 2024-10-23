/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// ClusterResourcePlacementCleanupFinalizer is a finalizer added by the CRP controller to all CRPs, to make sure
	// that the CRP controller can react to CRP deletions if necessary.
	ClusterResourcePlacementCleanupFinalizer = fleetPrefix + "crp-cleanup"

	// SchedulerCRPCleanupFinalizer is a finalizer added by the scheduler to CRPs, to make sure
	// that all bindings derived from a CRP can be cleaned up after the CRP is deleted.
	SchedulerCRPCleanupFinalizer = fleetPrefix + "scheduler-cleanup"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",shortName=crp,categories={fleet,fleet-placement}
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.policy.placementType`,name="Type",priority=1,type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ClusterResourcePlacementScheduled")].status`,name="Scheduled",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ClusterResourcePlacementScheduled")].observedGeneration`,name="Scheduled-Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ClusterResourcePlacementWorkSynchronized")].status`,name="Work-Synchronized",priority=1,type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ClusterResourcePlacementWorkSynchronized")].observedGeneration`,name="Work-Synchronized-Gen",priority=1,type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ClusterResourcePlacementAvailable")].status`,name="Available",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ClusterResourcePlacementAvailable")].observedGeneration`,name="Available-Gen",type=string
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
	// +kubebuilder:validation:Required
	Spec ClusterResourcePlacementSpec `json:"spec"`

	// The observed status of ClusterResourcePlacement.
	// +kubebuilder:validation:Optional
	Status ClusterResourcePlacementStatus `json:"status,omitempty"`
}

// ClusterResourcePlacementSpec defines the desired state of ClusterResourcePlacement.
type ClusterResourcePlacementSpec struct {
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=100

	// ResourceSelectors is an array of selectors used to select cluster scoped resources. The selectors are `ORed`.
	// You can have 1-100 selectors.
	// +kubebuilder:validation:Required
	ResourceSelectors []ClusterResourceSelector `json:"resourceSelectors"`

	// Policy defines how to select member clusters to place the selected resources.
	// If unspecified, all the joined member clusters are selected.
	// +kubebuilder:validation:Optional
	Policy *PlacementPolicy `json:"policy,omitempty"`

	// The rollout strategy to use to replace existing placement with new ones.
	// +kubebuilder:validation:Optional
	// +patchStrategy=retainKeys
	Strategy RolloutStrategy `json:"strategy,omitempty"`

	// The number of old ClusterSchedulingPolicySnapshot or ClusterResourceSnapshot resources to retain to allow rollback.
	// This is a pointer to distinguish between explicit zero and not specified.
	// Defaults to 10.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=10
	// +kubebuilder:validation:Optional
	RevisionHistoryLimit *int32 `json:"revisionHistoryLimit,omitempty"`
}

// ClusterResourceSelector is used to select cluster scoped resources as the target resources to be placed.
// If a namespace is selected, ALL the resources under the namespace are selected automatically.
// All the fields are `ANDed`. In other words, a resource must match all the fields to be selected.
type ClusterResourceSelector struct {
	// Group name of the cluster-scoped resource.
	// Use an empty string to select resources under the core API group (e.g., namespaces).
	// +kubebuilder:validation:Required
	Group string `json:"group"`

	// Version of the cluster-scoped resource.
	// +kubebuilder:validation:Required
	Version string `json:"version"`

	// Kind of the cluster-scoped resource.
	// Note: When `Kind` is `namespace`, ALL the resources under the selected namespaces are selected.
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// You can only specify at most one of the following two fields: Name and LabelSelector.
	// If none is specified, all the cluster-scoped resources with the given group, version and kind are selected.

	// Name of the cluster-scoped resource.
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`

	// A label query over all the cluster-scoped resources. Resources matching the query are selected.
	// Note that namespace-scoped resources can't be selected even if they match the query.
	// +kubebuilder:validation:Optional
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
	// +kubebuilder:validation:Optional
	PlacementType PlacementType `json:"placementType,omitempty"`

	// +kubebuilder:validation:MaxItems=100
	// ClusterNames contains a list of names of MemberCluster to place the selected resources.
	// Only valid if the placement type is "PickFixed"
	// +kubebuilder:validation:Optional
	ClusterNames []string `json:"clusterNames,omitempty"`

	// NumberOfClusters of placement. Only valid if the placement type is "PickN".
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Optional
	NumberOfClusters *int32 `json:"numberOfClusters,omitempty"`

	// Affinity contains cluster affinity scheduling rules. Defines which member clusters to place the selected resources.
	// Only valid if the placement type is "PickAll" or "PickN".
	// +kubebuilder:validation:Optional
	Affinity *Affinity `json:"affinity,omitempty"`

	// TopologySpreadConstraints describes how a group of resources ought to spread across multiple topology
	// domains. Scheduler will schedule resources in a way which abides by the constraints.
	// All topologySpreadConstraints are ANDed.
	// Only valid if the placement type is "PickN".
	// +kubebuilder:validation:Optional
	// +patchMergeKey=topologyKey
	// +patchStrategy=merge
	TopologySpreadConstraints []TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty" patchStrategy:"merge" patchMergeKey:"topologyKey"`

	// If specified, the ClusterResourcePlacement's Tolerations.
	// Tolerations cannot be updated or deleted.
	//
	// This field is beta-level and is for the taints and tolerations feature.
	// +kubebuilder:validation:MaxItems=100
	// +kubebuilder:validation:Optional
	Tolerations []Toleration `json:"tolerations,omitempty"`
}

// Affinity is a group of cluster affinity scheduling rules. More to be added.
type Affinity struct {
	// ClusterAffinity contains cluster affinity scheduling rules for the selected resources.
	// +kubebuilder:validation:Optional
	ClusterAffinity *ClusterAffinity `json:"clusterAffinity,omitempty"`
}

// ClusterAffinity contains cluster affinity scheduling rules for the selected resources.
type ClusterAffinity struct {
	// If the affinity requirements specified by this field are not met at
	// scheduling time, the resource will not be scheduled onto the cluster.
	// If the affinity requirements specified by this field cease to be met
	// at some point after the placement (e.g. due to an update), the system
	// may or may not try to eventually remove the resource from the cluster.
	// +kubebuilder:validation:Optional
	RequiredDuringSchedulingIgnoredDuringExecution *ClusterSelector `json:"requiredDuringSchedulingIgnoredDuringExecution,omitempty"`

	// The scheduler computes a score for each cluster at schedule time by iterating
	// through the elements of this field and adding "weight" to the sum if the cluster
	// matches the corresponding matchExpression. The scheduler then chooses the first
	// `N` clusters with the highest sum to satisfy the placement.
	// This field is ignored if the placement type is "PickAll".
	// If the cluster score changes at some point after the placement (e.g. due to an update),
	// the system may or may not try to eventually move the resource from a cluster with a lower score
	// to a cluster with higher score.
	// +kubebuilder:validation:Optional
	PreferredDuringSchedulingIgnoredDuringExecution []PreferredClusterSelector `json:"preferredDuringSchedulingIgnoredDuringExecution,omitempty"`
}

type ClusterSelector struct {
	// +kubebuilder:validation:MaxItems=10
	// ClusterSelectorTerms is a list of cluster selector terms. The terms are `ORed`.
	// +kubebuilder:validation:Required
	ClusterSelectorTerms []ClusterSelectorTerm `json:"clusterSelectorTerms"`
}

type PreferredClusterSelector struct {
	// Weight associated with matching the corresponding clusterSelectorTerm, in the range [-100, 100].
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=-100
	// +kubebuilder:validation:Maximum=100
	Weight int32 `json:"weight"`

	// A cluster selector term, associated with the corresponding weight.
	// +kubebuilder:validation:Required
	Preference ClusterSelectorTerm `json:"preference"`
}

// +enum
type PropertySortOrder string

const (
	// Descending instructs Fleet to sort in descending order, that is, the clusters with higher
	// observed values of a property are most preferred and should have higher weights. We will
	// use linear scaling to calculate the weight for each cluster based on the observed values.
	//
	// For example, with this order, if Fleet sorts all clusters by a specific property where the
	// observed values are in the range [10, 100], and a weight of 100 is specified;
	// Fleet will assign:
	//
	// * a weight of 100 to the cluster with the maximum observed value (100); and
	// * a weight of 0 to the cluster with the minimum observed value (10); and
	// * a weight of 11 to the cluster with an observed value of 20.
	//
	//   It is calculated using the formula below:
	//   ((20 - 10)) / (100 - 10)) * 100 = 11
	Descending PropertySortOrder = "Descending"

	// Ascending instructs Fleet to sort in ascending order, that is, the clusters with lower
	// observed values are most preferred and should have higher weights. We will use linear scaling
	// to calculate the weight for each cluster based on the observed values.
	//
	// For example, with this order, if Fleet sorts all clusters by a specific property where
	// the observed values are in the range [10, 100], and a weight of 100 is specified;
	// Fleet will assign:
	//
	// * a weight of 0 to the cluster with the  maximum observed value (100); and
	// * a weight of 100 to the cluster with the minimum observed value (10); and
	// * a weight of 89 to the cluster with an observed value of 20.
	//
	//   It is calculated using the formula below:
	//   (1 - ((20 - 10) / (100 - 10))) * 100 = 89
	Ascending PropertySortOrder = "Ascending"
)

// PropertySelectorOperator is the operator that can be used with PropertySelectorRequirements.
// +enum
type PropertySelectorOperator string

const (
	// PropertySelectorGreaterThan dictates Fleet to select cluster if its observed value of a given
	// property is greater than the value specified in the requirement.
	PropertySelectorGreaterThan PropertySelectorOperator = "Gt"
	// PropertySelectorGreaterThanOrEqualTo dictates Fleet to select cluster if its observed value
	// of a given property is greater than or equal to the value specified in the requirement.
	PropertySelectorGreaterThanOrEqualTo PropertySelectorOperator = "Ge"
	// PropertySelectorEqualTo dictates Fleet to select cluster if its observed value of a given
	// property is equal to the values specified in the requirement.
	PropertySelectorEqualTo PropertySelectorOperator = "Eq"
	// PropertySelectorNotEqualTo dictates Fleet to select cluster if its observed value of a given
	// property is not equal to the values specified in the requirement.
	PropertySelectorNotEqualTo PropertySelectorOperator = "Ne"
	// PropertySelectorLessThan dictates Fleet to select cluster if its observed value of a given
	// property is less than the value specified in the requirement.
	PropertySelectorLessThan PropertySelectorOperator = "Lt"
	// PropertySelectorLessThanOrEqualTo dictates Fleet to select cluster if its observed value of a
	// given property is less than or equal to the value specified in the requirement.
	PropertySelectorLessThanOrEqualTo PropertySelectorOperator = "Le"
)

// PropertySelectorRequirement is a specific property requirement when picking clusters for
// resource placement.
type PropertySelectorRequirement struct {
	// Name is the name of the property; it should be a Kubernetes label name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Operator specifies the relationship between a cluster's observed value of the specified
	// property and the values given in the requirement.
	// +kubebuilder:validation:Required
	Operator PropertySelectorOperator `json:"operator"`

	// Values are a list of values of the specified property which Fleet will compare against
	// the observed values of individual member clusters in accordance with the given
	// operator.
	//
	// At this moment, each value should be a Kubernetes quantity. For more information, see
	// https://pkg.go.dev/k8s.io/apimachinery/pkg/api/resource#Quantity.
	//
	// If the operator is Gt (greater than), Ge (greater than or equal to), Lt (less than),
	// or `Le` (less than or equal to), Eq (equal to), or Ne (ne), exactly one value must be
	// specified in the list.
	//
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:validation:Required
	Values []string `json:"values"`
}

// PropertySelector helps user specify property requirements when picking clusters for resource
// placement.
type PropertySelector struct {
	// MatchExpressions is an array of PropertySelectorRequirements. The requirements are AND'd.
	// +kubebuilder:validation:Required
	MatchExpressions []PropertySelectorRequirement `json:"matchExpressions"`
}

// PropertySorter helps user specify how to sort clusters based on a specific property.
type PropertySorter struct {
	// Name is the name of the property which Fleet sorts clusters by.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// SortOrder explains how Fleet should perform the sort; specifically, whether Fleet should
	// sort in ascending or descending order.
	// +kubebuilder:validation:Required
	SortOrder PropertySortOrder `json:"sortOrder"`
}

type ClusterSelectorTerm struct {
	// LabelSelector is a label query over all the joined member clusters. Clusters matching
	// the query are selected.
	//
	// If you specify both label and property selectors in the same term, the results are AND'd.
	// +kubebuilder:validation:Optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// PropertySelector is a property query over all joined member clusters. Clusters matching
	// the query are selected.
	//
	// If you specify both label and property selectors in the same term, the results are AND'd.
	//
	// At this moment, PropertySelector can only be used with
	// `RequiredDuringSchedulingIgnoredDuringExecution` affinity terms.
	//
	// This field is beta-level; it is for the property-based scheduling feature and is only
	// functional when a property provider is enabled in the deployment.
	// +kubebuilder:validation:Optional
	PropertySelector *PropertySelector `json:"propertySelector,omitempty"`

	// PropertySorter sorts all matching clusters by a specific property and assigns different weights
	// to each cluster based on their observed property values.
	//
	// At this moment, PropertySorter can only be used with
	// `PreferredDuringSchedulingIgnoredDuringExecution` affinity terms.
	//
	// This field is beta-level; it is for the property-based scheduling feature and is only
	// functional when a property provider is enabled in the deployment.
	// +kubebuilder:validation:Optional
	PropertySorter *PropertySorter `json:"propertySorter,omitempty"`
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
	// +kubebuilder:validation:Optional
	MaxSkew *int32 `json:"maxSkew,omitempty"`

	// TopologyKey is the key of cluster labels. Clusters that have a label with this key
	// and identical values are considered to be in the same topology.
	// We consider each <key, value> as a "bucket", and try to put balanced number
	// of replicas of the resource into each bucket honor the `MaxSkew` value.
	// It's a required field.
	// +kubebuilder:validation:Required
	TopologyKey string `json:"topologyKey"`

	// WhenUnsatisfiable indicates how to deal with the resource if it doesn't satisfy
	// the spread constraint.
	// - DoNotSchedule (default) tells the scheduler not to schedule it.
	// - ScheduleAnyway tells the scheduler to schedule the resource in any cluster,
	//   but giving higher precedence to topologies that would help reduce the skew.
	// It's an optional field.
	// +kubebuilder:validation:Optional
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
	// Type of rollout. The only supported types are "RollingUpdate" and "External".
	// Default is "RollingUpdate".
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=RollingUpdate
	// +kubebuilder:validation:Enum=RollingUpdate;External
	Type RolloutStrategyType `json:"type,omitempty"`

	// Rolling update config params. Present only if RolloutStrategyType = RollingUpdate.
	// +kubebuilder:validation:Optional
	RollingUpdate *RollingUpdateConfig `json:"rollingUpdate,omitempty"`

	// ApplyStrategy describes when and how to apply the selected resources to the target cluster.
	// +kubebuilder:validation:Optional
	ApplyStrategy *ApplyStrategy `json:"applyStrategy,omitempty"`
}

// ApplyStrategy describes when and how to apply the selected resource to the target cluster.
// Note: If multiple CRPs try to place the same resource with different apply strategy, the later ones will fail with the
// reason ApplyConflictBetweenPlacements.
type ApplyStrategy struct {
	// ComparisonOption controls how Fleet compares the desired state of a resource, as kept in
	// a hub cluster manifest, with the current state of the resource (if applicable) in the
	// member cluster.
	//
	// Available options are:
	//
	// * PartialComparison: with this option, Fleet will compare only fields that are managed by
	//   Fleet, i.e., the fields that are specified explicitly in the hub cluster manifest.
	//   Unmanaged fields are ignored. This is the default option.
	//
	// * FullComparison: with this option, Fleet will compare all fields of the resource,
	//   even if the fields are absent from the hub cluster manifest.
	//
	// Consider using the PartialComparison option if you would like to:
	//
	// * use the default values for certain fields; or
	// * let another agent, e.g., HPAs, VPAs, etc., on the member cluster side manage some fields; or
	// * allow ad-hoc or cluster-specific settings on the member cluster side.
	//
	// To use the FullComparison option, it is recommended that you:
	//
	// * specify all fields as appropriate in the hub cluster, even if you are OK with using default
	//   values;
	// * make sure that no fields are managed by agents other than Fleet on the member cluster
	//   side, such as HPAs, VPAs, or other controllers.
	//
	// See the Fleet documentation for further explanations and usage examples.
	//
	// +kubebuilder:default=PartialComparison
	// +kubebuilder:validation:Enum=PartialComparison;FullComparison
	// +kubebuilder:validation:Optional
	ComparisonOption ComparisonOptionType `json:"compareOption,omitempty"`

	// WhenToApply controls when Fleet would apply the manifests on the hub cluster to the member
	// clusters.
	//
	// Available options are:
	//
	// * Always: with this option, Fleet will periodically apply hub cluster manifests
	//   on the member cluster side; this will effectively overwrite any change in the fields
	//   managed by Fleet (i.e., specified in the hub cluster manifest). This is the default
	//   option.
	//
	//   Note that this option would revert any ad-hoc changes made on the member cluster side in
	//   the managed fields; if you would like to make temporary edits on the member cluster side
	//   in the managed fields, switch to IfNotDrifted option. Note that changes in unmanaged
	//   fields will be left alone; if you use the FullDiff compare option, such changes will
	//   be reported as drifts.
	//
	// * IfNotDrifted: with this option, Fleet will stop applying hub cluster manifests on
	//   clusters that have drifted from the desired state; apply ops would still continue on
	//   the rest of the clusters. Drifts are calculated using the ComparisonOption,
	//   as explained in the corresponding field.
	//
	//   Use this option if you would like Fleet to detect drifts in your multi-cluster setup.
	//   A drift occurs when an agent makes an ad-hoc change on the member cluster side that
	//   makes affected resources deviate from its desired state as kept in the hub cluster;
	//   and this option grants you an opportunity to view the drift details and take actions
	//   accordingly. The drift details will be reported in the CRP status.
	//
	//   To fix a drift, you may:
	//
	//   * revert the changes manually on the member cluster side
	//   * update the hub cluster manifest; this will trigger Fleet to apply the latest revision
	//     of the manifests, which will overwrite the drifted fields
	//     (if they are managed by Fleet)
	//   * switch to the Always option; this will trigger Fleet to apply the current revision
	//     of the manifests, which will overwrite the drifted fields (if they are managed by Fleet).
	//   * if applicable and necessary, delete the drifted resources on the member cluster side; Fleet
	//     will attempt to re-create them using the hub cluster manifests
	//
	// +kubebuilder:default=Always
	// +kubebuilder:validation:Enum=Always;IfNotDrifted
	// +kubebuilder:validation:Optional
	WhenToApply WhenToApplyType `json:"whenToApply,omitempty"`

	// Type is the apply strategy to use; it determines how Fleet applies manifests from the
	// hub cluster to a member cluster.
	//
	// Available options are:
	//
	// * ClientSideApply: Fleet uses three-way merge to apply manifests, similar to how kubectl
	//   performs a client-side apply. This is the default option.
	//
	//   Note that this strategy requires that Fleet keep the last applied configuration in the
	//   annotation of an applied resource. If the object gets so large that apply ops can no longer
	//   be executed, Fleet will switch to server-side apply.
	//
	//   Use ComparisonOption and WhenToApply settings to control when an apply op can be executed.
	//
	// * ServerSideApply: Fleet uses server-side apply to apply manifests; Fleet itself will
	//   become the field manager for specified fields in the manifests. Specify
	//   ServerSideApplyConfig as appropriate if you would like Fleet to take over field
	//   ownership upon conflicts. This is the recommended option for most scenarios; it might
	//   help reduce object size and safely resolve conflicts between field values. For more
	//   information, please refer to the Kubernetes documentation
	//   (https://kubernetes.io/docs/reference/using-api/server-side-apply/#comparison-with-client-side-apply).
	//
	//   Use ComparisonOption and WhenToApply settings to control when an apply op can be executed.
	//
	// * ReportDiff: Fleet will compare the desired state of a resource as kept in the hub cluster
	//   with its current state (if applicable) on the member cluster side, and report any
	//   differences. No actual apply ops would be executed, and resources will be left alone as they
	//   are on the member clusters.
	//
	//   Use ComparisonOption setting to control how the difference is calculated.
	//
	// For a comparison between the different strategies and usage examples, refer to the
	// Fleet documentation.
	//
	// +kubebuilder:default=ClientSideApply
	// +kubebuilder:validation:Enum=ClientSideApply;ServerSideApply;ReportDiff
	// +kubebuilder:validation:Optional
	Type ApplyStrategyType `json:"type,omitempty"`

	// AllowCoOwnership defines whether to apply the resource if it already exists in the target cluster and is not
	// solely owned by fleet (i.e., metadata.ownerReferences contains only fleet custom resources).
	// If true, apply the resource and add fleet as a co-owner.
	// If false, leave the resource unchanged and fail the apply.
	AllowCoOwnership bool `json:"allowCoOwnership,omitempty"`

	// ServerSideApplyConfig defines the configuration for server side apply. It is honored only when type is ServerSideApply.
	// +kubebuilder:validation:Optional
	ServerSideApplyConfig *ServerSideApplyConfig `json:"serverSideApplyConfig,omitempty"`

	// WhenToTakeOver determines the action to take when Fleet applies resources to a member
	// cluster for the first time and finds out that the resource already exists in the cluster.
	//
	// This setting is most relevant in cases where you would like Fleet to manage pre-existing
	// resources on a member cluster.
	//
	// Available options include:
	//
	// * Always: with this action, Fleet will apply the hub cluster manifests to the member
	//   clusters even if the affected resources already exist. This is the default action.
	//
	//   Note that this might lead to fields being overwritten on the member clusters, if they
	//   are specified in the hub cluster manifests.
	//
	// * IfNoDiff: with this action, Fleet will apply the hub cluster manifests to the member
	//   clusters if (and only if) pre-existing resources look the same as the hub cluster manifests.
	//   This is a safer option as pre-existing resources that are inconsistent with the hub cluster
	//   manifests will not be overwritten; in fact, Fleet will ignore them until the inconsistencies
	//   are resolved properly: any change you make to the hub cluster manifests would not be
	//   applied, and if you delete the manifests or even the ClusterResourcePlacement itself
	//   from the hub cluster, these pre-existing resources would not be taken away.
	//
	//   Fleet will check for inconsistencies in accordance with the ComparisonOption setting. See also
	//   the comments on the ComparisonOption field for more information.
	//
	//   If a diff has been found in a field that is **managed** by Fleet (i.e., the field
	//   **is specified ** in the hub cluster manifest), consider one of the following actions:
	//   * set the field in the member cluster to be of the same value as that in the hub cluster
	//     manifest.
	//   * update the hub cluster manifest so that its field value matches with that in the member
	//     cluster.
	//   * switch to the Always action, which will allow Fleet to overwrite the field with the
	//     value in the hub cluster manifest.
	//
	//   If a diff has been found in a field that is **not managed** by Fleet (i.e., the field
	//   **is not specified** in the hub cluster manifest), consider one of the following actions:
	//   * remove the field from the member cluster.
	//   * update the hub cluster manifest so that the field is included in the hub cluster manifest.
	//
	//   If appropriate, you may also delete the object from the member cluster; Fleet will recreate
	//   it using the hub cluster manifest.
	//
	// +kubebuilder:default=Always
	// +kubebuilder:validation:Enum=Always;IfNoDiff
	// +kubebuilder:validation:Optional
	WhenToTakeOver WhenToTakeOverType `json:"actionType,omitempty"`
}

// ComparisonOptionType describes the compare option that Fleet uses to detect drifts and/or
// calculate differences.
// +enum
type ComparisonOptionType string

const (
	// ComparisonOptionTypePartialComparison will compare only fields that are managed by Fleet, i.e.,
	// fields that are specified explicitly in the hub cluster manifest. Unmanaged fields
	// are ignored.
	ComparisonOptionTypePartialComparison ComparisonOptionType = "PartialDiff"

	// ComparisonOptionTypeFullDiff will compare all fields of the resource, even if the fields
	// are absent from the hub cluster manifest.
	ComparisonOptionTypeFullComparison ComparisonOptionType = "FullDiff"
)

// WhenToApplyType describes when Fleet would apply the manifests on the hub cluster to
// the member clusters.
type WhenToApplyType string

const (
	// WhenToApplyTypeAlways instructs Fleet to periodically apply hub cluster manifests
	// on the member cluster side; this will effectively overwrite any change in the fields
	// managed by Fleet (i.e., specified in the hub cluster manifest).
	WhenToApplyTypeAlways WhenToApplyType = "Always"

	// WhenToApplyTypeIfNotDrifted instructs Fleet to stop applying hub cluster manifests on
	// clusters that have drifted from the desired state; apply ops would still continue on
	// the rest of the clusters.
	WhenToApplyTypeIfNotDrifted WhenToApplyType = "IfNotDrifted"
)

// ApplyStrategyType describes the type of the strategy used to apply the resource to the target cluster.
// +enum
type ApplyStrategyType string

const (
	// ApplyStrategyTypeClientSideApply will use three-way merge patch similar to how `kubectl apply` does by storing
	// last applied state in the `last-applied-configuration` annotation.
	// When the `last-applied-configuration` annotation size is greater than 256kB, it falls back to the server-side apply.
	ApplyStrategyTypeClientSideApply ApplyStrategyType = "ClientSideApply"

	// ApplyStrategyTypeServerSideApply will use server-side apply to resolve conflicts between the resource to be placed
	// and the existing resource in the target cluster.
	// Details: https://kubernetes.io/docs/reference/using-api/server-side-apply
	ApplyStrategyTypeServerSideApply ApplyStrategyType = "ServerSideApply"

	// ApplyStrategyTypeReportDiff will report differences between the desired state of a
	// resource as kept in the hub cluster and its current state (if applicable) on the member
	// cluster side. No actual apply ops would be executed.
	ApplyStrategyTypeReportDiff ApplyStrategyType = "ReportDiff"
)

// ServerSideApplyConfig defines the configuration for server side apply.
// Details: https://kubernetes.io/docs/reference/using-api/server-side-apply/#conflicts
type ServerSideApplyConfig struct {
	// Force represents to force apply to succeed when resolving the conflicts
	// For any conflicting fields,
	// - If true, use the values from the resource to be applied to overwrite the values of the existing resource in the
	// target cluster, as well as take over ownership of such fields.
	// - If false, apply will fail with the reason ApplyConflictWithOtherApplier.
	//
	// For non-conflicting fields, values stay unchanged and ownership are shared between appliers.
	// +kubebuilder:validation:Optional
	ForceConflicts bool `json:"force"`
}

// WhenToTakeOverType describes the type of the action to take when we first apply the
// resources to the member cluster.
// +enum
type WhenToTakeOverType string

const (
	// WhenToTakeOverTypeIfNoDiff will apply manifests from the hub cluster only if there is no difference
	// between the current resource snapshot version on the hub cluster and the existing
	// resources on the member cluster. Otherwise, we will report the difference.
	WhenToTakeOverTypeIfNoDiff WhenToTakeOverType = "IfNoDiff"

	// WhenToTakeOverTypeAlways will always apply the resource to the member cluster regardless
	// if there are differences between the resource on the hub cluster and the existing resources on the member cluster.
	WhenToTakeOverTypeAlways WhenToTakeOverType = "Always"
)

// +enum
type RolloutStrategyType string

const (
	// RollingUpdateRolloutStrategyType replaces the old placed resource using rolling update
	// i.e. gradually create the new one while replace the old ones.
	RollingUpdateRolloutStrategyType RolloutStrategyType = "RollingUpdate"

	// ExternalRolloutStrategyType means there is an external rollout controller that will
	// handle the rollout of the resources.
	ExternalRolloutStrategyType RolloutStrategyType = "External"
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
	// The minimum of MaxUnavailable is 1 to avoid rolling out stuck during in-place resource update.
	// Defaults to 25%.
	// +kubebuilder:default="25%"
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern="^((100|[0-9]{1,2})%|[0-9]+)$"
	// +kubebuilder:validation:Optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`

	// The maximum number of clusters that can be scheduled above the desired number of clusters.
	// The desired number equals to the `NumberOfClusters` field when the placement type is `PickN`.
	// The desired number equals to the number of clusters scheduler selected when the placement type is `PickAll`.
	// Value can be an absolute number (ex: 5) or a percentage of desire (ex: 10%).
	// Absolute number is calculated from percentage by rounding up.
	// This does not apply to the case that we do in-place update of resources on the same cluster.
	// This can not be 0 if MaxUnavailable is 0.
	// Defaults to 25%.
	// +kubebuilder:default="25%"
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern="^((100|[0-9]{1,2})%|[0-9]+)$"
	// +kubebuilder:validation:Optional
	MaxSurge *intstr.IntOrString `json:"maxSurge,omitempty"`

	// UnavailablePeriodSeconds is used to configure the waiting time between rollout phases when we
	// cannot determine if the resources have rolled out successfully or not.
	// We have a built-in resource state detector to determine the availability status of following well-known Kubernetes
	// native resources: Deployment, StatefulSet, DaemonSet, Service, Namespace, ConfigMap, Secret,
	// ClusterRole, ClusterRoleBinding, Role, RoleBinding.
	// Please see [SafeRollout](https://github.com/Azure/fleet/tree/main/docs/concepts/SafeRollout/README.md) for more details.
	// For other types of resources, we consider them as available after `UnavailablePeriodSeconds` seconds
	// have passed since they were successfully applied to the target cluster.
	// Default is 60.
	// +kubebuilder:default=60
	// +kubebuilder:validation:Optional
	UnavailablePeriodSeconds *int `json:"unavailablePeriodSeconds,omitempty"`
}

// ClusterResourcePlacementStatus defines the observed state of the ClusterResourcePlacement object.
type ClusterResourcePlacementStatus struct {
	// SelectedResources contains a list of resources selected by ResourceSelectors.
	// +kubebuilder:validation:Optional
	SelectedResources []ResourceIdentifier `json:"selectedResources,omitempty"`

	// Resource index logically represents the generation of the selected resources.
	// We take a new snapshot of the selected resources whenever the selection or their content change.
	// Each snapshot has a different resource index.
	// One resource snapshot can contain multiple clusterResourceSnapshots CRs in order to store large amount of resources.
	// To get clusterResourceSnapshot of a given resource index, use the following command:
	// `kubectl get ClusterResourceSnapshot --selector=kubernetes-fleet.io/resource-index=$ObservedResourceIndex `
	// ObservedResourceIndex is the resource index that the conditions in the ClusterResourcePlacementStatus observe.
	// For example, a condition of `ClusterResourcePlacementWorkSynchronized` type
	// is observing the synchronization status of the resource snapshot with the resource index $ObservedResourceIndex.
	// +kubebuilder:validation:Optional
	ObservedResourceIndex string `json:"observedResourceIndex,omitempty"`

	// PlacementStatuses contains a list of placement status on the clusters that are selected by PlacementPolicy.
	// Each selected cluster according to the latest resource placement is guaranteed to have a corresponding placementStatuses.
	// In the pickN case, there are N placement statuses where N = NumberOfClusters; Or in the pickFixed case, there are
	// N placement statuses where N = ClusterNames.
	// In these cases, some of them may not have assigned clusters when we cannot fill the required number of clusters.
	// TODO, For pickAll type, considering providing unselected clusters info.
	// +kubebuilder:validation:Optional
	PlacementStatuses []ResourcePlacementStatus `json:"placementStatuses,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for ClusterResourcePlacement.
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ResourceIdentifier identifies one Kubernetes resource.
type ResourceIdentifier struct {
	// Group is the group name of the selected resource.
	// +kubebuilder:validation:Optional
	Group string `json:"group,omitempty"`

	// Version is the version of the selected resource.
	// +kubebuilder:validation:Required
	Version string `json:"version"`

	// Kind represents the Kind of the selected resources.
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// Name of the target resource.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the resource. Empty if the resource is cluster scoped.
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`

	// Envelope identifies the envelope object that contains this resource.
	// +kubebuilder:validation:Optional
	Envelope *EnvelopeIdentifier `json:"envelope,omitempty"`
}

// EnvelopeIdentifier identifies the envelope object that contains the selected resource.
type EnvelopeIdentifier struct {
	// Name of the envelope object.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the envelope object. Empty if the envelope object is cluster scoped.
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`

	// Type of the envelope object.
	// +kubebuilder:validation:Enum=ConfigMap
	// +kubebuilder:default=ConfigMap
	// +kubebuilder:validation:Optional
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
	// +kubebuilder:validation:Optional
	ClusterName string `json:"clusterName,omitempty"`

	// ApplicableResourceOverrides contains a list of applicable ResourceOverride snapshots associated with the selected
	// resources.
	//
	// This field is alpha-level and is for the override policy feature.
	// +kubebuilder:validation:Optional
	ApplicableResourceOverrides []NamespacedName `json:"applicableResourceOverrides,omitempty"`

	// ApplicableClusterResourceOverrides contains a list of applicable ClusterResourceOverride snapshots associated with
	// the selected resources.
	//
	// This field is alpha-level and is for the override policy feature.
	// +kubebuilder:validation:Optional
	ApplicableClusterResourceOverrides []string `json:"applicableClusterResourceOverrides,omitempty"`

	// +kubebuilder:validation:MaxItems=100

	// FailedPlacements is a list of all the resources failed to be placed to the given cluster or the resource is unavailable.
	// Note that we only include 100 failed resource placements even if there are more than 100.
	// This field is only meaningful if the `ClusterName` is not empty.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=100
	FailedPlacements []FailedResourcePlacement `json:"failedPlacements,omitempty"`

	// DriftedPlacements is a list of resources that have drifted from their desired states
	// kept in the hub cluster, as found by Fleet using the drift detection mechanism.
	//
	// To control the object size, only the first 100 drifted resources will be included.
	// This field is only meaningful if the `ClusterName` is not empty.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=100
	DriftedPlacements []DriftedResourcePlacement `json:"driftedPlacements,omitempty"`

	// DiffedPlacements is a list of resources that have configuration differences from their
	// corresponding hub cluster manifests. Fleet will report such differences when:
	//
	// * The CRP uses the ReportDiff apply strategy, which instructs Fleet to compare the hub
	//   cluster manifests against the live resources without actually performing any apply op; or
	// * Fleet finds a pre-existing resource on the member cluster side that does not match its
	//   hub cluster counterpart, and the CRP has been configured to only take over a resource if
	//   no configuration differences are found.
	//
	// To control the object size, only the first 100 diffed resources will be included.
	// This field is only meaningful if the `ClusterName` is not empty.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=100
	DiffedPlacements []DiffedResourcePlacement `json:"diffedPlacements,omitempty"`

	// Conditions is an array of current observed conditions for ResourcePlacementStatus.
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// FailedResourcePlacement contains the failure details of a failed resource placement.
type FailedResourcePlacement struct {
	// The resource failed to be placed.
	// +kubebuilder:validation:Required
	ResourceIdentifier `json:",inline"`

	// The failed condition status.
	// +kubebuilder:validation:Required
	Condition metav1.Condition `json:"condition"`
}

// PatchDetail describes a patch that explains an observed configuration drift or
// difference.
//
// A patch detail can be transcribed as a JSON patch operation, as specified in RFC 6902.
type PatchDetail struct {
	// The JSON path that points to a field that has drifted or has configuration differences.
	// +kubebuilder:validation:Required
	Path string `json:"path"`

	// The value at the JSON path from the member cluster side.
	//
	// This field can be empty if the JSON path does not exist on the member cluster side; i.e.,
	// applying the manifest from the hub cluster side would add a new field.
	// +kubebuilder:validation:Optional
	ValueInMember string `json:"valueInMember,omitempty"`

	// The value at the JSON path from the hub cluster side.
	//
	// This field can be empty if the JSON path does not exist on the hub cluster side; i.e.,
	// applying the manifest from the hub cluster side would remove the field.
	// +kubebuilder:validation:Optional
	ValueInHub string `json:"valueInHub,omitempty"`
}

// DriftedResourcePlacement contains the details of a resource with configuration drifts.
type DriftedResourcePlacement struct {
	// The resource that has drifted.
	// +kubebuilder:validation:Required
	ResourceIdentifier `json:",inline"`

	// ObservationTime is the time when we observe the configuration drifts for the resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	ObservationTime metav1.Time `json:"observationTime"`

	// TargetClusterObservedGeneration is the generation of the resource on the target cluster
	// that contains the configuration drifts.
	// +kubebuilder:validation:Required
	TargetClusterObservedGeneration int64 `json:"targetClusterObservedGeneration"`

	// FirstDriftedObservedTime is the first time the resource on the target cluster is
	// observed to have configuration drifts.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	FirstDriftedObservedTime metav1.Time `json:"firstDriftedObservedTime"`

	// ObservedDrifts are the details about the found configuration drifts. Note that
	// Fleet might truncate the details as appropriate to control the object size.
	//
	// Each detail entry specifies how the live state (the state on the member
	// cluster side) compares against the desired state (the state kept in the hub cluster manifest).
	//
	// An event about the details will be emitted as well.
	// +kubebuilder:validation:Optional
	ObservedDrifts []PatchDetail `json:"observedDrifts,omitempty"`
}

// DiffedResourcePlacement contains the details of a resource with configuration differences.
type DiffedResourcePlacement struct {
	// The resource that has drifted.
	// +kubebuilder:validation:Required
	ResourceIdentifier `json:",inline"`

	// ObservationTime is the time when we observe the configuration differences for the resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	ObservationTime metav1.Time `json:"observationTime"`

	// TargetClusterObservedGeneration is the generation of the resource on the target cluster
	// that contains the configuration differences.
	// +kubebuilder:validation:Required
	TargetClusterObservedGeneration int64 `json:"targetClusterObservedGeneration"`

	// FirstDiffedObservedTime is the first time the resource on the target cluster is
	// observed to have configuration differences.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	FirstDiffedObservedTime metav1.Time `json:"firstDiffedObservedTime"`

	// ObservedDiffs are the details about the found configuration differences. Note that
	// Fleet might truncate the details as appropriate to control the object size.
	//
	// Each detail entry specifies how the live state (the state on the member
	// cluster side) compares against the desired state (the state kept in the hub cluster manifest).
	//
	// An event about the details will be emitted as well.
	// +kubebuilder:validation:Optional
	ObservedDiffs []PatchDetail `json:"observedDiffs,omitempty"`
}

// Toleration allows ClusterResourcePlacement to tolerate any taint that matches
// the triple <key,value,effect> using the matching operator <operator>.
type Toleration struct {
	// Key is the taint key that the toleration applies to. Empty means match all taint keys.
	// If the key is empty, operator must be Exists; this combination means to match all values and all keys.
	// +kubebuilder:validation:Optional
	Key string `json:"key,omitempty"`

	// Operator represents a key's relationship to the value.
	// Valid operators are Exists and Equal. Defaults to Equal.
	// Exists is equivalent to wildcard for value, so that a
	// ClusterResourcePlacement can tolerate all taints of a particular category.
	// +kubebuilder:default=Equal
	// +kubebuilder:validation:Enum=Equal;Exists
	// +kubebuilder:validation:Optional
	Operator corev1.TolerationOperator `json:"operator,omitempty"`

	// Value is the taint value the toleration matches to.
	// If the operator is Exists, the value should be empty, otherwise just a regular string.
	// +kubebuilder:validation:Optional
	Value string `json:"value,omitempty"`

	// Effect indicates the taint effect to match. Empty means match all taint effects.
	// When specified, only allowed value is NoSchedule.
	// +kubebuilder:validation:Enum=NoSchedule
	// +kubebuilder:validation:Optional
	Effect corev1.TaintEffect `json:"effect,omitempty"`
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

	// ClusterResourcePlacementRolloutStartedConditionType indicates whether the selected resources start rolling out or
	// not.
	// Its condition status can be one of the following:
	// - "True" means the selected resources successfully start rolling out in all scheduled clusters.
	// - "False" means the selected resources have not been rolled out in all scheduled clusters yet.
	// - "Unknown" means we don't have a rollout decision yet.
	ClusterResourcePlacementRolloutStartedConditionType ClusterResourcePlacementConditionType = "ClusterResourcePlacementRolloutStarted"

	// ClusterResourcePlacementOverriddenConditionType indicates whether all the selected resources have been overridden
	// successfully before applying to the target cluster.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully overridden before applying to the target cluster or
	// override is not needed if there is no override defined with the reason of NoOverrideSpecified.
	// - "False" means some of them have failed.
	// - "Unknown" means we haven't finished the override yet.
	ClusterResourcePlacementOverriddenConditionType ClusterResourcePlacementConditionType = "ClusterResourcePlacementOverridden"

	// ClusterResourcePlacementWorkSynchronizedConditionType indicates whether the selected resources are created or updated
	// under the per-cluster namespaces (i.e., fleet-member-<member-name>) on the hub cluster.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully created or updated under the per-cluster namespaces
	// (i.e., fleet-member-<member-name>) on the hub cluster.
	// - "False" means all the selected resources have not been created or updated under the per-cluster namespaces
	// (i.e., fleet-member-<member-name>) on the hub cluster yet.
	// - "Unknown" means we haven't started processing the work yet.
	ClusterResourcePlacementWorkSynchronizedConditionType ClusterResourcePlacementConditionType = "ClusterResourcePlacementWorkSynchronized"

	// ClusterResourcePlacementAppliedConditionType indicates whether all the selected member clusters have applied
	// the selected resources locally.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully applied to all the target clusters or apply is not needed
	// if there are no cluster(s) selected by the scheduler.
	// - "False" means some of them have failed. We will place some of the detailed failure in the FailedResourcePlacement array.
	// - "Unknown" means we haven't finished the apply yet.
	ClusterResourcePlacementAppliedConditionType ClusterResourcePlacementConditionType = "ClusterResourcePlacementApplied"

	// ClusterResourcePlacementAvailableConditionType indicates whether the selected resources are available on all the
	// selected member clusters.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are available on all the selected member clusters.
	// - "False" means some of them are not available yet. We will place some of the detailed failure in the FailedResourcePlacement
	// array.
	// - "Unknown" means we haven't finished the apply yet so that we cannot check the resource availability.
	ClusterResourcePlacementAvailableConditionType ClusterResourcePlacementConditionType = "ClusterResourcePlacementAvailable"
)

// ResourcePlacementConditionType defines a specific condition of a resource placement.
// +enum
type ResourcePlacementConditionType string

const (
	// ResourceScheduledConditionType indicates whether we have successfully scheduled the selected resources.
	// Its condition status can be one of the following:
	// - "True" means we have successfully scheduled the resources to satisfy the placement requirement.
	// - "False" means we didn't fully satisfy the placement requirement. We will fill the Message field.
	ResourceScheduledConditionType ResourcePlacementConditionType = "Scheduled"

	// ResourceRolloutStartedConditionType indicates whether the selected resources start rolling out or
	// not.
	// Its condition status can be one of the following:
	// - "True" means the selected resources successfully start rolling out in the target clusters.
	// - "False" means the selected resources have not been rolled out in the target cluster yet to honor the rollout
	// strategy configurations specified in the placement
	// - "Unknown" means it is in the processing state.
	ResourceRolloutStartedConditionType ResourcePlacementConditionType = "RolloutStarted"

	// ResourceOverriddenConditionType indicates whether all the selected resources have been overridden successfully
	// before applying to the target cluster if there is any override defined.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully overridden before applying to the target cluster or
	// override is not needed if there is no override defined with the reason of NoOverrideSpecified.
	// - "False" means some of them have failed.
	// - "Unknown" means we haven't finished the override yet.
	ResourceOverriddenConditionType ResourcePlacementConditionType = "Overridden"

	// ResourceWorkSynchronizedConditionType indicates whether we have created or updated the corresponding work object(s)
	// under the per-cluster namespaces (i.e., fleet-member-<member-name>) which have the latest resources selected by
	// the placement.
	// Its condition status can be one of the following:
	// - "True" means we have successfully created the latest corresponding work(s) or updated the existing work(s) to
	// the latest.
	// - "False" means we have not created the latest corresponding work(s) or updated the existing work(s) to the latest
	// yet.
	// - "Unknown" means we haven't finished creating work yet.
	ResourceWorkSynchronizedConditionType ResourcePlacementConditionType = "WorkSynchronized"

	// ResourcesAppliedConditionType indicates whether the selected member cluster has applied the selected resources locally.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully applied to the target cluster.
	// - "False" means some of them have failed.
	// - "Unknown" means we haven't finished the apply yet.
	ResourcesAppliedConditionType ResourcePlacementConditionType = "Applied"

	// ResourcesAvailableConditionType indicates whether the selected resources are available on the selected member cluster.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are available on the target cluster.
	// - "False" means some of them are not available yet.
	// - "Unknown" means we haven't finished the apply yet so that we cannot check the resource availability.
	ResourcesAvailableConditionType ResourcePlacementConditionType = "Available"
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

// Tolerations returns tolerations for ClusterResourcePlacement.
func (m *ClusterResourcePlacement) Tolerations() []Toleration {
	if m.Spec.Policy != nil {
		return m.Spec.Policy.Tolerations
	}
	return nil
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
