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

package placementpolicy

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
)

// countAllClusters is the sentinel string value of the ClusterSelector count field that selects
// every cluster matching the given terms.
const countAllClusters = "All"

// maxClusterCount is the largest number of clusters a selector's count may request. It mirrors the
// upper bound of the count field's CRD pattern (^([1-9][0-9]{0,2}|All)$, i.e. 1..999). The pattern
// constrains only the string form of the int-or-string field, so the integer form is bounded here
// instead -- both to hold the intended limit and to keep the sum of counts across selectors from
// overflowing the int32 the desired-cluster total is accumulated in.
const maxClusterCount = 999

// resolvedCounts is the outcome of interpreting a ClusterSelector's count and minCount fields.
type resolvedCounts struct {
	// selectAll is true when the selector requests all matching clusters (count = "All").
	selectAll bool
	// desired is the desired number of clusters; meaningless when selectAll is set.
	desired int32
	// minimum is the fulfillment floor: the selector counts as fulfilled once this many
	// clusters match. Per the API defaults, it equals desired when count is an integer and
	// 1 when count is "All", unless minCount is set explicitly.
	minimum int32
}

// resolveCounts interprets the count and minCount fields of a cluster selector, applying the
// defaulting rules documented on the PlacementPolicy API.
func resolveCounts(selector *kfplacementv1alpha1.ClusterSelector) (resolvedCounts, error) {
	rc := resolvedCounts{}

	count := selector.Count
	if count == nil {
		// The CRD defaults the field to 1; the object might not have passed through the API
		// server defaulting chain (e.g., in unit tests), so the rule is applied here as well.
		count = &intstr.IntOrString{Type: intstr.Int, IntVal: 1}
	}
	switch count.Type {
	case intstr.String:
		if count.StrVal == countAllClusters {
			rc.selectAll = true
			rc.minimum = 1
			break
		}
		// The CRD pattern admits digit strings alongside "All", and the minCount CEL rule converts
		// them with int(self.count), so a manifest with count: "3" passes admission; parse it here
		// rather than rejecting a value the API accepts. Anything else, or a digit string outside the
		// bound (from an object that bypassed admission), is an error.
		n, err := strconv.Atoi(count.StrVal)
		if err != nil || n < 1 || n > maxClusterCount {
			return rc, fmt.Errorf("invalid count value %q: only %q and integers in [1, %d] are supported", count.StrVal, countAllClusters, maxClusterCount)
		}
		desired := int32(n) //nolint:gosec // n is bounded to [1, maxClusterCount] above, so the conversion cannot overflow.
		rc.desired = desired
		rc.minimum = desired
	case intstr.Int:
		// The CRD pattern bounds only the string form, so the integer form is bounded here; the upper
		// bound also keeps two maximum-valued selectors from overflowing the aggregated desired total.
		if count.IntVal < 1 || count.IntVal > maxClusterCount {
			return rc, fmt.Errorf("invalid count value %d: must be an integer in [1, %d]", count.IntVal, maxClusterCount)
		}
		rc.desired = count.IntVal
		rc.minimum = count.IntVal
	default:
		return rc, fmt.Errorf("invalid count type %d", count.Type)
	}

	if selector.MinCount != nil {
		rc.minimum = *selector.MinCount
	}
	return rc, nil
}

