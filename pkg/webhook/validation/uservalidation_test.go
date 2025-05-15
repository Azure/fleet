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

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/utils"
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
					RequestKind: &utils.RoleMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{mastersGroup},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{mastersGroup}), admissionv1.Create, &utils.RoleMetaGVK, "", types.NamespacedName{Name: "test-role", Namespace: "test-namespace"})),
		},
		"allow user in kubeadm:cluster-admin group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-role",
					Namespace:   "test-namespace",
					RequestKind: &utils.RoleMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{kubeadmClusterAdminsGroup},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{kubeadmClusterAdminsGroup}), admissionv1.Create, &utils.RoleMetaGVK, "", types.NamespacedName{Name: "test-role", Namespace: "test-namespace"})),
		},
		// UT to test GenerateGroupString in pkg/utils/common.gp
		"allow user in system:masters group along with 10 other groups": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-role",
					Namespace:   "test-namespace",
					RequestKind: &utils.RoleMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   manyGroups,
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "test-user", "groups: [random0, random1, random2,......]", admissionv1.Create, &utils.RoleMetaGVK, "", types.NamespacedName{Name: "test-role", Namespace: "test-namespace"})),
		},
		"allow white listed user not in system:masters group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-role-binding",
					Namespace:   "test-namespace",
					RequestKind: &utils.RoleBindingMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					Operation: admissionv1.Update,
				},
			},
			whiteListedUsers: []string{"test-user"},
			wantResponse:     admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.RoleBindingMetaGVK, "", types.NamespacedName{Name: "test-role-binding", Namespace: "test-namespace"})),
		},
		"allow valid service account": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-role-binding",
					Namespace:   "test-namespace",
					RequestKind: &utils.RoleBindingMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{serviceAccountsGroup},
					},
					Operation: admissionv1.Delete,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{serviceAccountsGroup}), admissionv1.Delete, &utils.RoleBindingMetaGVK, "", types.NamespacedName{Name: "test-role-binding", Namespace: "test-namespace"})),
		},
		"allow user in system:node group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-pod",
					Namespace:   "test-namespace",
					RequestKind: &utils.PodMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{nodeGroup},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{nodeGroup}), admissionv1.Create, &utils.PodMetaGVK, "", types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"})),
		},
		"allow system:kube-scheduler user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-pod",
					Namespace:   "test-namespace",
					RequestKind: &utils.PodMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "system:kube-scheduler",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Update,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "system:kube-scheduler", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.PodMetaGVK, "", types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"})),
		},
		"allow system:kube-controller-manager user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-pod",
					Namespace:   "test-namespace",
					RequestKind: &utils.PodMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "system:kube-controller-manager",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Delete,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "system:kube-controller-manager", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Delete, &utils.PodMetaGVK, "", types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"})),
		},
		"fail to validate user with invalid username, groups": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-role",
					Namespace:   "test-namespace",
					RequestKind: &utils.RoleMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					Operation: admissionv1.Delete,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Delete, &utils.RoleMetaGVK, "", types.NamespacedName{Name: "test-role", Namespace: "test-namespace"})),
		},
		"allow aks-support user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					RequestKind: &utils.MCMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "aks-support",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "aks-support", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Create, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateUserForResource(testCase.req, testCase.whiteListedUsers)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestValidateFleetMemberClusterUpdate(t *testing.T) {
	testCases := map[string]struct {
		denyModifyMemberClusterLabels bool
		oldMC                         *clusterv1beta1.MemberCluster
		newMC                         *clusterv1beta1.MemberCluster
		req                           admission.Request
		whiteListedUsers              []string
		wantResponse                  admission.Response
	}{
		"allow label modification by system:masters resources when flag is set to true": {
			denyModifyMemberClusterLabels: true,
			oldMC: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-mc",
					Labels: map[string]string{"key1": "value1"},
					Annotations: map[string]string{
						"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
					},
				},
			},
			newMC: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-mc",
					Labels: map[string]string{"key1": "value2"},
					Annotations: map[string]string{
						"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
					},
				},
			},
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					UserInfo: authenticationv1.UserInfo{
						Username: "mastersUser",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "mastersUser", utils.GenerateGroupString([]string{"system:masters"}),
				admissionv1.Update, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
		"deny label modification by non-system:masters resource when flag is set to true": {
			denyModifyMemberClusterLabels: true,
			oldMC: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-mc",
					Labels: map[string]string{"key1": "value1"},
					Annotations: map[string]string{
						"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
					},
				},
			},
			newMC: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-mc",
					Labels: map[string]string{"key1": "value2"},
					Annotations: map[string]string{
						"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
					},
				},
			},
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					UserInfo: authenticationv1.UserInfo{
						Username: "nonSystemMastersUser",
						Groups:   []string{"system:authenticated"},
					},
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Denied(DeniedModifyMemberClusterLabels),
		},
		"deny label modification by fleet agent when flag is set to true": {
			denyModifyMemberClusterLabels: true,
			oldMC: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-mc",
					Labels: map[string]string{"key1": "value1"},
					Annotations: map[string]string{
						"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
					},
				},
			},
			newMC: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-mc",
					Labels: map[string]string{"key2": "value2"},
					Annotations: map[string]string{
						"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
					},
				},
			},
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					UserInfo: authenticationv1.UserInfo{
						Username: "system:serviceaccount:fleet-system:hub-agent-sa",
						Groups:   []string{"system:serviceaccounts"},
					},
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Denied(DeniedModifyMemberClusterLabels),
		},
		"allow label modification by non system:masters resource when flag is set to false": {
			denyModifyMemberClusterLabels: false,
			oldMC: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-mc",
					Labels: map[string]string{"key1": "value1"},
					Annotations: map[string]string{
						"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
					},
				},
			},
			newMC: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-mc",
					Labels: map[string]string{"key1": "value2"},
					Annotations: map[string]string{
						"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
					},
				},
			},

			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					UserInfo: authenticationv1.UserInfo{
						Username: "nonSystemMastersUser",
						Groups:   []string{"system:authenticated"},
					},
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, "nonSystemMastersUser", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateFleetMemberClusterUpdate(*testCase.newMC, *testCase.oldMC, testCase.req, testCase.whiteListedUsers, testCase.denyModifyMemberClusterLabels)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
