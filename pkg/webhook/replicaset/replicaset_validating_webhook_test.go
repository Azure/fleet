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

package replicaset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestHandle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("appsv1.AddToScheme() = %v, want nil", err)
	}
	decoder := admission.NewDecoder(scheme)

	rsInDefaultNS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-replicaset",
			Namespace: "default",
		},
	}
	rsInFleetNS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-replicaset",
			Namespace: "fleet-system",
		},
	}
	rsInKubeNS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-replicaset",
			Namespace: "kube-system",
		},
	}

	rsInDefaultNSBytes, err := json.Marshal(rsInDefaultNS)
	if err != nil {
		t.Fatalf("json.Marshal() = %v, want nil", err)
	}
	rsInFleetNSBytes, err := json.Marshal(rsInFleetNS)
	if err != nil {
		t.Fatalf("json.Marshal() = %v, want nil", err)
	}
	rsInKubeNSBytes, err := json.Marshal(rsInKubeNS)
	if err != nil {
		t.Fatalf("json.Marshal() = %v, want nil", err)
	}

	userInfo := authenticationv1.UserInfo{
		Username: "test-user",
		Groups:   []string{"system:authenticated"},
	}

	testCases := map[string]struct {
		req          admission.Request
		wantResponse admission.Response
		cmpOpts      []cmp.Option
	}{
		"deny CREATE in non-reserved namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-replicaset",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    rsInDefaultNSBytes,
						Object: rsInDefaultNS,
					},
					UserInfo: userInfo,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(replicaSetDeniedFormat, "default", "test-replicaset")),
		},
		"allow CREATE in fleet- reserved namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-replicaset",
					Namespace: "fleet-system",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    rsInFleetNSBytes,
						Object: rsInFleetNS,
					},
					UserInfo: userInfo,
				},
			},
			wantResponse: admission.Allowed(""),
		},
		"allow CREATE in kube- reserved namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-replicaset",
					Namespace: "kube-system",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    rsInKubeNSBytes,
						Object: rsInKubeNS,
					},
					UserInfo: userInfo,
				},
			},
			wantResponse: admission.Allowed(""),
		},
		"allow UPDATE (non-CREATE operation)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-replicaset",
					Namespace: "default",
					Operation: admissionv1.Update,
					UserInfo:  userInfo,
				},
			},
			wantResponse: admission.Allowed(""),
		},
		"allow DELETE (non-CREATE operation)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-replicaset",
					Namespace: "default",
					Operation: admissionv1.Delete,
					UserInfo:  userInfo,
				},
			},
			wantResponse: admission.Allowed(""),
		},
		"error on malformed request object": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-replicaset",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: []byte("not valid json"),
					},
					UserInfo: userInfo,
				},
			},
			// The exact error message from the decoder is implementation-defined,
			// so it is excluded from the comparison; the status code and
			// allowed=false are still compared.
			wantResponse: admission.Errored(http.StatusBadRequest, errors.New("")),
			cmpOpts:      []cmp.Option{cmpopts.IgnoreFields(metav1.Status{}, "Message")},
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			v := replicaSetValidator{decoder: decoder}
			gotResponse := v.Handle(context.Background(), testCase.req)
			if diff := cmp.Diff(gotResponse, testCase.wantResponse, testCase.cmpOpts...); diff != "" {
				t.Errorf("Handle() mismatch (-got +want):\n%s", diff)
			}
		})
	}
}
