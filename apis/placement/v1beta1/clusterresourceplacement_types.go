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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubefleet-dev/kubefleet/apis"
)

const (
	// PlacementCleanupFinalizer is a finalizer added by the placement controller to all placement objects, to make sure
	// that the placement controller can react to placement object deletions if necessary.
	PlacementCleanupFinalizer = FleetPrefix + "crp-cleanup"

	// SchedulerCleanupFinalizer is a finalizer added by the scheduler to placement objects, to make sure
	// that all bindings derived from a placement object can be cleaned up after the placement object is deleted.
	SchedulerCleanupFinalizer = FleetPrefix + "scheduler-cleanup"
)

// make sure the PlacementObj and PlacementObjList interfaces are implemented by the
// ClusterResourcePlacement and ResourcePlacement types.
var _ PlacementObj = &ClusterResourcePlacement{}
var _ PlacementObj = &ResourcePlacement{}
var _ PlacementObjList = &ClusterResourcePlacementList{}
var _ PlacementObjList = &ResourcePlacementList{}

// PlacementSpecGetterSetter offers the functionality to work with the PlacementSpecGetterSetter.
// +kubebuilder:object:generate=false
type PlacementSpecGetterSetter interface {
	GetPlacementSpec() *PlacementSpec
	SetPlacementSpec(PlacementSpec)
}

// PlacementStatusGetterSetter offers the functionality to work with the PlacementStatusGetterSetter.
// +kubebuilder:object:generate=false
type PlacementStatusGetterSetter interface {
	GetPlacementStatus() *PlacementStatus
	SetPlacementStatus(PlacementStatus)
}

// PlacementObj offers the functionality to work with fleet placement object.
// +kubebuilder:object:generate=false
type PlacementObj interface {
	apis.ConditionedObj
	PlacementSpecGetterSetter
	PlacementStatusGetterSetter
}

// PlacementListItemGetter offers the functionality to get a list of PlacementObj items.
// +kubebuilder:object:generate=false
type PlacementListItemGetter interface {
	GetPlacementObjs() []PlacementObj
}

// PlacementObjList offers the functionality to work with fleet placement object list.
// +kubebuilder:object:generate=false
type PlacementObjList interface {
	client.ObjectList
	PlacementListItemGetter
}

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
	// +kubebuilder:validation:XValidation:rule="!(has(oldSelf.policy) && !has(self.policy))",message="policy cannot be removed once set"
	// +kubebuilder:validation:XValidation:rule="!(self.statusReportingScope == 'NamespaceAccessible' && size(self.resourceSelectors.filter(x, x.kind == 'Namespace')) != 1)",message="when statusReportingScope is NamespaceAccessible, exactly one resourceSelector with kind 'Namespace' is required"
	// +kubebuilder:validation:XValidation:rule="!has(oldSelf.statusReportingScope) || self.statusReportingScope == oldSelf.statusReportingScope",message="statusReportingScope is immutable"
	Spec PlacementSpec `json:"spec"`

	// The observed status of ClusterResourcePlacement.
	// +kubebuilder:validation:Optional
	Status PlacementStatus `json:"status,omitempty"`
}

// PlacementSpec defines the desired state of ClusterResourcePlacement and ResourcePlacement.
type PlacementSpec struct {
	// ResourceSelectors is an array of selectors used to select cluster scoped resources. The selectors are `ORed`.
	// You can have 1-100 selectors.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=100
	ResourceSelectors []ResourceSelectorTerm `json:"resourceSelectors"`

	// Policy defines how to select member clusters to place the selected resources.
	// If unspecified, all the joined member clusters are selected.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="!(self.placementType != oldSelf.placementType)",message="placement type is immutable"
	Policy *PlacementPolicy `json:"policy,omitempty"`

	// The rollout strategy to use to replace existing placement with new ones.
	// +kubebuilder:validation:Optional
	// +patchStrategy=retainKeys
	Strategy RolloutStrategy `json:"strategy,omitempty"`

	// The number of old SchedulingPolicySnapshot or ResourceSnapshot resources to retain to allow rollback.
	// This is a pointer to distinguish between explicit zero and not specified.
	// Defaults to 10.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=10
	// +kubebuilder:validation:Optional
	RevisionHistoryLimit *int32 `json:"revisionHistoryLimit,omitempty"`

	// StatusReportingScope controls where ClusterResourcePlacement status information is made available.
	// When set to "ClusterScopeOnly", status is accessible only through the cluster-scoped ClusterResourcePlacement object.
	// When set to "NamespaceAccessible", a ClusterResourcePlacementStatus object is created in the target namespace,
	// providing namespace-scoped access to the placement status alongside the cluster-scoped status. This option is only
	// supported when the ClusterResourcePlacement targets exactly one namespace.
	// Defaults to "ClusterScopeOnly".
	// +kubebuilder:default=ClusterScopeOnly
	// +kubebuilder:validation:Enum=ClusterScopeOnly;NamespaceAccessible
	// +kubebuilder:validation:Optional
	StatusReportingScope StatusReportingScope `json:"statusReportingScope,omitempty"`
}

