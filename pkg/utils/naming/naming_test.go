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

package naming

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"
)

func TestHash(t *testing.T) {
	testCases := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "same input hashes the same", a: "apps/Deployment/prod/app", b: "apps/Deployment/prod/app", want: true},
		{name: "namespace is part of the identity", a: "apps/Deployment/prod/app", b: "apps/Deployment/staging/app", want: false},
		{name: "api group is part of the identity", a: "apps/Widget/prod/app", b: "example.com/Widget/prod/app", want: false},
		{name: "separator placement matters", a: "apps/Deployment/prod/app", b: "apps/Deployment/prod-app/", want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotA, gotB := Hash(tc.a), Hash(tc.b)
			if (gotA == gotB) != tc.want {
				t.Errorf("Hash(%q) = %v, Hash(%q) = %v, want equal to be %v", tc.a, gotA, tc.b, gotB, tc.want)
			}
		})
	}
}

// TestHashShape pins the properties every generated name depends on: a fixed width, and a value
// made only of characters legal anywhere in a Kubernetes name.
func TestHashShape(t *testing.T) {
	for _, input := range []string{"", "app", strings.Repeat("x", 4096), "ünïcödé/名前"} {
		got := Hash(input)
		if len(got) != HashLength {
			t.Errorf("len(Hash(%q)) = %d, want %d", input, len(got), HashLength)
		}
		if strings.Trim(got, "0123456789abcdef") != "" {
			t.Errorf("Hash(%q) = %v, want only lowercase hexadecimal characters", input, got)
		}
	}
}

