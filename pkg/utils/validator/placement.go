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

// Package validator provides utils to validate all fleet custom resources.
package validator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	apiErrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
)

var ResourceInformer informer.Manager
var RestMapper meta.RESTMapper

var (
	invalidTolerationErrFmt      = "invalid toleration %+v: %s"
	invalidTolerationKeyErrFmt   = "invalid toleration key %+v: %s"
	invalidTolerationValueErrFmt = "invalid toleration value %+v: %s"
	uniqueTolerationErrFmt       = "toleration %+v already exists, tolerations must be unique"

	// Webhook validation message format strings
	AllowUpdateOldInvalidFmt   = "allow update on old invalid v1beta1 %s with DeletionTimestamp set"
	DenyUpdateOldInvalidFmt    = "deny update on old invalid v1beta1 %s with DeletionTimestamp not set %s"
	DenyCreateUpdateInvalidFmt = "deny create/update v1beta1 %s has invalid fields %s"
	AllowModifyFmt             = "any user is allowed to modify v1beta1 %s"

	// Below is the map of supported capacity types.
	supportedResourceCapacityTypesMap = map[string]bool{propertyprovider.AllocatableCapacityName: true, propertyprovider.AvailableCapacityName: true, propertyprovider.TotalCapacityName: true}
	resourceCapacityTypes             = supportedResourceCapacityTypes()
)

// validatePlacement validates a placement object (either ClusterResourcePlacement or ResourcePlacement).
func validatePlacement(name string, resourceSelectors []placementv1beta1.ResourceSelectorTerm, policy *placementv1beta1.PlacementPolicy, strategy placementv1beta1.RolloutStrategy, isClusterScoped bool) error {
	allErr := make([]error, 0)

	if len(name) > validation.DNS1035LabelMaxLength {
		allErr = append(allErr, fmt.Errorf("the name field cannot have length exceeding %d", validation.DNS1035LabelMaxLength))
	}

	for _, selector := range resourceSelectors {
		if selector.LabelSelector != nil {
			if len(selector.Name) != 0 {
				allErr = append(allErr, fmt.Errorf("the labelSelector and name fields are mutually exclusive in selector %+v", selector))
			}
			allErr = append(allErr, validateLabelSelector(selector.LabelSelector, "resource selector"))
		}

		gk := schema.GroupKind{
			Group: selector.Group,
			Kind:  selector.Kind,
		}
		if _, err := RestMapper.RESTMapping(gk, selector.Version); err != nil {
			allErr = append(allErr, fmt.Errorf("failed to get GVR of the selector: %w", err))
			return apiErrors.NewAggregate(allErr) // skip next check if we cannot get GVR
		}

		if ResourceInformer != nil {
			gvk := schema.GroupVersionKind{
				Group:   selector.Group,
				Version: selector.Version,
				Kind:    selector.Kind,
			}
			// Only check cluster scope for ClusterResourcePlacement
			if isClusterScoped && !ResourceInformer.IsClusterScopedResources(gvk) {
				allErr = append(allErr, fmt.Errorf("the resource is not found in schema (please retry) or it is not a cluster scoped resource: %v", gvk))
			}

			// Only check namespace scope for ResourcePlacement
			if !isClusterScoped && ResourceInformer.IsClusterScopedResources(gvk) {
				allErr = append(allErr, fmt.Errorf("the resource is not found in schema (please retry) or it is a cluster scoped resource: %v", gvk))
			}
		} else {
			err := fmt.Errorf("cannot perform resource scope check for now, please retry")
			klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "resource informer is nil")
			allErr = append(allErr, fmt.Errorf("cannot perform resource scope check for now, please retry"))
		}
	}

	if policy != nil {
		if err := validatePlacementPolicy(policy); err != nil {
			allErr = append(allErr, fmt.Errorf("the placement policy field is invalid: %w", err))
		}
	}

	if err := validateRolloutStrategy(strategy); err != nil {
		allErr = append(allErr, fmt.Errorf("the rollout Strategy field  is invalid: %w", err))
	}

	return apiErrors.NewAggregate(allErr)
}

