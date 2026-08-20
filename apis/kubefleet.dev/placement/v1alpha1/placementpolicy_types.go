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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// The condition types for PlacementPolicy and ClusterPlacementPolicy API objects.
const (
	PlacementPolicyCondTypeResourceCollected = "ResourceCollected"
	PlacementPolicyCondTypeScheduled         = "Scheduled"
	PlacementPolicyCondTypeSynchronized      = "Synchronized"
	PlacementPolicyCondTypeAvailable         = "Available"
)

// The reasons for each condition type of PlacementPolicy and ClusterPlacementPolicy API objects.
const (
	PlacementPolicyResourceCollectedCondReasonAllResourcesCollected        = "AllResourcesCollected"
	PlacementPolicyResourceCollectedCondReasonFailedToCollectSomeResources = "FailedToCollectSomeResources"

	PlacementPolicyScheduledCondReasonFoundAllClusters         = "FoundAllRequiredClusters"
	PlacementPolicyScheduledCondReasonFailedToFindSomeClusters = "FailedToFindSomeRequiredClusters"

	PlacementPolicySynchronizedCondReasonAllClustersSynchronized         = "AllClustersSynchronized"
	PlacementPolicySynchronizedCondReasonFailedToSynchronizeSomeClusters = "FailedToSynchronizeSomeClusters"

	PlacementPolicyAvailableCondReasonAllClustersAvailable    = "ResourcesAvailableOnAllClusters"
	PlacementPolicyAvailableCondReasonSomeClustersUnavailable = "ResourcesUnavailableOnSomeClusters"
)

// PlacementPolicy is the KubeFleet API that enables users to place resources within a namespace across
// member clusters.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={kubefleet, kubefleet-placement}
// +kubebuilder:storageversion
type PlacementPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the placement policy.
	// +kubebuilder:validation:Required
	Spec PlacementPolicySpec `json:"spec,omitempty"`

	// The observed status of the placement policy.
	// +kubebuilder:validation:Optional
	Status PlacementPolicyStatus `json:"status,omitempty"`
}

// Note (chenyu1): some validations are moved as VAPs, as they involve information that is only available at runtime (e.g.,
// the namespace of the current object).

// ClusterPlacementPolicy is the KubeFleet API that enables users to place namespaced and cluster-scoped resources across
// member clusters.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={kubefleet, kubefleet-placement}
// +kubebuilder:storageversion
type ClusterPlacementPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the cluster placement policy.
	// +kubebuilder:validation:Required
	Spec PlacementPolicySpec `json:"spec,omitempty"`

	// The observed status of the cluster placement policy.
	// +kubebuilder:validation:Optional
	Status PlacementPolicyStatus `json:"status,omitempty"`
}

