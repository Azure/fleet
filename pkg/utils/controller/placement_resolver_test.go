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

package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
)

func TestResolvePlacementFromKey(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	tests := []struct {
		name          string
		placementKey  queue.PlacementKey
		objects       []client.Object
		expectedType  string
		expectedName  string
		expectedNS    string
		expectCluster bool
		expectedErr   error
	}{
		{
			name:         "cluster resource placement",
			placementKey: queue.PlacementKey("test-crp"),
			objects: []client.Object{
				&fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crp",
					},
				},
			},
			expectedType:  "*v1beta1.ClusterResourcePlacement",
			expectedName:  "test-crp",
			expectedNS:    "",
			expectCluster: true,
			expectedErr:   nil,
		},
		{
			name:         "namespaced resource placement",
			placementKey: queue.PlacementKey("test-ns/test-rp"),
			objects: []client.Object{
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rp",
						Namespace: "test-ns",
					},
				},
			},
			expectedType:  "*v1beta1.ResourcePlacement",
			expectedName:  "test-rp",
			expectedNS:    "test-ns",
			expectCluster: false,
			expectedErr:   nil,
		},
		{
			name:         "empty placement key",
			placementKey: queue.PlacementKey(""),
			objects:      []client.Object{},
			expectedErr:  ErrUnexpectedBehavior,
		},
		{
			name:         "invalid placement key with multiple '/'",
			placementKey: queue.PlacementKey("test-ns/test-rp/extra"),
			objects:      []client.Object{},
			expectedErr:  ErrUnexpectedBehavior,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			placement, err := FetchPlacementFromKey(context.Background(), fakeClient, tt.placementKey)

			if tt.expectedErr != nil {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				if !errors.Is(err, tt.expectedErr) {
					t.Fatalf("Expected error: %v, but got: %v", tt.expectedErr, err)
				}
				return
			}

			if tt.expectedErr != nil {
				if placement != nil {
					t.Fatalf("Expected nil placement but got: %v", placement)
				}
				return
			}

			if placement == nil {
				t.Fatalf("Expected placement but got nil")
			}

			// Determine if this is a cluster-scoped placement based on namespace
			isCluster := placement.GetNamespace() == ""
			if isCluster != tt.expectCluster {
				t.Errorf("Expected cluster-scoped: %v, got: %v", tt.expectCluster, isCluster)
			}

			if diff := cmp.Diff(tt.expectedName, placement.GetName()); diff != "" {
				t.Errorf("Name mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.expectedNS, placement.GetNamespace()); diff != "" {
				t.Errorf("Namespace mismatch (-want +got):\n%s", diff)
			}

			// Check the concrete type
			switch tt.expectedType {
			case "*v1beta1.ClusterResourcePlacement":
				if _, ok := placement.(*fleetv1beta1.ClusterResourcePlacement); !ok {
					t.Errorf("Expected type ClusterResourcePlacement, but got: %T", placement)
				}
			case "*v1beta1.ResourcePlacement":
				if _, ok := placement.(*fleetv1beta1.ResourcePlacement); !ok {
					t.Errorf("Expected type ResourcePlacement, but got: %T", placement)
				}
			default:
				t.Errorf("Unexpected expectedType: %s", tt.expectedType)
			}
		})
	}
}

func TestGetPlacementKeyFromObj(t *testing.T) {
	tests := []struct {
		name      string
		placement fleetv1beta1.PlacementObj
		wantKey   queue.PlacementKey
	}{
		{
			name: "cluster resource placement",
			placement: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
			},
			wantKey: queue.PlacementKey("test-crp"),
		},
		{
			name: "namespaced resource placement",
			placement: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rp",
					Namespace: "test-ns",
				},
			},
			wantKey: queue.PlacementKey("test-ns/test-rp"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := GetPlacementKeyFromObj(tt.placement)
			if key != tt.wantKey {
				t.Errorf("GetPlacementKeyFromObj() = %v, want %v", key, tt.wantKey)
			}
		})
	}
}