// validateTerms structurally validates selector terms without evaluating them against any
// cluster, so that invalid selectors are detected even when the fleet has no schedulable
// clusters at all: label expression operators must be label-applicable, and numeric property
// expressions must carry exactly one parseable quantity.
func validateTerms(terms []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm) error {
	for i := range terms {
		term := &terms[i]
		// matchLabels is a free-form map on the CRD: admission stores whatever keys and values
		// are given, and labels.SelectorFromSet performs no validation of its own. An invalid
		// key or value therefore yields a selector that matches no cluster, which the policy
		// would misread as an ordinarily unfulfilled selector and answer by provisioning a
		// cluster no manifest can ever satisfy. Reject it structurally instead.
		//
		// The keys are validated in sorted order so that a term carrying more than one invalid
		// entry always surfaces the same error. The message ends up on the policy's Scheduled
		// condition, and a message that changed from one reconcile to the next -- as it would if
		// the map were ranged in Go's randomized order -- would defeat the status no-op check and
		// spin the reconcile.
		for _, key := range slices.Sorted(maps.Keys(term.MatchLabels)) {
			if errs := validation.IsQualifiedName(key); len(errs) != 0 {
				return fmt.Errorf("invalid matchLabels key %q: %s", key, strings.Join(errs, "; "))
			}
			if errs := validation.IsValidLabelValue(term.MatchLabels[key]); len(errs) != 0 {
				return fmt.Errorf("invalid matchLabels value %q for key %q: %s", term.MatchLabels[key], key, strings.Join(errs, "; "))
			}
		}
		for j := range term.MatchLabelExpressions {
			expr := &term.MatchLabelExpressions[j]
			op, err := labelSelectionOperatorFor(expr.Operator)
			if err != nil {
				return fmt.Errorf("invalid label expression on key %s: %w", expr.Key, err)
			}
			if _, err := labels.NewRequirement(expr.Key, op, expr.Values); err != nil {
				return fmt.Errorf("invalid label expression on key %s: %w", expr.Key, err)
			}
		}
		for j := range term.MatchClusterPropertyExpressions {
			expr := &term.MatchClusterPropertyExpressions[j]
			if err := validatePropertyKey(expr.Key); err != nil {
				return err
			}
			switch expr.Operator {
			case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorIn,
				kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNotIn:
				// In and NotIn test set membership by comparing the property's value as a string.
				// A resource property is a quantity whose canonical string form is not unique --
				// 8000m and 8 are equal -- so a membership test against it silently mismatches
				// (In: ["8000m"] never matches a cluster reporting "8"), which would misreport the
				// selector as unfulfilled and add a spurious claim. The API reserves these
				// operators for labels and string-based properties, so a resource-prefixed key
				// here is rejected rather than evaluated.
				if strings.HasPrefix(expr.Key, propertyprovider.ResourcePropertyNamePrefix) {
					return fmt.Errorf("operator %s is not applicable to the resource property %s: resource properties are numeric, use Gt, Lt, Ge, Le, Eq, or Ne", expr.Operator, expr.Key)
				}
			case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists,
				kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorDoesNotExist:
				// Presence operators carry no numeric constraints and apply uniformly to string
				// and resource properties; the CRD validation rules cover their value arity.
				// Unlike In/NotIn above, these are intentionally left valid on a resource key: a
				// presence check is a map-key lookup, immune to the quantity-canonicalization that
				// breaks membership tests, and the evaluation path (stringPropertyValueFrom) already
				// supports it. The API type doc frames the string operators more narrowly; leaving
				// that wording in step with this enforced rule is a deferred, separate change -- do
				// not "reconcile" the two by banning presence checks on resource keys here.
			case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt,
				kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorLt,
				kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGe,
				kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorLe,
				kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorEq,
				kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNe:
				if len(expr.Values) != 1 {
					return fmt.Errorf("cluster property expression on key %s must have exactly one value for operator %s", expr.Key, expr.Operator)
				}
				if _, err := resource.ParseQuantity(expr.Values[0]); err != nil {
					return fmt.Errorf("value %s in cluster property expression on key %s is not a valid quantity: %w", expr.Values[0], expr.Key, err)
				}
			default:
				return fmt.Errorf("invalid operator %s in cluster property expression on key %s", expr.Operator, expr.Key)
			}
		}
	}
	return nil
}

