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

package resource

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHashOf(t *testing.T) {
	testCases := []struct {
		name  string
		input any
	}{
		{
			name: "resource snapshot spec",
			input: &placementv1beta1.ResourceSnapshotSpec{
				SelectedResources: []placementv1beta1.ResourceContent{},
			},
		},
		{
			name:  "nil resource",
			input: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := HashOf(tc.input)
			if err != nil {
				t.Fatalf("HashOf() got error %v, want nil", err)
			}
			if len(got) == 0 {
				t.Errorf("HashOf() got empty, want not empty")
			}
		})
	}
}

// TestCalculateSizeDeltaOverLimitFor tests the CalculateSizeDeltaOverLimitFor function.
func TestCalculateSizeDeltaOverLimitFor(t *testing.T) {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app",
			Namespace: "default",
		},
		Data: map[string]string{
			"key": "value",
		},
	}
	cmBytes, err := json.Marshal(cm)
	if err != nil {
		t.Fatalf("Failed to marshal configMap")
	}
	cmSizeBytes := len(cmBytes)

	testCases := []struct {
		name           string
		sizeLimitBytes int
		wantErred      bool
	}{
		{
			name:           "under size limit (negative delta)",
			sizeLimitBytes: 10000,
		},
		{
			name:           "over size limit (positive delta)",
			sizeLimitBytes: 1,
		},
		{
			name: "invalid size limit (negative size limit)",
			// Invalid size limit.
			sizeLimitBytes: -1,
			wantErred:      true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sizeDeltaBytes, err := CalculateSizeDeltaOverLimitFor(cm, tc.sizeLimitBytes)

			if tc.wantErred {
				if err == nil {
					t.Fatalf("CalculateSizeDeltaOverLimitFor() error = nil, want erred")
				}
				return
			}
			// Note: this test spec uses runtime calculation rather than static values for expected
			// size delta comparison as different platforms have slight differences in the serialization process,
			// which may produce different sizing results.
			if !cmp.Equal(sizeDeltaBytes, cmSizeBytes-tc.sizeLimitBytes) {
				t.Errorf("CalculateSizeDeltaOverLimitFor() = %d, want %d", sizeDeltaBytes, cmSizeBytes-tc.sizeLimitBytes)
			}
		})
	}
}
