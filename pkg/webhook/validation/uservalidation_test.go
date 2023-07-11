package validation

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

func TestValidateUserForResource(t *testing.T) {
	testCases := map[string]struct {
		userInfo         v1.UserInfo
		whiteListedUsers []string
		resKind          string
		resName          string
		resNamespace     string
		wantResponse     admission.Response
	}{
		"allow user in system:masters group": {
			userInfo: v1.UserInfo{
				Username: "test-user",
				Groups:   []string{mastersGroup},
			},
			resKind:      "Role",
			resName:      "test-role",
			resNamespace: "test-namespace",
			wantResponse: admission.Allowed("user: test-user in groups: [system:masters] is allowed to modify fleet resource Role: test-role/test-namespace"),
		},
		"allow white listed user not in system:masters group": {
			userInfo: v1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			resKind:          "RoleBinding",
			resName:          "test-role-binding",
			resNamespace:     "test-namespace",
			whiteListedUsers: []string{"test-user"},
			wantResponse:     admission.Allowed("user: test-user in groups: [test-group] is allowed to modify fleet resource RoleBinding: test-role-binding/test-namespace"),
		},
		"allow valid service account": {
			userInfo: v1.UserInfo{
				Username: "test-user",
				Groups:   []string{serviceAccountsGroup, serviceAccountsKubeSystemGroup, authenticatedGroup},
			},
			resKind:      "RoleBinding",
			resName:      "test-role-binding",
			resNamespace: "test-namespace",
			wantResponse: admission.Allowed("user: test-user in groups: [system:serviceaccounts system:serviceaccounts:kube-system system:authenticated] is allowed to modify fleet resource RoleBinding: test-role-binding/test-namespace"),
		},
		"fail to validate user with invalid username, groups": {
			userInfo: v1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			resKind:      "Role",
			resName:      "test-role",
			resNamespace: "test-namespace",
			wantResponse: admission.Denied("user: test-user in groups: [test-group] is not allowed to modify fleet resource Role: test-role/test-namespace"),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateUserForResource(testCase.whiteListedUsers, testCase.userInfo, testCase.resKind, testCase.resName, testCase.resNamespace)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestValidateUserForFleetCR(t *testing.T) {
	testCases := map[string]struct {
		client           client.Client
		whiteListedUsers []string
		userInfo         v1.UserInfo
		wantResult       bool
	}{
		"allow use in system:masters group": {
			userInfo: v1.UserInfo{
				Username: "test-user",
				Groups:   []string{"system:masters"},
			},
			wantResult: true,
		},
		"allow white listed user not in system:masters group": {
			userInfo: v1.UserInfo{
				Username: "test-user",
			},
			whiteListedUsers: []string{"test-user"},
			wantResult:       true,
		},
		"allow member cluster identity": {
			client: &test.MockClient{
				MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					o := list.(*fleetv1alpha1.MemberClusterList)
					*o = fleetv1alpha1.MemberClusterList{
						Items: []fleetv1alpha1.MemberCluster{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "test-member-cluster",
								},
								Spec: fleetv1alpha1.MemberClusterSpec{
									Identity: rbacv1.Subject{
										Name: "member-cluster-identity",
									},
								},
							},
						},
					}
					return nil
				},
			},
			userInfo: v1.UserInfo{
				Username: "member-cluster-identity",
			},
			wantResult: true,
		},
		"fail to validate user with invalid username, groups": {
			client: &test.MockClient{
				MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					return nil
				},
			},
			userInfo: v1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResult: false,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateUserForFleetCR(context.Background(), testCase.client, testCase.whiteListedUsers, testCase.userInfo)
			assert.Equal(t, testCase.wantResult, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
