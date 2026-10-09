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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

func TestMutatingPath(t *testing.T) {
	want := "/mutate-batch-v1-job"
	if MutatingPath != want {
		t.Errorf("MutatingPath = %q, want %q", MutatingPath, want)
	}
}

func TestMutatingHandle(t *testing.T) {
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

	job := newTestJob("test-job", "default", nil, nil)
	jobBytes := marshalOrFatal(t, job)
	jobWithLabels := newTestJob(
		"test-job",
		"default",
		map[string]string{"app": "test"},
		map[string]string{"app": "test"},
	)
	jobWithLabelsBytes := marshalOrFatal(t, jobWithLabels)
	alreadyLabeledJob := newTestJob(
		"test-job",
		"default",
		map[string]string{"app": "test", utils.ReconcileLabelKey: utils.ReconcileLabelValue},
		map[string]string{"app": "test", utils.ReconcileLabelKey: utils.ReconcileLabelValue},
	)
	alreadyLabeledJobBytes := marshalOrFatal(t, alreadyLabeledJob)
	reservedJob := newTestJob("test-job", "kube-system", nil, nil)
	reservedJobBytes := marshalOrFatal(t, reservedJob)
	fleetSystemJob := newTestJob("test-job", utils.FleetSystemNamespace, nil, nil)
	fleetSystemJobBytes := marshalOrFatal(t, fleetSystemJob)

	wantMutatedResponse := admission.Response{
		AdmissionResponse: admissionv1.AdmissionResponse{
			Allowed:   true,
			PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
		},
		Patches: []jsonpatch.JsonPatchOperation{
			{
				Operation: "add",
				Path:      "/metadata/labels",
				Value: map[string]any{
					utils.ReconcileLabelKey: utils.ReconcileLabelValue,
				},
			},
			{
				Operation: "add",
				Path:      "/spec/template/metadata/labels",
				Value: map[string]any{
					utils.ReconcileLabelKey: utils.ReconcileLabelValue,
				},
			},
		},
	}

	testCases := map[string]struct {
		req          admission.Request
		wantResponse admission.Response
	}{
		"mutate job and pod template on create for aksService user": {
			req:          newAdmissionRequest("test-job", "default", admissionv1.Create, jobBytes, aksServiceUser),
			wantResponse: wantMutatedResponse,
		},
		"mutate job and pod template on update for aksService user": {
			req:          newAdmissionRequest("test-job", "default", admissionv1.Update, jobBytes, aksServiceUser),
			wantResponse: wantMutatedResponse,
		},
		"mutate job and pod template while preserving existing labels": {
			req: newAdmissionRequest("test-job", "default", admissionv1.Create, jobWithLabelsBytes, aksServiceUser),
			wantResponse: admission.Response{
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed:   true,
					PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
				},
				Patches: []jsonpatch.JsonPatchOperation{
					{
						Operation: "add",
						Path:      "/metadata/labels/fleet.azure.com~1reconcile",
						Value:     utils.ReconcileLabelValue,
					},
					{
						Operation: "add",
						Path:      "/spec/template/metadata/labels/fleet.azure.com~1reconcile",
						Value:     utils.ReconcileLabelValue,
					},
				},
			},
		},
		"return no-op patch when both labels are already present": {
			req: newAdmissionRequest("test-job", "default", admissionv1.Update, alreadyLabeledJobBytes, aksServiceUser),
			wantResponse: admission.Response{
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed: true,
				},
				Patches: []jsonpatch.JsonPatchOperation{},
			},
		},
		"skip non-aksService user on create": {
			req:          newAdmissionRequest("test-job", "default", admissionv1.Create, jobBytes, regularUser),
			wantResponse: admission.Allowed("user is not aksService, no mutation needed"),
		},
		"skip non-aksService user on update": {
			req:          newAdmissionRequest("test-job", "default", admissionv1.Update, jobBytes, regularUser),
			wantResponse: admission.Allowed("user is not aksService, no mutation needed"),
		},
		"skip aksService user without system masters": {
			req:          newAdmissionRequest("test-job", "default", admissionv1.Create, jobBytes, aksServiceUserNoMasters),
			wantResponse: admission.Allowed("user is not aksService, no mutation needed"),
		},
		"skip non-aksService user with system masters": {
			req:          newAdmissionRequest("test-job", "default", admissionv1.Update, jobBytes, regularUserWithMasters),
			wantResponse: admission.Allowed("user is not aksService, no mutation needed"),
		},
		"skip kube-system namespace": {
			req: newAdmissionRequest("test-job", "kube-system", admissionv1.Create, reservedJobBytes, aksServiceUser),
			wantResponse: admission.Allowed(
				fmt.Sprintf("namespace %s is a reserved system namespace, no mutation needed", "kube-system"),
			),
		},
		"skip fleet-system namespace": {
			req: newAdmissionRequest("test-job", utils.FleetSystemNamespace, admissionv1.Update, fleetSystemJobBytes, aksServiceUser),
			wantResponse: admission.Allowed(
				fmt.Sprintf("namespace %s is a reserved system namespace, no mutation needed", utils.FleetSystemNamespace),
			),
		},
		"error on malformed request object": {
			req:          newAdmissionRequest("test-job", "default", admissionv1.Create, []byte("not valid json"), aksServiceUser),
			wantResponse: admission.Errored(http.StatusBadRequest, errors.New("")),
		},
	}

	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			mutator := &jobMutator{decoder: decoder}
			gotResponse := mutator.Handle(context.Background(), tc.req)
			cmpOptions := []cmp.Option{
				cmpopts.IgnoreFields(metav1.Status{}, "Message"),
				cmpopts.SortSlices(func(a, b jsonpatch.JsonPatchOperation) bool {
					return a.Path < b.Path
				}),
			}
			if diff := cmp.Diff(tc.wantResponse, gotResponse, cmpOptions...); diff != "" {
				t.Errorf("Handle() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func newAdmissionRequest(name, namespace string, operation admissionv1.Operation, raw []byte, userInfo authenticationv1.UserInfo) admission.Request {
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Name:      name,
			Namespace: namespace,
			Operation: operation,
			Object:    runtime.RawExtension{Raw: raw},
			UserInfo:  userInfo,
		},
	}
}

func marshalOrFatal(t *testing.T, object any) []byte {
	t.Helper()
	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("json.Marshal() = %v, want nil", err)
	}
	return raw
}

func newTestJob(name, namespace string, jobLabels, podTemplateLabels map[string]string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    jobLabels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podTemplateLabels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{Name: "test", Image: "busybox"},
					},
				},
			},
		},
	}
}
