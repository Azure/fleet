package validation

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/pkg/utils"
)

func TestValidateUserForCRD(t *testing.T) {
	testCases := map[string]struct {
		client     client.Client
		userInfo   v1.UserInfo
		wantResult bool
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
			gotResult := ValidateUserForCRD(testCase.userInfo)
			assert.Equal(t, testCase.wantResult, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