type PlacementPolicySpec struct {
	// A list of cluster selectors that specifies the target clusters where KubeFleet should place
	// the resources. A cluster selector consists of a list of label and cluster property selectors
	// and a count; and for each cluster selector, KubeFleet will pick `count` number of clusters
	// that match the given selectors for placing the resources.
	//
	// If not specified, KubeFleet will place the resources to all available member clusters.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	ClusterSelectors []ClusterSelector `json:"clusterSelectors,omitempty"`

	// A list of resource selectors that specifies the resources that KubeFleet should place across
	// the target clusters.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	// +kubebuilder:validation:XValidation:rule="self.all(x, !(has(x.name) && size(x.name) > 0 && has(x.labelSelector)))",message="name and labelSelector are mutually exclusive in a resource selector"
	ResourceSelectors []ResourceSelector `json:"resourceSelectors,omitempty"`

	// The resource revision history limit for this placement policy.
	//
	// KubeFleet will snapshot the resources selected by a placement policy; when the resources are
	// updated and a rollout is triggered on the placement policy, KubeFleet will create a new
	// revision of the selected resources (in the form of a resource snapshot), which tracks the state of
	// the selected resources at that point of time. These revisions are kept for auditing and
	// failure recovery purposes; one can inspect them to see the past state of the selected resources,
	// or roll back to a previous revision if the latest revision is not working as expected.
	//
	// It is also possible to manually request a new resource revision to be created.
	//
	// The default value is 3.
	//
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=20
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=3
	ResourceRevisionHistoryLimit *int32 `json:"resourceRevisionHistoryLimit,omitempty"`

	// The strategy that KubeFleet uses to synchronize the selected resources to the target clusters.
	// Set the strategy to configure how selected resources are applied to a target cluster, how to handle
	// drifts/conflicts when applying the resources, what to do with placed resources when the placement policy
	// is deleted, and many more.
	//
	// +kubebuilder:validation:Optional
	SyncStrategy *SyncStrategy `json:"syncStrategy,omitempty"`

	// The tolerations which allows KubeFleet to synchronize selected resources to tainted target
	// clusters.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=10
	// +kubebuilder:validation:XValidation:rule="self.all(t, t.key != '' || (t.operator == 'Exists' && t.value == ''))",message="operator must be Exists and value must be empty when key is empty"
	// +kubebuilder:validation:XValidation:rule="self.all(t, t.operator != 'Exists' || t.value == '')",message="value must be empty when operator is Exists"
	Tolerations []Toleration `json:"tolerations,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(self.minCount) || !has(self.count) || (type(self.count) == string && self.count == 'All') || (type(self.count) == int && self.minCount <= self.count) || (type(self.count) == string && self.count.matches('^[0-9]+$') && self.minCount <= int(self.count))",message="minCount must be less than or equal to count when count is not All"
type ClusterSelector struct {
	// A list of terms that form the selector. The terms are ORed, i.e., a cluster would match the selector
	// if it matches any of the terms.
	//
	// If not specified, the selector will match all clusters.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=5
	Terms []ClusterLabelAndPropertySelectorTerm `json:"terms,omitempty"`

	// The desired number of clusters that KubeFleet should select based on the given terms.
	//
	// The default value is 1. To select all clusters that match the given terms, use the value "All".
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern="^([1-9][0-9]{0,2}|All)$"
	Count *intstr.IntOrString `json:"count,omitempty"`

	// The minimum number of clusters that KubeFleet should select based on the given terms, when KubeFleet is not able
	// to find the desired number of clusters.
	//
	// The default value is set to the same value of `count`, if `count` is an integer. If `count` is set to "All", the
	// default value of `minCount` is 1.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=999
	MinCount *int32 `json:"minCount,omitempty"`

	// The action to take when KubeFleet is not able to find the desired (minimum) number of clusters based on the given terms.
	//
	// Available options are:
	// * RequestCluster: KubeFleet will submit a cluster request to signal that a new cluster is needed to complete the placement.
	//   It is up to the platform/cloud provider to fulfill the request.
	// * KeepSearching: KubeFleet will keep searching for clusters that match the given terms silently; no cluster request will be
	//   submitted.
	//
	// This field takes effect only when cluster requests are enabled in KubeFleet.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=RequestCluster
	// +kubebuilder:validation:Enum=RequestCluster;KeepSearching
	WhenUnfulfilled WhenUnfulfilledOption `json:"whenUnfulfilled,omitempty"`
}

type ClusterLabelAndPropertySelectorTerm struct {
	// One can mix and match `MatchLabels`, `MatchLabelExpressions`, and `MatchClusterPropertyExpressions`
	// in a selector term as needed. The requirements/constraints will be ANDed.
	//
	// If none of the fields are specified, the selector term will match all clusters.

	// A list of label key-value pairs that a cluster must have to match this selector term.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxProperties=10
	MatchLabels map[string]string `json:"matchLabels,omitempty"`

	// A list of label expressions that a cluster must all satisfy to match this selector term.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=10
	MatchLabelExpressions []LabelClusterPropertyExpression `json:"matchLabelExpressions,omitempty"`

	// A list of cluster property expressions that a cluster must all satisfy to match this selector term.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=10
	MatchClusterPropertyExpressions []LabelClusterPropertyExpression `json:"matchClusterPropertyExpressions,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="(self.operator == 'In' || self.operator == 'NotIn') ? (has(self.values) && size(self.values) > 0) : true",message="values must be non-empty when operator is In or NotIn"
// +kubebuilder:validation:XValidation:rule="(self.operator == 'Exists' || self.operator == 'DoesNotExist') ? (!has(self.values) || size(self.values) == 0) : true",message="values must be empty when operator is Exists or DoesNotExist"
// +kubebuilder:validation:XValidation:rule="(self.operator == 'Gt' || self.operator == 'Lt' || self.operator == 'Ge' || self.operator == 'Le' || self.operator == 'Eq' || self.operator == 'Ne') ? (has(self.values) && size(self.values) == 1) : true",message="values must contain exactly one element when operator is Gt, Lt, Ge, Le, Eq, or Ne"
type LabelClusterPropertyExpression struct {
	// The key of the label or cluster property that selector applies to.
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// The operator that specifies the relationship between the current value under the key and the given values.
	//
	// If the operation is In, NotIn, Exists, or DoesNotExist, the key must be one referring to a label, or to a string-based
	// cluster property.
	// If the operation is Gt, Lt, Ge, Le, Eq, or Ne, the key must be one referring to a numeric-based cluster property.
	// Applying an unsupported operator to a key will cause an error at the scheduling phase.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=In;NotIn;Exists;DoesNotExist;Gt;Lt;Ge;Le;Eq;Ne
	Operator LabelClusterPropertyExpressionOperator `json:"operator"`

	// The values that are used in conjunction with the operator to determine if a selector matches.
	//
	// If the operator is In or NotIn, the values array must be non-empty.
	// If the operator is Exists or DoesNotExist, the values array must be empty.
	// If the operator is Gt, Lt, Ge, Le, Eq, or Ne, the values array must contain exactly one element.
	// +kubebuilder:validation:Optional
	Values []string `json:"values,omitempty"`
}

type LabelClusterPropertyExpressionOperator string

const (
	// The operators applicable to labels and string-based cluster properties.
	LabelClusterPropertyExpressionOperatorIn           LabelClusterPropertyExpressionOperator = "In"
	LabelClusterPropertyExpressionOperatorNotIn        LabelClusterPropertyExpressionOperator = "NotIn"
	LabelClusterPropertyExpressionOperatorExists       LabelClusterPropertyExpressionOperator = "Exists"
	LabelClusterPropertyExpressionOperatorDoesNotExist LabelClusterPropertyExpressionOperator = "DoesNotExist"

	// The operators applicable to numeric-based cluster properties.
	LabelClusterPropertyExpressionOperatorGt LabelClusterPropertyExpressionOperator = "Gt"
	LabelClusterPropertyExpressionOperatorLt LabelClusterPropertyExpressionOperator = "Lt"
	LabelClusterPropertyExpressionOperatorGe LabelClusterPropertyExpressionOperator = "Ge"
	LabelClusterPropertyExpressionOperatorLe LabelClusterPropertyExpressionOperator = "Le"
	LabelClusterPropertyExpressionOperatorEq LabelClusterPropertyExpressionOperator = "Eq"
	LabelClusterPropertyExpressionOperatorNe LabelClusterPropertyExpressionOperator = "Ne"
)

type WhenUnfulfilledOption string

const (
	WhenUnfulfilledOptionRequestCluster WhenUnfulfilledOption = "RequestCluster"
	WhenUnfulfilledOptionKeepSearching  WhenUnfulfilledOption = "KeepSearching"
)

type ResourceSelector struct {
	// The API group, version, and kind of the resource(s) to select.
	//
	// For resources in the core API group, set the APIGroup field to an empty string (""), in consistency
	// with common Kubernetes practices.

	// +kubebuilder:validation:Optional
	APIGroup string `json:"apiGroup,omitempty"`

	// +kubebuilder:validation:Required
	APIVersion string `json:"apiVersion,omitempty"`

	// +kubebuilder:validation:Required
	Kind string `json:"kind,omitempty"`

	// The name of the resource to select.
	//
	// Alternatively, one can use the LabelSelector field to select multiple resources by their labels.
	//
	// This field is mutually exclusive with the LabelSelector field.
	//
	// +kubebuilder:validation:Optional
	Name string `json:"name"`

	// The label selector that selects multiple resources for placement.
	//
	// Alternatively, one can use the Name field to select a single resource by its name.
	//
	// This field is mutually exclusive with the Name field.
	//
	// +kubebuilder:validation:Optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// The namespace of the resource to select.
	//
	// This field applies only when selecting namespaced resources using the ClusterPlacementPolicy API.
	//
	// For usage with the PlacementPolicy API, this field must be set empty or use the same value of the PlacementPolicy's
	// namespace itself.
	//
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
}

type SyncStrategy struct {
	// The method KubeFleet uses to apply resources to target clusters.
	//
	// Available options are:
	// * ClientSideApply: KubeFleet applies resources to a target cluster using three-way merge patch, similar
	//   to how the Kubernetes CLI performs a client-side apply.
	// * ServerSideApply: KubeFleet applies resources to a target cluster using server-side apply, which allows
	//   the API server to manage conflicts and merge changes.
	//
	// The default value is ClientSideApply.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=ClientSideApply;ServerSideApply
	// +kubebuilder:default=ClientSideApply
	ApplyMethod ApplyMethod `json:"applyMethod,omitempty"`

	// The options for running server-side apply ops. This field takes effect only if the apply method is
	// set to ServerSideApply.
	//
	// +kubebuilder:validation:Optional
	ServerSideApplyOptions *ServerSideApplyOptions `json:"serverSideApplyOptions,omitempty"`

	// How to handle resource co-ownership. This is most relevant when KubeFleet must manage resources that
	// are already (or expected to be) owned by other non-KubeFleet controllers in target clusters.
	//
	// Available options are:
	// * ShareOwnership: KubeFleet registers itself as a co-owner of the resource.
	// * ReportError: KubeFleet reports an error when a resource to be placed is already owned by other controllers.
	//
	// The default value is ReportError.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=ShareOwnership;ReportError
	// +kubebuilder:default=ReportError
	WhenOwnedByOthers WhenOwnedByOthersOption `json:"whenOwnedByOthers,omitempty"`

	// The action to take when a resource on the target cluster side has drifted from its desired state as controlled
	// by the placement. A drift can occur when a user or a controller on the target cluster makes an inadvertent change
	// to a KubeFleet-managed resource.
	//
	// Available options are:
	// * ApplyAnyway: KubeFleet applies the desired state, which might overwrite the drift.
	// * ReportError: KubeFleet reports an error and leaves the drift as is.
	//
	// The default value is ApplyAnyway.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=ApplyAnyway;ReportError
	// +kubebuilder:default=ApplyAnyway
	WhenDrifted WhenDriftedOption `json:"whenDrifted,omitempty"`

	// The action to take when a resource to be placed already exists on the target cluster side and is not managed
	// by KubeFleet.
	//
	// Available options are:
	// * AlwaysTakeOver: KubeFleet takes over the resource by registering itself as an owner of the resource (if
	//   the resource has no owner or co-ownership is allowed). This enables KubeFleet to adopt the existing resource for
	//   centralized management.
	// * TakeOverIfNoDiff: KubeFleet takes over the resource only if the existing resource reads the same as the desired state
	//   specified on the hub cluster side.
	// * ReportError: KubeFleet reports an error and leaves the existing resource as is.
	//
	// The default value is ReportError.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=AlwaysTakeOver;TakeOverIfNoDiff;ReportError
	// +kubebuilder:default=ReportError
	WhenAlreadyExists WhenAlreadyExistsOption `json:"whenAlreadyExists,omitempty"`

	// The action to take on resources managed by a KubeFleet placement when the placement itself is deleted.
	//
	// Available options are:
	// * CleanUpResources: KubeFleet deletes all the resources managed by the placement.
	// * OrphanResources: KubeFleet relinquishes ownership of such resources and leaves them as they are on target clusters.
	//
	// The default value is CleanUpResources.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=CleanUpResources;OrphanResources
	// +kubebuilder:default=CleanUpResources
	WhenPlacementDeleted WhenPlacementDeletedOption `json:"whenPlacementDeleted,omitempty"`

	// The action to take when a resource to be placed is namespaced but its namespace does not exist on a target cluster.
	//
	// Available options are:
	// * CreateNamespace: KubeFleet creates the namespace on the target cluster. Note that the namespace itself will not be
	//   managed by KubeFleet, and thus will not be deleted even if the placement itself has been deleted.
	// * ReportError: KubeFleet reports an error and does not place the resource to the target cluster.
	//
	// The default value is CreateNamespace.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=CreateNamespace;ReportError
	// +kubebuilder:default=CreateNamespace
	WhenNamespaceDoesNotExist WhenNamespaceDoesNotExistOption `json:"whenNamespaceDoesNotExist,omitempty"`

	// How to compare the states between the target cluster side and the hub cluster side, when calculating drifts
	// or diffs.
	//
	// Available options are:
	// * PartialComparison: KubeFleet compares only the resource fields that have been explicitly specified on the hub cluster
	//   side.
	// * FullComparison: KubeFleet compares all the fields of a resource, including those that are not specified on
	//   the hub cluster side.
	//
	// The default value is PartialComparison.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=PartialComparison;FullComparison
	// +kubebuilder:default=PartialComparison
	ComparisonOption ComparisonOption `json:"comparisonOption,omitempty"`
}

type ApplyMethod string

const (
	ApplyMethodServerSideApply ApplyMethod = "ServerSideApply"
	ApplyMethodClientSideApply ApplyMethod = "ClientSideApply"
)

type ServerSideApplyOptions struct {
	ForceConflicts bool `json:"forceConflicts,omitempty"`
}

type WhenOwnedByOthersOption string

const (
	WhenOwnedByOthersOptionShareOwnership WhenOwnedByOthersOption = "ShareOwnership"
	WhenOwnedByOthersOptionReportError    WhenOwnedByOthersOption = "ReportError"
)

type WhenDriftedOption string

const (
	WhenDriftedOptionApplyAnyway WhenDriftedOption = "ApplyAnyway"
	WhenDriftedOptionReportError WhenDriftedOption = "ReportError"
)

type WhenAlreadyExistsOption string

const (
	WhenAlreadyExistsOptionAlwaysTakeOver   WhenAlreadyExistsOption = "AlwaysTakeOver"
	WhenAlreadyExistsOptionTakeOverIfNoDiff WhenAlreadyExistsOption = "TakeOverIfNoDiff"
	WhenAlreadyExistsOptionReportError      WhenAlreadyExistsOption = "ReportError"
)

type WhenPlacementDeletedOption string

const (
	WhenPlacementDeletedOptionCleanUpResources WhenPlacementDeletedOption = "CleanUpResources"
	WhenPlacementDeletedOptionOrphanResources  WhenPlacementDeletedOption = "OrphanResources"
)

type ComparisonOption string

const (
	ComparisonOptionPartialComparison ComparisonOption = "PartialComparison"
	ComparisonOptionFullComparison    ComparisonOption = "FullComparison"
)

type WhenNamespaceDoesNotExistOption string

const (
	WhenNamespaceDoesNotExistOptionCreateNamespace WhenNamespaceDoesNotExistOption = "CreateNamespace"
	WhenNamespaceDoesNotExistOptionReportError     WhenNamespaceDoesNotExistOption = "ReportError"
)

type Toleration struct {
	// The key of the taint that the toleration applies to.
	//
	// If set to empty, the toleration matches all taint keys; and in this case, the Operator field must be set to Exists.
	// This effectively sets the placement to tolerate all taints, regardless of their configuration.
	//
	// +kubebuilder:validation:Optional
	Key string `json:"key,omitempty"`

	// The relationship between the key and value of the taint that the toleration applies to.
	//
	// Available options are Exists and Equal:
	// * Exists: the toleration matches a taint as long as the it has the same key, regardless of its value.
	// * Equal: the toleration matches a taint only if it has the same key and value.
	//
	// If set to Exists, the Value field must be left empty.
	//
	// Defaults to Equal.
	//
	// +kubebuilder:default=Equal
	// +kubebuilder:validation:Enum=Equal;Exists
	// +kubebuilder:validation:Optional
	Operator corev1.TolerationOperator `json:"operator,omitempty"`

	// The value of the taint that the toleration applies to.
	//
	// If the Operator field is set to Exists, this field must be left empty.
	//
	// +kubebuilder:validation:Optional
	Value string `json:"value,omitempty"`

	// The effect of the taint that the toleration applies to.
	//
	// If set to empty, the toleration matches all taint effects.
	//
	// Currently the only accepted value is NoSchedule.
	//
	// +kubebuilder:validation:Enum=NoSchedule
	// +kubebuilder:default=NoSchedule
	// +kubebuilder:validation:Optional
	Effect corev1.TaintEffect `json:"effect,omitempty"`
}

type PlacementPolicyStatus struct {
	// A list of conditions that describe the workload placement.
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// The name of the latest revision of the resources selected by this placement policy, in the form of a resource snapshot.
	// +kubebuilder:validation:Optional
	LatestResourceRevisionName *string `json:"latestResourceRevisionName,omitempty"`

	// The number of clusters that are expected to be selected by this placement.
	DesiredClusters *int32 `json:"desiredClusters,omitempty"`
	// The number of clusters that have been selected by this placement.
	ScheduledClusters *int32 `json:"scheduledClusters,omitempty"`
	// The number of clusters that have resources synchronized with their desired state on the hub cluster side.
	SynchronizedClusters *int32 `json:"synchronizedClusters,omitempty"`
	// The number of clusters that have resources in the available state, as verified by KubeFleet's availability check.
	ResourcesAvailableClusters *int32 `json:"resourcesAvailableClusters,omitempty"`

	// The number of ongoing cluster requests that have been submitted by this placement.
	OngoingClusterRequests *int32 `json:"ongoingClusterRequests,omitempty"`

	// The binding manager that is currently managing the bindings for this placement.
	// +kubebuilder:validation:Optional
	BindingManager *BindingManager `json:"bindingManager,omitempty"`
}

type BindingManager struct {
	// A name of the controller that manages the bindings for this placement.
	//
	// +kubebuilder:validation:Required
	ControllerName string `json:"controllerName"`

	// A list of references to the objects that are currently managing the bindings for this placement,
	// under the reconciliation of the specified controller.
	//
	// +kubebuilder:validation:Optional
	ObjectRefs []ObjectReference `json:"objectRefs,omitempty"`
}

// The list objects for the PlacementPolicy and ClusterPlacementPolicy APIs.

// PlacementPolicyList contains a list of PlacementPolicy.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PlacementPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []PlacementPolicy `json:"items"`
}

// ClusterPlacementPolicyList contains a list of ClusterPlacementPolicy.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterPlacementPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClusterPlacementPolicy `json:"items"`
}

// Set up the API types with the scheme builder.
func init() {
	SchemeBuilder.Register(&PlacementPolicy{}, &PlacementPolicyList{})
	SchemeBuilder.Register(&ClusterPlacementPolicy{}, &ClusterPlacementPolicyList{})
}
