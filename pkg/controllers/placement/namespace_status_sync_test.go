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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
)

var (
	// Define comparison options for ignoring auto-generated and time-dependent fields.
	crpsCmpOpts = []cmp.Option{
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "UID", "CreationTimestamp", "Generation", "ManagedFields"),
		cmpopts.IgnoreFields(placementv1beta1.ClusterResourcePlacementStatus{}, "LastUpdatedTime"),
	}
)

func TestHandleNamespaceAccessibleCRP(t *testing.T) {
	testCases := []struct {
		name              string
		placementObjName  string
		existingObjects   []client.Object
		targetNamespace   string
		wantCRPSOperation bool
		wantCRPCondition  *metav1.Condition
		wantError         bool
	}{
		{
			name:             "successful sync - create new CRPS and set StatusSynced to True",
			placementObjName: "test-crp",
			existingObjects: []client.Object{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-crp",
						Generation: 1,
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
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Type:   "TestCondition",
								Status: metav1.ConditionTrue,
								Reason: "TestReason",
							},
						},
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace",
					},
				},
			},
			targetNamespace:   "test-namespace",
			wantCRPSOperation: true,
			wantCRPCondition: &metav1.Condition{
				Type:               string(placementv1beta1.ClusterResourcePlacementStatusSyncedConditionType),
				Status:             metav1.ConditionTrue,
				Reason:             "StatusSyncSucceeded",
				Message:            fmt.Sprintf(successfulCRPSMessageFmt, "test-namespace"),
				ObservedGeneration: 1,
			},
			wantError: false,
		},
		{
			name:             "successful sync - update existing CRPS and set StatusSynced to True",
			placementObjName: "test-crp",
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
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
								Status: metav1.ConditionFalse,
								Reason: "OldReason",
							},
						},
					},
				},
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-crp",
						Generation: 1,
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
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
								Status: metav1.ConditionTrue,
								Reason: "UpdatedReason",
							},
						},
					},
				},
			},
			targetNamespace:   "test-namespace",
			wantCRPSOperation: true,
			wantCRPCondition: &metav1.Condition{
				Type:               string(placementv1beta1.ClusterResourcePlacementStatusSyncedConditionType),
				Status:             metav1.ConditionTrue,
				Reason:             "StatusSyncSucceeded",
				Message:            fmt.Sprintf(successfulCRPSMessageFmt, "test-namespace"),
				ObservedGeneration: 1,
			},
			wantError: false,
		},
		{
			name:             "sync failure - missing namespace selector and set Scheduled condition to false",
			placementObjName: "test-crp-no-ns",
			existingObjects: []client.Object{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-crp-no-ns",
						Generation: 1,
					},
					Spec: placementv1beta1.PlacementSpec{
						StatusReportingScope: placementv1beta1.NamespaceAccessible,
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "rbac.authorization.k8s.io",
								Version: "v1",
								Kind:    "ClusterRole",
								Name:    "test-cluster-role",
							},
						},
					},
					Status: placementv1beta1.PlacementStatus{
						ObservedResourceIndex: "0",
					},
				},
			},
			targetNamespace:   "",
			wantCRPSOperation: false,
			wantCRPCondition: &metav1.Condition{
				Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
				Status:             metav1.ConditionFalse,
				Reason:             condition.InvalidResourceSelectorsReason,
				Message:            noNamespaceResourceSelectorMsg,
				ObservedGeneration: 1,
			},
			wantError: false,
		},
	}

	for _, tc := range testCases {
		scheme := statusSyncServiceScheme(t)
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				WithStatusSubresource(&placementv1beta1.ClusterResourcePlacement{}).
				Build()

			reconciler := &Reconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Get the CRP from the fake client to ensure it has proper metadata and when handleNamespaceAccessibleCRP is called CRP must already exist.
			gotCRP := &placementv1beta1.ClusterResourcePlacement{}
			if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: tc.placementObjName}, gotCRP); err != nil {
				t.Fatalf("Failed to get CRP from fake client: %v", err)
			}

			// Call handleNamespaceAccessibleCRP with the CRP from the client.
			err := reconciler.handleNamespaceAccessibleCRP(context.Background(), gotCRP)

			// Check if error expectation matches.
			if tc.wantError && err == nil {
				t.Fatal("Expected error but got none")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("handleNamespaceAccessibleCRP() failed: %v", err)
			}

			// Check ClusterResourcePlacementStatus creation/update using cmp.Diff.
			if tc.wantCRPSOperation && tc.targetNamespace != "" {
				gotCRPS := &placementv1beta1.ClusterResourcePlacementStatus{}
				getErr := fakeClient.Get(context.Background(), types.NamespacedName{
					Name:      tc.placementObjName,
					Namespace: tc.targetNamespace,
				}, gotCRPS)

				if getErr != nil {
					if !tc.wantError {
						t.Fatalf("Expected ClusterResourcePlacementStatus to be created/updated but got error: %v", getErr)
					}
				} else {
					// Construct expected CRPS based on the CRP.
					wantCRPS := &placementv1beta1.ClusterResourcePlacementStatus{
						ObjectMeta: metav1.ObjectMeta{
							Name:      tc.placementObjName,
							Namespace: tc.targetNamespace,
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion:         "placement.kubernetes-fleet.io/v1beta1",
									Kind:               "ClusterResourcePlacement",
									Name:               tc.placementObjName,
									Controller:         ptr.To(true),
									BlockOwnerDeletion: ptr.To(true),
								},
							},
						},
						PlacementStatus: gotCRP.Status,
					}

					// Filter out StatusSynced condition from the expected CRPS.
					filteredConditions := make([]metav1.Condition, 0)
					for _, cond := range gotCRP.Status.Conditions {
						if cond.Type != string(placementv1beta1.ClusterResourcePlacementStatusSyncedConditionType) {
							filteredConditions = append(filteredConditions, cond)
						}
					}
					wantCRPS.PlacementStatus.Conditions = filteredConditions

					// Use cmp.Diff to compare CRPS objects.
					if diff := cmp.Diff(wantCRPS, gotCRPS, crpsCmpOpts...); diff != "" {
						t.Fatalf("ClusterResourcePlacementStatus mismatch (-want +got):\n%s", diff)
					}

					// Additional validation that LastUpdatedTime is set (ignored in comparison).
					if gotCRPS.LastUpdatedTime.IsZero() {
						t.Fatal("Expected LastUpdatedTime to be set on CRPS")
					}
				}
			} else {
				// Ensure no CRPS exists in any namespace.
				crpsList := &placementv1beta1.ClusterResourcePlacementStatusList{}
				if err := fakeClient.List(context.Background(), crpsList); err != nil {
					t.Fatalf("Failed to list ClusterResourcePlacementStatus: %v", err)
				}
				if len(crpsList.Items) != 0 {
					t.Fatalf("Expected no ClusterResourcePlacementStatus to exist, but found %d", len(crpsList.Items))
				}
			}

			verifyCRPCondition(t, fakeClient, tc.placementObjName, tc.wantCRPCondition)
		})
	}
}

