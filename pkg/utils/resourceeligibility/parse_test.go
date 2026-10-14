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

package resourceeligibility

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestParseOneGVK tests the ParseOneGVK function.
func TestParseOneGVK(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		wantGVKs []schema.GroupVersionKind
		wantErr  bool
	}{
		{
			name:     "group only",
			input:    "apps",
			wantGVKs: []schema.GroupVersionKind{{Group: "apps"}},
		},
		{
			name:     "group and version",
			input:    "apps/v1",
			wantGVKs: []schema.GroupVersionKind{{Group: "apps", Version: "v1"}},
		},
		{
			name:     "group, version, and kind",
			input:    "apps/v1/Deployment",
			wantGVKs: []schema.GroupVersionKind{{Group: "apps", Version: "v1", Kind: "Deployment"}},
		},
		{
			name:  "group, version, and multiple kinds",
			input: "apps/v1/Deployment,StatefulSet",
			wantGVKs: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "StatefulSet"},
			},
		},
		{
			name:     "core API group, version only",
			input:    "v1",
			wantGVKs: []schema.GroupVersionKind{{Version: "v1"}},
		},
		{
			name:     "core API group, version and kind",
			input:    "v1/ConfigMap",
			wantGVKs: []schema.GroupVersionKind{{Version: "v1", Kind: "ConfigMap"}},
		},
		{
			name:  "core API group, version and multiple kinds",
			input: "v1/ConfigMap,Secret",
			wantGVKs: []schema.GroupVersionKind{
				{Version: "v1", Kind: "ConfigMap"},
				{Version: "v1", Kind: "Secret"},
			},
		},
		{
			name:     "core API group, empty kind",
			input:    "v1/",
			wantGVKs: []schema.GroupVersionKind{{Version: "v1"}},
		},
		{
			name:    "core API group, too many segments",
			input:   "v1/ConfigMap/extra",
			wantErr: true,
		},
		{
			name:    "core API group, multiple kinds with a leading empty kind",
			input:   "v1/,ConfigMap",
			wantErr: true,
		},
		{
			name:    "core API group, multiple kinds with a trailing empty kind",
			input:   "v1/ConfigMap,",
			wantErr: true,
		},
		{
			name:    "core API group, multiple kinds with an empty kind in between",
			input:   "v1/ConfigMap,,Secret",
			wantErr: true,
		},
		{
			// A whitespace-only kind must not be trimmed into an empty kind, which would act as a wildcard.
			name:    "core API group, multiple kinds with a whitespace-only kind in between",
			input:   "v1/ConfigMap, ,Secret",
			wantErr: true,
		},
		{
			name:    "core API group, multiple kinds that are all empty",
			input:   "v1/,",
			wantErr: true,
		},
		{
			name:     "wildcard version and kind",
			input:    "apps/*/*",
			wantGVKs: []schema.GroupVersionKind{{Group: "apps", Version: wildcard, Kind: wildcard}},
		},
		{
			name:     "empty version segment",
			input:    "apps//Deployment",
			wantGVKs: []schema.GroupVersionKind{{Group: "apps", Kind: "Deployment"}},
		},
		{
			name:     "empty kind segment",
			input:    "apps/v1/",
			wantGVKs: []schema.GroupVersionKind{{Group: "apps", Version: "v1"}},
		},
		{
			name:     "whitespace-only kind segment",
			input:    "apps/v1/ ",
			wantGVKs: []schema.GroupVersionKind{{Group: "apps", Version: "v1"}},
		},
		{
			name:    "multiple kinds with a leading empty kind",
			input:   "apps/v1/,Deployment",
			wantErr: true,
		},
		{
			name:    "multiple kinds with a trailing empty kind",
			input:   "apps/v1/Deployment,",
			wantErr: true,
		},
		{
			name:    "multiple kinds with an empty kind in between",
			input:   "apps/v1/Deployment,,StatefulSet",
			wantErr: true,
		},
		{
			// A whitespace-only kind must not be trimmed into an empty kind, which would act as a wildcard.
			name:    "multiple kinds with a whitespace-only kind in between",
			input:   "apps/v1/Deployment, ,StatefulSet",
			wantErr: true,
		},
		{
			name:    "multiple kinds that are all empty",
			input:   "apps/v1/,",
			wantErr: true,
		},
		{
			name:    "empty version segment, multiple kinds with an empty kind",
			input:   "apps//Deployment,",
			wantErr: true,
		},
		{
			name:    "wildcard API group",
			input:   "*",
			wantErr: true,
		},
		{
			name:    "wildcard API group with a version",
			input:   "*/v1",
			wantErr: true,
		},
		{
			name:    "wildcard API group with a version and a kind",
			input:   "*/v1/Deployment",
			wantErr: true,
		},
		{
			name:     "group with dots",
			input:    "networking.k8s.io/v1/Ingress",
			wantGVKs: []schema.GroupVersionKind{{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}},
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "empty group segment",
			input:   "/v1/Deployment",
			wantErr: true,
		},
		{
			name:    "too many segments",
			input:   "apps/v1/Deployment/extra",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotGVKs, gotErr := ParseOneGVK(tc.input)
			if (gotErr != nil) != tc.wantErr {
				t.Fatalf("ParseOneGVK(%s) error = %v, wantErr %t", tc.input, gotErr, tc.wantErr)
			}
			if diff := cmp.Diff(gotGVKs, tc.wantGVKs); diff != "" {
				t.Errorf("ParseOneGVK(%s) mismatch (-got, +want):\n%s", tc.input, diff)
			}
		})
	}
}