// Tolerations returns tolerations for PlacementSpec to handle nil policy case.
func (p *PlacementSpec) Tolerations() []Toleration {
	if p.Policy != nil {
		return p.Policy.Tolerations
	}
	return nil
}

// ResourceSelectorTerm is used to select resources as the target resources to be placed.
// All the fields are `ANDed`. In other words, a resource must match all the fields to be selected.
type ResourceSelectorTerm struct {
	// Group name of the be selected resource.
	// Use an empty string to select resources under the core API group (e.g., namespaces).
	// +kubebuilder:validation:Required
	Group string `json:"group"`

	// Version of the to be selected resource.
	// +kubebuilder:validation:Required
	Version string `json:"version"`

	// Kind of the to be selected resource.
	// Note: When `Kind` is `namespace`, by default ALL the resources under the selected namespaces are selected.
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// You can only specify at most one of the following two fields: Name and LabelSelector.
	// If none is specified, all the be selected resources with the given group, version and kind are selected.

	// Name of the be selected  resource.
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`

	// A label query over all the be selected  resources. Resources matching the query are selected.
	// Note that namespace-scoped resources can't be selected even if they match the query.
	// +kubebuilder:validation:Optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// SelectionScope defines the scope of resource selections when the Kind is `namespace`.
	// +kubebuilder:validation:Enum=NamespaceOnly;NamespaceWithResources
	// +kubebuilder:default=NamespaceWithResources
	// +kubebuilder:validation:Optional
	SelectionScope SelectionScope `json:"selectionScope,omitempty"`
}

// SelectionScope defines the scope of resource selections.
type SelectionScope string

const (
	// NamespaceOnly means only the namespace itself is selected.
	NamespaceOnly SelectionScope = "NamespaceOnly"

	// NamespaceWithResources means all the resources under the namespace including namespace itself are selected.
	NamespaceWithResources SelectionScope = "NamespaceWithResources"
)

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

// StatusReportingScope defines where ClusterResourcePlacement status information is made available.
// This setting only applies to ClusterResourcePlacements that select resources from a single namespace.
// It enables different levels of access to placement status across cluster and namespace scopes.
// +enum
type StatusReportingScope string

const (

	// ClusterScopeOnly makes status available only through the cluster-scoped ClusterResourcePlacement object.
	// This is the default behavior where status information is accessible only to users with cluster-level permissions.
	ClusterScopeOnly StatusReportingScope = "ClusterScopeOnly"

	// NamespaceAccessible makes status available in both cluster and namespace scopes.
	// In addition to the cluster-scoped status, a ClusterResourcePlacementStatus object is created
	// in the target namespace, enabling namespace-scoped users to access placement status information.
	NamespaceAccessible StatusReportingScope = "NamespaceAccessible"
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

	// DeleteStrategy configures the deletion behavior when the ClusterResourcePlacement is deleted.
	// +kubebuilder:validation:Optional
	DeleteStrategy *DeleteStrategy `json:"deleteStrategy,omitempty"`

	// ReportBackStrategy describes how to report back the status of applied resources on the member cluster.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="(self == null) || (self.type == 'Mirror' ? size(self.destination) != 0 : true)",message="when reportBackStrategy.type is 'Mirror', a destination must be specified"
	ReportBackStrategy *ReportBackStrategy `json:"reportBackStrategy,omitempty"`
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
	ComparisonOption ComparisonOptionType `json:"comparisonOption,omitempty"`

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
	//   Note that this option would revert any ad-hoc changes made on the member cluster side in the
	//   managed fields; if you would like to make temporary edits on the member cluster side
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
	//   If configuration differences are found on a resource, Fleet will consider this as an apply
	//   error, which might block rollout depending on the specified rollout strategy.
	//
	//   Use ComparisonOption setting to control how the difference is calculated.
	//
	// ClientSideApply and ServerSideApply apply strategies only work when Fleet can assume
	// ownership of a resource (e.g., the resource is created by Fleet, or Fleet has taken over
	// the resource). See the comments on the WhenToTakeOver field for more information.
	// ReportDiff apply strategy, however, will function regardless of Fleet's ownership
	// status. One may set up a CRP with the ReportDiff strategy and the Never takeover option,
	// and this will turn Fleet into a detection tool that reports only configuration differences
	// but do not touch any resources on the member cluster side.
	//
	// For a comparison between the different strategies and usage examples, refer to the
	// Fleet documentation.
	//
	// +kubebuilder:default=ClientSideApply
	// +kubebuilder:validation:Enum=ClientSideApply;ServerSideApply;ReportDiff
	// +kubebuilder:validation:Optional
	Type ApplyStrategyType `json:"type,omitempty"`

	// AllowCoOwnership controls whether co-ownership between Fleet and other agents are allowed
	// on a Fleet-managed resource. If set to false, Fleet will refuse to apply manifests to
	// a resource that has been owned by one or more non-Fleet agents.
	//
	// Note that Fleet does not support the case where one resource is being placed multiple
	// times by different CRPs on the same member cluster. An apply error will be returned if
	// Fleet finds that a resource has been owned by another placement attempt by Fleet, even
	// with the AllowCoOwnership setting set to true.
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
	//
	//   This is a safer option as pre-existing resources that are inconsistent with the hub cluster
	//   manifests will not be overwritten; Fleet will ignore them until the inconsistencies
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
	// * Never: with this action, Fleet will not apply a hub cluster manifest to the member
	//   clusters if there is a corresponding pre-existing resource. However, if a manifest
	//   has never been applied yet; or it has a corresponding resource which Fleet has assumed
	//   ownership, apply op will still be executed.
	//
	//   This is the safest option; one will have to remove the pre-existing resources (so that
	//   Fleet can re-create them) or switch to a different
	//   WhenToTakeOver option before Fleet starts processing the corresponding hub cluster
	//   manifests.
	//
	//   If you prefer Fleet stop processing all manifests, use this option along with the
	//   ReportDiff apply strategy type. This setup would instruct Fleet to touch nothing
	//   on the member cluster side but still report configuration differences between the
	//   hub cluster and member clusters. Fleet will not give up ownership
	//   that it has already assumed though.
	//
	// +kubebuilder:default=Always
	// +kubebuilder:validation:Enum=Always;IfNoDiff;Never
	// +kubebuilder:validation:Optional
	WhenToTakeOver WhenToTakeOverType `json:"whenToTakeOver,omitempty"`
}

// ComparisonOptionType describes the compare option that Fleet uses to detect drifts and/or
// calculate differences.
// +enum
type ComparisonOptionType string

const (
	// ComparisonOptionTypePartialComparison will compare only fields that are managed by Fleet, i.e.,
	// fields that are specified explicitly in the hub cluster manifest. Unmanaged fields
	// are ignored.
	ComparisonOptionTypePartialComparison ComparisonOptionType = "PartialComparison"

	// ComparisonOptionTypeFullDiff will compare all fields of the resource, even if the fields
	// are absent from the hub cluster manifest.
	ComparisonOptionTypeFullComparison ComparisonOptionType = "FullComparison"
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
	// WhenToTakeOverTypeIfNoDiff instructs Fleet to apply a manifest with a corresponding
	// pre-existing resource on a member cluster if and only if the pre-existing resource
	// looks the same as the manifest. Should there be any inconsistency, Fleet will skip
	// the apply op; no change will be made on the resource and Fleet will not claim
	// ownership on it.
	//
	// Note that this will not stop Fleet from processing other manifests in the same
	// placement that do not concern the takeover process (e.g., the manifests that have
	// not been created yet, or that are already under the management of Fleet).
	WhenToTakeOverTypeIfNoDiff WhenToTakeOverType = "IfNoDiff"

	// WhenToTakeOverTypeAlways instructs Fleet to always apply manifests to a member cluster,
	// even if there are some corresponding pre-existing resources. Some fields on these
	// resources might be overwritten, and Fleet will claim ownership on them.
	WhenToTakeOverTypeAlways WhenToTakeOverType = "Always"

	// WhenToTakeOverTypeNever instructs Fleet to never apply a manifest to a member cluster
	// if there is a corresponding pre-existing resource.
	//
	// Note that this will not stop Fleet from processing other manifests in the same placement
	// that do not concern the takeover process (e.g., the manifests that have not been created
	// yet, or that are already under the management of Fleet).
	//
	// If you would like Fleet to stop processing manifests all together and do not assume
	// ownership on any pre-existing resources, use this option along with the ReportDiff
	// apply strategy type. This setup would instruct Fleet to touch nothing on the member
	// cluster side but still report configuration differences between the hub cluster
	// and member clusters. Fleet will not give up ownership that it has already assumed, though.
	WhenToTakeOverTypeNever WhenToTakeOverType = "Never"
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
	// The minimum of MaxUnavailable is 0 to allow no downtime moving a placement from one cluster to another.
	// Please set it to be greater than 0 to avoid rolling out stuck during in-place resource update.
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

// PlacementStatus defines the observed status of the ClusterResourcePlacement and ResourcePlacement object.
type PlacementStatus struct {
	// SelectedResources contains a list of resources selected by ResourceSelectors.
	// This field is only meaningful if the `ObservedResourceIndex` is not empty.
	// +kubebuilder:validation:Optional
	SelectedResources []ResourceIdentifier `json:"selectedResources,omitempty"`

	// Resource index logically represents the generation of the selected resources.
	// We take a new snapshot of the selected resources whenever the selection or their content change.
	// Each snapshot has a different resource index.
	// One resource snapshot can contain multiple clusterResourceSnapshots CRs in order to store large amount of resources.
	// To get clusterResourceSnapshot of a given resource index, use the following command:
	// `kubectl get ClusterResourceSnapshot --selector=kubernetes-fleet.io/resource-index=$ObservedResourceIndex`
	// If the rollout strategy type is `RollingUpdate`, `ObservedResourceIndex` is the default-latest resource snapshot index.
	// If the rollout strategy type is `External`, rollout and version control are managed by an external controller,
	// and this field is not empty only if all targeted clusters observe the same resource index in `PlacementStatuses`.
	// +kubebuilder:validation:Optional
	ObservedResourceIndex string `json:"observedResourceIndex,omitempty"`

	// PerClusterPlacementStatuses contains a list of placement status on the clusters that are selected by PlacementPolicy.
	// Each selected cluster according to the observed resource placement is guaranteed to have a corresponding placementStatuses.
	// In the pickN case, there are N placement statuses where N = NumberOfClusters; Or in the pickFixed case, there are
	// N placement statuses where N = ClusterNames.
	// In these cases, some of them may not have assigned clusters when we cannot fill the required number of clusters.
	// TODO, For pickAll type, considering providing unselected clusters info.
	// +kubebuilder:validation:Optional
	PerClusterPlacementStatuses []PerClusterPlacementStatus `json:"placementStatuses,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for ClusterResourcePlacement.
	// All conditions except `ClusterResourcePlacementScheduled` correspond to the resource snapshot at the index specified by `ObservedResourceIndex`.
	// For example, a condition of `ClusterResourcePlacementWorkSynchronized` type
	// is observing the synchronization status of the resource snapshot with index `ObservedResourceIndex`.
	// If the rollout strategy type is `External`, and `ObservedResourceIndex` is unset due to clusters reporting different resource indices,
	// conditions except `ClusterResourcePlacementScheduled` will be empty or set to Unknown.
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

	// TO-DO (chenyu1): drop the enum value ConfigMap after the new envelope forms become fully available.

	// Type of the envelope object.
	// +kubebuilder:validation:Enum=ConfigMap;ClusterResourceEnvelope;ResourceEnvelope
	// +kubebuilder:default=ConfigMap
	// +kubebuilder:validation:Optional
	Type EnvelopeType `json:"type"`
}

