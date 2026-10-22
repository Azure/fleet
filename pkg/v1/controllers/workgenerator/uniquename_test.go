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

package workgenerator

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

// dottedName returns a name of the given length with a dot as every 10th character, starting from the given offset
// (0-9), except for the last character, e.g., dottedName(23, 9) = "abcdefghi.klmnopqrs.uvw".
func dottedName(length, dotOffset int) string {
	var sb strings.Builder
	for idx := 0; idx < length; idx++ {
		if idx%10 == dotOffset && idx != length-1 {
			sb.WriteByte('.')
			continue
		}
		sb.WriteByte(byte('a' + idx%26))
	}
	return sb.String()
}

// customDerivedFromSourceFormatter is a derivedFromSourceFormatter with fully customizable values.
type customDerivedFromSourceFormatter struct {
	sourceType string
	sourceID   string
	dnsLabel   string
}

func (f *customDerivedFromSourceFormatter) SourceID() string       { return f.sourceID }
func (f *customDerivedFromSourceFormatter) SourceType() string     { return f.sourceType }
func (f *customDerivedFromSourceFormatter) StrictDNSLabel() string { return f.dnsLabel }

func snapshotFormatter(subIdx string) derivedFromSourceFormatter {
	return &placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: subIdx}
}

func bindingForUniqueName(namespace, policyName string) placementv1alpha1.PlacementBindingAccessor {
	binding := newPlacementBindingObj(namespace, "binding")
	binding.SetSpec(placementv1alpha1.PlacementBindingSpec{PlacementPolicyName: policyName})
	return binding
}

// TestUniqueNameForWorkDerivedFromPlacementResourceSnapshot tests the
// uniqueNameForWorkDerivedFromPlacementResourceSnapshot function.
//
// The hashes below are the first 12 hex characters of the SHA-256 digests of the hash inputs documented in
// uniquename.go (e.g., `team-a/red-work`), computed independently of the implementation.
func TestUniqueNameForWorkDerivedFromPlacementResourceSnapshot(t *testing.T) {
	// The dot-free form of a dotted name, truncated to 233 characters (251 - len("-work-") - 12).
	dottedPolicyName := dottedName(300, 5)
	wantTruncatedDottedPolicyName := strings.ReplaceAll(dottedPolicyName, ".", "")[:233]

	testCases := []struct {
		name       string
		namespace  string
		policyName string
		isPrimary  bool
		formatter  derivedFromSourceFormatter
		wantName   string
	}{
		{
			name:       "primary, namespace-scoped policy",
			namespace:  "team-a",
			policyName: "red",
			isPrimary:  true,
			formatter:  snapshotFormatter("0"),
			wantName:   "team-a-red-work-25dda90d22a3",
		},
		{
			name:       "primary, cluster-scoped policy",
			namespace:  "",
			policyName: "red",
			isPrimary:  true,
			formatter:  snapshotFormatter("0"),
			wantName:   "red-work-830565d28775",
		},
		{
			name:       "secondary, sub-index 1",
			namespace:  "team-a",
			policyName: "red",
			formatter:  snapshotFormatter("1"),
			wantName:   "team-a-red-work-1-309264462839",
		},
		{
			name:       "secondary, sub-index 12",
			namespace:  "team-a",
			policyName: "red",
			formatter:  snapshotFormatter("12"),
			wantName:   "team-a-red-work-12-14a0ae508b07",
		},
		{
			// `red` in `team-a` yields the same name segment as `a-red` in `team`; only the hash tells them apart.
			name:       "primary, name segment shared with another namespace/name combination",
			namespace:  "team",
			policyName: "a-red",
			isPrimary:  true,
			formatter:  snapshotFormatter("0"),
			wantName:   "team-a-red-work-0d3acb940e0a",
		},
		{
			// Dots are removed from the name segment, but the hash is computed over the original values.
			name:       "primary, names with dots",
			namespace:  "ns.one",
			policyName: "my.policy.v1",
			isPrimary:  true,
			formatter:  snapshotFormatter("0"),
			wantName:   "nsone-mypolicyv1-work-81ba5d26981e",
		},
		{
			name:       "primary, exactly at the length limit (not truncated)",
			namespace:  "",
			policyName: strings.Repeat("a", 233),
			isPrimary:  true,
			formatter:  snapshotFormatter("0"),
			wantName:   strings.Repeat("a", 233) + "-work-049cbf283748",
		},
		{
			name:       "primary, one character over the length limit (truncated)",
			namespace:  "",
			policyName: strings.Repeat("a", 234),
			isPrimary:  true,
			formatter:  snapshotFormatter("0"),
			wantName:   strings.Repeat("a", 233) + "-work-4c7c1e597554",
		},
		{
			name:       "primary, namespace and policy name combined over the length limit (truncated)",
			namespace:  strings.Repeat("n", 100),
			policyName: strings.Repeat("p", 200),
			isPrimary:  true,
			formatter:  snapshotFormatter("0"),
			// The namespaced name segment is `[NAMESPACE]-[NAME]`, truncated to 233 characters.
			wantName: strings.Repeat("n", 100) + "-" + strings.Repeat("p", 132) + "-work-d43a46802b5b",
		},
		{
			// Truncation happens after the dots are removed, so the name never ends with a dot.
			name:       "primary, dotted policy name over the length limit (truncated)",
			namespace:  "",
			policyName: dottedPolicyName,
			isPrimary:  true,
			formatter:  snapshotFormatter("0"),
			wantName:   wantTruncatedDottedPolicyName + "-work-fadf9e3d4a62",
		},
		{
			// The available length (251 - len("-work--") - 12 = 232) is split evenly between the name segment
			// and the source label; the short label fits as is.
			name:       "secondary, over the length limit (name segment truncated)",
			namespace:  "",
			policyName: strings.Repeat("a", 300),
			formatter:  snapshotFormatter("1"),
			wantName:   strings.Repeat("a", 116) + "-work-1-c3937b0a58ec",
		},
		{
			name:       "secondary, name segment and source label both over the length limit (both truncated)",
			namespace:  "",
			policyName: strings.Repeat("p", 200),
			formatter: &customDerivedFromSourceFormatter{
				sourceType: "test-source",
				sourceID:   "id-1",
				dnsLabel:   strings.Repeat("l", 200),
			},
			wantName: strings.Repeat("p", 116) + "-work-" + strings.Repeat("l", 116) + "-c17475bfc3d0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			binding := bindingForUniqueName(tc.namespace, tc.policyName)
			gotName := uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, tc.isPrimary, tc.formatter)
			if gotName != tc.wantName {
				t.Errorf("uniqueNameForWorkDerivedFromPlacementResourceSnapshot(%q, %q, %t) = %q, want %q",
					tc.namespace, tc.policyName, tc.isPrimary, gotName, tc.wantName)
			}
			if len(gotName) > nameLenLimit {
				t.Errorf("len(uniqueNameForWorkDerivedFromPlacementResourceSnapshot(%q, %q, %t)) = %d, want <= %d",
					tc.namespace, tc.policyName, tc.isPrimary, len(gotName), nameLenLimit)
			}
			if errs := validation.IsDNS1123Subdomain(gotName); len(errs) > 0 {
				t.Errorf("uniqueNameForWorkDerivedFromPlacementResourceSnapshot(%q, %q, %t) = %q, which is not a valid DNS subdomain: %v",
					tc.namespace, tc.policyName, tc.isPrimary, gotName, errs)
			}
		})
	}
}

