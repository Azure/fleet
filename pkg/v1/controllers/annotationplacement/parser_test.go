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

package annotationplacement

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

const (
	regionLabel = "topology.kubernetes.io/region"
	aliasLabel  = "kubefleet.dev/cluster-alias"

	// qualifiedNameMaxLength is the longest name segment of a label key that Kubernetes accepts.
	// It mirrors the unexported limit that validation.IsQualifiedName enforces.
	qualifiedNameMaxLength = 63

	// crdPath is the placement policy CRD, read by TestLimitsMatchAPIValidation.
	crdPath = "../../../../config/crd/bases/placement.kubefleet.dev_placementpolicies.yaml"
)

// selectorOf builds the cluster selector that the parser is expected to produce for one segment of
// the annotation; a nil matchLabels stands for a selector that carries no terms.
func selectorOf(count intstr.IntOrString, matchLabels map[string]string) kfplacementv1alpha1.ClusterSelector {
	selector := kfplacementv1alpha1.ClusterSelector{Count: &count}
	if matchLabels != nil {
		selector.Terms = []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: matchLabels},
		}
	}
	return selector
}

func TestParseClusterSelectors(t *testing.T) {
	one, three, all := intstr.FromInt32(1), intstr.FromInt32(3), intstr.FromString("All")

	testCases := []struct {
		name  string
		value string
		want  []kfplacementv1alpha1.ClusterSelector
	}{
		{
			name:  "single label matcher with the region shorthand",
			value: "region=eastus",
			want: []kfplacementv1alpha1.ClusterSelector{
				selectorOf(one, map[string]string{regionLabel: "eastus"}),
			},
		},
		{
			// The first example given in FEP-0001.
			name:  "one cluster per region",
			value: "region=eastus;region=westus;region=centralus",
			want: []kfplacementv1alpha1.ClusterSelector{
				selectorOf(one, map[string]string{regionLabel: "eastus"}),
				selectorOf(one, map[string]string{regionLabel: "westus"}),
				selectorOf(one, map[string]string{regionLabel: "centralus"}),
			},
		},
		{
			// The second example given in FEP-0001.
			name:  "named clusters through the alias shorthand",
			value: "alias=bravelion;alias=smartfish",
			want: []kfplacementv1alpha1.ClusterSelector{
				selectorOf(one, map[string]string{aliasLabel: "bravelion"}),
				selectorOf(one, map[string]string{aliasLabel: "smartfish"}),
			},
		},
		{
			// The third example given in FEP-0001.
			name:  "mixed counts and multiple matchers per selector",
			value: "env=staging,count=All;env=canary,region=eastus,count=1",
			want: []kfplacementv1alpha1.ClusterSelector{
				selectorOf(all, map[string]string{"env": "staging"}),
				selectorOf(one, map[string]string{"env": "canary", regionLabel: "eastus"}),
			},
		},
		{
			name:  "count on its own selects the whole fleet",
			value: "count=All",
			want:  []kfplacementv1alpha1.ClusterSelector{selectorOf(all, nil)},
		},
		{
			name:  "count directive may precede the label matchers",
			value: "count=3,env=prod",
			want: []kfplacementv1alpha1.ClusterSelector{
				selectorOf(three, map[string]string{"env": "prod"}),
			},
		},
		{
			name:  "fully qualified label keys are passed through",
			value: fmt.Sprintf("%s=eastus", regionLabel),
			want: []kfplacementv1alpha1.ClusterSelector{
				selectorOf(one, map[string]string{regionLabel: "eastus"}),
			},
		},
		{
			// Shorthands are expanded on an exact match only; Kubernetes label keys are
			// case-sensitive, so a differently cased key is a different label.
			name:  "shorthand expansion is case-sensitive",
			value: "Region=eastus",
			want: []kfplacementv1alpha1.ClusterSelector{
				selectorOf(one, map[string]string{"Region": "eastus"}),
			},
		},
		{
			name:  "surrounding whitespace is ignored",
			value: " region = eastus , count = 3 ; env=prod ",
			want: []kfplacementv1alpha1.ClusterSelector{
				selectorOf(three, map[string]string{regionLabel: "eastus"}),
				selectorOf(one, map[string]string{"env": "prod"}),
			},
		},
		{
			name:  "count at the top of the supported range",
			value: fmt.Sprintf("env=prod,count=%d", maxCount),
			want: []kfplacementv1alpha1.ClusterSelector{
				selectorOf(intstr.FromInt32(maxCount), map[string]string{"env": "prod"}),
			},
		},
		{
			name:  "label value at the longest length Kubernetes accepts",
			value: "env=" + strings.Repeat("a", validation.LabelValueMaxLength),
			want: []kfplacementv1alpha1.ClusterSelector{
				selectorOf(one, map[string]string{"env": strings.Repeat("a", validation.LabelValueMaxLength)}),
			},
		},
		{
			name: "label matchers at the supported maximum",
			value: func() string {
				matchers := make([]string, 0, maxMatchLabels)
				for i := 0; i < maxMatchLabels; i++ {
					matchers = append(matchers, fmt.Sprintf("key%d=value", i))
				}
				return strings.Join(matchers, matcherSeparator)
			}(),
			want: func() []kfplacementv1alpha1.ClusterSelector {
				matchLabels := make(map[string]string, maxMatchLabels)
				for i := 0; i < maxMatchLabels; i++ {
					matchLabels[fmt.Sprintf("key%d", i)] = "value"
				}
				return []kfplacementv1alpha1.ClusterSelector{selectorOf(one, matchLabels)}
			}(),
		},
		{
			// The count directive is reserved on an exact match only, like the shorthands above.
			name:  "the count keyword is case-sensitive",
			value: "Count=5",
			want: []kfplacementv1alpha1.ClusterSelector{
				selectorOf(one, map[string]string{"Count": "5"}),
			},
		},
		{
			name:  "selector count at the supported maximum",
			value: strings.TrimSuffix(strings.Repeat("region=eastus;", maxSelectors), ";"),
			want: func() []kfplacementv1alpha1.ClusterSelector {
				selectors := make([]kfplacementv1alpha1.ClusterSelector, 0, maxSelectors)
				for i := 0; i < maxSelectors; i++ {
					selectors = append(selectors, selectorOf(one, map[string]string{regionLabel: "eastus"}))
				}
				return selectors
			}(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseClusterSelectors(tc.value)
			if err != nil {
				t.Fatalf("parseClusterSelectors(%q) = %v, want no error", tc.value, err)
			}
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("parseClusterSelectors(%q) mismatch (-got, +want):\n%s", tc.value, diff)
			}
		})
	}
}