func TestTrimSeparators(t *testing.T) {
	testCases := []struct {
		name     string
		fragment string
		want     string
	}{
		{name: "nothing to trim", fragment: "app", want: "app"},
		{name: "trailing dash", fragment: "app-", want: "app"},
		{name: "trailing dot", fragment: "app.", want: "app"},
		{name: "trailing underscore", fragment: "app_", want: "app"},
		{name: "run of separators", fragment: "app-._-", want: "app"},
		{name: "leading separators are left alone", fragment: "-app", want: "-app"},
		{name: "separators in the middle are left alone", fragment: "my-app.v2", want: "my-app.v2"},
		{name: "entirely separators", fragment: "---", want: ""},
		{name: "empty", fragment: "", want: ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TrimSeparators(tc.fragment); got != tc.want {
				t.Errorf("TrimSeparators(%q) = %v, want %v", tc.fragment, got, tc.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	testCases := []struct {
		name      string
		s         string
		maxLength int
		want      string
	}{
		{name: "shorter than the limit", s: "app", maxLength: 10, want: "app"},
		{name: "exactly at the limit", s: "app", maxLength: 3, want: "app"},
		{name: "over the limit", s: "application", maxLength: 4, want: "appl"},
		// The cut landing on a separator is the case that produced an invalid generated name in
		// practice: the character after the cut would begin a new label.
		{name: "cut lands on a dash", s: "my-application", maxLength: 3, want: "my"},
		{name: "cut lands on a dot", s: "my.application", maxLength: 3, want: "my"},
		{name: "cut lands inside a run of separators", s: "my-._application", maxLength: 5, want: "my"},
		{name: "cut lands just past a run of separators", s: "my-._application", maxLength: 6, want: "my-._a"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Truncate(tc.s, tc.maxLength)
			if got != tc.want {
				t.Errorf("Truncate(%q, %d) = %v, want %v", tc.s, tc.maxLength, got, tc.want)
			}
			if len(got) > tc.maxLength {
				t.Errorf("len(Truncate(%q, %d)) = %d, want at most %d", tc.s, tc.maxLength, len(got), tc.maxLength)
			}
		})
	}
}

func TestLabelValue(t *testing.T) {
	testCases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "short value is kept as is", value: "app", want: "app"},
		{
			name:  "value at the limit is kept as is",
			value: strings.Repeat("a", validation.LabelValueMaxLength),
			want:  strings.Repeat("a", validation.LabelValueMaxLength),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LabelValue(tc.value); got != tc.want {
				t.Errorf("LabelValue(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestLabelValueShortening covers the values that do not fit, where the result is not a value the
// test can spell out but a set of properties it must hold: it has to be a legal label value, it
// has to stay within the limit, and it has to distinguish inputs that share a prefix.
func TestLabelValueShortening(t *testing.T) {
	long := strings.Repeat("a", validation.LabelValueMaxLength+1)
	testCases := []string{
		long,
		long + "b",
		// A value whose truncation point falls on a separator, which must not survive into the
		// shortened form.
		strings.Repeat("a", labelValuePrefixMaxLength-1) + "-" + strings.Repeat("b", 40),
		strings.Repeat("a", labelValuePrefixMaxLength-1) + "." + strings.Repeat("c", 40),
	}

	seen := make(map[string]string, len(testCases))
	for _, value := range testCases {
		got := LabelValue(value)
		if len(got) > validation.LabelValueMaxLength {
			t.Errorf("len(LabelValue(%q)) = %d, want at most %d", value, len(got), validation.LabelValueMaxLength)
		}
		if errs := validation.IsValidLabelValue(got); len(errs) > 0 {
			t.Errorf("LabelValue(%q) = %v, want a valid label value: %s", value, got, strings.Join(errs, "; "))
		}
		if previous, collided := seen[got]; collided {
			t.Errorf("LabelValue(%q) = %v, want a value distinct from that of %q", value, got, previous)
		}
		seen[got] = value
	}
}

// TestSanitize pins the transform that lets a name legal for its own API but not as a Kubernetes
// object name pass through into a generated name: every result must be legal as a DNS-1123 name
// segment, and the characters a name already allows must survive unchanged.
func TestSanitize(t *testing.T) {
	testCases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "already legal", value: "my-app", want: "my-app"},
		{name: "uppercase is lowercased", value: "MyApp", want: "myapp"},
		// The RBAC name that motivated the sanitizer: a colon is legal in the name but not in a
		// Kubernetes object name.
		{name: "rbac name with a colon", value: "system:aggregate-to-admin", want: "system-aggregate-to-admin"},
		{name: "underscores become dashes", value: "my_app", want: "my-app"},
		// A dot becomes a dash like any other separator: kept as a dot, an invalid character beside it
		// would leave a label bounded by a dash, which the API server rejects.
		{name: "dot becomes a dash", value: "my.app", want: "my-app"},
		{name: "dot next to an illegal character does not leave a bad label", value: "my.:app", want: "my--app"},
		{name: "adjacent dots do not leave an empty label", value: "a..b", want: "a--b"},
		{name: "leading separators are trimmed", value: ":::app", want: "app"},
		{name: "trailing separators are trimmed", value: "app:::", want: "app"},
		{name: "entirely illegal collapses to empty", value: ":::", want: ""},
		{name: "empty", value: "", want: ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Sanitize(tc.value)
			if got != tc.want {
				t.Errorf("Sanitize(%q) = %v, want %v", tc.value, got, tc.want)
			}
			if got != "" {
				if errs := validation.IsDNS1123Subdomain(got); len(errs) > 0 {
					t.Errorf("Sanitize(%q) = %v, want a valid DNS-1123 subdomain: %s", tc.value, got, strings.Join(errs, "; "))
				}
			}
		})
	}
}

// TestLabelValueSanitizes covers values that carry characters a label value forbids. The wider
// label-value character set keeps case and underscores that Sanitize would drop, but a colon still
// has to go, and the result must be a value the API server accepts.
func TestLabelValueSanitizes(t *testing.T) {
	testCases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "case and underscores are kept", value: "My_App", want: "My_App"},
		{name: "colon becomes a dash", value: "system:aggregate-to-admin", want: "system-aggregate-to-admin"},
		{name: "leading separators are trimmed", value: ":app", want: "app"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := LabelValue(tc.value)
			if got != tc.want {
				t.Errorf("LabelValue(%q) = %v, want %v", tc.value, got, tc.want)
			}
			if errs := validation.IsValidLabelValue(got); len(errs) > 0 {
				t.Errorf("LabelValue(%q) = %v, want a valid label value: %s", tc.value, got, strings.Join(errs, "; "))
			}
		})
	}
}

// TestTruncateNonPositiveBudget covers a caller whose own arithmetic left nothing for the value.
// The package exists to keep budget mistakes from reaching the API server; it must not turn one
// into a panic inside a reconcile loop either.
func TestTruncateNonPositiveBudget(t *testing.T) {
	for _, maxLength := range []int{0, -1, -64} {
		if got := Truncate("application", maxLength); got != "" {
			t.Errorf("Truncate(%q, %d) = %v, want the empty string", "application", maxLength, got)
		}
	}
}