// validatePropertyKey checks that a resource-property key names a known capacity type and a
// non-empty resource; non-resource property keys carry no structural constraints.
func validatePropertyKey(key string) error {
	if !strings.HasPrefix(key, propertyprovider.ResourcePropertyNamePrefix) {
		// A non-resource property key names a property a cluster reports. Admission accepts a
		// present-but-malformed key (an empty string, say) as an ordinary map entry, and a
		// DoesNotExist expression on such a key would then report the property absent on every
		// cluster -- matching all of them and potentially withdrawing a still-needed claim.
		// Reject it here rather than treat it as an always-absent property.
		return validateNonResourcePropertyName(key)
	}
	name := strings.TrimPrefix(key, propertyprovider.ResourcePropertyNamePrefix)
	capacityType, resourceName, ok := strings.Cut(name, "-")
	if !ok || capacityType == "" || resourceName == "" {
		return fmt.Errorf("invalid resource property name %s in cluster property expression", key)
	}
	switch capacityType {
	case propertyprovider.TotalCapacityName, propertyprovider.AllocatableCapacityName, propertyprovider.AvailableCapacityName:
	default:
		return fmt.Errorf("invalid capacity type %s in cluster property expression key %s", capacityType, key)
	}
	// A non-empty resource-name portion is not enough: a key such as
	// resources.kubernetes-fleet.io/allocatable-not a resource clears the checks above yet names a
	// resource no cluster can ever report, so Exists would raise an unfulfillable claim and
	// DoesNotExist would match every cluster. The resource name is what indexes into the cluster's
	// reported ResourceList, so it -- not the whole key -- is validated as a qualified name: this
	// rejects the malformed case while still admitting a domain-qualified extended resource such as
	// nvidia.com/gpu, which a whole-key check would reject for carrying a second slash. This is a
	// deliberate divergence from validateName in pkg/utils/validator, which validates the whole key
	// and so rejects such extended resources; do not "align" this with that validator, as that would
	// reintroduce the rejection of a resource the cluster genuinely reports.
	if errs := validation.IsQualifiedName(resourceName); len(errs) != 0 {
		return fmt.Errorf("invalid resource name %s in cluster property expression key %s: %s", resourceName, key, strings.Join(errs, "; "))
	}
	return nil
}

// validateNonResourcePropertyName checks a non-resource cluster property name against the grammar
// the property provider uses: one or more slash-separated segments, where an optional first segment
// is a DNS subdomain or qualified name and every remaining segment is a qualified name. This mirrors
// validateName in pkg/utils/validator so that a multi-segment provider name such as the Azure
// per-SKU key "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/count" is accepted -- IsQualifiedName
// alone rejects any name carrying more than one slash and would misflag those legitimate keys.
func validateNonResourcePropertyName(name string) error {
	segs := strings.Split(name, "/")
	if len(segs) <= 1 {
		if errs := validation.IsQualifiedName(name); len(errs) != 0 {
			return fmt.Errorf("invalid cluster property key %q: %s", name, strings.Join(errs, "; "))
		}
		return nil
	}
	prefix := segs[0]
	if len(validation.IsDNS1123Subdomain(prefix)) != 0 && len(validation.IsQualifiedName(prefix)) != 0 {
		return fmt.Errorf("invalid cluster property key %q: first segment %q is neither a valid DNS subdomain nor a valid qualified name", name, prefix)
	}
	for _, seg := range segs[1:] {
		if errs := validation.IsQualifiedName(seg); len(errs) != 0 {
			return fmt.Errorf("invalid cluster property key %q: segment %q is not valid: %s", name, seg, strings.Join(errs, "; "))
		}
	}
	return nil
}

// matchesTerms reports whether the member cluster satisfies any of the given selector terms
// (terms are ORed). An empty term list matches every cluster, in consistency with the API
// contract for both cluster selectors and cluster claims.
func matchesTerms(cluster *clusterv1beta1.MemberCluster, terms []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm) (bool, error) {
	if len(terms) == 0 {
		return true, nil
	}
	// A term that cannot be evaluated does not veto the disjunction: a later term matching on
	// its own proves the whole OR true regardless of what the broken term would have said. Only
	// when no term matches does the error surface, since "no match" cannot be distinguished
	// from "could not tell" at that point.
	var firstErr error
	for i := range terms {
		matched, err := matchesTerm(cluster, &terms[i])
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("selector term %d: %w", i, err)
			}
			continue
		}
		if matched {
			return true, nil
		}
	}
	return false, firstErr
}

// matchesTerm reports whether the member cluster satisfies a single selector term; the
// requirement groups within a term (matchLabels, matchLabelExpressions, and
// matchClusterPropertyExpressions) are ANDed, as are the requirements within each group.
func matchesTerm(cluster *clusterv1beta1.MemberCluster, term *kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm) (bool, error) {
	if !labels.SelectorFromSet(term.MatchLabels).Matches(labels.Set(cluster.Labels)) {
		return false, nil
	}

	matched, err := labelExpressionsMatch(cluster, term.MatchLabelExpressions)
	if err != nil || !matched {
		return matched, err
	}

	return propertyExpressionsMatch(cluster, term.MatchClusterPropertyExpressions)
}

