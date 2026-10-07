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
	regularUser := authenticationv1.UserInfo{
		Username: "regular-user",
		Groups:   []string{"system:authenticated"},
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
		"deny regular user with label on job metadata": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Create, jobMetadataLabeled, regularUser),
			wantAllowed: false,
		},
		"deny regular user with label on pod template": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Update, podTemplateLabeled, regularUser),
			wantAllowed: false,
		},
		"allow aksService user with labels": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Update, podTemplateLabeled, aksServiceUser),
			wantAllowed: true,
		},
		"allow delete operation": {
			req:         newAdmissionRequest("test-job", "default", admissionv1.Delete, jobMetadataLabeled, regularUser),
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
