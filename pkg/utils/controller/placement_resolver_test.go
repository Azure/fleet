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
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
)

func TestFetchPlacementFromKey(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	tests := []struct {
		name          string
		placementKey  queue.PlacementKey
		objects       []client.Object
		wantPlacement fleetv1beta1.PlacementObj
		wantErr       bool
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
			wantPlacement: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
			},
			wantErr: false,
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
			wantPlacement: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rp",
					Namespace: "test-ns",
				},
			},
			wantErr: false,
		},
		{
			name:          "empty placement key",
			placementKey:  queue.PlacementKey(""),
			objects:       []client.Object{},
			wantPlacement: nil,
			wantErr:       true,
			expectedErr:   ErrUnexpectedBehavior,
		},
		{
			name:          "invalid placement key with multiple '/'",
			placementKey:  queue.PlacementKey("test-ns/test-rp/extra"),
			objects:       []client.Object{},
			wantPlacement: nil,
			wantErr:       true,
			expectedErr:   ErrUnexpectedBehavior,
		},
		{
			name:          "cluster resource placement not found",
			placementKey:  queue.PlacementKey("nonexistent-crp"),
			objects:       []client.Object{},
			wantPlacement: nil,
			wantErr:       true,
		},
		{
			name:          "namespaced resource placement not found",
			placementKey:  queue.PlacementKey("test-ns/nonexistent-rp"),
			objects:       []client.Object{},
			wantPlacement: nil,
			wantErr:       true,
		},
		{
			name:         "cluster resource placement with multiple objects",
			placementKey: queue.PlacementKey("target-crp"),
			objects: []client.Object{
				&fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-crp",
					},
				},
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-rp",
						Namespace: "other-ns",
					},
				},
				&fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "target-crp",
					},
				},
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "another-rp",
						Namespace: "another-ns",
					},
				},
			},
			wantPlacement: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-crp",
				},
			},
			wantErr: false,
		},
		{
			name:         "namespaced resource placement with multiple objects",
			placementKey: queue.PlacementKey("target-ns/target-rp"),
			objects: []client.Object{
				&fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "some-crp",
					},
				},
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-rp",
						Namespace: "other-ns",
					},
				},
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "target-rp",
						Namespace: "target-ns",
					},
				},
				&fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp",
					},
				},
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "target-rp",
						Namespace: "different-ns",
					},
				},
			},
			wantPlacement: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "target-rp",
					Namespace: "target-ns",
				},
			},
			wantErr: false,
		},
		{
			name:         "cluster resource placement with same-named namespace placement",
			placementKey: queue.PlacementKey("same-name"),
			objects: []client.Object{
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "same-name",
						Namespace: "some-ns",
					},
				},
				&fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "same-name",
					},
				},
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "same-name",
						Namespace: "other-ns",
					},
				},
			},
			wantPlacement: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "same-name",
				},
			},
			wantErr: false,
		},
		{
			name:         "namespaced placement with same-named cluster placement",
			placementKey: queue.PlacementKey("specific-ns/same-name"),
			objects: []client.Object{
				&fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "same-name",
					},
				},
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "same-name",
						Namespace: "other-ns",
					},
				},
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "same-name",
						Namespace: "specific-ns",
					},
				},
			},
			wantPlacement: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "same-name",
					Namespace: "specific-ns",
				},
			},
			wantErr: false,
		},
		{
			name:         "mixed setup - resolve cluster resource placement",
			placementKey: queue.PlacementKey("test-crp"),
			objects: []client.Object{
				&fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crp",
					},
				},
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rp",
						Namespace: "test-ns",
					},
				},
			},
			wantPlacement: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
			},
			wantErr: false,
		},
		{
			name:         "mixed setup - resolve namespaced resource placement",
			placementKey: queue.PlacementKey("test-ns/test-rp"),
			objects: []client.Object{
				&fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crp",
					},
				},
				&fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rp",
						Namespace: "test-ns",
					},
				},
			},
			wantPlacement: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rp",
					Namespace: "test-ns",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			placement, err := FetchPlacementFromKey(context.Background(), fakeClient, tt.placementKey)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				if tt.expectedErr != nil && !errors.Is(err, tt.expectedErr) {
					t.Fatalf("Expected error: %v, but got: %v", tt.expectedErr, err)
				}
				if placement != nil {
					t.Fatalf("Expected nil placement but got: %v", placement)
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected no error but got: %v", err)
			}

			if placement == nil {
				t.Fatalf("Expected placement but got nil")
			}

			// Use cmp.Diff to compare the actual result with expected placement
			// Ignore resource version field for consistent comparison
			if diff := cmp.Diff(placement, tt.wantPlacement,
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")); diff != "" {
				t.Errorf("FetchPlacementFromKey() diff (-got +want):\n%s", diff)
			}

			// Verify the concrete type matches expected
			switch tt.wantPlacement.(type) {
			case *fleetv1beta1.ClusterResourcePlacement:
				if _, ok := placement.(*fleetv1beta1.ClusterResourcePlacement); !ok {
					t.Errorf("Expected type ClusterResourcePlacement, but got: %T", placement)
				}
			case *fleetv1beta1.ResourcePlacement:
				if _, ok := placement.(*fleetv1beta1.ResourcePlacement); !ok {
					t.Errorf("Expected type ResourcePlacement, but got: %T", placement)
				}
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
			key := GetObjectKeyFromObj(tt.placement)
			if key != tt.wantKey {
				t.Errorf("GetPlacementKeyFromObj() = %v, want %v", key, tt.wantKey)
			}
		})
	}
}
func TestGetPlacementNameFromKey(t *testing.T) {
	tests := []struct {
		name              string
		placementKey      queue.PlacementKey
		expectedNamespace string
		expectedName      string
		expectedErr       error
	}{
		{
			name:              "cluster resource placement",
			placementKey:      queue.PlacementKey("test-crp"),
			expectedNamespace: "",
			expectedName:      "test-crp",
			expectedErr:       nil,
		},
		{
			name:              "namespaced resource placement",
			placementKey:      queue.PlacementKey("test-ns/test-rp"),
			expectedNamespace: "test-ns",
			expectedName:      "test-rp",
			expectedErr:       nil,
		},
		{
			name:         "empty placement key",
			placementKey: queue.PlacementKey(""),
			expectedErr:  ErrUnexpectedBehavior,
		},
		{
			name:         "invalid placement key with multiple separators",
			placementKey: queue.PlacementKey("test-ns/test-rp/extra"),
			expectedErr:  ErrUnexpectedBehavior,
		},
		{
			name:         "invalid placement key with two separators together",
			placementKey: queue.PlacementKey("test-ns//extra"),
			expectedErr:  ErrUnexpectedBehavior,
		},
		{
			name:         "invalid placement key with only separator",
			placementKey: queue.PlacementKey("/"),
			expectedErr:  ErrUnexpectedBehavior,
		},
		{
			name:         "invalid placement key starting with separator",
			placementKey: queue.PlacementKey("/test-rp"),
			expectedErr:  ErrUnexpectedBehavior,
		},
		{
			name:         "invalid placement key ending with separator",
			placementKey: queue.PlacementKey("test-ns/"),
			expectedErr:  ErrUnexpectedBehavior,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, name, err := ExtractNamespaceNameFromKey(tt.placementKey)

			if tt.expectedErr != nil {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				if !errors.Is(err, tt.expectedErr) {
					t.Fatalf("Expected error: %v, but got: %v", tt.expectedErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if namespace != tt.expectedNamespace {
				t.Errorf("Expected namespace: %s, got: %s", tt.expectedNamespace, namespace)
			}

			if name != tt.expectedName {
				t.Errorf("Expected name: %s, got: %s", tt.expectedName, name)
			}
		})
	}
}