// labelExpressionsMatch evaluates matchLabelExpressions against the cluster's labels, using the
// upstream label selector semantics (e.g., NotIn and DoesNotExist match when the key is absent).
func labelExpressionsMatch(cluster *clusterv1beta1.MemberCluster, exprs []kfplacementv1alpha1.LabelClusterPropertyExpression) (bool, error) {
	for i := range exprs {
		expr := &exprs[i]
		op, err := labelSelectionOperatorFor(expr.Operator)
		if err != nil {
			return false, fmt.Errorf("invalid label expression on key %s: %w", expr.Key, err)
		}
		req, err := labels.NewRequirement(expr.Key, op, expr.Values)
		if err != nil {
			return false, fmt.Errorf("invalid label expression on key %s: %w", expr.Key, err)
		}
		if !req.Matches(labels.Set(cluster.Labels)) {
			return false, nil
		}
	}
	return true, nil
}

// labelSelectionOperatorFor maps a label-applicable expression operator to its upstream label
// selection counterpart; numeric operators are rejected, as the API reserves them for cluster
// properties.
func labelSelectionOperatorFor(op kfplacementv1alpha1.LabelClusterPropertyExpressionOperator) (selection.Operator, error) {
	switch op {
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorIn:
		return selection.In, nil
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNotIn:
		return selection.NotIn, nil
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists:
		return selection.Exists, nil
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorDoesNotExist:
		return selection.DoesNotExist, nil
	default:
		return "", fmt.Errorf("operator %s is not applicable to labels", op)
	}
}

// propertyExpressionsMatch evaluates matchClusterPropertyExpressions against the cluster's
// reported properties. String-based operators (In, NotIn, Exists, DoesNotExist) compare the raw
// property value and mirror the label selector absence semantics; numeric operators (Gt, Lt, Ge,
// Le, Eq, Ne) compare quantities, and a cluster that does not report the property does not match.
func propertyExpressionsMatch(cluster *clusterv1beta1.MemberCluster, exprs []kfplacementv1alpha1.LabelClusterPropertyExpression) (bool, error) {
	for i := range exprs {
		matched, err := propertyExpressionMatches(cluster, &exprs[i])
		if err != nil || !matched {
			return matched, err
		}
	}
	return true, nil
}

func propertyExpressionMatches(cluster *clusterv1beta1.MemberCluster, expr *kfplacementv1alpha1.LabelClusterPropertyExpression) (bool, error) {
	switch expr.Operator {
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorIn,
		kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNotIn,
		kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists,
		kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorDoesNotExist:
		return stringPropertyMatches(cluster, expr)
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt,
		kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorLt,
		kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGe,
		kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorLe,
		kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorEq,
		kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNe:
		return numericPropertyMatches(cluster, expr)
	default:
		return false, fmt.Errorf("invalid operator %s in cluster property expression on key %s", expr.Operator, expr.Key)
	}
}

// stringPropertyMatches evaluates a string-based operator against a cluster property value,
// mirroring the absence semantics of label selectors (NotIn and DoesNotExist match when the
// property is not reported).
func stringPropertyMatches(cluster *clusterv1beta1.MemberCluster, expr *kfplacementv1alpha1.LabelClusterPropertyExpression) (bool, error) {
	value, found := stringPropertyValueFrom(cluster, expr.Key)

	switch expr.Operator {
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists:
		return found, nil
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorDoesNotExist:
		return !found, nil
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorIn:
		if !found {
			return false, nil
		}
		return slices.Contains(expr.Values, value), nil
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNotIn:
		if !found {
			return true, nil
		}
		return !slices.Contains(expr.Values, value), nil
	default:
		// Callers dispatch only string-based operators here.
		return false, fmt.Errorf("invalid string operator %s in cluster property expression on key %s", expr.Operator, expr.Key)
	}
}