// ValidateClusterResourcePlacement validates a ClusterResourcePlacement object.
func ValidateClusterResourcePlacement(clusterResourcePlacement *placementv1beta1.ClusterResourcePlacement) error {
	return validatePlacement(
		clusterResourcePlacement.Name,
		clusterResourcePlacement.Spec.ResourceSelectors,
		clusterResourcePlacement.Spec.Policy,
		clusterResourcePlacement.Spec.Strategy,
		true, // isClusterScoped
	)
}

// ValidateResourcePlacement validates a ResourcePlacement object.
func ValidateResourcePlacement(resourcePlacement *placementv1beta1.ResourcePlacement) error {
	return validatePlacement(
		resourcePlacement.Name,
		resourcePlacement.Spec.ResourceSelectors,
		resourcePlacement.Spec.Policy,
		resourcePlacement.Spec.Strategy,
		false, // isClusterScoped
	)
}

func IsPlacementPolicyTypeUpdated(oldPolicy, currentPolicy *placementv1beta1.PlacementPolicy) bool {
	if oldPolicy == nil && currentPolicy != nil {
		// if placement policy is left blank, by default PickAll is chosen.
		return currentPolicy.PlacementType != placementv1beta1.PickAllPlacementType
	}
	// this case is essentially user trying to change placement type, if old placement type wasn't PickAll.
	if oldPolicy != nil && currentPolicy == nil {
		return oldPolicy.PlacementType != placementv1beta1.PickAllPlacementType
	}
	if oldPolicy != nil && currentPolicy != nil {
		return currentPolicy.PlacementType != oldPolicy.PlacementType
	}
	// general case where placement type wasn't updated but other fields in placement policy might have changed.
	return false
}

func validatePlacementPolicy(policy *placementv1beta1.PlacementPolicy) error {
	allErr := make([]error, 0)
	switch policy.PlacementType {
	case placementv1beta1.PickFixedPlacementType:
		if err := validatePolicyForPickFixedPlacementType(policy); err != nil {
			allErr = append(allErr, err)
		}
	case placementv1beta1.PickAllPlacementType:
		if err := validatePolicyForPickAllPlacementType(policy); err != nil {
			allErr = append(allErr, err)
		}
	case placementv1beta1.PickNPlacementType:
		if err := validatePolicyForPickNPolicyType(policy); err != nil {
			allErr = append(allErr, err)
		}
	}

	return apiErrors.NewAggregate(allErr)
}

func validatePolicyForPickFixedPlacementType(policy *placementv1beta1.PlacementPolicy) error {
	allErr := make([]error, 0)
	if len(policy.ClusterNames) == 0 {
		allErr = append(allErr, fmt.Errorf("cluster names cannot be empty for policy type %s", placementv1beta1.PickFixedPlacementType))
	}
	uniqueClusterNames := make(map[string]bool)
	for _, name := range policy.ClusterNames {
		nameErr := validation.IsDNS1123Subdomain(name)
		if nameErr != nil {
			allErr = append(allErr, fmt.Errorf("PickFixed cluster name %s is not a valid member name: %s", name, strings.Join(nameErr, "; ")))
		}
		if len(name) > validation.DNS1035LabelMaxLength {
			allErr = append(allErr, fmt.Errorf("PickFixed cluster name %s cannot have length exceeding %d", name, validation.DNS1035LabelMaxLength))
		}
		if _, ok := uniqueClusterNames[name]; ok {
			allErr = append(allErr, fmt.Errorf("cluster names must be unique for policy type %s", placementv1beta1.PickFixedPlacementType))
			break
		}
		uniqueClusterNames[name] = true
	}
	if policy.NumberOfClusters != nil {
		allErr = append(allErr, fmt.Errorf("number of clusters must be nil for policy type %s, only valid for PickN placement policy type", placementv1beta1.PickFixedPlacementType))
	}
	if policy.Affinity != nil {
		allErr = append(allErr, fmt.Errorf("affinity must be nil for policy type %s, only valid for PickAll/PickN placement policy types", placementv1beta1.PickFixedPlacementType))
	}
	if len(policy.TopologySpreadConstraints) > 0 {
		allErr = append(allErr, fmt.Errorf("topology spread constraints needs to be empty for policy type %s, only valid for PickN policy type", placementv1beta1.PickFixedPlacementType))
	}
	if policy.Tolerations != nil {
		allErr = append(allErr, fmt.Errorf("tolerations needs to be empty for policy type %s, only valid for PickAll/PickN", placementv1beta1.PickFixedPlacementType))
	}

	return apiErrors.NewAggregate(allErr)
}

