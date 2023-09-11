package validation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

func TestValidateUserForResource(t *testing.T) {
	testCases := map[string]struct {
		resKind          string
		namespacedName   types.NamespacedName
		whiteListedUsers []string
		userInfo         authenticationv1.UserInfo
		wantResponse     admission.Response
	}{
		"allow user in system:masters group": {
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{mastersGroup},
			},
			resKind:        "Role",
			namespacedName: types.NamespacedName{Name: "test-role", Namespace: "test-namespace"},
			wantResponse:   admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{mastersGroup}, "Role", types.NamespacedName{Name: "test-role", Namespace: "test-namespace"})),
		},
		"allow white listed user not in system:masters group": {
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			resKind:          "RoleBinding",
			namespacedName:   types.NamespacedName{Name: "test-role-binding", Namespace: "test-namespace"},
			whiteListedUsers: []string{"test-user"},
			wantResponse:     admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, "RoleBinding", types.NamespacedName{Name: "test-role-binding", Namespace: "test-namespace"})),
		},
		"allow valid service account": {
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{serviceAccountsGroup},
			},
			resKind:        "RoleBinding",
			namespacedName: types.NamespacedName{Name: "test-role-binding", Namespace: "test-namespace"},
			wantResponse:   admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{serviceAccountsGroup}, "RoleBinding", types.NamespacedName{Name: "test-role-binding", Namespace: "test-namespace"})),
		},
		"allow user in system:node group": {
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{nodeGroup},
			},
			resKind:        "Pod",
			namespacedName: types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"},
			wantResponse:   admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{nodeGroup}, "Pod", types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"})),
		},
		"allow system:kube-scheduler user": {
			userInfo: authenticationv1.UserInfo{
				Username: "system:kube-scheduler",
				Groups:   []string{"system:authenticated"},
			},
			resKind:        "Pod",
			namespacedName: types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"},
			wantResponse:   admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "system:kube-scheduler", []string{"system:authenticated"}, "Pod", types.NamespacedName{Name: "test-pod", Namespace: "test-namespace"})),
		},
		"fail to validate user with invalid username, groups": {
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			resKind:        "Role",
			namespacedName: types.NamespacedName{Name: "test-role", Namespace: "test-namespace"},
			wantResponse:   admission.Denied(fmt.Sprintf(resourceDeniedFormat, "test-user", []string{"test-group"}, "Role", types.NamespacedName{Name: "test-role", Namespace: "test-namespace"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateUserForResource(testCase.resKind, testCase.namespacedName, testCase.whiteListedUsers, testCase.userInfo)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestValidateUserForFleetCRD(t *testing.T) {
	testCases := map[string]struct {
		group            string
		namespacedName   types.NamespacedName
		whiteListedUsers []string
		userInfo         authenticationv1.UserInfo
		wantResponse     admission.Response
	}{
		"allow user in system:masters group to modify other CRD": {
			group:          "other-group",
			namespacedName: types.NamespacedName{Name: "test-crd"},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(crdAllowedFormat, "test-user", []string{mastersGroup}, types.NamespacedName{Name: "test-crd"})),
		},
		"allow user in system:masters group to modify fleet CRD": {
			group:          "fleet.azure.com",
			namespacedName: types.NamespacedName{Name: "memberclusters.fleet.azure.com"},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(crdAllowedFormat, "test-user", []string{mastersGroup}, types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
		},
		"allow white listed user to modify fleet CRD": {
			group:          "fleet.azure.com",
			namespacedName: types.NamespacedName{Name: "memberclusters.fleet.azure.com"},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			whiteListedUsers: []string{"test-user"},
			wantResponse:     admission.Allowed(fmt.Sprintf(crdAllowedFormat, "test-user", []string{"test-group"}, types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
		},
		"deny user not in system:masters group to modify fleet CRD": {
			group:          "fleet.azure.com",
			namespacedName: types.NamespacedName{Name: "memberclusters.fleet.azure.com"},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(crdDeniedFormat, "test-user", []string{"test-group"}, types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateUserForFleetCRD(testCase.group, testCase.namespacedName, testCase.whiteListedUsers, testCase.userInfo)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestValidateMemberClusterUpdate(t *testing.T) {
	testCases := map[string]struct {
		currentObj       client.Object
		oldObj           client.Object
		whiteListedUsers []string
		userInfo         authenticationv1.UserInfo
		wantResponse     admission.Response
	}{
		"allow any user to modify MC labels": {
			currentObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-mc",
					Labels: map[string]string{"test-key": "test-value"},
				},
			},
			oldObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, "MemberCluster", types.NamespacedName{Name: "test-mc"})),
		},
		"allow any user to modify MC annotations": {
			currentObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-mc",
					Annotations: map[string]string{"test-key": "test-value"},
				},
			},
			oldObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, "MemberCluster", types.NamespacedName{Name: "test-mc"})),
		},
		"allow system:masters group user to modify MC spec": {
			currentObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-mc",
					Annotations: map[string]string{"test-key": "test-value"},
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateLeave,
				},
			},
			oldObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateJoin,
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"system:masters"}, "MemberCluster", types.NamespacedName{Name: "test-mc"})),
		},
		"allow system:masters group user to modify MC status": {
			currentObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Status: fleetv1alpha1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			oldObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"system:masters"}, "MemberCluster", types.NamespacedName{Name: "test-mc"})),
		},
		"allow whitelisted user to modify MC status": {
			currentObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateLeave,
				},
				Status: fleetv1alpha1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			oldObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateJoin,
				},
			},
			whiteListedUsers: []string{"test-user"},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, "MemberCluster", types.NamespacedName{Name: "test-mc"})),
		},
		"deny update of member cluster spec by non system:masters group": {
			currentObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-mc",
					Annotations: map[string]string{"test-key": "test-value"},
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateLeave,
				},
			},
			oldObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateJoin,
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "test-user", []string{"test-group"}, "MemberCluster", types.NamespacedName{Name: "test-mc"})),
		},
		"deny update of member cluster spec by non whitelisted user ": {
			currentObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-mc",
					Annotations: map[string]string{"test-key": "test-value"},
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateLeave,
				},
			},
			oldObj: &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind: "MemberCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateJoin,
				},
			},
			whiteListedUsers: []string{"test-user1"},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "test-user", []string{"test-group"}, "MemberCluster", types.NamespacedName{Name: "test-mc"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateMemberClusterUpdate(testCase.currentObj, testCase.oldObj, testCase.whiteListedUsers, testCase.userInfo)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
