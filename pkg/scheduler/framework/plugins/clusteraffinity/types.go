/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusteraffinity

import (
	"fmt"
	"math"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	// resourcePropertyNamePrefix is the prefix (also known as the subdomain) of the label name
	// associated with all resource properties.
	resourcePropertyNamePrefix = "resources.kubernetes-fleet.io/"

	// Below are a list of supported capacity types.
	totalCapacityName       = "total"
	allocatableCapacityName = "allocatable"
	availableCapacityName   = "available"
)

// clusterRequirement is a type alias for ClusterSelectorTerm in the API, which allows
// easy method extension.
type clusterRequirement placementv1beta1.ClusterSelectorTerm

// retrieveResourceUsageFrom retrieves a resource property value from a member cluster.
//
// Note that it will return nil if the property is not available for the cluster;
// the zero value of resource.Quantity, i.e., resource.Quantity{}, is a valid
// quantity.
func retrieveResourceUsageFrom(cluster *clusterv1beta1.MemberCluster, name string) (*resource.Quantity, error) {
	// Split the name into two segments, the capacity type, and the resource name.
	//
	// As a pre-defined rule, all the resource properties are assigned a label name of the format
	// `[PREFIX]/[CAPACITY_TYPE]-[RESOURCE_NAME]`; for example, the allocatable CPU capacity of a
	// a cluster has the label name, `resources.kubernetes-fleet.io/allocatable-cpu`.
	segs := strings.Split(name, "-")
	if len(segs) != 2 || len(segs[0]) == 0 || len(segs[1]) == 0 {
		return nil, fmt.Errorf("invalid resource property name: %s", name)
	}
	cn, tn := segs[0], segs[1]

	// Query the resource usage data.
	var q resource.Quantity
	var found bool
	switch cn {
	case totalCapacityName:
		// The property concerns the total capacity of a resource.
		q, found = cluster.Status.ResourceUsage.Capacity[corev1.ResourceName(tn)]
	case allocatableCapacityName:
		// The property concerns the allocatable capacity of a resource.
		q, found = cluster.Status.ResourceUsage.Allocatable[corev1.ResourceName(tn)]
	case availableCapacityName:
		// The property concerns the available capacity of a resource.
		q, found = cluster.Status.ResourceUsage.Available[corev1.ResourceName(tn)]
	default:
		// The property concerns a capacity type that cannot be recognized.
		return nil, fmt.Errorf("invalid capacity type %s in resource property name %s", cn, name)
	}

	if !found {
		// The property concerns a resource that is not present in the resource usage data.
		//
		// It cound be that the resource is not available in the cluster; consequently Fleet
		// does not consider this as an error.
		return nil, nil
	}
	return &q, nil
}

// retrievePropertyValueFrom retrieves a property value, resource or non-resource,
// from a member cluster.
//
// Note that it will return nil if the property is not available for the cluster;
// the zero value of resource.Quantity, i.e., resource.Quantity{}, is a valid
// quantity.
func retrievePropertyValueFrom(cluster *clusterv1beta1.MemberCluster, name string) (*resource.Quantity, error) {
	// Check if the expression concerns a resource property.
	var q *resource.Quantity
	var err error
	if strings.HasPrefix(name, resourcePropertyNamePrefix) {
		name, _ := strings.CutPrefix(name, resourcePropertyNamePrefix)

		// Retrieve the property value from the cluster resource usage data.
		q, err = retrieveResourceUsageFrom(cluster, name)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve resource property value for %s from cluster %s: %w", name, cluster.Name, err)
		}
	} else {
		v, found := cluster.Status.Properties[clusterv1beta1.PropertyName(name)]
		if !found {
			// The property is not available for the cluster.
			//
			// Note that this is not considered an error.
			return nil, nil
		}
		qv, err := resource.ParseQuantity(v.Value)
		if err != nil {
			return nil, fmt.Errorf("value %s of property %s from cluster %s is not a valid quantity: %w", v.Value, name, cluster.Name, err)
		}
		q = &qv
	}
	return q, nil
}

