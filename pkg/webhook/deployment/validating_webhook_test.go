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
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

func TestValidationPath(t *testing.T) {
	want := "/validate-apps-v1-deployment"
	if ValidationPath != want {
		t.Errorf("ValidationPath = %q, want %q", ValidationPath, want)
	}
}

func TestValidatingHandle(t *testing.T) {
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
		Groups:   []string{"system:authenticated"},
	}

	regularUserWithMasters := authenticationv1.UserInfo{
		Username: "regular-user",
		Groups:   []string{utils.SystemMastersGroup, "system:authenticated"},
	}

	// Deployment without the reconcile label.
	deployNoReconcileLabel := newTestDeployment("test-deploy", "default", nil, nil)

	// Deployment with reconcile label on deployment metadata only.
	deployWithReconcileLabelOnDeploy := newTestDeployment("test-deploy", "default",
		map[string]string{"app": "myapp", utils.ReconcileLabelKey: utils.ReconcileLabelValue},
		map[string]string{"app": "myapp"},
	)

	// Deployment with reconcile label on pod template only.
	deployWithReconcileLabelOnPodTemplate := newTestDeployment("test-deploy", "default",
		map[string]string{"app": "myapp"},
		map[string]string{"app": "myapp", utils.ReconcileLabelKey: utils.ReconcileLabelValue},
	)

	// Deployment with reconcile label on both.
	deployWithReconcileLabelOnBoth := newTestDeployment("test-deploy", "default",
		map[string]string{"app": "myapp", utils.ReconcileLabelKey: utils.ReconcileLabelValue},
		map[string]string{"app": "myapp", utils.ReconcileLabelKey: utils.ReconcileLabelValue},
	)

	// Deployment in kube-system with reconcile label.
	deployKubeSystem := newTestDeployment("test-deploy", kubeSystemNamespace,
		map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
		nil,
	)

	// Deployment in fleet-system with reconcile label.
	deployFleetSystem := newTestDeployment("test-deploy", utils.FleetSystemNamespace,
		map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
		nil,
	)

	marshalOrFatal := func(t *testing.T, obj interface{}) []byte {
		t.Helper()
		b, err := json.Marshal(obj)
		if err != nil {
			t.Fatalf("json.Marshal() = %v, want nil", err)
		}
		return b
	}

	deployNoReconcileLabelBytes := marshalOrFatal(t, deployNoReconcileLabel)
	deployWithReconcileLabelOnDeployBytes := marshalOrFatal(t, deployWithReconcileLabelOnDeploy)
	deployWithReconcileLabelOnPodTemplateBytes := marshalOrFatal(t, deployWithReconcileLabelOnPodTemplate)
	deployWithReconcileLabelOnBothBytes := marshalOrFatal(t, deployWithReconcileLabelOnBoth)
	deployKubeSystemBytes := marshalOrFatal(t, deployKubeSystem)
	deployFleetSystemBytes := marshalOrFatal(t, deployFleetSystem)

	testCases := map[string]struct {
		req         admission.Request
		wantAllowed bool
		wantErrCode int32
	}{
		"allow deployment without reconcile label from regular user on CREATE": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object:    runtime.RawExtension{Raw: deployNoReconcileLabelBytes, Object: deployNoReconcileLabel},
					UserInfo:  regularUser,
				},
			},
			wantAllowed: true,
		},
		"allow deployment without reconcile label from regular user on UPDATE": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Update,
					Object:    runtime.RawExtension{Raw: deployNoReconcileLabelBytes, Object: deployNoReconcileLabel},
					UserInfo:  regularUser,
				},
			},
			wantAllowed: true,
		},
		"deny regular user from setting reconcile label in kube-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: kubeSystemNamespace,
					Operation: admissionv1.Create,
					Object:    runtime.RawExtension{Raw: deployKubeSystemBytes, Object: deployKubeSystem},
					UserInfo:  regularUser,
				},
			},
			wantAllowed: false,
		},
		"deny regular user from setting reconcile label in fleet-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: utils.FleetSystemNamespace,
					Operation: admissionv1.Create,
					Object:    runtime.RawExtension{Raw: deployFleetSystemBytes, Object: deployFleetSystem},
					UserInfo:  regularUser,
				},
			},
			wantAllowed: false,
		},
		"allow aksService user with reconcile label in kube-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: kubeSystemNamespace,
					Operation: admissionv1.Create,
					Object:    runtime.RawExtension{Raw: deployKubeSystemBytes, Object: deployKubeSystem},
					UserInfo:  aksServiceUser,
				},
			},
			wantAllowed: true,
		},
		"allow DELETE operation even with reconcile label from regular user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Delete,
					Object:    runtime.RawExtension{Raw: deployWithReconcileLabelOnBothBytes, Object: deployWithReconcileLabelOnBoth},
					UserInfo:  regularUser,
				},
			},
			wantAllowed: true,
		},
		"allow CONNECT operation even with reconcile label from regular user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Connect,
					Object:    runtime.RawExtension{Raw: deployWithReconcileLabelOnBothBytes, Object: deployWithReconcileLabelOnBoth},
					UserInfo:  regularUser,
				},
			},
			wantAllowed: true,
		},
		"allow aksService user with system:masters to set reconcile label on deployment metadata": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object:    runtime.RawExtension{Raw: deployWithReconcileLabelOnDeployBytes, Object: deployWithReconcileLabelOnDeploy},
					UserInfo:  aksServiceUser,
				},
			},
			wantAllowed: true,
		},
		"allow aksService user with system:masters to set reconcile label on pod template": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object:    runtime.RawExtension{Raw: deployWithReconcileLabelOnPodTemplateBytes, Object: deployWithReconcileLabelOnPodTemplate},
					UserInfo:  aksServiceUser,
				},
			},
			wantAllowed: true,
		},
		"allow aksService user with system:masters to set reconcile label on both on UPDATE": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Update,
					Object:    runtime.RawExtension{Raw: deployWithReconcileLabelOnBothBytes, Object: deployWithReconcileLabelOnBoth},
					UserInfo:  aksServiceUser,
				},
			},
			wantAllowed: true,
		},
		"deny aksService user without system:masters group from setting reconcile label": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object:    runtime.RawExtension{Raw: deployWithReconcileLabelOnDeployBytes, Object: deployWithReconcileLabelOnDeploy},
					UserInfo:  aksServiceUserNoMasters,
				},
			},
			wantAllowed: false,
		},
		"deny regular user from setting reconcile label on deployment metadata": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object:    runtime.RawExtension{Raw: deployWithReconcileLabelOnDeployBytes, Object: deployWithReconcileLabelOnDeploy},
					UserInfo:  regularUser,
				},
			},
			wantAllowed: false,
		},
		"deny regular user from setting reconcile label on pod template only": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object:    runtime.RawExtension{Raw: deployWithReconcileLabelOnPodTemplateBytes, Object: deployWithReconcileLabelOnPodTemplate},
					UserInfo:  regularUser,
				},
			},
			wantAllowed: false,
		},
		"deny regular user from setting reconcile label on both deployment and pod template via UPDATE": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Update,
					Object:    runtime.RawExtension{Raw: deployWithReconcileLabelOnBothBytes, Object: deployWithReconcileLabelOnBoth},
					UserInfo:  regularUser,
				},
			},
			wantAllowed: false,
		},
		"deny regular user with system:masters but wrong username from setting reconcile label": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-deploy",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object:    runtime.RawExtension{Raw: deployWithReconcileLabelOnDeployBytes, Object: deployWithReconcileLabelOnDeploy},
					UserInfo:  regularUserWithMasters,
				},
			},
			wantAllowed: false,
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
			wantAllowed: false,
			wantErrCode: http.StatusBadRequest,
		},
	}

	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			v := &deploymentValidator{decoder: decoder}
			gotResponse := v.Handle(context.Background(), tc.req)

			if gotResponse.Allowed != tc.wantAllowed {
				t.Errorf("Handle() Allowed = %v, want %v, reason = %v", gotResponse.Allowed, tc.wantAllowed, gotResponse.Result)
			}

			if tc.wantErrCode != 0 {
				wantResponse := admission.Errored(tc.wantErrCode, errors.New(""))
				cmpOptions := []cmp.Option{
					cmpopts.IgnoreFields(metav1.Status{}, "Message"),
				}
				if diff := cmp.Diff(wantResponse, gotResponse, cmpOptions...); diff != "" {
					t.Errorf("Handle() error response mismatch (-want +got):\n%s", diff)
				}
			}

			// Denied responses should include a non-empty reason message.
			if !tc.wantAllowed && tc.wantErrCode == 0 && gotResponse.Result != nil {
				if gotResponse.Result.Message == "" {
					t.Error("Handle() denied response should include a non-empty reason message")
				}
			}
		})
	}
}