// formatLimit renders a CRD size limit, which is absent as often as it is set.
func formatLimit(limit *int64) string {
	if limit == nil {
		return "no limit"
	}
	return strconv.FormatInt(*limit, 10)
}

// TestLimitsMatchAPIValidation ties the parser's limits to the validation markers on the placement
// policy CRD.
//
// The parser turns an annotation that exceeds a limit into an error reported against the annotation
// itself. Were the API to tighten a limit on its own, the parser would instead keep building policy
// objects that the API server rejects, and the failure would surface as an endless retry far away
// from the annotation that caused it.
func TestLimitsMatchAPIValidation(t *testing.T) {
	rawCRD, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) = %v, want no error", crdPath, err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(rawCRD, &crd); err != nil {
		t.Fatalf("Unmarshal(%q) = %v, want no error", crdPath, err)
	}

	var schema *apiextensionsv1.JSONSchemaProps
	for idx := range crd.Spec.Versions {
		if crd.Spec.Versions[idx].Name == kfplacementv1alpha1.GroupVersion.Version {
			schema = crd.Spec.Versions[idx].Schema.OpenAPIV3Schema
			break
		}
	}
	if schema == nil {
		t.Fatalf("the %s CRD has no %s version", crd.Name, kfplacementv1alpha1.GroupVersion.Version)
	}

	clusterSelectors := schema.Properties["spec"].Properties["clusterSelectors"]
	if got := clusterSelectors.MaxItems; got == nil || *got != maxSelectors {
		t.Errorf("the CRD caps clusterSelectors at %s, want maxSelectors (%d)", formatLimit(got), maxSelectors)
	}
	// Every step below walks into the schema, so a renamed or restructured field must stop the test
	// with a message that names the missing step rather than panic on a nil pointer.
	if clusterSelectors.Items == nil || clusterSelectors.Items.Schema == nil {
		t.Fatalf("the CRD schema of clusterSelectors carries no item schema")
	}

	selector := clusterSelectors.Items.Schema.Properties
	terms := selector["terms"]
	if terms.Items == nil || terms.Items.Schema == nil {
		t.Fatalf("the CRD schema of the selector terms carries no item schema")
	}
	matchLabels := terms.Items.Schema.Properties["matchLabels"]
	if got := matchLabels.MaxProperties; got == nil || *got != maxMatchLabels {
		t.Errorf("the CRD caps matchLabels at %s, want maxMatchLabels (%d)", formatLimit(got), maxMatchLabels)
	}

	// The count field is an int-or-string, and the API bounds each form separately: a pattern for
	// the string form, checked here by exercising it, and a CEL rule for the integer form, checked
	// here by asserting the rule spells out the same bounds the parser enforces. A pattern alone
	// would leave the integer form unbounded.
	count := selector["count"]
	countPattern, err := regexp.Compile(count.Pattern)
	if err != nil {
		t.Fatalf("Compile(%q) = %v, want no error", count.Pattern, err)
	}
	if got := strconv.Itoa(maxCount); !countPattern.MatchString(got) {
		t.Errorf("the CRD pattern %q rejects maxCount (%s), want it accepted", count.Pattern, got)
	}
	if got := strconv.Itoa(maxCount + 1); countPattern.MatchString(got) {
		t.Errorf("the CRD pattern %q accepts %s, want maxCount (%d) to be the ceiling", count.Pattern, got, maxCount)
	}
	if !countPattern.MatchString(countAll) {
		t.Errorf("the CRD pattern %q rejects %q, want it accepted", count.Pattern, countAll)
	}

	// The rule's spelling is free to change; what must not drift is the ceiling it encodes, in
	// both the rule and the message a user is shown. The behavior itself is pinned separately by
	// the integration suite, which submits out-of-range counts to a real API server.
	wantCeiling := fmt.Sprintf("%d", maxCount)
	integerRuleFound := false
	for _, validation := range count.XValidations {
		if !strings.Contains(validation.Rule, wantCeiling) {
			continue
		}
		integerRuleFound = true
		if !strings.Contains(validation.Message, wantCeiling) {
			t.Errorf("the CRD rejects an out-of-range count with the message %q, want it to name the ceiling (%s)", validation.Message, wantCeiling)
		}
	}
	if !integerRuleFound {
		rules := make([]string, 0, len(count.XValidations))
		for _, validation := range count.XValidations {
			rules = append(rules, validation.Rule)
		}
		t.Errorf("the CRD validates count with the rules %q, want one to bound the integer form at maxCount (%s)", rules, wantCeiling)
	}

	if count.Default == nil {
		t.Fatalf("the CRD schema of the selector count carries no default")
	}
	wantDefault := strconv.Itoa(defaultCount)
	if got := string(count.Default.Raw); got != wantDefault {
		t.Errorf("the CRD defaults count to %s, want defaultCount (%s)", got, wantDefault)
	}
}