// EnvelopeType defines the type of the envelope object.
// +enum
type EnvelopeType string

const (
	// ConfigMapEnvelopeType means the envelope object is of type `ConfigMap`.
	// TO-DO (chenyu1): drop this type after the configMap-based envelopes become obsolete.
	ConfigMapEnvelopeType EnvelopeType = "ConfigMap"

	// ClusterResourceEnvelopeType is the envelope type that represents the ClusterResourceEnvelope custom resource.
	ClusterResourceEnvelopeType EnvelopeType = "ClusterResourceEnvelope"

	// ResourceEnvelopeType is the envelope type that represents the ResourceEnvelope custom resource.
	ResourceEnvelopeType EnvelopeType = "ResourceEnvelope"
)

// PerClusterPlacementStatus represents the placement status of selected resources for one target cluster.
type PerClusterPlacementStatus struct {
	// ClusterName is the name of the cluster this resource is assigned to.
	// If it is not empty, its value should be unique cross all placement decisions for the Placement.
	// +kubebuilder:validation:Optional
	ClusterName string `json:"clusterName,omitempty"`

	// ObservedResourceIndex is the index of the resource snapshot that is currently being rolled out to the given cluster.
	// This field is only meaningful if the `ClusterName` is not empty.
	// +kubebuilder:validation:Optional
	ObservedResourceIndex string `json:"observedResourceIndex,omitempty"`

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

	// Conditions is an array of current observed conditions on the cluster.
	// Each condition corresponds to the resource snapshot at the index specified by `ObservedResourceIndex`.
	// For example, the condition of type `RolloutStarted` is observing the rollout status of the resource snapshot with index `ObservedResourceIndex`.
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// FailedResourcePlacement contains the failure details of a failed resource placement.
type FailedResourcePlacement struct {
	// The resource failed to be placed.
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
	ResourceIdentifier `json:",inline"`

	// ObservationTime is the time when we observe the configuration differences for the resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	ObservationTime metav1.Time `json:"observationTime"`

	// TargetClusterObservedGeneration is the generation of the resource on the target cluster
	// that contains the configuration differences.
	//
	// This might be nil if the resource has not been created yet on the target cluster.
	//
	// +kubebuilder:validation:Optional
	TargetClusterObservedGeneration *int64 `json:"targetClusterObservedGeneration"`

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

// ClusterResourcePlacementConditionType defines a specific condition of a cluster resource placement object.
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

	// ClusterResourcePlacementDiffReportedConditionType indicates whether Fleet has reported
	// configuration differences between the desired states of resources as kept in the hub cluster
	// and the current states on the all member clusters.
	//
	// It can have the following condition statuses:
	// * True: Fleet has reported complete sets of configuration differences on all member clusters.
	// * False: Fleet has not yet reported complete sets of configuration differences on some member
	//   clusters, or an error has occurred.
	// * Unknown: Fleet has not finished processing the diff reporting yet.
	ClusterResourcePlacementDiffReportedConditionType ClusterResourcePlacementConditionType = "ClusterResourcePlacementDiffReported"

	// ClusterResourcePlacementStatusSyncedConditionType indicates whether Fleet has successfully
	// created or updated the ClusterResourcePlacementStatus object in the target namespace when
	// StatusReportingScope is NamespaceAccessible.
	//
	// It can have the following condition statuses:
	// * True: Fleet has successfully created or updated the ClusterResourcePlacementStatus object
	//   in the target namespace.
	// * False: Fleet has failed to create or update the ClusterResourcePlacementStatus object
	//   in the target namespace.
	ClusterResourcePlacementStatusSyncedConditionType ClusterResourcePlacementConditionType = "ClusterResourcePlacementStatusSynced"
)

// ResourcePlacementConditionType defines a specific condition of a resource placement object.
// +enum
type ResourcePlacementConditionType string

const (
	// ResourcePlacementScheduledConditionType indicates whether we have successfully scheduled the ResourcePlacement.
	// Its condition status can be one of the following:
	// - "True" means we have successfully scheduled the resources to fully satisfy the placement requirement.
	// - "False" means we didn't fully satisfy the placement requirement. We will fill the Reason field.
	// - "Unknown" means we don't have a scheduling decision yet.
	ResourcePlacementScheduledConditionType ResourcePlacementConditionType = "ResourcePlacementScheduled"

	// ResourcePlacementRolloutStartedConditionType indicates whether the selected resources start rolling out or not.
	// Its condition status can be one of the following:
	// - "True" means the selected resources successfully start rolling out in all scheduled clusters.
	// - "False" means the selected resources have not been rolled out in all scheduled clusters yet.
	// - "Unknown" means we don't have a rollout decision yet.
	ResourcePlacementRolloutStartedConditionType ResourcePlacementConditionType = "ResourcePlacementRolloutStarted"

	// ResourcePlacementOverriddenConditionType indicates whether all the selected resources have been overridden
	// successfully before applying to the target cluster.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully overridden before applying to the target cluster or
	// override is not needed if there is no override defined with the reason of NoOverrideSpecified.
	// - "False" means some of them have failed.
	// - "Unknown" means we haven't finished the override yet.
	ResourcePlacementOverriddenConditionType ResourcePlacementConditionType = "ResourcePlacementOverridden"

	// ResourcePlacementWorkSynchronizedConditionType indicates whether the selected resources are created or updated
	// under the per-cluster namespaces (i.e., fleet-member-<member-name>) on the hub cluster.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully created or updated under the per-cluster namespaces
	// (i.e., fleet-member-<member-name>) on the hub cluster.
	// - "False" means all the selected resources have not been created or updated under the per-cluster namespaces
	// (i.e., fleet-member-<member-name>) on the hub cluster yet.
	// - "Unknown" means we haven't started processing the work yet.
	ResourcePlacementWorkSynchronizedConditionType ResourcePlacementConditionType = "ResourcePlacementWorkSynchronized"

	// ResourcePlacementAppliedConditionType indicates whether all the selected member clusters have applied
	// the selected resources locally.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully applied to all the target clusters or apply is not needed
	// if there are no cluster(s) selected by the scheduler.
	// - "False" means some of them have failed. We will place some of the detailed failure in the FailedResourcePlacement array.
	// - "Unknown" means we haven't finished the apply yet.
	ResourcePlacementAppliedConditionType ResourcePlacementConditionType = "ResourcePlacementApplied"

	// ResourcePlacementAvailableConditionType indicates whether the selected resources are available on all the
	// selected member clusters.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are available on all the selected member clusters.
	// - "False" means some of them are not available yet. We will place some of the detailed failure in the FailedResourcePlacement
	// array.
	// - "Unknown" means we haven't finished the apply yet so that we cannot check the resource availability.
	ResourcePlacementAvailableConditionType ResourcePlacementConditionType = "ResourcePlacementAvailable"

	// ResourcePlacementDiffReportedConditionType indicates whether Fleet has reported
	// configuration differences between the desired states of resources as kept in the hub cluster
	// and the current states on the all member clusters.
	//
	// It can have the following condition statuses:
	// * True: Fleet has reported complete sets of configuration differences on all member clusters.
	// * False: Fleet has not yet reported complete sets of configuration differences on some member
	//   clusters, or an error has occurred.
	// * Unknown: Fleet has not finished processing the diff reporting yet.
	ResourcePlacementDiffReportedConditionType ResourcePlacementConditionType = "ResourcePlacementDiffReported"
)

// PerClusterPlacementConditionType defines a specific condition of a per cluster placement.
// +enum
type PerClusterPlacementConditionType string

const (
	// PerClusterScheduledConditionType indicates whether we have successfully scheduled the selected resources on a particular cluster.
	// Its condition status can be one of the following:
	// - "True" means we have successfully scheduled the resources to satisfy the placement requirement.
	// - "False" means we didn't fully satisfy the placement requirement. We will fill the Message field.
	PerClusterScheduledConditionType PerClusterPlacementConditionType = "Scheduled"

	// PerClusterRolloutStartedConditionType indicates whether the selected resources start rolling out on that particular member cluster.
	// Its condition status can be one of the following:
	// - "True" means the selected resources successfully start rolling out in the target clusters.
	// - "False" means the selected resources have not been rolled out in the target cluster yet to honor the rollout
	// strategy configurations specified in the placement
	// - "Unknown" means it is in the processing state.
	PerClusterRolloutStartedConditionType PerClusterPlacementConditionType = "RolloutStarted"

	// PerClusterOverriddenConditionType indicates whether all the selected resources have been overridden successfully
	// before applying to the target cluster if there is any override defined.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully overridden before applying to the target cluster or
	// override is not needed if there is no override defined with the reason of NoOverrideSpecified.
	// - "False" means some of them have failed.
	// - "Unknown" means we haven't finished the override yet.
	PerClusterOverriddenConditionType PerClusterPlacementConditionType = "Overridden"

	// PerClusterWorkSynchronizedConditionType indicates whether we have created or updated the corresponding work object(s)
	// under that particular cluster namespaces (i.e., fleet-member-<member-name>) which have the latest resources selected by
	// the placement.
	// Its condition status can be one of the following:
	// - "True" means we have successfully created the latest corresponding work(s) or updated the existing work(s) to
	// the latest.
	// - "False" means we have not created the latest corresponding work(s) or updated the existing work(s) to the latest
	// yet.
	// - "Unknown" means we haven't finished creating work yet.
	PerClusterWorkSynchronizedConditionType PerClusterPlacementConditionType = "WorkSynchronized"

	// PerClusterAppliedConditionType indicates whether the selected member cluster has applied the selected resources.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are successfully applied to the target cluster.
	// - "False" means some of them have failed.
	// - "Unknown" means we haven't finished the apply yet.
	PerClusterAppliedConditionType PerClusterPlacementConditionType = "Applied"

	// PerClusterAvailableConditionType indicates whether the selected resources are available on the selected member cluster.
	// Its condition status can be one of the following:
	// - "True" means all the selected resources are available on the target cluster.
	// - "False" means some of them are not available yet.
	// - "Unknown" means we haven't finished the apply yet so that we cannot check the resource availability.
	PerClusterAvailableConditionType PerClusterPlacementConditionType = "Available"

	// ResourceDiffReportedConditionType indicates whether Fleet has reported configuration
	// differences between the desired states of resources as kept in the hub cluster and the
	// current states on the selected member cluster.
	//
	// It can have the following condition statuses:
	// * True: Fleet has reported the complete set of configuration differences on the member cluster.
	// * False: Fleet has not yet reported the complete set of configuration differences on the
	//   member cluster, or an error has occurred.
	// * Unknown: Fleet has not finished processing the diff reporting yet.
	PerClusterDiffReportedConditionType PerClusterPlacementConditionType = "DiffReported"
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

// DeleteStrategy configures the deletion behavior when a placement is deleted.
type DeleteStrategy struct {
	// PropagationPolicy controls whether to delete placed resources when placement is deleted.
	//
	// Available options:
	//
	// * Delete: all placed resources on member clusters will be deleted when
	//   the placement is deleted. This is the default behavior.
	//
	// * Abandon: all placed resources on member clusters will be left intact (abandoned)
	//   when the placement is deleted.
	//
	// +kubebuilder:validation:Enum=Abandon;Delete
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Optional
	PropagationPolicy DeletePropagationPolicy `json:"propagationPolicy,omitempty"`
}

// DeletePropagationPolicy identifies the propagation policy when a placement is deleted.
// +enum
type DeletePropagationPolicy string

const (
	// DeletePropagationPolicyAbandon instructs Fleet to leave (abandon) all placed resources on member
	// clusters when the placement is deleted.
	DeletePropagationPolicyAbandon DeletePropagationPolicy = "Abandon"

	// DeletePropagationPolicyDelete instructs Fleet to delete all placed resources on member clusters
	// when the placement is deleted. This is the default behavior.
	DeletePropagationPolicyDelete DeletePropagationPolicy = "Delete"
)

type ReportBackStrategyType string

const (
	// ReportBackStrategyTypeDisabled disables status back-reporting from the member clusters.
	ReportBackStrategyTypeDisabled ReportBackStrategyType = "Disabled"

	// ReportBackStrategyTypeMirror enables status back-reporting by
	// copying the status fields verbatim to some destination on the hub cluster side.
	ReportBackStrategyTypeMirror ReportBackStrategyType = "Mirror"
)

type ReportBackDestination string

const (
	// ReportBackDestinationOriginalResource implies the status fields will be copied verbatim to the
	// the original resource on the hub cluster side. This is only performed when the placement object has a
	// scheduling policy that selects exactly one member cluster (i.e., a pickFixed scheduling policy with
	// exactly one cluster name, or a pickN scheduling policy with the numberOfClusters field set to 1).
	ReportBackDestinationOriginalResource ReportBackDestination = "OriginalResource"

	// ReportBackDestinationWorkAPI implies the status fields will be copied verbatim via the Work API
	// on the hub cluster side. Users may look up the status of a specific resource applied to a specific
	// member cluster by inspecting the corresponding Work object on the hub cluster side.
	ReportBackDestinationWorkAPI ReportBackDestination = "WorkAPI"
)

// ReportBackStrategy describes how to report back the resource status from member clusters.
type ReportBackStrategy struct {
	// Type dictates the type of the report back strategy to use.
	//
	// Available options include:
	//
	// * Disabled: status back-reporting is disabled. This is the default behavior.
	//
	// * Mirror: status back-reporting is enabled by copying the status fields verbatim to
	//   a destination on the hub cluster side; see the Destination field for more information.
	//
	// +kubebuilder:default=Disabled
	// +kubebuilder:validation:Enum=Disabled;Mirror
	// +kubebuilder:validation:Required
	Type ReportBackStrategyType `json:"type"`

	// Destination dictates where to copy the status fields to when the report back strategy type is Mirror.
	//
	// Available options include:
	//
	// * OriginalResource: the status fields will be copied verbatim to the original resource on the hub cluster side.
	//   This is only performed when the placement object has a scheduling policy that selects exactly one member cluster
	//   (i.e., a pickFixed scheduling policy with exactly one cluster name, or a pickN scheduling policy with the numberOfClusters
	//   field set to 1).
	//
	// * WorkAPI: the status fields will be copied verbatim via the Work API on the hub cluster side. Users may look up
	//   the status of a specific resource applied to a specific member cluster by inspecting the corresponding Work object
	//   on the hub cluster side. This is the default behavior.
	//
	// +kubebuilder:validation:Enum=OriginalResource;WorkAPI
	// +kubebuilder:validation:Optional
	Destination *ReportBackDestination `json:"destination,omitempty"`
}

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

// GetPlacementObjs returns the placement objects in the list.
func (crpl *ClusterResourcePlacementList) GetPlacementObjs() []PlacementObj {
	objs := make([]PlacementObj, len(crpl.Items))
	for i := range crpl.Items {
		objs[i] = &crpl.Items[i]
	}
	return objs
}

// GetCondition returns the condition of the ClusterResourcePlacement objects.
func (m *ClusterResourcePlacement) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

// GetPlacementSpec returns the placement spec.
func (m *ClusterResourcePlacement) GetPlacementSpec() *PlacementSpec {
	return &m.Spec
}

// SetPlacementSpec sets the placement spec.
func (m *ClusterResourcePlacement) SetPlacementSpec(spec PlacementSpec) {
	spec.DeepCopyInto(&m.Spec)
}

// GetPlacementStatus returns the placement status.
func (m *ClusterResourcePlacement) GetPlacementStatus() *PlacementStatus {
	return &m.Status
}

// SetPlacementStatus sets the placement status.
func (m *ClusterResourcePlacement) SetPlacementStatus(status PlacementStatus) {
	status.DeepCopyInto(&m.Status)
}

const (
	// ResourcePlacementCleanupFinalizer is a finalizer added by the RP controller to all RPs, to make sure
	// that the RP controller can react to RP deletions if necessary.
	ResourcePlacementCleanupFinalizer = FleetPrefix + "rp-cleanup"
)

// +genclient
// +genclient:Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced",shortName=rp,categories={fleet,fleet-placement}
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.metadata.generation`,name="Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.policy.placementType`,name="Type",priority=1,type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ResourcePlacementScheduled")].status`,name="Scheduled",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ResourcePlacementScheduled")].observedGeneration`,name="Scheduled-Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ResourcePlacementWorkSynchronized")].status`,name="Work-Synchronized",priority=1,type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ResourcePlacementWorkSynchronized")].observedGeneration`,name="Work-Synchronized-Gen",priority=1,type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ResourcePlacementAvailable")].status`,name="Available",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ResourcePlacementAvailable")].observedGeneration`,name="Available-Gen",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ResourcePlacement is used to select namespace scoped resources, including built-in resources and custom resources,
// and placement them onto selected member clusters in a fleet.
// `SchedulingPolicySnapshot` and `ResourceSnapshot` objects are created in the same namespace when there are changes in the
// system to keep the history of the changes affecting a `ResourcePlacement`. We will also create `ResourceBinding` objects in the same namespace.
type ResourcePlacement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ResourcePlacement.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="!(has(oldSelf.policy) && !has(self.policy))",message="policy cannot be removed once set"
	Spec PlacementSpec `json:"spec"`

	// The observed status of ResourcePlacement.
	// +kubebuilder:validation:Optional
	Status PlacementStatus `json:"status,omitempty"`
}

