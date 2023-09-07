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
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

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

func TestValidateInternalMemberClusterUpdate(t *testing.T) {
	mockClient := &test.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			o := obj.(*fleetv1alpha1.MemberCluster)
			*o = fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					Identity: rbacv1.Subject{
						Name: "test-identity",
					},
				},
			}
			return nil
		},
	}
	testCases := map[string]struct {
		client           client.Client
		imc              fleetv1alpha1.InternalMemberCluster
		whiteListedUsers []string
		userInfo         authenticationv1.UserInfo
		subResource      string
		wantResponse     admission.Response
	}{
		"allow user in IMC identity with IMC status update": {
			client: mockClient,
			imc: fleetv1alpha1.InternalMemberCluster{
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
			userInfo: authenticationv1.UserInfo{
				Username: "test-identity",
				Groups:   []string{"test-group"},
			},
			subResource:  "status",
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-identity", []string{"test-group"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"allow hub-agent-sa in IMC identity with IMC status update": {
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					o := obj.(*fleetv1alpha1.MemberCluster)
					*o = fleetv1alpha1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-mc",
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
			imc: fleetv1alpha1.InternalMemberCluster{
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
			userInfo: authenticationv1.UserInfo{
				Username: "system:serviceaccount:fleet-system:hub-agent-sa",
				Groups:   []string{"system:serviceaccounts"},
			},
			subResource:  "status",
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "system:serviceaccount:fleet-system:hub-agent-sa", []string{"system:serviceaccounts"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"deny user in system:masters group with IMC status update": {
			client: mockClient,
			imc: fleetv1alpha1.InternalMemberCluster{
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
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"system:masters"},
			},
			subResource:  "status",
			wantResponse: admission.Denied(fmt.Sprintf(resourceStatusUpdateNotAllowedFormat, "testUser", []string{"system:masters"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"allow user in system:masters group with IMC spec update": {
			client: mockClient,
			imc: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					HeartbeatPeriodSeconds: 10,
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "testUser", []string{"system:masters"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"deny user not in system:masters group with IMC spec update": {
			client: mockClient,
			imc: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					HeartbeatPeriodSeconds: 10,
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"testGroup"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "testUser", []string{"testGroup"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"allow user in system:masters group with IMC object meta update": {
			client: mockClient,
			imc: fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{Kind: "InternalMemberCluster"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "test-ns",
					Labels:    map[string]string{"test-key": "test-value"},
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
			imc: fleetv1alpha1.InternalMemberCluster{
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
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"system:masters"},
			},
			subResource:  "status",
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedGetMCFailed, "testUser", []string{"system:masters"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateInternalMemberClusterUpdate(context.Background(), testCase.client, testCase.imc, testCase.whiteListedUsers, testCase.userInfo, testCase.subResource)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestValidateWorkUpdate(t *testing.T) {
	mockClient := &test.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			if key.Name == "test-mc" {
				o := obj.(*fleetv1alpha1.MemberCluster)
				*o = fleetv1alpha1.MemberCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-mc",
					},
					Spec: fleetv1alpha1.MemberClusterSpec{
						Identity: rbacv1.Subject{
							Name: "test-identity",
						},
					},
				}
				return nil
			}
			return errors.New("cannot find member cluster")
		},
	}
	testCases := map[string]struct {
		client           client.Client
		work             workv1alpha1.Work
		whiteListedUsers []string
		userInfo         authenticationv1.UserInfo
		subResource      string
		wantResponse     admission.Response
	}{
		"allow user in work identity with status update": {
			client: mockClient,
			work: workv1alpha1.Work{
				TypeMeta: metav1.TypeMeta{Kind: "Work"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
				},
				Status: workv1alpha1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Applied",
							Status:             metav1.ConditionTrue,
							Reason:             "appliedWorkComplete",
							Message:            "Apply work complete",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "test-identity",
				Groups:   []string{"test-group"},
			},
			subResource:  "status",
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-identity", []string{"test-group"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"allow hub-agent-sa in work identity with status update": {
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					if key.Name == "test-mc" {
						o := obj.(*fleetv1alpha1.MemberCluster)
						*o = fleetv1alpha1.MemberCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-mc",
							},
							Spec: fleetv1alpha1.MemberClusterSpec{
								Identity: rbacv1.Subject{
									Name: "hub-agent-sa",
								},
							},
						}
						return nil
					}
					return errors.New("cannot find member cluster")
				},
			},
			work: workv1alpha1.Work{
				TypeMeta: metav1.TypeMeta{Kind: "Work"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
				},
				Status: workv1alpha1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Applied",
							Status:             metav1.ConditionTrue,
							Reason:             "appliedWorkComplete",
							Message:            "Apply work complete",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "system:serviceaccount:fleet-system:hub-agent-sa",
				Groups:   []string{"system:serviceaccounts"},
			},
			subResource:  "status",
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "system:serviceaccount:fleet-system:hub-agent-sa", []string{"system:serviceaccounts"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"deny user in system:masters group with work status update": {
			client: mockClient,
			work: workv1alpha1.Work{
				TypeMeta: metav1.TypeMeta{Kind: "Work"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
				},
				Status: workv1alpha1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Applied",
							Status:             metav1.ConditionTrue,
							Reason:             "appliedWorkComplete",
							Message:            "Apply work complete",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"system:masters"},
			},
			subResource:  "status",
			wantResponse: admission.Denied(fmt.Sprintf(resourceStatusUpdateNotAllowedFormat, "testUser", []string{"system:masters"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in system:masters group with work spec update": {
			client: mockClient,
			work: workv1alpha1.Work{
				TypeMeta: metav1.TypeMeta{Kind: "Work"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
				},
				Spec: workv1alpha1.WorkSpec{
					Workload: workv1alpha1.WorkloadTemplate{
						Manifests: []workv1alpha1.Manifest{},
					},
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "testUser", []string{"system:masters"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"deny user not in system:masters group with work spec update": {
			client: mockClient,
			work: workv1alpha1.Work{
				TypeMeta: metav1.TypeMeta{Kind: "Work"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
				},
				Spec: workv1alpha1.WorkSpec{
					Workload: workv1alpha1.WorkloadTemplate{
						Manifests: []workv1alpha1.Manifest{},
					},
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"testGroup"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "testUser", []string{"testGroup"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in system:masters group with work object meta update": {
			client: mockClient,
			work: workv1alpha1.Work{
				TypeMeta: metav1.TypeMeta{Kind: "Work"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
					Labels:    map[string]string{"test-key": "test-value"},
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"system:masters"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "testUser", []string{"system:masters"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"allow request if MC get fails": {
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					return errors.New("get error")
				},
			},
			work: workv1alpha1.Work{
				TypeMeta: metav1.TypeMeta{Kind: "Work"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
				},
			},
			userInfo: authenticationv1.UserInfo{
				Username: "testUser",
				Groups:   []string{"system:masters"},
			},
			subResource:  "status",
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedGetMCFailed, "testUser", []string{"system:masters"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := ValidateWorkUpdate(context.Background(), testCase.client, testCase.work, testCase.whiteListedUsers, testCase.userInfo, testCase.subResource)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
