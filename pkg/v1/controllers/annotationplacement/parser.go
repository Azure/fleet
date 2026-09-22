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

// Package annotationplacement implements FEP-0001 annotation-based placement: it keeps a placement
// policy object in sync with the kubefleet.dev/cluster-selectors annotation set on a Kubernetes
// resource, so that simple placement scenarios can be expressed without authoring a placement
// policy by hand.
package annotationplacement

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

const (
	// selectorSeparator separates the cluster selectors within the annotation value.
	selectorSeparator = ";"
	// matcherSeparator separates the label matchers, and the count directive, within one selector.
	matcherSeparator = ","
	// keyValueSeparator separates a label matcher's key from its value.
	keyValueSeparator = "="

	// countKey is the reserved matcher key that sets the desired cluster count of a selector.
	countKey = "count"
	// countAll is the count value that selects every cluster matching the selector's terms.
	countAll = "All"

	// regionShorthand and aliasShorthand are the label key shorthands FEP-0001 reserves.
	regionShorthand = "region"
	aliasShorthand  = "alias"
)

// The limits below mirror the validation markers on the placement policy API, so that a rejected
// annotation is reported against the annotation itself rather than surfacing later as an opaque
// failure to create the policy object.
const (
	// maxSelectors mirrors MaxItems on PlacementPolicySpec.ClusterSelectors.
	maxSelectors = 10
	// maxMatchLabels mirrors MaxProperties on ClusterLabelAndPropertySelectorTerm.MatchLabels.
	maxMatchLabels = 10
	// maxCount mirrors the ceiling on ClusterSelector.Count, which the API enforces once per form:
	// the Pattern marker bounds the string form, and a CEL rule bounds the integer form, since a
	// pattern does not constrain the integer side of an int-or-string.
	maxCount = 999
)

// defaultCount is the desired cluster count of a selector that does not carry a count directive. It
// matches the API's own default for the field, and is set explicitly rather than left to the API
// server so that the policy object built from an annotation compares equal to the one the API
// server stores; otherwise every reconciliation would observe a difference and issue an update.
const defaultCount = 1

// shorthandLabelKeys maps the label key shorthands reserved by FEP-0001 to the keys they expand to.
var shorthandLabelKeys = map[string]string{
	regionShorthand: corev1.LabelTopologyRegion,
	aliasShorthand:  placementv1beta1.ClusterAliasLabel,
}

// parseClusterSelectors converts the value of the kubefleet.dev/cluster-selectors annotation into
// the cluster selectors of a placement policy.
//
// Errors returned by this function are always caused by the annotation's own contents, so they are
// categorized as user errors: they are never transient, and a caller should surface them to the
// user rather than retry. The message stays human-readable, since it is what the event recorded on
// the annotated resource shows: the category is for callers, the text is for whoever wrote the
// annotation.
func parseClusterSelectors(value string) ([]kfplacementv1alpha1.ClusterSelector, error) {
	if strings.TrimSpace(value) == "" {
		return nil, kferrors.NewUserError(nil, "the annotation value is empty; it must list at least one cluster selector")
	}

	segments := strings.Split(value, selectorSeparator)
	if len(segments) > maxSelectors {
		return nil, kferrors.NewUserError(nil, fmt.Sprintf("the annotation lists %d cluster selectors, more than the supported maximum of %d", len(segments), maxSelectors))
	}

	selectors := make([]kfplacementv1alpha1.ClusterSelector, 0, len(segments))
	for idx, segment := range segments {
		selector, err := parseSelector(segment)
		if err != nil {
			// The index is 1-based: it is read by whoever wrote the annotation, who counts the
			// selectors in it by eye.
			return nil, kferrors.NewUserError(err, fmt.Sprintf("cluster selector %d (%q) is invalid", idx+1, strings.TrimSpace(segment)))
		}
		selectors = append(selectors, selector)
	}
	return selectors, nil
}

