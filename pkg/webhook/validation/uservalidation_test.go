package validation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

func TestValidateUserForResource(t *testing.T) {
	testCases := map[string]struct {
		req              admission.Request
		whiteListedUsers []string
		wantResponse     admission.Response
	}{
		"allow user in system:masters group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-role",
					Namespace: "test-namespace",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Role",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{mastersGroup},
					},
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{mastersGroup}, "Role", "", types.NamespacedName{Name: "test-role", Namespace: "test-namespace"})),
		},
		"allow white listed user not in system:masters group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-role-binding",
					Namespace: "test-namespace",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "RoleBinding",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
				},
			},
			whiteListedUsers: []string{"test-user"},
			wantResponse:     admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, "RoleBinding", "", types.NamespacedName{Name: "test-role-binding", Namespace: "test-namespace"})),
		},
		"allow valid service account": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-role-binding",
					Namespace: "test-namespace",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "RoleBinding",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{serviceAccountsGroup},
					},
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{serviceAccountsGroup}, "RoleBinding", "", types.NamespacedName{Name: "test-role-binding", Namespace: "test-namespace"})),
		},
		"allow user in system:node group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-pod",
					Namespace: "test-namespace",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Pod",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{nodeGroup},
					},
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{nodeGroup}, "Pod", "", types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"})),
		},
		"allow system:kube-scheduler user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-pod",
					Namespace: "test-namespace",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Pod",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "system:kube-scheduler",
						Groups:   []string{"system:authenticated"},
					},
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "system:kube-scheduler", []string{"system:authenticated"}, "Pod", "", types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"})),
		},
		"allow system:kube-controller-manager user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-pod",
					Namespace: "test-namespace",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Pod",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "system:kube-controller-manager",
						Groups:   []string{"system:authenticated"},
					},
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "system:kube-controller-manager", []string{"system:authenticated"}, "Pod", "", types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"})),
		},
		"fail to validate user with invalid username, groups": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-role",
					Namespace: "test-namespace",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Role",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "test-user", []string{"test-group"}, "Role", "", types.NamespacedName{Name: "test-role", Namespace: "test-namespace"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateUserForResource(testCase.req, testCase.whiteListedUsers)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