// numericPropertyMatches evaluates a numeric operator against a cluster property value; the
// values are compared as Kubernetes resource quantities. A cluster that does not report the
// property does not match, in consistency with the property selector semantics of the
// scheduler's cluster affinity plugin.
func numericPropertyMatches(cluster *clusterv1beta1.MemberCluster, expr *kfplacementv1alpha1.LabelClusterPropertyExpression) (bool, error) {
	observed, err := numericPropertyValueFrom(cluster, expr.Key)
	if err != nil {
		return false, err
	}
	if observed == nil {
		return false, nil
	}

	if len(expr.Values) != 1 {
		// The CRD validation enforces exactly one value for numeric operators; this guards
		// against objects that bypassed admission.
		return false, fmt.Errorf("cluster property expression on key %s must have exactly one value for operator %s", expr.Key, expr.Operator)
	}
	expected, err := resource.ParseQuantity(expr.Values[0])
	if err != nil {
		return false, fmt.Errorf("value %s in cluster property expression on key %s is not a valid quantity: %w", expr.Values[0], expr.Key, err)
	}

	cmp := observed.Cmp(expected)
	switch expr.Operator {
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt:
		return cmp > 0, nil
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorLt:
		return cmp < 0, nil
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGe:
		return cmp >= 0, nil
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorLe:
		return cmp <= 0, nil
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorEq:
		return cmp == 0, nil
	case kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNe:
		return cmp != 0, nil
	default:
		// Callers dispatch only numeric operators here.
		return false, fmt.Errorf("invalid numeric operator %s in cluster property expression on key %s", expr.Operator, expr.Key)
	}
}

// stringPropertyValueFrom returns the raw value of a non-resource cluster property.
func stringPropertyValueFrom(cluster *clusterv1beta1.MemberCluster, name string) (string, bool) {
	if strings.HasPrefix(name, propertyprovider.ResourcePropertyNamePrefix) {
		// Resource properties are quantities; string-based operators still see them through
		// their canonical string form so that Exists/DoesNotExist work uniformly.
		q, err := resourceUsageValueFrom(cluster, strings.TrimPrefix(name, propertyprovider.ResourcePropertyNamePrefix))
		if err != nil || q == nil {
			return "", false
		}
		return q.String(), true
	}
	v, found := cluster.Status.Properties[clusterv1beta1.PropertyName(name)]
	if !found {
		return "", false
	}
	return v.Value, true
}

// numericPropertyValueFrom retrieves a property value, resource or non-resource, from a member
// cluster as a quantity. It returns nil (and no error) when the cluster does not report the
// property.
func numericPropertyValueFrom(cluster *clusterv1beta1.MemberCluster, name string) (*resource.Quantity, error) {
	if strings.HasPrefix(name, propertyprovider.ResourcePropertyNamePrefix) {
		q, err := resourceUsageValueFrom(cluster, strings.TrimPrefix(name, propertyprovider.ResourcePropertyNamePrefix))
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve resource property %s from cluster %s: %w", name, cluster.Name, err)
		}
		return q, nil
	}

	v, found := cluster.Status.Properties[clusterv1beta1.PropertyName(name)]
	if !found {
		return nil, nil
	}
	q, err := resource.ParseQuantity(v.Value)
	if err != nil {
		return nil, fmt.Errorf("value %s of property %s from cluster %s is not a valid quantity: %w", v.Value, name, cluster.Name, err)
	}
	return &q, nil
}

// resourceUsageValueFrom retrieves a resource usage value from a member cluster; the name is the
// property name with the resource property prefix removed, e.g. "allocatable-cpu". It returns
// nil (and no error) when the cluster does not report the resource.
func resourceUsageValueFrom(cluster *clusterv1beta1.MemberCluster, name string) (*resource.Quantity, error) {
	// Resource properties follow the `[PREFIX]/[CAPACITY_TYPE]-[RESOURCE_NAME]` naming rule,
	// e.g. `resources.kubernetes-fleet.io/allocatable-cpu`; the prefix has been removed by the
	// caller.
	capacityType, resourceName, found := strings.Cut(name, "-")
	if !found || capacityType == "" || resourceName == "" {
		return nil, fmt.Errorf("invalid resource property name: %s", name)
	}

	var q resource.Quantity
	var reported bool
	switch capacityType {
	case propertyprovider.TotalCapacityName:
		q, reported = cluster.Status.ResourceUsage.Capacity[corev1.ResourceName(resourceName)]
	case propertyprovider.AllocatableCapacityName:
		q, reported = cluster.Status.ResourceUsage.Allocatable[corev1.ResourceName(resourceName)]
	case propertyprovider.AvailableCapacityName:
		q, reported = cluster.Status.ResourceUsage.Available[corev1.ResourceName(resourceName)]
	default:
		return nil, fmt.Errorf("invalid capacity type %s in resource property name %s", capacityType, name)
	}
	if !reported {
		return nil, nil
	}
	return &q, nil
}