// parseSelector converts one semicolon-delimited segment of the annotation into a cluster selector.
func parseSelector(segment string) (kfplacementv1alpha1.ClusterSelector, error) {
	var selector kfplacementv1alpha1.ClusterSelector

	matchLabels := make(map[string]string)
	var count *intstr.IntOrString

	for _, matcher := range strings.Split(segment, matcherSeparator) {
		matcher = strings.TrimSpace(matcher)
		if matcher == "" {
			return selector, fmt.Errorf("it has an empty label matcher")
		}

		rawKey, rawValue, found := strings.Cut(matcher, keyValueSeparator)
		if !found {
			return selector, fmt.Errorf("the label matcher %q is not in the KEY=VALUE format", matcher)
		}
		rawKey, rawValue = strings.TrimSpace(rawKey), strings.TrimSpace(rawValue)

		if rawKey == countKey {
			if count != nil {
				return selector, fmt.Errorf("it sets %s more than once", countKey)
			}
			parsed, err := parseCount(rawValue)
			if err != nil {
				return selector, err
			}
			count = parsed
			continue
		}

		key, err := parseLabelKey(rawKey)
		if err != nil {
			return selector, err
		}
		if _, duplicate := matchLabels[key]; duplicate {
			return selector, fmt.Errorf("it matches on the label key %q more than once", key)
		}
		if err := validateLabelValue(key, rawValue); err != nil {
			return selector, err
		}
		matchLabels[key] = rawValue
	}

	if len(matchLabels) > maxMatchLabels {
		return selector, fmt.Errorf("it matches on %d label keys, more than the supported maximum of %d", len(matchLabels), maxMatchLabels)
	}

	if count == nil {
		fallback := intstr.FromInt32(defaultCount)
		count = &fallback
	}
	selector.Count = count
	// A selector that carries only a count directive has no terms, which the API reads as a match on
	// every cluster; this is deliberate, as it lets `count=All` on its own select the whole fleet.
	if len(matchLabels) > 0 {
		selector.Terms = []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: matchLabels},
		}
	}
	return selector, nil
}

// parseCount converts the value of a count directive into the desired cluster count of a selector.
func parseCount(value string) (*intstr.IntOrString, error) {
	if value == countAll {
		all := intstr.FromString(countAll)
		return &all, nil
	}
	if strings.EqualFold(value, countAll) {
		return nil, fmt.Errorf("the %s value %q is not recognized; it must be spelled exactly %q", countKey, value, countAll)
	}

	// ParseUint accepts exactly a run of decimal digits: no sign, no whitespace, no exponent.
	parsed, err := strconv.ParseUint(value, 10, 32)
	switch {
	case errors.Is(err, strconv.ErrRange):
		return nil, fmt.Errorf("the %s value %q is out of the supported range (1-%d)", countKey, value, maxCount)
	case err != nil:
		return nil, fmt.Errorf("the %s value %q is neither a positive integer nor %q", countKey, value, countAll)
	}
	if parsed < 1 || parsed > maxCount {
		return nil, fmt.Errorf("the %s value %d is out of the supported range (1-%d)", countKey, parsed, maxCount)
	}

	count := intstr.FromInt32(int32(parsed))
	return &count, nil
}

// parseLabelKey expands a label key shorthand, if one is used, and verifies that the result is a
// key that Kubernetes accepts.
func parseLabelKey(key string) (string, error) {
	if expanded, isShorthand := shorthandLabelKeys[key]; isShorthand {
		return expanded, nil
	}
	if key == "" {
		return "", fmt.Errorf("it has a label matcher with an empty label key")
	}
	if errs := validation.IsQualifiedName(key); len(errs) > 0 {
		return "", fmt.Errorf("the label key %q is not valid: %s", key, strings.Join(errs, "; "))
	}
	return key, nil
}

// validateLabelValue verifies that a label matcher's value is one that Kubernetes accepts.
//
// Kubernetes considers the empty string a valid label value, but this surface rejects it: an empty
// value in a shorthand annotation is far more often a typo than a deliberate match on a label set
// to the empty string, and the placement policy API remains available for the latter.
func validateLabelValue(key, value string) error {
	if value == "" {
		return fmt.Errorf("the label key %q is matched against an empty value; use a placement policy object to match on an empty label value", key)
	}
	if errs := validation.IsValidLabelValue(value); len(errs) > 0 {
		return fmt.Errorf("the value %q of the label key %q is not valid: %s", value, key, strings.Join(errs, "; "))
	}
	return nil
}
