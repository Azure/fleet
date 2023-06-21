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

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

func TestValidateUserForCRD(t *testing.T) {
	testCases := map[string]struct {
		userInfo   v1.UserInfo
		wantResult bool
	}{
		"allow user in system:masters group": {
			userInfo: v1.UserInfo{
				Username: "test-user",
				Groups:   []string{"system:masters"},
			},
			wantResult: true,
		},
		"fail to validate user with invalid username, groups": {
			userInfo: v1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResult: false,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateUserForCRD(testCase.userInfo)
			assert.Equal(t, testCase.wantResult, gotResult, utils.TestCaseMsg, testName)
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