// TestUniqueNameForWorkDerivedFromPlacementResourceSnapshotUniqueness verifies that the generated names are
// deterministic and distinct across sources and across namespace/name combinations.
func TestUniqueNameForWorkDerivedFromPlacementResourceSnapshotUniqueness(t *testing.T) {
	type input struct {
		namespace  string
		policyName string
		isPrimary  bool
		subIdx     string
	}
	inputs := []input{
		{namespace: "team-a", policyName: "red", isPrimary: true, subIdx: "0"},
		{namespace: "team-a", policyName: "red", subIdx: "1"},
		{namespace: "team-a", policyName: "red", subIdx: "2"},
		{namespace: "team-a", policyName: "red", subIdx: "11"},
		{namespace: "team", policyName: "a-red", isPrimary: true, subIdx: "0"},
		{namespace: "team", policyName: "a-red", subIdx: "1"},
		{namespace: "", policyName: "team-a-red", isPrimary: true, subIdx: "0"},
		{namespace: "", policyName: "team-a-red", subIdx: "1"},
		// Names that differ only beyond the truncation point.
		{namespace: "", policyName: strings.Repeat("a", 240) + "x", isPrimary: true, subIdx: "0"},
		{namespace: "", policyName: strings.Repeat("a", 240) + "y", isPrimary: true, subIdx: "0"},
		{namespace: "", policyName: strings.Repeat("a", 240) + "x", subIdx: "1"},
		{namespace: "", policyName: strings.Repeat("a", 240) + "y", subIdx: "1"},
	}

	seen := make(map[string]input, len(inputs))
	for _, in := range inputs {
		gotName := uniqueNameForWorkDerivedFromPlacementResourceSnapshot(
			bindingForUniqueName(in.namespace, in.policyName), in.isPrimary, snapshotFormatter(in.subIdx))
		gotAgain := uniqueNameForWorkDerivedFromPlacementResourceSnapshot(
			bindingForUniqueName(in.namespace, in.policyName), in.isPrimary, snapshotFormatter(in.subIdx))
		if gotName != gotAgain {
			t.Errorf("uniqueNameForWorkDerivedFromPlacementResourceSnapshot(%+v) = %q then %q, want a deterministic result", in, gotName, gotAgain)
		}
		if other, found := seen[gotName]; found {
			t.Errorf("uniqueNameForWorkDerivedFromPlacementResourceSnapshot(%+v) = %q, which collides with the name for %+v", in, gotName, other)
		}
		seen[gotName] = in
	}
}

// TestPlacementResourceSnapshotDerivedFromSourceFormatter tests the formatter for placement resource snapshots.
func TestPlacementResourceSnapshotDerivedFromSourceFormatter(t *testing.T) {
	testCases := []struct {
		subIdx             string
		wantSourceID       string
		wantSourceType     string
		wantStrictDNSLabel string
	}{
		{subIdx: "0", wantSourceID: "0", wantSourceType: "placement-resource-snapshot", wantStrictDNSLabel: "0"},
		{subIdx: "12", wantSourceID: "12", wantSourceType: "placement-resource-snapshot", wantStrictDNSLabel: "12"},
	}

	for _, tc := range testCases {
		t.Run("sub-index "+tc.subIdx, func(t *testing.T) {
			f := snapshotFormatter(tc.subIdx)
			if got := f.SourceID(); got != tc.wantSourceID {
				t.Errorf("SourceID() = %q, want %q", got, tc.wantSourceID)
			}
			if got := f.SourceType(); got != tc.wantSourceType {
				t.Errorf("SourceType() = %q, want %q", got, tc.wantSourceType)
			}
			if got := f.StrictDNSLabel(); got != tc.wantStrictDNSLabel {
				t.Errorf("StrictDNSLabel() = %q, want %q", got, tc.wantStrictDNSLabel)
			}
		})
	}
}
