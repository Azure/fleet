package validation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/assert"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

func TestValidateMCOrNSUpdate(t *testing.T) {
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
		"allow any user to modify Namespace labels": {
			currentObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-ns",
					Labels: map[string]string{"test-key": "test-value"},
				},
			},
			oldObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, "Namespace", types.NamespacedName{Name: "test-ns"})),
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
		"allow any user to modify NS annotations": {
			currentObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ns",
					Annotations: map[string]string{"test-key": "test-value"},
				},
			},
			oldObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, "Namespace", types.NamespacedName{Name: "test-ns"})),
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
		"allow system:masters group user to modify NS spec": {
			currentObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ns",
					Annotations: map[string]string{"test-key": "test-value"},
				},
				Spec: corev1.NamespaceSpec{
					Finalizers: []corev1.FinalizerName{"test-finalizer"},
				},
			},
			oldObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"system:masters"}, "Namespace", types.NamespacedName{Name: "test-ns"})),
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
		"allow system:masters group user to modify NS status": {
			currentObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
				Status: corev1.NamespaceStatus{
					Phase: "active",
				},
			},
			oldObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"system:masters"}, "Namespace", types.NamespacedName{Name: "test-ns"})),
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
		"allow whitelisted user to modify NS status": {
			currentObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
				Status: corev1.NamespaceStatus{
					Phase: "active",
				},
			},
			oldObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
			whiteListedUsers: []string{"test-user"},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, "Namespace", types.NamespacedName{Name: "test-ns"})),
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
		"deny update of namespace spec by non system:masters group": {
			currentObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ns",
					Annotations: map[string]string{"test-key": "test-value"},
				},
				Spec: corev1.NamespaceSpec{
					Finalizers: []corev1.FinalizerName{"test-finalizer"},
				},
			},
			oldObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "test-user", []string{"test-group"}, "Namespace", types.NamespacedName{Name: "test-ns"})),
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
		"deny update of namespace spec by non whitelisted user ": {
			currentObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ns",
					Annotations: map[string]string{"test-key": "test-value"},
				},
				Spec: corev1.NamespaceSpec{
					Finalizers: []corev1.FinalizerName{"test-finalizer"},
				},
			},
			oldObj: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
			whiteListedUsers: []string{"test-user1"},
			userInfo: authenticationv1.UserInfo{
				Username: "test-user",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "test-user", []string{"test-group"}, "Namespace", types.NamespacedName{Name: "test-ns"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateMCOrNSUpdate(testCase.currentObj, testCase.oldObj, testCase.whiteListedUsers, testCase.userInfo)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestValidateInternalMemberClusterUpdate(t *testing.T) {
	testCases := map[string]struct {
		client           client.Client
		currentIMC       fleetv1alpha1.InternalMemberCluster
		oldIMC           fleetv1alpha1.InternalMemberCluster
		whiteListedUsers []string
		userInfo         authenticationv1.UserInfo
		wantResponse     admission.Response
	}{
		"allow user in IMC identity with status update": {
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					o := obj.(*fleetv1alpha1.MemberCluster)
					*o = fleetv1alpha1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-mc",
							Namespace: "test-namespace",
						},
						Spec: fleetv1alpha1.MemberClusterSpec{
							Identity: rbacv1.Subject{
								Name: "test-identity",
							},
						},
					}
					return nil
				},
			},
			currentIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Status: fleetv1alpha1.InternalMemberClusterStatus{
					ResourceUsage: fleetv1alpha1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.Quantity{Format: "format1"},
						},
					},
				},
			},
			oldIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Status: fleetv1alpha1.InternalMemberClusterStatus{
					ResourceUsage: fleetv1alpha1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.Quantity{Format: "format2"},
						},
					},
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-identity",
				Groups:   []string{"test-group"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-identity", []string{"test-group"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"allow hub-agent-sa in IMC identity with status update": {
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					o := obj.(*fleetv1alpha1.MemberCluster)
					*o = fleetv1alpha1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-mc",
							Namespace: "test-namespace",
						},
						Spec: fleetv1alpha1.MemberClusterSpec{
							Identity: rbacv1.Subject{
								Name: "hub-agent-sa",
							},
						},
					}
					return nil
				},
			},
			currentIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Status: fleetv1alpha1.InternalMemberClusterStatus{
					ResourceUsage: fleetv1alpha1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.Quantity{Format: "format1"},
						},
					},
				},
			},
			oldIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Status: fleetv1alpha1.InternalMemberClusterStatus{
					ResourceUsage: fleetv1alpha1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.Quantity{Format: "format2"},
						},
					},
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "system:serviceaccount:fleet-system:hub-agent-sa",
				Groups:   []string{"system:serviceaccounts"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "system:serviceaccount:fleet-system:hub-agent-sa", []string{"system:serviceaccounts"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"deny user in system:masters group with status update": {
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					o := obj.(*fleetv1alpha1.MemberCluster)
					*o = fleetv1alpha1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-mc",
							Namespace: "test-namespace",
						},
						Spec: fleetv1alpha1.MemberClusterSpec{
							Identity: rbacv1.Subject{
								Name: "test-identity",
							},
						},
					}
					return nil
				},
			},
			currentIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Status: fleetv1alpha1.InternalMemberClusterStatus{
					ResourceUsage: fleetv1alpha1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.Quantity{Format: "format1"},
						},
					},
				},
			},
			oldIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Status: fleetv1alpha1.InternalMemberClusterStatus{
					ResourceUsage: fleetv1alpha1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.Quantity{Format: "format2"},
						},
					},
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(imcStatusUpdateNotAllowedFormat, "testUser", []string{"system:masters"}, types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"allow user in system:masters group with spec update": {
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					o := obj.(*fleetv1alpha1.MemberCluster)
					*o = fleetv1alpha1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-mc",
							Namespace: "test-namespace",
						},
						Spec: fleetv1alpha1.MemberClusterSpec{
							Identity: rbacv1.Subject{
								Name: "test-identity",
							},
						},
					}
					return nil
				},
			},
			currentIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					HeartbeatPeriodSeconds: 10,
				},
			},
			oldIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					HeartbeatPeriodSeconds: 11,
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "testUser", []string{"system:masters"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"deny user not in system:masters group with spec update": {
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					o := obj.(*fleetv1alpha1.MemberCluster)
					*o = fleetv1alpha1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-mc",
							Namespace: "test-namespace",
						},
						Spec: fleetv1alpha1.MemberClusterSpec{
							Identity: rbacv1.Subject{
								Name: "test-identity",
							},
						},
					}
					return nil
				},
			},
			currentIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					HeartbeatPeriodSeconds: 10,
				},
			},
			oldIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					HeartbeatPeriodSeconds: 11,
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"testGroup"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "testUser", []string{"testGroup"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"allow user in system:masters group with object meta update": {
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					o := obj.(*fleetv1alpha1.MemberCluster)
					*o = fleetv1alpha1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-mc",
							Namespace: "test-namespace",
						},
						Spec: fleetv1alpha1.MemberClusterSpec{
							Identity: rbacv1.Subject{
								Name: "test-identity",
							},
						},
					}
					return nil
				},
			},
			currentIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
					Labels:    map[string]string{"test-key": "test-value"},
				},
			},
			oldIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "testUser", []string{"system:masters"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"allow request if MC get fails": {
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					return errors.New("get error")
				},
			},
			currentIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Status: fleetv1alpha1.InternalMemberClusterStatus{
					ResourceUsage: fleetv1alpha1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.Quantity{Format: "format1"},
						},
					},
				},
			},
			oldIMC: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Status: fleetv1alpha1.InternalMemberClusterStatus{
					ResourceUsage: fleetv1alpha1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.Quantity{Format: "format2"},
						},
					},
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(imcAllowedGetMCFailed, "testUser", []string{"system:masters"}, types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateInternalMemberClusterUpdate(context.Background(), testCase.client, testCase.currentIMC, testCase.oldIMC, testCase.whiteListedUsers, testCase.userInfo)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
