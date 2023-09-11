package fleetresourcehandler

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	resourceAllowedFormat                = "user: %s in groups: %v is allowed to modify resource %s: %+v"
	resourceDeniedFormat                 = "user: %s in groups: %v is not allowed to modify resource %s: %+v"
	resourceStatusUpdateNotAllowedFormat = "user: %s in groups: %v is not allowed to update %s status: %+v"
	resourceAllowedGetMCFailed           = "user: %s in groups: %v is allowed to updated %s: %+v because we failed to get MC"
)

func TestHandleInternalMemberCluster(t *testing.T) {
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
		request           admission.Request
		resourceValidator fleetResourceValidator
		wantResponse      admission.Response
	}{
		"allow user in MC identity with IMC status update": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-mc",
					Namespace: "test-ns",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "InternalMemberCluster",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-identity",
						Groups:   []string{"test-group"},
					},
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-identity", []string{"test-group"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"allow hub-agent-sa in MC identity with IMC status update": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-mc",
					Namespace: "test-ns",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "InternalMemberCluster",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "system:serviceaccount:fleet-system:hub-agent-sa",
						Groups:   []string{"system:serviceaccounts"},
					},
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
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
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "system:serviceaccount:fleet-system:hub-agent-sa", []string{"system:serviceaccounts"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"deny user in system:masters group with IMC status update": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-mc",
					Namespace: "test-ns",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "InternalMemberCluster",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceStatusUpdateNotAllowedFormat, "testUser", []string{"system:masters"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"allow user in system:masters group with IMC non-status update": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-mc",
					Namespace: "test-ns",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "InternalMemberCluster",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "testUser", []string{"system:masters"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"deny user not in system:masters group with IMC non-status update": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-mc",
					Namespace: "test-ns",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "InternalMemberCluster",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "testUser", []string{"testGroup"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
		"allow request if MC get fails": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-mc",
					Namespace: "test-ns",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "InternalMemberCluster",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return errors.New("get error")
					},
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedGetMCFailed, "testUser", []string{"system:masters"}, "InternalMemberCluster", types.NamespacedName{Name: "test-mc", Namespace: "test-ns"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleInternalMemberCluster(context.Background(), testCase.request)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestHandleWork(t *testing.T) {
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
		request           admission.Request
		resourceValidator fleetResourceValidator
		wantResponse      admission.Response
	}{
		"allow user in MC identity with status update": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Work",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-identity",
						Groups:   []string{"test-group"},
					},
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-identity", []string{"test-group"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"allow hub-agent-sa in MC identity with status update": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Work",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "system:serviceaccount:fleet-system:hub-agent-sa",
						Groups:   []string{"system:serviceaccounts"},
					},
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
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
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "system:serviceaccount:fleet-system:hub-agent-sa", []string{"system:serviceaccounts"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"deny user in system:masters group with work status update": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Work",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceStatusUpdateNotAllowedFormat, "testUser", []string{"system:masters"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in system:masters group with work non-status update": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Work",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "testUser", []string{"system:masters"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"deny user not in system:masters group with work non-status update": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Work",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "testUser", []string{"testGroup"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"allow request if MC get fails": {
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-work",
					Namespace: "test-ns",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Work",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedGetMCFailed, "testUser", []string{"system:masters"}, "Work", types.NamespacedName{Name: "test-work", Namespace: "test-ns"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleWork(context.Background(), testCase.request)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