// TestParseGVKs tests the ParseGVKs function.
func TestParseGVKs(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		wantGVKs []schema.GroupVersionKind
		wantErr  bool
	}{
		{
			name:     "single entry",
			input:    "apps/v1/Deployment",
			wantGVKs: []schema.GroupVersionKind{{Group: "apps", Version: "v1", Kind: "Deployment"}},
		},
		{
			name:  "multiple entries of mixed forms",
			input: "apps/v1/Deployment;v1/ConfigMap;batch/v1;networking.k8s.io",
			wantGVKs: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Version: "v1", Kind: "ConfigMap"},
				{Group: "batch", Version: "v1"},
				{Group: "networking.k8s.io"},
			},
		},
		{
			name:  "multiple kinds in entries",
			input: "apps/v1/Deployment,StatefulSet;v1/ConfigMap,Secret",
			wantGVKs: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "StatefulSet"},
				{Version: "v1", Kind: "ConfigMap"},
				{Version: "v1", Kind: "Secret"},
			},
		},
		{
			name:  "duplicate entries",
			input: "apps/v1/Deployment;apps/v1/Deployment",
			wantGVKs: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "Deployment"},
			},
		},
		{
			name:  "empty input",
			input: "",
		},
		{
			name:  "whitespace-only input",
			input: "   ",
		},
		{
			// The flag help text documents entries separated by "; ".
			name:  "surrounding whitespace on entries",
			input: " networking.k8s.io/v1beta1/Ingress,IngressClass; v1/ConfigMap ",
			wantGVKs: []schema.GroupVersionKind{
				{Group: "networking.k8s.io", Version: "v1beta1", Kind: "Ingress"},
				{Group: "networking.k8s.io", Version: "v1beta1", Kind: "IngressClass"},
				{Version: "v1", Kind: "ConfigMap"},
			},
		},
		{
			name:    "whitespace-only entry in between",
			input:   "apps/v1; ;batch/v1",
			wantErr: true,
		},
		{
			name:  "surrounding whitespace on segments and kinds",
			input: "apps / v1 / Deployment , StatefulSet ; v1 / ConfigMap",
			wantGVKs: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "StatefulSet"},
				{Version: "v1", Kind: "ConfigMap"},
			},
		},
		{
			name:    "empty entry in between",
			input:   "apps/v1;;batch/v1",
			wantErr: true,
		},
		{
			name:    "trailing separator",
			input:   "apps/v1;",
			wantErr: true,
		},
		{
			name:    "one invalid entry",
			input:   "apps/v1;apps/v1/Deployment/extra",
			wantErr: true,
		},
		{
			name:    "one entry with an empty kind among multiple kinds",
			input:   "apps/v1/Deployment,;v1/ConfigMap",
			wantErr: true,
		},
		{
			name:    "one core API group entry with a whitespace-only kind among multiple kinds",
			input:   "apps/v1/Deployment; v1/ConfigMap, ,Secret",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotGVKs, gotErr := ParseGVKs(tc.input)
			if (gotErr != nil) != tc.wantErr {
				t.Fatalf("ParseGVKs(%s) error = %v, wantErr %t", tc.input, gotErr, tc.wantErr)
			}
			if diff := cmp.Diff(gotGVKs, tc.wantGVKs); diff != "" {
				t.Errorf("ParseGVKs(%s) mismatch (-got, +want):\n%s", tc.input, diff)
			}
		})
	}
}