func validatePolicyForPickAllPlacementType(policy *placementv1beta1.PlacementPolicy) error {
	allErr := make([]error, 0)
	if len(policy.ClusterNames) > 0 {
		allErr = append(allErr, fmt.Errorf("cluster names needs to be empty for policy type %s, only valid for PickFixed policy type", placementv1beta1.PickAllPlacementType))
	}
	if policy.NumberOfClusters != nil {
		allErr = append(allErr, fmt.Errorf("number of clusters must be nil for policy type %s, only valid for PickN placement policy type", placementv1beta1.PickAllPlacementType))
	}
	// Allowing user to supply empty cluster affinity, only validating cluster affinity if non-nil
	if policy.Affinity != nil && policy.Affinity.ClusterAffinity != nil {
		allErr = append(allErr, validateClusterAffinity(policy.Affinity.ClusterAffinity, policy.PlacementType))
	}
	if len(policy.TopologySpreadConstraints) > 0 {
		allErr = append(allErr, fmt.Errorf("topology spread constraints needs to be empty for policy type %s, only valid for PickN policy type", placementv1beta1.PickAllPlacementType))
	}
	allErr = append(allErr, validateTolerations(policy.Tolerations))

	return apiErrors.NewAggregate(allErr)
}

func validatePolicyForPickNPolicyType(policy *placementv1beta1.PlacementPolicy) error {
	allErr := make([]error, 0)
	if len(policy.ClusterNames) > 0 {
		allErr = append(allErr, fmt.Errorf("cluster names needs to be empty for policy type %s, only valid for PickFixed policy type", placementv1beta1.PickNPlacementType))
	}
	if policy.NumberOfClusters != nil {
		if *policy.NumberOfClusters < 0 {
			allErr = append(allErr, fmt.Errorf("number of clusters cannot be %d for policy type %s", *policy.NumberOfClusters, placementv1beta1.PickNPlacementType))
		}
	} else {
		allErr = append(allErr, fmt.Errorf("number of cluster cannot be nil for policy type %s", placementv1beta1.PickNPlacementType))
	}
	// Allowing user to supply empty cluster affinity, only validating cluster affinity if non-nil
	if policy.Affinity != nil && policy.Affinity.ClusterAffinity != nil {
		allErr = append(allErr, validateClusterAffinity(policy.Affinity.ClusterAffinity, policy.PlacementType))
	}
	if len(policy.TopologySpreadConstraints) > 0 {
		allErr = append(allErr, validateTopologySpreadConstraints(policy.TopologySpreadConstraints))
	}
	allErr = append(allErr, validateTolerations(policy.Tolerations))

	return apiErrors.NewAggregate(allErr)
}

func validateClusterAffinity(clusterAffinity *placementv1beta1.ClusterAffinity, placementType placementv1beta1.PlacementType) error {
	allErr := make([]error, 0)
	// Both RequiredDuringSchedulingIgnoredDuringExecution and PreferredDuringSchedulingIgnoredDuringExecution are optional fields, so validating only if non-nil/length is greater than zero
	switch placementType {
	case placementv1beta1.PickAllPlacementType:
		if clusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			allErr = append(allErr, validateClusterSelector(clusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution))
		}
		if len(clusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution) > 0 {
			allErr = append(allErr, fmt.Errorf("PreferredDuringSchedulingIgnoredDuringExecution will be ignored for placement policy type %s", placementType))
		}
	case placementv1beta1.PickNPlacementType:
		if clusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			allErr = append(allErr, validateClusterSelector(clusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution))
		}
		if len(clusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution) > 0 {
			allErr = append(allErr, validatePreferredClusterSelectors(clusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution))
		}
	}
	return apiErrors.NewAggregate(allErr)
}

