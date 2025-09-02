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

package placement

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

var (
	// Define comparison options for ignoring auto-generated and time-dependent fields.
	crpsCmpOpts = []cmp.Option{
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "UID", "CreationTimestamp", "Generation", "ManagedFields"),
		cmpopts.IgnoreFields(placementv1beta1.ClusterResourcePlacementStatus{}, "LastUpdatedTime"),
	}
)

func TestExtractNamespaceFromResourceSelectors(t *testing.T) {
	testCases := []struct {
		name      string
		placement placementv1beta1.ClusterResourcePlacement
		want      string
	}{
		{
			name: "NamespaceAccessible with namespace selector",
			placement: placementv1beta1.ClusterResourcePlacement{
				Spec: placementv1beta1.PlacementSpec{
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
				},
			},
			want: "test-namespace",
		},
		{
			name: "ClusterScopeOnly should return empty",
			placement: placementv1beta1.ClusterResourcePlacement{
				Spec: placementv1beta1.PlacementSpec{
					StatusReportingScope: placementv1beta1.ClusterScopeOnly,
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
				},
			},
			want: "",
		},
		{
			name: "StatusReportingScope is not specified, should return empty",
			placement: placementv1beta1.ClusterResourcePlacement{
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
				},
			},
			want: "",
		},
		{
			name: "NamespaceAccessible without namespace selector", // CEL validation should prevent this case.
			placement: placementv1beta1.ClusterResourcePlacement{
				Spec: placementv1beta1.PlacementSpec{
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "apps",
							Version: "v1",
							Kind:    "Deployment",
							Name:    "test-deployment",
						},
					},
				},
			},
			want: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractNamespaceFromResourceSelectors(&tc.placement)
			if got != tc.want {
				t.Errorf("extractNamespaceFromResourceSelectors() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSyncClusterResourcePlacementStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add client go scheme: %v", err)
	}
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add fleet scheme: %v", err)
	}

	testCases := []struct {
		name            string
		placementObj    placementv1beta1.PlacementObj
		existingObjects []client.Object
		expectOperation bool
		targetNamespace string
	}{
		{
			name: "Create new ClusterResourcePlacementStatus",
			placementObj: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.PlacementSpec{
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
				},
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "test-index",
					Conditions: []metav1.Condition{
						{
							Type:   "TestCondition",
							Status: metav1.ConditionTrue,
							Reason: "TestReason",
						},
					},
				},
			},
			existingObjects: []client.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace",
					},
				},
			},
			expectOperation: true,
			targetNamespace: "test-namespace",
		},
		{
			name: "Update existing ClusterResourcePlacementStatus",
			placementObj: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.PlacementSpec{
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
				},
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "updated-index",
					Conditions: []metav1.Condition{
						{
							Type:   "UpdatedCondition",
							Status: metav1.ConditionTrue,
							Reason: "UpdatedReason",
						},
					},
				},
			},
			existingObjects: []client.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace",
					},
				},
				&placementv1beta1.ClusterResourcePlacementStatus{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-crp",
						Namespace: "test-namespace",
					},
					PlacementStatus: placementv1beta1.PlacementStatus{
						ObservedResourceIndex: "old-index",
					},
					LastUpdatedTime: metav1.Now(),
				},
			},
			expectOperation: true,
			targetNamespace: "test-namespace",
		},
		{
			name: "ClusterScopeOnly should not sync",
			placementObj: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.PlacementSpec{
					StatusReportingScope: placementv1beta1.ClusterScopeOnly,
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
				},
			},
			existingObjects: []client.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace",
					},
				},
			},
			expectOperation: false,
			targetNamespace: "test-namespace",
		},
		{
			name: "ResourcePlacement should not sync",
			placementObj: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rp",
					Namespace: "test-namespace",
				},
				Spec: placementv1beta1.PlacementSpec{
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
				},
			},
			existingObjects: []client.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace",
					},
				},
			},
			expectOperation: false,
			targetNamespace: "test-namespace",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				Build()

			reconciler := &Reconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			err := reconciler.syncClusterResourcePlacementStatus(context.Background(), tc.placementObj)
			if err != nil {
				t.Fatalf("syncClusterResourcePlacementStatus() failed: %v", err)
			}

			crpStatus := &placementv1beta1.ClusterResourcePlacementStatus{}
			err = fakeClient.Get(context.Background(), types.NamespacedName{Name: tc.placementObj.GetName(), Namespace: tc.targetNamespace}, crpStatus)

			if !tc.expectOperation {
				if err == nil {
					t.Fatal("Expected no ClusterResourcePlacementStatus to be present, but one exists")
				}
				// CRPS should not exist.
				if k8serrors.IsNotFound(err) {
					return
				}
			}

			if err != nil {
				t.Fatalf("expected ClusterResourcePlacementStatus to exist but got error: %v", err)
			}

			// Verify LastUpdatedTime is set.
			if crpStatus.LastUpdatedTime.IsZero() {
				t.Fatal("Expected LastUpdatedTime to be set, but it was zero")
			}

			// Verify the ClusterResourcePlacementStatus exists.
			crp, ok := tc.placementObj.(*placementv1beta1.ClusterResourcePlacement)
			if !ok {
				return // ResourcePlacement case.
			}

			// Use cmp.Diff to compare the key fields.
			wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crp.Name,
					Namespace: tc.targetNamespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "placement.kubernetes-fleet.io/v1beta1",
							Kind:               "ClusterResourcePlacement",
							Name:               crp.Name,
							UID:                crp.UID,
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				PlacementStatus: crp.Status,
			}

			// Ignore metadata fields that Kubernetes sets automatically and LastUpdatedTime since it's time-dependent.
			if diff := cmp.Diff(wantStatus, *crpStatus, crpsCmpOpts...); diff != "" {
				t.Fatalf("ClusterResourcePlacementStatus mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
