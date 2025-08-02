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

package approve

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func TestRun(t *testing.T) {
	wantCondition := metav1.Condition{
		Type:    string(placementv1beta1.ApprovalRequestConditionApproved),
		Status:  metav1.ConditionTrue,
		Reason:  "ClusterApprovalRequestApproved",
		Message: "ClusterApprovalRequest has been approved",
	}

	tests := []struct {
		name                       string
		kind                       string
		requestName                string
		existingClusterApprovalReq *placementv1beta1.ClusterApprovalRequest
		wantCondition              *metav1.Condition
		wantErr                    bool
		wantErrMsg                 string
	}{
		{
			name:        "empty kind should fail",
			kind:        "",
			requestName: "test-name",
			wantErr:     true,
			wantErrMsg:  "resource kind is required",
		},
		{
			name:        "empty name should fail",
			kind:        "clusterapprovalrequest",
			requestName: "",
			wantErr:     true,
			wantErrMsg:  "resource name is required",
		},
		{
			name:        "unsupported kind should fail",
			kind:        "unsupported",
			requestName: "test-name",
			wantErr:     true,
			wantErrMsg:  "unsupported resource kind \"unsupported\", only 'clusterapprovalrequest' is supported",
		},
		{
			name:        "successfully approve ClusterApprovalRequest",
			kind:        "clusterapprovalrequest",
			requestName: "test-approval",
			existingClusterApprovalReq: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-approval",
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test-update-run",
					TargetStage:     "test-stage",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{},
				},
			},
			wantCondition: &wantCondition,
			wantErr:       false,
		},
		{
			name:        "approve ClusterApprovalRequest with existing conditions",
			kind:        "clusterapprovalrequest",
			requestName: "test-approval-existing",
			existingClusterApprovalReq: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-approval-existing",
					Generation: 2,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test-update-run",
					TargetStage:     "test-stage",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "SomeOtherCondition",
							Status: metav1.ConditionTrue,
							Reason: "SomeReason",
						},
					},
				},
			},
			wantCondition: &wantCondition,
			wantErr:       false,
		},
		{
			name:        "update existing Approved condition",
			kind:        "clusterapprovalrequest",
			requestName: "test-approval-update",
			existingClusterApprovalReq: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-approval-update",
					Generation: 3,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test-update-run",
					TargetStage:     "test-stage",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Type:    string(placementv1beta1.ApprovalRequestConditionApproved),
							Status:  metav1.ConditionFalse,
							Reason:  "OldReason",
							Message: "Old message",
						},
					},
				},
			},
			wantCondition: &wantCondition,
			wantErr:       false,
		},
		{
			name:                       "ClusterApprovalRequest not found",
			kind:                       "clusterapprovalrequest",
			requestName:                "non-existent-approval",
			existingClusterApprovalReq: nil,
			wantErr:                    true,
			wantErrMsg:                 "not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := setupScheme(t)
			var objects []client.Object
			if tc.existingClusterApprovalReq != nil {
				objects = append(objects, tc.existingClusterApprovalReq)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(&placementv1beta1.ClusterApprovalRequest{}).
				Build()

			// Test the core approval logic directly
			o := &approveOptions{
				kind:      tc.kind,
				name:      tc.requestName,
				hubClient: fakeClient,
			}
			err := o.run(context.Background())

			if tc.wantErr {
				if err == nil {
					t.Errorf("want error but got nil")
					return
				}
				if tc.wantErrMsg != "" && !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Errorf("want error to contain %q, got %q", tc.wantErrMsg, err.Error())
				}
				return
			} else if err != nil {
				t.Errorf("unwanted error: %v", err)
				return
			}

			// Verify the ClusterApprovalRequest was updated correctly.
			var updatedCAR placementv1beta1.ClusterApprovalRequest
			err = fakeClient.Get(context.Background(), client.ObjectKey{Name: tc.requestName}, &updatedCAR)
			if err != nil {
				t.Errorf("failed to get updated ClusterApprovalRequest: %v", err)
				return
			}

			// Check that the Approved condition exists and is correct.
			approvedCondition := meta.FindStatusCondition(updatedCAR.Status.Conditions, tc.wantCondition.Type)
			if diff := cmp.Diff(tc.wantCondition, approvedCondition,
				cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration")); diff != "" {
				t.Errorf("condition mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// setupScheme creates a scheme with the necessary APIs for testing.
func setupScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add placement v1beta1 scheme: %v", err)
	}
	return scheme
}