func validateTolerations(tolerations []placementv1beta1.Toleration) error {
	allErr := make([]error, 0)
	tolerationMap := make(map[placementv1beta1.Toleration]bool)
	for _, toleration := range tolerations {
		if toleration.Key != "" {
			for _, msg := range validation.IsQualifiedName(toleration.Key) {
				allErr = append(allErr, fmt.Errorf(invalidTolerationKeyErrFmt, toleration, msg))
			}
		}
		switch toleration.Operator {
		case corev1.TolerationOpExists:
			if toleration.Value != "" {
				allErr = append(allErr, fmt.Errorf(invalidTolerationErrFmt, toleration, "toleration value needs to be empty, when operator is Exists"))
			}
		case corev1.TolerationOpEqual:
			if toleration.Key == "" {
				allErr = append(allErr, fmt.Errorf(invalidTolerationErrFmt, toleration, "toleration key cannot be empty, when operator is Equal"))
			}
			for _, msg := range validation.IsValidLabelValue(toleration.Value) {
				allErr = append(allErr, fmt.Errorf(invalidTolerationValueErrFmt, toleration, msg))
			}
		}
		if tolerationMap[toleration] {
			allErr = append(allErr, fmt.Errorf(uniqueTolerationErrFmt, toleration))
		}
		tolerationMap[toleration] = true
	}
	return apiErrors.NewAggregate(allErr)
}

func IsTolerationsUpdatedOrDeleted(oldTolerations []placementv1beta1.Toleration, newTolerations []placementv1beta1.Toleration) bool {
	newTolerationsMap := make(map[placementv1beta1.Toleration]bool)
	for _, newToleration := range newTolerations {
		newTolerationsMap[newToleration] = true
	}
	for _, oldToleration := range oldTolerations {
		if !newTolerationsMap[oldToleration] {
			return true
		}
	}
	return false
}

func validateTopologySpreadConstraints(topologyConstraints []placementv1beta1.TopologySpreadConstraint) error {
	allErr := make([]error, 0)
	for _, tc := range topologyConstraints {
		if len(tc.WhenUnsatisfiable) > 0 && tc.WhenUnsatisfiable != placementv1beta1.DoNotSchedule && tc.WhenUnsatisfiable != placementv1beta1.ScheduleAnyway {
			allErr = append(allErr, fmt.Errorf("unknown unsatisfiable type %s", tc.WhenUnsatisfiable))
		}
	}
	return apiErrors.NewAggregate(allErr)
}

func validateClusterSelector(clusterSelector *placementv1beta1.ClusterSelector) error {
	allErr := make([]error, 0)
	for _, clusterSelectorTerm := range clusterSelector.ClusterSelectorTerms {
		// Since label selector is a required field in ClusterSelectorTerm, not checking to see if it's an empty object.
		allErr = append(allErr, validateLabelSelector(clusterSelectorTerm.LabelSelector, "cluster selector"))

		// Affinity is RequiredDuringSchedulingIgnoredDuringExecution, so check that PropertySorter is nil.
		if clusterSelectorTerm.PropertySorter != nil {
			allErr = append(allErr, fmt.Errorf("PropertySorter is not allowed for RequiredDuringSchedulingIgnoredDuringExecution affinity"))
		}

		// Affinity is RequiredDuringSchedulingIgnoredDuringExecution, so validate PropertySelector if exists
		if clusterSelectorTerm.PropertySelector != nil {
			allErr = append(allErr, validatePropertySelector(clusterSelectorTerm.PropertySelector))
		}
	}
	return apiErrors.NewAggregate(allErr)
}

