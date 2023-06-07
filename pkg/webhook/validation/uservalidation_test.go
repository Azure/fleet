package validation

import (
	"context"
	"errors"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

func TestValidateUser(t *testing.T) {
	testCases := map[string]struct {
		client   client.Client
		userInfo v1.UserInfo
		wantErr  error
	}{
		"allow user in system:masters group": {
			client: &test.MockClient{
				MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					return nil
				},
			},
			userInfo: v1.UserInfo{
				Username: "test-user",
				Groups:   []string{"system:masters"},
			},
			wantErr: nil,
		},
		"allow user in system:bootstrappers group": {
			client: &test.MockClient{
				MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					return nil
				},
			},
			userInfo: v1.UserInfo{
				Username: "test-user",
				Groups:   []string{"system:bootstrappers", "system:authenticated"},
			},
			wantErr: nil,
		},
		"allow user in system:serviceaccounts group, has service account prefix in name": {
			client: &test.MockClient{
				MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					return nil
				},
			},
			userInfo: v1.UserInfo{
				Username: "service:account:test-user",
				Groups:   []string{"system:masters", "system:serviceaccounts"},
			},
			wantErr: nil,
		},
		"allow member cluster identity": {
			client: &test.MockClient{
				MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					o := list.(*v1alpha1.MemberClusterList)
					*o = v1alpha1.MemberClusterList{
						Items: []v1alpha1.MemberCluster{
							{
								Spec: v1alpha1.MemberClusterSpec{
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
				Groups:   []string{"system:authenticated"},
			},
			wantErr: nil,
		},
		"fail to list member clusters": {
			client: &test.MockClient{
				MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					return errors.New("failed to member clusters")
				},
			},
			userInfo: v1.UserInfo{
				Username: "test-user",
				Groups:   []string{"system:authenticated"},
			},
			wantErr: errors.New("failed to member clusters"),
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
			wantErr: errors.New("failed to validate user test-user in groups [test-group]"),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotErr := ValidateUser(context.Background(), testCase.client, testCase.userInfo)
			assert.Equal(t, testCase.wantErr, gotErr, utils.TestCaseMsg, testName)
		})
	}
}
