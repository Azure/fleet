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

package workapplier

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

// TestShouldCleanUpManifest tests the shouldCleanUpManifest function.
func TestShouldCleanUpManifest(t *testing.T) {
	identifier := placementv1alpha1.ManifestIdentifier{
		Ordinal:    0,
		APIGroup:   "",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Resource:   "configmaps",
		Name:       "configmap-1",
		Namespace:  "ns-1",
	}
	appliedCond := func(status metav1.ConditionStatus, reason string) []metav1.Condition {
		return []metav1.Condition{
			{
				Type:   placementv1alpha1.WorkCondTypeApplied,
				Status: status,
				Reason: reason,
			},
		}
	}

	// Any manifest that can be identified and has been processed should be considered for cleanup, regardless of
	// its apply result.
	for _, applyResType := range allApplyResTypes.UnsortedList() {
		t.Run(string(applyResType), func(t *testing.T) {
			// The Applied condition is set to True only for successful apply ops.
			status := metav1.ConditionFalse
			if applyResType == ApplyResTypeApplied || applyResType == ApplyResTypeAppliedWithFailedDriftDetection {
				status = metav1.ConditionTrue
			}
			manifestStatus := &placementv1alpha1.PerManifestStatus{
				Identifier: identifier,
				Conditions: appliedCond(status, string(applyResType)),
			}
			want := true
			if got := shouldCleanUpManifest(manifestStatus); got != want {
				t.Errorf("shouldCleanUpManifest(%v) = %v, want %v", manifestStatus, got, want)
			}
		})
	}

	testCases := []struct {
		name           string
		manifestStatus *placementv1alpha1.PerManifestStatus
		want           bool
	}{
		{
			name: "no applied condition",
			manifestStatus: &placementv1alpha1.PerManifestStatus{
				Identifier: identifier,
				Conditions: []metav1.Condition{},
			},
			want: false,
		},
		{
			name: "unknown apply result type",
			manifestStatus: &placementv1alpha1.PerManifestStatus{
				Identifier: identifier,
				Conditions: appliedCond(metav1.ConditionFalse, "UnknownApplyResultType"),
			},
			want: true,
		},
		{
			name: "identifier without resource",
			manifestStatus: &placementv1alpha1.PerManifestStatus{
				Identifier: placementv1alpha1.ManifestIdentifier{
					Ordinal:    0,
					APIGroup:   "malformed",
					APIVersion: "v10",
					Kind:       "Unknown",
					Name:       "configmap-1",
					Namespace:  "ns-1",
				},
				Conditions: appliedCond(metav1.ConditionFalse, string(ApplyResTypeDecodingErred)),
			},
			want: false,
		},
		{
			name: "identifier without name",
			manifestStatus: &placementv1alpha1.PerManifestStatus{
				Identifier: placementv1alpha1.ManifestIdentifier{
					Ordinal:    0,
					APIGroup:   "",
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Resource:   "configmaps",
					Namespace:  "ns-1",
				},
				Conditions: appliedCond(metav1.ConditionTrue, string(ApplyResTypeApplied)),
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldCleanUpManifest(tc.manifestStatus); got != tc.want {
				t.Errorf("shouldCleanUpManifest(%v) = %v, want %v", tc.manifestStatus, got, tc.want)
			}
		})
	}
}