func validatePreferredClusterSelectors(preferredClusterSelectors []placementv1beta1.PreferredClusterSelector) error {
	allErr := make([]error, 0)
	for _, preferredClusterSelector := range preferredClusterSelectors {
		// API server validation on object occurs before webhook is triggered hence not validating weight.
		allErr = append(allErr, validateLabelSelector(preferredClusterSelector.Preference.LabelSelector, "preferred cluster selector"))

		// Affinity is PreferredDuringSchedulingIgnoredDuringExecution, so check that PropertySelector is nil.
		if preferredClusterSelector.Preference.PropertySelector != nil {
			allErr = append(allErr, fmt.Errorf("PropertySelector is not allowed for PreferredDuringSchedulingIgnoredDuringExecution affinity"))
		}

		if preferredClusterSelector.Preference.PropertySorter != nil {
			allErr = append(allErr, validatePropertySorter(preferredClusterSelector.Preference.PropertySorter))
		}
	}
	return apiErrors.NewAggregate(allErr)
}

func validateLabelSelector(labelSelector *metav1.LabelSelector, parent string) error {
	if _, err := metav1.LabelSelectorAsSelector(labelSelector); err != nil {
		return fmt.Errorf("the labelSelector in %s %+v is invalid: %w", parent, labelSelector, err)
	}
	return nil
}

func validateRolloutStrategy(rolloutStrategy placementv1beta1.RolloutStrategy) error {
	allErr := make([]error, 0)

	if rolloutStrategy.Type != "" && rolloutStrategy.Type != placementv1beta1.RollingUpdateRolloutStrategyType &&
		rolloutStrategy.Type != placementv1beta1.ExternalRolloutStrategyType {
		allErr = append(allErr, fmt.Errorf("unsupported rollout strategy type `%s`", rolloutStrategy.Type))
	}

	if rolloutStrategy.RollingUpdate != nil {
		if rolloutStrategy.Type == placementv1beta1.ExternalRolloutStrategyType {
			allErr = append(allErr, fmt.Errorf("rollingUpdateConifg is not valid for ExternalRollout strategy type"))
		}
		if rolloutStrategy.RollingUpdate.UnavailablePeriodSeconds != nil && *rolloutStrategy.RollingUpdate.UnavailablePeriodSeconds < 0 {
			allErr = append(allErr, fmt.Errorf("unavailablePeriodSeconds must be greater than or equal to 0, got %d", *rolloutStrategy.RollingUpdate.UnavailablePeriodSeconds))
		}
		if rolloutStrategy.RollingUpdate.MaxUnavailable != nil {
			value, err := intstr.GetScaledValueFromIntOrPercent(rolloutStrategy.RollingUpdate.MaxUnavailable, 10, true)
			if err != nil {
				allErr = append(allErr, fmt.Errorf("maxUnavailable `%+v` is invalid: %w", rolloutStrategy.RollingUpdate.MaxUnavailable, err))
			}
			if value < 0 {
				allErr = append(allErr, fmt.Errorf("maxUnavailable must be greater than or equal to 0, got `%+v`", rolloutStrategy.RollingUpdate.MaxUnavailable))
			}
		}
		if rolloutStrategy.RollingUpdate.MaxSurge != nil {
			value, err := intstr.GetScaledValueFromIntOrPercent(rolloutStrategy.RollingUpdate.MaxSurge, 10, true)
			if err != nil {
				allErr = append(allErr, fmt.Errorf("maxSurge `%+v` is invalid: %w", rolloutStrategy.RollingUpdate.MaxSurge, err))
			}
			if value < 0 {
				allErr = append(allErr, fmt.Errorf("maxSurge must be greater than or equal to 0, got `%+v`", rolloutStrategy.RollingUpdate.MaxSurge))
			}
		}
	}

	// server-side apply strategy type is only valid for server-side apply strategy type
	if rolloutStrategy.ApplyStrategy != nil {
		if rolloutStrategy.ApplyStrategy.Type != placementv1beta1.ApplyStrategyTypeServerSideApply && rolloutStrategy.ApplyStrategy.ServerSideApplyConfig != nil {
			allErr = append(allErr, errors.New("serverSideApplyConfig is only valid for ServerSideApply strategy type"))
		}
	}

	return apiErrors.NewAggregate(allErr)
}

// validatePropertySelector validates the property selector
func validatePropertySelector(propertySelector *placementv1beta1.PropertySelector) error {
	return validatePropertySelectorRequirements(propertySelector.MatchExpressions)
}

