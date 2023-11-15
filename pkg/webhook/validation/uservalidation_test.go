package validation

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

var (
	roleGVK        = metav1.GroupVersionKind{Group: metav1.SchemeGroupVersion.Group, Version: metav1.SchemeGroupVersion.Version, Kind: "Role"}
	roleBindingGVK = metav1.GroupVersionKind{Group: metav1.SchemeGroupVersion.Group, Version: metav1.SchemeGroupVersion.Version, Kind: "RoleBinding"}
	podGVK         = metav1.GroupVersionKind{Group: metav1.SchemeGroupVersion.Group, Version: metav1.SchemeGroupVersion.Version, Kind: "Pod"}
)

func TestValidateUserForResource(t *testing.T) {
	manyGroups := []string{mastersGroup, "random0", "random1", "random2", "random3", "random4", "random5", "random6", "random7", "random8", "random9"}
	sort.Strings(manyGroups)
	testCases := map[string]struct {
		req              admission.Request
		whiteListedUsers []string
		wantResponse     admission.Response
	}{
		"allow user in system:masters group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-role",
					Namespace:   "test-namespace",
					RequestKind: &roleGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{mastersGroup},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{mastersGroup}), admissionv1.Create, &roleGVK, "", types.NamespacedName{Name: "test-role", Namespace: "test-namespace"})),
		},
		// UT to test GenerateGroupString in pkg/utils/common.gp
		"allow user in system:masters group along with 10 other groups": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-role",
					Namespace:   "test-namespace",
					RequestKind: &roleGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   manyGroups,
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "test-user", "groups: [random0, random1, random2,......]", admissionv1.Create, &roleGVK, "", types.NamespacedName{Name: "test-role", Namespace: "test-namespace"})),
		},
		"allow white listed user not in system:masters group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-role-binding",
					Namespace:   "test-namespace",
					RequestKind: &roleBindingGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					Operation: admissionv1.Update,
				},
			},
			whiteListedUsers: []string{"test-user"},
			wantResponse:     admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &roleBindingGVK, "", types.NamespacedName{Name: "test-role-binding", Namespace: "test-namespace"})),
		},
		"allow valid service account": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-role-binding",
					Namespace:   "test-namespace",
					RequestKind: &roleBindingGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{serviceAccountsGroup},
					},
					Operation: admissionv1.Delete,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{serviceAccountsGroup}), admissionv1.Delete, &roleBindingGVK, "", types.NamespacedName{Name: "test-role-binding", Namespace: "test-namespace"})),
		},
		"allow user in system:node group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-pod",
					Namespace:   "test-namespace",
					RequestKind: &podGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{nodeGroup},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{nodeGroup}), admissionv1.Create, &podGVK, "", types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"})),
		},
		"allow system:kube-scheduler user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-pod",
					Namespace:   "test-namespace",
					RequestKind: &podGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "system:kube-scheduler",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Update,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "system:kube-scheduler", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &podGVK, "", types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"})),
		},
		"allow system:kube-controller-manager user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-pod",
					Namespace:   "test-namespace",
					RequestKind: &podGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "system:kube-controller-manager",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Delete,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "system:kube-controller-manager", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Delete, &podGVK, "", types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"})),
		},
		"fail to validate user with invalid username, groups": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-role",
					Namespace:   "test-namespace",
					RequestKind: &roleGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					Operation: admissionv1.Delete,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Delete, &roleGVK, "", types.NamespacedName{Name: "test-role", Namespace: "test-namespace"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateUserForResource(testCase.req, testCase.whiteListedUsers)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