func TestParseClusterSelectorsInvalid(t *testing.T) {
	testCases := []struct {
		name string
		// value is the annotation value under test.
		value string
		// wantErrContains is a fragment the error message must carry, so that the annotation's
		// author can tell which part of the value was rejected and why.
		wantErrContains string
	}{
		{
			name:            "empty value",
			value:           "",
			wantErrContains: "empty",
		},
		{
			name:            "whitespace-only value",
			value:           "   ",
			wantErrContains: "empty",
		},
		{
			name:            "more selectors than supported",
			value:           strings.TrimSuffix(strings.Repeat("region=eastus;", maxSelectors+1), ";"),
			wantErrContains: "more than the supported maximum",
		},
		{
			// Selectors are counted from one in the error, since whoever wrote the annotation
			// counts them by eye.
			name:            "empty selector between two others",
			value:           "region=eastus;;region=westus",
			wantErrContains: "cluster selector 2",
		},
		{
			name:            "empty selector reports the reason as well as the position",
			value:           "region=eastus;;region=westus",
			wantErrContains: "empty label matcher",
		},
		{
			name:            "trailing separator leaves an empty selector",
			value:           "region=eastus;",
			wantErrContains: "empty label matcher",
		},
		{
			name:            "trailing matcher separator",
			value:           "region=eastus,",
			wantErrContains: "empty label matcher",
		},
		{
			name:            "matcher without a value",
			value:           "region",
			wantErrContains: "KEY=VALUE",
		},
		{
			name:            "matcher with an empty key",
			value:           "=eastus",
			wantErrContains: "empty label key",
		},
		{
			name:            "label key that Kubernetes rejects",
			value:           "not a key=eastus",
			wantErrContains: "is not valid",
		},
		{
			name:            "label value that Kubernetes rejects",
			value:           "env=not a value",
			wantErrContains: "is not valid",
		},
		{
			name:            "label matched against an empty value",
			value:           "env=",
			wantErrContains: "empty value",
		},
		{
			// A value one byte past what Kubernetes accepts. Left unchecked, this would become a
			// policy object that the API server rejects, turning a typo into an endless retry.
			name:            "label value one byte too long",
			value:           "env=" + strings.Repeat("a", validation.LabelValueMaxLength+1),
			wantErrContains: "is not valid",
		},
		{
			name:            "label key one byte too long",
			value:           strings.Repeat("k", qualifiedNameMaxLength+1) + "=prod",
			wantErrContains: "is not valid",
		},
		{
			// strings.Cut splits on the first separator only, so the value keeps the rest; it is
			// then rejected, since a label value cannot contain a separator.
			name:            "value containing the key-value separator",
			value:           "env=prod=east",
			wantErrContains: "is not valid",
		},
		{
			name:            "same label key matched twice",
			value:           "env=staging,env=canary",
			wantErrContains: "more than once",
		},
		{
			// The shorthand and the key it expands to are the same label, which would otherwise
			// silently drop one of the two matchers.
			name:            "shorthand collides with its expanded key",
			value:           fmt.Sprintf("region=eastus,%s=westus", regionLabel),
			wantErrContains: "more than once",
		},
		{
			name:            "count set twice",
			value:           "env=prod,count=1,count=2",
			wantErrContains: "more than once",
		},
		{
			name:            "count of zero",
			value:           "env=prod,count=0",
			wantErrContains: "out of the supported range",
		},
		{
			name:            "count above the supported range",
			value:           fmt.Sprintf("env=prod,count=%d", maxCount+1),
			wantErrContains: "out of the supported range",
		},
		{
			// All digits, but too large to hold; this must be reported like any other out-of-range
			// count rather than escaping as a conversion failure.
			name:            "count too large to be represented",
			value:           "env=prod,count=99999999999999999999",
			wantErrContains: "out of the supported range",
		},
		{
			name:            "negative count",
			value:           "env=prod,count=-1",
			wantErrContains: "neither a positive integer",
		},
		{
			name:            "explicitly signed count",
			value:           "env=prod,count=+5",
			wantErrContains: "neither a positive integer",
		},
		{
			name:            "count that is not a number",
			value:           "env=prod,count=many",
			wantErrContains: "neither a positive integer",
		},
		{
			// A near miss on the All sentinel is worth its own message; the API accepts the exact
			// spelling only.
			name:            "lowercased All sentinel",
			value:           "env=prod,count=all",
			wantErrContains: `spelled exactly "All"`,
		},
		{
			name:            "uppercased All sentinel",
			value:           "env=prod,count=ALL",
			wantErrContains: `spelled exactly "All"`,
		},
		{
			name: "more label matchers than supported in one selector",
			value: func() string {
				matchers := make([]string, 0, maxMatchLabels+1)
				for i := 0; i <= maxMatchLabels; i++ {
					matchers = append(matchers, fmt.Sprintf("key%d=value", i))
				}
				return strings.Join(matchers, matcherSeparator)
			}(),
			wantErrContains: "more than the supported maximum",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseClusterSelectors(tc.value)
			if err == nil {
				t.Fatalf("parseClusterSelectors(%q) = %v, want an error", tc.value, got)
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Errorf("parseClusterSelectors(%q) = %v, want an error containing %q", tc.value, err, tc.wantErrContains)
			}
		})
	}
}