func validatePropertySelectorRequirements(propertySelectorRequirements []placementv1beta1.PropertySelectorRequirement) error {
	var allErr []error
	for _, req := range propertySelectorRequirements {
		if err := validateName(req.Name); err != nil {
			allErr = append(allErr, fmt.Errorf("invalid property name %s: %w", req.Name, err))
		}
		if err := validateOperator(req.Operator, req.Values); err != nil {
			allErr = append(allErr, err)
		}
		if err := validateValues(req.Values); err != nil {
			allErr = append(allErr, fmt.Errorf("invalid values for property %s: %w", req.Name, err))
		}
		// TODO: Check for logical contradictions
	}
	return apiErrors.NewAggregate(allErr)
}

func validatePropertySorter(propertySorter *placementv1beta1.PropertySorter) error {
	var allErr []error
	if err := validateName(propertySorter.Name); err != nil {
		allErr = append(allErr, err)
	}
	if propertySorter.SortOrder != placementv1beta1.Descending && propertySorter.SortOrder != placementv1beta1.Ascending {
		allErr = append(allErr, fmt.Errorf("invalid property sort order %s", propertySorter.SortOrder))
	}
	return apiErrors.NewAggregate(allErr)
}

func validateName(name string) error {
	// we expect the resource property names to be in this format `[PREFIX]/[CAPACITY_TYPE]-[RESOURCE_NAME]`.
	if strings.HasPrefix(name, propertyprovider.ResourcePropertyNamePrefix) {
		resourcePropertyName, _ := strings.CutPrefix(name, propertyprovider.ResourcePropertyNamePrefix)
		// n=2 since we only care about the first segment to check capacity type.
		segments := strings.SplitN(resourcePropertyName, "-", 2)
		if len(segments) != 2 {
			return fmt.Errorf("invalid resource property name %s, expected format is [PREFIX]/[CAPACITY_TYPE]-[RESOURCE_NAME]", name)
		}
		if !supportedResourceCapacityTypesMap[segments[0]] {
			return fmt.Errorf("invalid capacity type in resource property name %s, supported values are %+v", name, resourceCapacityTypes)
		}

		if errs := validation.IsQualifiedName(name); errs != nil {
			return fmt.Errorf("property name %s is not valid: %s", name, strings.Join(errs, "; "))
		}
		return nil
	}

	// For other properties, they should have a name that is formatted as follows:
	//
	// It should be a string of one or more segments, separated by slashes (/) if applicable;
	// each segment must be 63 characters or less, start and end with an alphanumeric character,
	// and can include dashes (-), underscores (_), dots (.), and alphanumerics in between.
	//
	// Optionally, the property name can have a prefix, which must be a DNS subdomain up to 253 characters,
	// followed by a slash (/).
	segs := strings.Split(name, "/")
	if len(segs) <= 1 {
		// The property name does not have a slash; it has no prefix.
		if errs := validation.IsQualifiedName(name); errs != nil {
			return fmt.Errorf("property name %s is not valid: %s", name, strings.Join(errs, "; "))
		}
	} else {
		// The property name might have a prefix.
		possiblePrefix := segs[0]

		subDomainErrs := validation.IsDNS1123Subdomain(possiblePrefix)
		qualifiedNameErrs := validation.IsQualifiedName(possiblePrefix)
		if len(subDomainErrs) != 0 && len(qualifiedNameErrs) != 0 {
			return fmt.Errorf("property name first segment %s is not valid: it is neither a valid DNS subdomain (%s) nor a valid qualified name (%s)", possiblePrefix, strings.Join(subDomainErrs, "; "), strings.Join(qualifiedNameErrs, "; "))
		}

		segsLeft := segs[1:]
		for idx := range segsLeft {
			seg := segsLeft[idx]
			if errs := validation.IsQualifiedName(seg); errs != nil {
				return fmt.Errorf("property name segment %s is not valid: %s", seg, strings.Join(errs, "; "))
			}
		}
	}
	return nil
}