func TestIsAKSService(t *testing.T) {
	testCases := map[string]struct {
		userInfo authenticationv1.UserInfo
		want     bool
	}{
		"aksService with system:masters": {
			userInfo: authenticationv1.UserInfo{
				Username: utils.AKSServiceUserName,
				Groups:   []string{utils.SystemMastersGroup},
			},
			want: true,
		},
		"aksService with system:masters and other groups": {
			userInfo: authenticationv1.UserInfo{
				Username: utils.AKSServiceUserName,
				Groups:   []string{"system:authenticated", utils.SystemMastersGroup, "custom-group"},
			},
			want: true,
		},
		"aksService without system:masters": {
			userInfo: authenticationv1.UserInfo{
				Username: utils.AKSServiceUserName,
				Groups:   []string{"system:authenticated"},
			},
			want: false,
		},
		"aksService with empty groups": {
			userInfo: authenticationv1.UserInfo{
				Username: utils.AKSServiceUserName,
				Groups:   []string{},
			},
			want: false,
		},
		"regular user with system:masters": {
			userInfo: authenticationv1.UserInfo{
				Username: "regular-user",
				Groups:   []string{utils.SystemMastersGroup},
			},
			want: false,
		},
		"empty username with system:masters": {
			userInfo: authenticationv1.UserInfo{
				Username: "",
				Groups:   []string{utils.SystemMastersGroup},
			},
			want: false,
		},
	}

	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			got := utils.IsAKSService(tc.userInfo)
			if got != tc.want {
				t.Errorf("isAKSService(%+v) = %v, want %v", tc.userInfo, got, tc.want)
			}
		})
	}
}

// newTestDeployment creates a Deployment for testing with the given labels.
func newTestDeployment(name, namespace string, deployLabels, podTemplateLabels map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    deployLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podTemplateLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "nginx"},
					},
				},
			},
		},
	}
}