// ResourcePlacementList contains a list of ResourcePlacement.
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ResourcePlacementList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourcePlacement `json:"items"`
}

// SetConditions sets the conditions of the ResourcePlacement.
func (m *ResourcePlacement) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

// GetCondition returns the condition of the ResourcePlacement objects.
func (m *ResourcePlacement) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

// GetPlacementSpec returns the placement spec.
func (m *ResourcePlacement) GetPlacementSpec() *PlacementSpec {
	return &m.Spec
}

// SetPlacementSpec sets the placement spec.
func (m *ResourcePlacement) SetPlacementSpec(spec PlacementSpec) {
	spec.DeepCopyInto(&m.Spec)
}

// GetPlacementStatus returns the placement status.
func (m *ResourcePlacement) GetPlacementStatus() *PlacementStatus {
	return &m.Status
}

// SetPlacementStatus sets the placement status.
func (m *ResourcePlacement) SetPlacementStatus(status PlacementStatus) {
	status.DeepCopyInto(&m.Status)
}

// GetPlacementObjs returns the placement objects in the list.
func (rpl *ResourcePlacementList) GetPlacementObjs() []PlacementObj {
	objs := make([]PlacementObj, len(rpl.Items))
	for i := range rpl.Items {
		objs[i] = &rpl.Items[i]
	}
	return objs
}