func TestValidateNamespaceSelectorConsistency(t *testing.T) {
	testCases := []struct {
		name             string
		placementObjName string
		existingObjects  []client.Object
		wantCRPCondition *metav1.Condition
		wantIsValid      bool
		wantError        bool
	}{
		{
			name:             "no namespace selector - should set Scheduled to false and return false",
			placementObjName: "test-crp-no-selector",
			existingObjects: []client.Object{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-crp-no-selector",
						Generation: 1,
					},
					Spec: placementv1beta1.PlacementSpec{
						StatusReportingScope: placementv1beta1.NamespaceAccessible,
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "rbac.authorization.k8s.io",
								Version: "v1",
								Kind:    "ClusterRole",
								Name:    "test-cluster-role",
							},
						},
					},
					Status: placementv1beta1.PlacementStatus{
						ObservedResourceIndex: "0",
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							{
								Group:     "",
								Version:   "v1",
								Kind:      "Namespace",
								Name:      "test-namespace",
								Namespace: "",
							},
						},
					},
				},
			},
			wantCRPCondition: &metav1.Condition{
				Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
				Status:             metav1.ConditionFalse,
				Reason:             condition.InvalidResourceSelectorsReason,
				Message:            noNamespaceResourceSelectorMsg,
				ObservedGeneration: 1,
			},
			wantIsValid: false,
			wantError:   false,
		},
		{
			name:             "empty selected resources status - should pass validation and return true",
			placementObjName: "test-crp-empty-selected",
			existingObjects: []client.Object{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-crp-empty-selected",
						Generation: 1,
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
						ObservedResourceIndex: "0",
						SelectedResources:     []placementv1beta1.ResourceIdentifier{}, // Empty selected resources.
					},
				},
			},
			wantCRPCondition: nil, // No condition should be set
			wantIsValid:      true,
			wantError:        false,
		},
		{
			name:             "consistent namespace selector - should pass validation and return true",
			placementObjName: "test-crp-consistent",
			existingObjects: []client.Object{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-crp-consistent",
						Generation: 1,
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
						ObservedResourceIndex: "0",
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							{
								Group:     "",
								Version:   "v1",
								Kind:      "Namespace",
								Name:      "test-namespace",
								Namespace: "",
							},
						},
					},
				},
			},
			wantCRPCondition: nil, // No condition should be set.
			wantIsValid:      true,
			wantError:        false,
		},
		{
			name:             "inconsistent namespace selector - should set Scheduled to false and return false",
			placementObjName: "test-crp-inconsistent",
			existingObjects: []client.Object{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-crp-inconsistent",
						Generation: 2,
					},
					Spec: placementv1beta1.PlacementSpec{
						StatusReportingScope: placementv1beta1.NamespaceAccessible,
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "",
								Version: "v1",
								Kind:    "Namespace",
								Name:    "new-namespace", // Changed from original.
							},
						},
					},
					Status: placementv1beta1.PlacementStatus{
						ObservedResourceIndex: "0",
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							{
								Group:     "",
								Version:   "v1",
								Kind:      "Namespace",
								Name:      "original-namespace", // Original namespace in status.
								Namespace: "",
							},
						},
					},
				},
			},
			wantCRPCondition: &metav1.Condition{
				Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
				Status:             metav1.ConditionFalse,
				Reason:             condition.InvalidResourceSelectorsReason,
				Message:            fmt.Sprintf(namespaceConsistencyMessageFmt, "new-namespace", "original-namespace"),
				ObservedGeneration: 2,
			},
			wantIsValid: false,
			wantError:   false,
		},
	}

	for _, tc := range testCases {
		scheme := statusSyncServiceScheme(t)
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				WithStatusSubresource(&placementv1beta1.ClusterResourcePlacement{}).
				Build()

			reconciler := &Reconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Get the CRP from the fake client to ensure it has proper metadata and when validateNamespaceSelectorConsistency is called CRP must already exist.
			gotCRP := &placementv1beta1.ClusterResourcePlacement{}
			if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: tc.placementObjName}, gotCRP); err != nil {
				t.Fatalf("Failed to get CRP from fake client: %v", err)
			}

			// Call validateNamespaceSelectorConsistency.
			gotIsValid, err := reconciler.validateNamespaceSelectorConsistency(context.Background(), gotCRP)

			// Check if error expectation matches.
			if tc.wantError && err == nil {
				t.Fatal("Expected error but got none")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("validateNamespaceSelectorConsistency() failed: %v", err)
			}

			// Check if validity expectation matches.
			if tc.wantIsValid != gotIsValid {
				t.Fatalf("validateNamespaceSelectorConsistency() validity mismatch: want %v, got %v", tc.wantIsValid, gotIsValid)
			}

			verifyCRPCondition(t, fakeClient, tc.placementObjName, tc.wantCRPCondition)
		})
	}
}

func statusSyncServiceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add client go scheme: %v", err)
	}
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return scheme
}

func verifyCRPCondition(t *testing.T, fakeClient client.WithWatch, placementName string, wantCondition *metav1.Condition) {
	updatedCRP := &placementv1beta1.ClusterResourcePlacement{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: placementName}, updatedCRP); err != nil {
		t.Fatalf("Failed to get updated CRP: %v", err)
	}

	var gotCondition *metav1.Condition
	if wantCondition == nil {
		gotCondition = nil
	} else {
		switch wantCondition.Type {
		case string(placementv1beta1.ClusterResourcePlacementStatusSyncedConditionType):
			gotCondition = updatedCRP.GetCondition(string(placementv1beta1.ClusterResourcePlacementStatusSyncedConditionType))
		case string(placementv1beta1.ClusterResourcePlacementScheduledConditionType):
			gotCondition = updatedCRP.GetCondition(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))
		}
	}

	if diff := cmp.Diff(wantCondition, gotCondition, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")); diff != "" {
		t.Fatalf("Namespace Accessible condition mismatch (-want +got):\n%s", diff)
	}
}
