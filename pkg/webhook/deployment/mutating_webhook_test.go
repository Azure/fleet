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

package deployment

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
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

const (
	kubeSystemNamespace = "kube-system"
)

func TestMutatingPath(t *testing.T) {
	want := "/mutate-apps-v1-deployment"
	if MutatingPath != want {
		t.Errorf("MutatingPath = %q, want %q", MutatingPath, want)
	}
}

func TestHandle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("appsv1.AddToScheme() = %v, want nil", err)
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
		Groups:   []string{utils.SystemMastersGroup, "system:authenticated"},
	}

	// Deployment with no existing labels.
	deployNoLabels := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deploy",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "nginx"},
					},
				},
			},
		},
	}

	// Deployment with existing labels on both deployment and pod template.
	deployWithLabels := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deploy-labels",
			Namespace: "default",
			Labels:    map[string]string{"app": "myapp", "tier": "frontend"},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "myapp"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "myapp", "tier": "frontend"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "myapp", Image: "myapp:latest"},
					},
				},
			},
		},
	}

	// Deployment in kube-system namespace.
	deployKubeSystem := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deploy",
			Namespace: kubeSystemNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "nginx"},
					},
				},
			},
		},
	}

	// Deployment in fleet-system namespace.
	deployFleetSystem := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deploy",
			Namespace: utils.FleetSystemNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "nginx"},
					},
				},
			},
		},
	}

	// Deployment that already has the reconcile label.
	deployAlreadyLabeled := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deploy-already-labeled",
			Namespace: "default",
			Labels:    map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue, "app": "myapp"},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "myapp"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue, "app": "myapp"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "myapp", Image: "myapp:latest"},
					},
				},
			},
		},
	}

	deployNoLabelsBytes, err := json.Marshal(deployNoLabels)
	if err != nil {
		t.Fatalf("json.Marshal(deployNoLabels) = %v, want nil", err)
	}
	deployWithLabelsBytes, err := json.Marshal(deployWithLabels)
	if err != nil {
		t.Fatalf("json.Marshal(deployWithLabels) = %v, want nil", err)
	}
	deployKubeSystemBytes, err := json.Marshal(deployKubeSystem)
	if err != nil {
		t.Fatalf("json.Marshal(deployKubeSystem) = %v, want nil", err)
	}
	deployFleetSystemBytes, err := json.Marshal(deployFleetSystem)
	if err != nil {
		t.Fatalf("json.Marshal(deployFleetSystem) = %v, want nil", err)
	}
	deployAlreadyLabeledBytes, err := json.Marshal(deployAlreadyLabeled)
	if err != nil {
		t.Fatalf("json.Marshal(deployAlreadyLabeled) = %v, want nil", err)
	}

	testCases := map[string]struct {
		req          admission.Request
		wantResponse admission.Response
	}{
		"allow and skip mutation for kube-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: kubeSystemNamespace,
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    deployKubeSystemBytes,
						Object: deployKubeSystem,
					},
					UserInfo: aksServiceUser,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf("namespace %s is a reserved system namespace, no mutation needed", kubeSystemNamespace)),
		},
		"allow and skip mutation for fleet-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: utils.FleetSystemNamespace,
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    deployFleetSystemBytes,
						Object: deployFleetSystem,
					},
					UserInfo: aksServiceUser,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf("namespace %s is a reserved system namespace, no mutation needed", utils.FleetSystemNamespace)),
		},
		"allow and skip mutation for non-aksService user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    deployNoLabelsBytes,
						Object: deployNoLabels,
					},
					UserInfo: regularUser,
				},
			},
			wantResponse: admission.Allowed("user is not aksService, no mutation needed"),
		},
		"allow and skip mutation for aksService user not in system:masters group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    deployNoLabelsBytes,
						Object: deployNoLabels,
					},
					UserInfo: aksServiceUserNoMasters,
				},
			},
			wantResponse: admission.Allowed("user is not aksService, no mutation needed"),
		},
		"mutate deployment with no labels by aksService user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    deployNoLabelsBytes,
						Object: deployNoLabels,
					},
					UserInfo: aksServiceUser,
				},
			},
			wantResponse: admission.Response{
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
			},
		},
		"mutate deployment with existing labels by aksService user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy-labels",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    deployWithLabelsBytes,
						Object: deployWithLabels,
					},
					UserInfo: aksServiceUser,
				},
			},
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
		"no-op patch when reconcile label already present": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy-already-labeled",
					Namespace: "default",
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw:    deployAlreadyLabeledBytes,
						Object: deployAlreadyLabeled,
					},
					UserInfo: aksServiceUser,
				},
			},
			wantResponse: admission.Response{
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed: true,
				},
				Patches: []jsonpatch.JsonPatchOperation{},
			},
		},
		"error on malformed request object": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: []byte("not valid json"),
					},
					UserInfo: aksServiceUser,
				},
			},
			// The exact error message from the decoder is implementation-defined;
			// only the status code and allowed=false are compared.
			wantResponse: admission.Errored(http.StatusBadRequest, errors.New("")),
		},
	}

	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			m := &deploymentMutator{decoder: decoder}
			gotResponse := m.Handle(context.Background(), tc.req)
			cmpOptions := []cmp.Option{
				cmpopts.IgnoreFields(metav1.Status{}, "Message"),
				cmpopts.SortSlices(func(a, b jsonpatch.JsonPatchOperation) bool {
					if a.Path == b.Path {
						return a.Operation < b.Operation
					}
					return a.Path < b.Path
				}),
			}
			if diff := cmp.Diff(tc.wantResponse, gotResponse, cmpOptions...); diff != "" {
				t.Errorf("Handle() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