// +genclient
// +genclient:Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced",shortName=crps,categories={fleet,fleet-placement}
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.sourceStatus.observedResourceIndex`,name="Resource-Index",type=string
// +kubebuilder:printcolumn:JSONPath=`.lastUpdatedTime`,name="Last-Updated",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterResourcePlacementStatus is a namespaced resource that mirrors the PlacementStatus of a corresponding
// ClusterResourcePlacement object. This allows namespace-scoped access to cluster-scoped placement status.
// The LastUpdatedTime field is updated whenever the CRPS object is updated.
//
// This object will be created within the target namespace that contains resources being managed by the CRP.
// When multiple ClusterResourcePlacements target the same namespace, each ClusterResourcePlacementStatus within that
// namespace is uniquely identified by its object name, which corresponds to the specific ClusterResourcePlacement
// that created it.
//
// The name of this object should be the same as the name of the corresponding ClusterResourcePlacement.
type ClusterResourcePlacementStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Source status copied from the corresponding ClusterResourcePlacement.
	// +kubebuilder:validation:Required
	PlacementStatus `json:"sourceStatus,omitempty"`

	// LastUpdatedTime is the timestamp when this CRPS object was last updated.
	// This field is set to the current time whenever the CRPS object is created or modified.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastUpdatedTime metav1.Time `json:"lastUpdatedTime,omitempty"`
}

// ClusterResourcePlacementStatusList contains a list of ClusterResourcePlacementStatus.
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterResourcePlacementStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourcePlacementStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterResourcePlacement{}, &ClusterResourcePlacementList{}, &ResourcePlacement{}, &ResourcePlacementList{}, &ClusterResourcePlacementStatus{}, &ClusterResourcePlacementStatusList{})
}
