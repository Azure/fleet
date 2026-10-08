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

package job

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

func TestValidationPath(t *testing.T) {
	want := "/validate-batch-v1-job"
	if ValidationPath != want {
		t.Errorf("ValidationPath = %q, want %q", ValidationPath, want)
	}
}

func TestValidatingHandle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatalf("batchv1.AddToScheme() = %v, want nil", err)
	}
	decoder := admission.NewDecoder(scheme)

	aksServiceUser := authenticationv1.UserInfo{
		Username: utils.AKSServiceUserName,
		Groups:   []string{utils.SystemMastersGroup},
	}
	aksServiceUserNoMasters := authenticationv1.UserInfo{
		Username: utils.AKSServiceUserName,
		Groups:   []string{"system:authenticated"},
	}
	regularUser := authenticationv1.UserInfo{
		Username: "regular-user",
		Groups:   []string{"system:authenticated"},
	}
	regularUserWithMasters := authenticationv1.UserInfo{
		Username: "regular-user",
		Groups:   []string{utils.SystemMastersGroup},
	}

	unlabeledJob := marshalOrFatal(t, newTestJob("test-job", "default", nil, nil))
	jobMetadataLabeled := marshalOrFatal(t, newTestJob(
		"test-job",
		"default",
		map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
		nil,
	))
	podTemplateLabeled := marshalOrFatal(t, newTestJob(
		"test-job",
		"default",
		nil,
		map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
	))
	bothLabeled := marshalOrFatal(t, newTestJob(
		"test-job",
		"default",
		map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
		map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
	))
	jobMetadataWithDifferentReconcileValue := marshalOrFatal(t, newTestJob(
		"test-job",
		"default",
		map[string]string{utils.ReconcileLabelKey: "other-value"},
		nil,
	))
	reservedNamespaceLabeled := marshalOrFatal(t, newTestJob(
		"test-job",
		"kube-system",
		map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
		nil,
	))

	testCases := map[string]struct {
		req         admission.Request
		wantAllowed bool
		wantErrCode int32
	}{
		"allow unlabeled job from regular user": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Create, unlabeledJob, regularUser),
			wantAllowed: true,
		},
		"allow update that removes labels from both locations": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Update, unlabeledJob, regularUser),
			wantAllowed: true,
		},
		"allow aksService user to create with label on job metadata": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Create, jobMetadataLabeled, aksServiceUser),
			wantAllowed: true,
		},
		"allow aksService user to create with label on pod template": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Create, podTemplateLabeled, aksServiceUser),
			wantAllowed: true,
		},
		"allow aksService user to update with labels on both locations": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Update, bothLabeled, aksServiceUser),
			wantAllowed: true,
		},
		"allow aksService user to create labeled job in reserved namespace": {
			req:         newAdmissionRequest("test-job", "kube-system", admissionv1.Create, reservedNamespaceLabeled, aksServiceUser),
			wantAllowed: true,
		},
		"deny regular user create with label on job metadata": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Create, jobMetadataLabeled, regularUser),
			wantAllowed: false,
		},
		"deny regular user create with different reconcile label value": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Create, jobMetadataWithDifferentReconcileValue, regularUser),
			wantAllowed: false,
		},
		"deny regular user create with label on pod template": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Create, podTemplateLabeled, regularUser),
			wantAllowed: false,
		},
		"deny regular user update with label on job metadata": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Update, jobMetadataLabeled, regularUser),
			wantAllowed: false,
		},
		"deny regular user update with label on pod template": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Update, podTemplateLabeled, regularUser),
			wantAllowed: false,
		},
		"deny regular user update with labels on both locations": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Update, bothLabeled, regularUser),
			wantAllowed: false,
		},
		"deny regular user create with label in reserved namespace": {
			req:         newAdmissionRequest("test-job", "kube-system", admissionv1.Create, reservedNamespaceLabeled, regularUser),
			wantAllowed: false,
		},
		"deny aksService user without system masters": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Create, bothLabeled, aksServiceUserNoMasters),
			wantAllowed: false,
		},
		"deny non-aksService user with system masters": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Update, bothLabeled, regularUserWithMasters),
			wantAllowed: false,
		},
		"allow delete operation": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Delete, jobMetadataLabeled, regularUser),
			wantAllowed: true,
		},
		"allow connect operation": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Connect, bothLabeled, regularUser),
			wantAllowed: true,
		},
		"error on malformed request object": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Create, []byte("not valid json"), aksServiceUser),
			wantAllowed: false,
			wantErrCode: http.StatusBadRequest,
		},
	}

	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			validator := &jobValidator{decoder: decoder}
			gotResponse := validator.Handle(context.Background(), tc.req)
			if gotResponse.Allowed != tc.wantAllowed {
				t.Errorf("Handle() Allowed = %v, want %v, reason = %v", gotResponse.Allowed, tc.wantAllowed, gotResponse.Result)
			}

			if tc.wantErrCode != 0 {
				wantResponse := admission.Errored(tc.wantErrCode, errors.New(""))
				if diff := cmp.Diff(wantResponse, gotResponse, cmpopts.IgnoreFields(metav1.Status{}, "Message")); diff != "" {
					t.Errorf("Handle() error response mismatch (-want +got):\n%s", diff)
				}
			}

			if !tc.wantAllowed && tc.wantErrCode == 0 && (gotResponse.Result == nil || gotResponse.Result.Message == "") {
				t.Error("Handle() denied response should include a non-empty reason message")
			}
		})
	}
}