func validateOperator(op placementv1beta1.PropertySelectorOperator, values []string) error {
	// TODO: Restructure for Eq (bundle operator and value validation logic)
	validOperators := map[placementv1beta1.PropertySelectorOperator]bool{
		placementv1beta1.PropertySelectorGreaterThan:          true,
		placementv1beta1.PropertySelectorGreaterThanOrEqualTo: true,
		placementv1beta1.PropertySelectorLessThan:             true,
		placementv1beta1.PropertySelectorLessThanOrEqualTo:    true,
		placementv1beta1.PropertySelectorEqualTo:              true,
		placementv1beta1.PropertySelectorNotEqualTo:           true,
	}
	if validOperators[op] && len(values) != 1 {
		return fmt.Errorf("operator %s requires exactly one value, got %d", op, len(values))
	}
	return nil
}

func validateValues(values []string) error {
	for _, value := range values {
		if _, err := resource.ParseQuantity(value); err != nil {
			return fmt.Errorf("value %s is not a valid resource.Quantity: %w", value, err)
		}
	}
	return nil
}

func supportedResourceCapacityTypes() []string {
	i := 0
	capacityTypes := make([]string, len(supportedResourceCapacityTypesMap))
	for key := range supportedResourceCapacityTypesMap {
		capacityTypes[i] = key
		i++
	}
	sort.Strings(capacityTypes)
	return capacityTypes
}

// HandlePlacementValidation provides consolidated webhook validation logic for placement objects.
// This function accepts higher-order functions for type-specific operations.
func HandlePlacementValidation(
	ctx context.Context,
	req admission.Request,
	decoder webhook.AdmissionDecoder,
	resourceType string,
	decodeFunc func(admission.Request, webhook.AdmissionDecoder) (placementv1beta1.PlacementObj, error),
	decodeOldFunc func(admission.Request, webhook.AdmissionDecoder) (placementv1beta1.PlacementObj, error),
	validateFunc func(placementv1beta1.PlacementObj) error,
) admission.Response {
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update {
		klog.V(2).InfoS("handling placement", "resourceType", resourceType, "operation", req.Operation, "namespacedName", types.NamespacedName{Name: req.Name, Namespace: req.Namespace})

		placement, err := decodeFunc(req, decoder)
		if err != nil {
			klog.ErrorS(err, "failed to decode v1beta1 placement object for create/update operation", "resourceType", resourceType, "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
			return admission.Errored(http.StatusBadRequest, err)
		}

		if req.Operation == admissionv1.Update {
			oldPlacement, err := decodeOldFunc(req, decoder)
			if err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}

			// Special case: allow updates to old placement objects with invalid fields so that we can
			// update the placement to remove finalizer then delete it.
			if err := validateFunc(oldPlacement); err != nil {
				if placement.GetDeletionTimestamp() != nil {
					return admission.Allowed(fmt.Sprintf(AllowUpdateOldInvalidFmt, resourceType))
				}
				return admission.Denied(fmt.Sprintf(DenyUpdateOldInvalidFmt, resourceType, err))
			}

			// Handle update case where placement type should be immutable.
			if IsPlacementPolicyTypeUpdated(oldPlacement.GetPlacementSpec().Policy, placement.GetPlacementSpec().Policy) {
				return admission.Denied("placement type is immutable")
			}

			// Handle update case where existing tolerations were updated/deleted
			if IsTolerationsUpdatedOrDeleted(oldPlacement.GetPlacementSpec().Tolerations(), placement.GetPlacementSpec().Tolerations()) {
				return admission.Denied("tolerations have been updated/deleted, only additions to tolerations are allowed")
			}
		}

		if err := validateFunc(placement); err != nil {
			klog.V(2).InfoS("v1beta1 placement has invalid fields, request is denied", "resourceType", resourceType, "operation", req.Operation, "namespacedName", types.NamespacedName{Name: placement.GetName(), Namespace: req.Namespace})
			return admission.Denied(fmt.Sprintf(DenyCreateUpdateInvalidFmt, resourceType, err))
		}
	}

	return admission.Allowed(fmt.Sprintf(AllowModifyFmt, resourceType))
}
