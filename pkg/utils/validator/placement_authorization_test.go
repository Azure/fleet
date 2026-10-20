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

package validator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/authorization"
)

type stubPlacementAuthorizer struct {
	result authorization.Result
}

func (a *stubPlacementAuthorizer) Authorize(context.Context, authenticationv1.UserInfo, placementv1beta1.PlacementObj) authorization.Result {
	return a.result
}

func TestHandlePlacementValidationAuthorizationDecision(t *testing.T) {
	const denialReason = "requester cannot get selected resources"
	reviewErr := errors.New("authorization API unavailable")
	type outcome struct {
		Allowed bool
		Code    int32
		Message string
	}
	tests := []struct {
		name   string
		result authorization.Result
		want   outcome
	}{
		{
			name:   "allowed",
			result: authorization.Result{Decision: authorization.DecisionAllowed},
			want:   outcome{Allowed: true, Code: http.StatusOK, Message: fmt.Sprintf(AllowModifyFmt, "RP")},
		},
		{
			name:   "denied",
			result: authorization.Result{Decision: authorization.DecisionDenied, DenialReason: denialReason},
			want:   outcome{Code: http.StatusForbidden, Message: denialReason},
		},
		{
			name:   "denial takes precedence over additional review errors",
			result: authorization.Result{Decision: authorization.DecisionDenied, DenialReason: denialReason, Err: reviewErr},
			want:   outcome{Code: http.StatusForbidden, Message: denialReason},
		},
		{
			name:   "indeterminate",
			result: authorization.Result{Decision: authorization.DecisionIndeterminate, Err: reviewErr},
			want:   outcome{Code: http.StatusInternalServerError, Message: reviewErr.Error()},
		},
		{
			name: "zero decision",
			want: outcome{Code: http.StatusInternalServerError, Message: `selected-resource authorization returned an unknown decision ""`},
		},
		{
			name:   "unknown decision",
			result: authorization.Result{Decision: "Unknown"},
			want:   outcome{Code: http.StatusInternalServerError, Message: `selected-resource authorization returned an unknown decision "Unknown"`},
		},
		{
			name:   "indeterminate without error",
			result: authorization.Result{Decision: authorization.DecisionIndeterminate},
			want:   outcome{Code: http.StatusInternalServerError, Message: "selected-resource authorization returned an indeterminate decision without an error"},
		},
		{
			name:   "denied without reason",
			result: authorization.Result{Decision: authorization.DecisionDenied},
			want:   outcome{Code: http.StatusInternalServerError, Message: "selected-resource authorization returned a denial without a reason"},
		},
		{
			name:   "allowed with error",
			result: authorization.Result{Decision: authorization.DecisionAllowed, Err: reviewErr},
			want:   outcome{Code: http.StatusInternalServerError, Message: "selected-resource authorization returned an allowed decision with denial or error details"},
		},
		{
			name:   "allowed with denial reason",
			result: authorization.Result{Decision: authorization.DecisionAllowed, DenialReason: denialReason},
			want:   outcome{Code: http.StatusInternalServerError, Message: "selected-resource authorization returned an allowed decision with denial or error details"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := HandlePlacementValidation(
				context.Background(),
				admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Create}},
				nil,
				"RP",
				func(admission.Request, webhook.AdmissionDecoder) (placementv1beta1.PlacementObj, error) {
					return &placementv1beta1.ResourcePlacement{}, nil
				},
				nil,
				func(placementv1beta1.PlacementObj) error { return nil },
				&stubPlacementAuthorizer{result: tt.result},
			)
			got := outcome{Allowed: response.Allowed, Code: response.Result.Code, Message: response.Result.Message}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("HandlePlacementValidation() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