// Matches checks if the cluster matches a cluster requirement.
//
// This is an extended method for the ClusterSelectorTerm API.
func (c *clusterRequirement) Matches(cluster *clusterv1beta1.MemberCluster) (bool, error) {
	// Match the cluster against the label selector.
	if c.LabelSelector != nil {
		ls, err := metav1.LabelSelectorAsSelector(c.LabelSelector)
		if err != nil {
			return false, fmt.Errorf("failed to parse label selector: %w", err)
		}
		if !ls.Matches(labels.Set(cluster.Labels)) {
			// The cluster does not match with the label selector; it is ineligible for resource
			// placement.
			return false, nil
		}
	}

	// Match the cluster against the property selector.
	if c.PropertySelector == nil || len(c.PropertySelector.MatchExpressions) == 0 {
		// The term does not feature a property selector; no check is needed.
		return true, nil
	}

	for _, exp := range c.PropertySelector.MatchExpressions {
		// Compare the observed value with the expected one using the specified operator.
		q, err := retrievePropertyValueFrom(cluster, exp.Name)
		if err != nil {
			return false, err
		}
		if q == nil {
			// The property is not available for the cluster.
			return false, nil
		}

		// With the current set of operators, only one expected value can be specified.
		if len(exp.Values) != 1 {
			// The property selector expression is invalid, as there are too many expected
			// values.
			//
			// Normally this should never happen.
			return false, fmt.Errorf("more than one value in the property selector expression")
		}
		expectedQ, err := resource.ParseQuantity(exp.Values[0])
		if err != nil {
			return false, fmt.Errorf("value specified in property selector %s is not a valid resource quantity: %w", exp.Values[0], err)
		}

		switch exp.Operator {
		case placementv1beta1.PropertySelectorEqualTo:
			if !q.Equal(expectedQ) {
				// The observed value is not equal to the expected one (equality is expected)
				return false, nil
			}
		case placementv1beta1.PropertySelectorNotEqualTo:
			if q.Equal(expectedQ) {
				// The observed value is equal to the expected one (inequality is expected).
				return false, nil
			}
		case placementv1beta1.PropertySelectorGreaterThan:
			if q.Cmp(expectedQ) <= 0 {
				// The observed value is less than or equal to the expected one (expected to be
				// greater than the value).
				return false, nil
			}
		case placementv1beta1.PropertySelectorGreaterThanOrEqualTo:
			if q.Cmp(expectedQ) < 0 {
				// The observed value is less than the expected one (expected to be greater
				// than or equal to the value).
				return false, nil
			}
		case placementv1beta1.PropertySelectorLessThan:
			if q.Cmp(expectedQ) >= 0 {
				// The observed value is greater than or equal to the expected one (expected to be
				// less than the value).
				return false, nil
			}
		case placementv1beta1.PropertySelectorLessThanOrEqualTo:
			if q.Cmp(expectedQ) > 0 {
				// The observed value is greater than the expected one (expected to be less than
				// or equal to the value).
				return false, nil
			}
		default:
			// The operator is not recognized; normally this should never happen.
			return false, fmt.Errorf("invalid operator: %s", exp.Operator)
		}
	}
	// The cluster matches the property selector.
	return true, nil
}

// clusterPreference is a type alias for PreferredClusterSelector in the API, which allows
// easy method extension.
type clusterPreference placementv1beta1.PreferredClusterSelector

// interpolateWeightFor interpolates weight based on the observed value of a property.
func interpolateWeightFor(cluster *clusterv1beta1.MemberCluster, property string, sortOrder placementv1beta1.PropertySortOrder, weight int32, state *pluginState) (int32, error) {
	q, err := retrievePropertyValueFrom(cluster, property)
	if err != nil {
		return 0, fmt.Errorf("failed to perform weight interpolation based on %s for cluster %s: %w", property, cluster.Name, err)
	}
	if q == nil {
		// The property is not available for the cluster.
		return 0, nil
	}

	// Read the pre-prepared min/max values from the state, calculated in the PreScore stage.
	mm, ok := state.minMaxValuesByProperty[property]
	if !ok {
		return 0, fmt.Errorf("failed to look up extremums for property %s, no state is prepared", property)
	}
	if mm.min == nil || mm.max == nil {
		// The extremums are not available; this can happen when none of the clusters support
		// the property.
		//
		// Normally this will never occur as the check before has guaranteed that at least
		// observation has been made.
		return 0, fmt.Errorf("extremums for property %s are not available, yet a reading can be found from cluster %s", property, cluster.Name)
	}
	minQ, maxQ := mm.min, mm.max

	// Cast the quantities as floats to allow ratio estimation.
	//
	// This conversion will incur precision loss, though in most cases such loss has very limited
	// impact.
	f := q.AsApproximateFloat64()
	minF := minQ.AsApproximateFloat64()
	maxF := maxQ.AsApproximateFloat64()

	// Do a sanity check to ensure correctness.
	//
	// Normally this check would never fail.
	isInvalid := (math.IsInf(minF, 0) ||
		math.IsInf(maxF, 0) ||
		minF > maxF ||
		f < minF ||
		f > maxF)
	if isInvalid {
		return 0, fmt.Errorf("cannot interpolate weight, observed value %v, observed min %v, observed max %v", f, minF, maxF)
	}

	switch sortOrder {
	case placementv1beta1.Descending:
		w := ((f - minF) / (maxF - minF)) * float64(weight)
		// Round the value.
		return int32(math.Round(w)), nil
	case placementv1beta1.Ascending:
		w := (1 - (f-minF)/(maxF-minF)) * float64(weight)
		// Round the value.
		return int32(math.Round(w)), nil
	default:
		// An invalid sort order is present. Normally this should never occur.
		return 0, fmt.Errorf("cannot interpolate weight as sort order %s is invalid", sortOrder)
	}
}

// Scores calculates the score of a cluster based on the cluster preference.
//
// This is an extended method for the PreferredClusterSelector API.
func (c *clusterPreference) Scores(state *pluginState, cluster *clusterv1beta1.MemberCluster) (int32, error) {
	matched := true
	if c.Preference.LabelSelector != nil {
		ls, err := metav1.LabelSelectorAsSelector(c.Preference.LabelSelector)
		if err != nil {
			return 0, fmt.Errorf("failed to parse label selector: %w", err)
		}
		matched = ls.Matches(labels.Set(cluster.Labels))
	}

	switch {
	case c.Preference.PropertySorter == nil && matched:
		// No sorting is needed; if the cluster can be selected by the label selector,
		// assign the full weight.
		return c.Weight, nil
	case !matched:
		// Regardless of whether sorting is needed; if the cluster cannot be selected
		// by the label selector, it will receive no weight.
		return 0, nil
	default:
		// Interpolate the weight based on the sorting result.
		w, err := interpolateWeightFor(cluster, c.Preference.PropertySorter.Name, c.Preference.PropertySorter.SortOrder, c.Weight, state)
		if err != nil {
			return 0, err
		}
		return w, nil
	}
}
