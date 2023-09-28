package fleetresourcehandler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	resourceAllowedFormat      = "user: %s in groups: %v is allowed to %s resource %s/%s: %+v"
	resourceDeniedFormat       = "user: %s in groups: %v is not allowed to %s resource %s/%s: %+v"
	resourceAllowedGetMCFailed = "user: %s in groups: %v is allowed to %s resource %s/%s: %+v because we failed to get MC"

	mcName = "test-mc"
)

func TestHandleCRD(t *testing.T) {
	testCases := map[string]struct {
		req               admission.Request
		resourceValidator fleetResourceValidator
		wantResponse      admission.Response
	}{
		"allow any user group to modify fleet unrelated CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crd",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "CustomResourceDefinition",
					},
					Operation: admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{},
			wantResponse:      admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, admissionv1.Create, "CustomResourceDefinition", "", types.NamespacedName{Name: "test-crd"})),
		},
		"allow user in system:masters group to modify fleet CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "memberclusters.fleet.azure.com",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "CustomResourceDefinition",
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{},
			wantResponse:      admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"system:masters"}, admissionv1.Update, "CustomResourceDefinition", "", types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
		},
		"allow white listed user to modify fleet CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "memberclusters.fleet.azure.com",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "CustomResourceDefinition",
					},
					Operation: admissionv1.Delete,
				},
			},
			resourceValidator: fleetResourceValidator{
				whiteListedUsers: []string{"test-user"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, admissionv1.Delete, "CustomResourceDefinition", "", types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
		},
		"deny user not in system:masters group to modify fleet CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "memberclusters.fleet.azure.com",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "CustomResourceDefinition",
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "test-user", []string{"test-group"}, admissionv1.Create, "CustomResourceDefinition", "", types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleCRD(testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestHandleMemberCluster(t *testing.T) {
	MCObject := &fleetv1alpha1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
		},
	}
	labelUpdatedMCObject := &fleetv1alpha1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-mc",
			Labels: map[string]string{"test-key": "test-value"},
		},
	}
	annotationUpdatedMCObject := &fleetv1alpha1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-mc",
			Annotations: map[string]string{"test-key": "test-value"},
		},
	}
	specUpdatedMCObject := &fleetv1alpha1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
		},
		Spec: fleetv1alpha1.MemberClusterSpec{
			State: fleetv1alpha1.ClusterStateLeave,
		},
	}
	statusUpdatedMCObject := &fleetv1alpha1.MemberCluster{
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
	}

	MCObjectBytes, err := json.Marshal(MCObject)
	assert.Nil(t, err)
	labelUpdatedMCObjectBytes, err := json.Marshal(labelUpdatedMCObject)
	assert.Nil(t, err)
	annotationUpdatedMCObjectBytes, err := json.Marshal(annotationUpdatedMCObject)
	assert.Nil(t, err)
	specUpdatedMCObjectBytes, err := json.Marshal(specUpdatedMCObject)
	assert.Nil(t, err)
	statusUpdatedMCObjectBytes, err := json.Marshal(statusUpdatedMCObject)
	assert.Nil(t, err)

	scheme := runtime.NewScheme()
	err = fleetv1alpha1.AddToScheme(scheme)
	assert.Nil(t, err)
	decoder, err := admission.NewDecoder(scheme)
	assert.Nil(t, err)

	testCases := map[string]struct {
		req               admission.Request
		resourceValidator fleetResourceValidator
		wantResponse      admission.Response
	}{
		"allow create MC for user in system:masters group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw:    labelUpdatedMCObjectBytes,
						Object: labelUpdatedMCObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "MemberCluster",
					},
					Operation: admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"system:masters"}, admissionv1.Create, "MemberCluster", "", types.NamespacedName{Name: "test-mc"})),
		},
		"allow any user to modify MC labels": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw:    labelUpdatedMCObjectBytes,
						Object: labelUpdatedMCObject,
					},
					OldObject: runtime.RawExtension{
						Raw:    MCObjectBytes,
						Object: MCObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "MemberCluster",
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, admissionv1.Update, "MemberCluster", "", types.NamespacedName{Name: "test-mc"})),
		},
		"allow any user to modify MC annotations": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw:    annotationUpdatedMCObjectBytes,
						Object: annotationUpdatedMCObject,
					},
					OldObject: runtime.RawExtension{
						Raw:    MCObjectBytes,
						Object: MCObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "MemberCluster",
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, admissionv1.Update, "MemberCluster", "", types.NamespacedName{Name: "test-mc"})),
		},
		"allow system:masters group user to modify MC spec": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw:    specUpdatedMCObjectBytes,
						Object: specUpdatedMCObject,
					},
					OldObject: runtime.RawExtension{
						Raw:    MCObjectBytes,
						Object: MCObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "MemberCluster",
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"system:masters"}, admissionv1.Update, "MemberCluster", "", types.NamespacedName{Name: "test-mc"})),
		},
		"allow system:masters group user to modify MC status": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw:    statusUpdatedMCObjectBytes,
						Object: statusUpdatedMCObject,
					},
					OldObject: runtime.RawExtension{
						Raw:    MCObjectBytes,
						Object: MCObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "MemberCluster",
					},
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"system:masters"}, admissionv1.Update, "MemberCluster", "status", types.NamespacedName{Name: "test-mc"})),
		},
		"allow whitelisted user to modify MC status": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw:    statusUpdatedMCObjectBytes,
						Object: statusUpdatedMCObject,
					},
					OldObject: runtime.RawExtension{
						Raw:    MCObjectBytes,
						Object: MCObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "MemberCluster",
					},
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-user", []string{"test-group"}, admissionv1.Update, "MemberCluster", "status", types.NamespacedName{Name: "test-mc"})),
		},
		"deny update of member cluster spec by non system:masters group": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw:    specUpdatedMCObjectBytes,
						Object: specUpdatedMCObject,
					},
					OldObject: runtime.RawExtension{
						Raw:    MCObjectBytes,
						Object: MCObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "MemberCluster",
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "test-user", []string{"test-group"}, admissionv1.Update, "MemberCluster", "", types.NamespacedName{Name: "test-mc"})),
		},
		"deny update of member cluster spec by non whitelisted user ": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw:    specUpdatedMCObjectBytes,
						Object: specUpdatedMCObject,
					},
					OldObject: runtime.RawExtension{
						Raw:    MCObjectBytes,
						Object: MCObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "MemberCluster",
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user1"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "test-user", []string{"test-group"}, admissionv1.Update, "MemberCluster", "", types.NamespacedName{Name: "test-mc"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleMemberCluster(testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestHandleNamespace(t *testing.T) {
	testCases := map[string]struct {
		req               admission.Request
		resourceValidator fleetResourceValidator
		wantResponse      admission.Response
	}{
		"allow user to modify non-reserved namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-namespace",
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed("namespace name doesn't begin with fleet, fleet-member, kube prefixes so we allow all operations on these namespaces"),
		},
		"deny user not in system:masters group to modify namespace named fleet-member": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "fleet-member",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Namespace",
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Denied("request is trying to modify a namespace called fleet-member which is not allowed"),
		},
		"deny user not in system:masters group to modify namespace named fleet-member prefixed namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "fleet-member-test-mc",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Namespace",
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "testUser", []string{"testGroup"}, admissionv1.Create, "Namespace", "", types.NamespacedName{Name: "fleet-member-test-mc"})),
		},
		"deny user not in system:masters group to modify namespace named fleet": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "fleet",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Namespace",
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Denied("request is trying to modify a namespace called fleet which is not allowed"),
		},
		"deny user not in system:masters group to modify fleet-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "fleet-system",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Namespace",
					},
					Operation: admissionv1.Update,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "testUser", []string{"testGroup"}, admissionv1.Update, "Namespace", "", types.NamespacedName{Name: "fleet-system"})),
		},
		"deny user not in system:masters group to modify namespace named kube": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "kube",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Namespace",
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Denied("request is trying to modify a namespace called kube which is not allowed"),
		},
		"deny user not in system:masters group to modify kube-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "kube-system",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Namespace",
					},
					Operation: admissionv1.Delete,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "testUser", []string{"testGroup"}, admissionv1.Delete, "Namespace", "", types.NamespacedName{Name: "kube-system"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleNamespace(testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestHandleFleetMemberNamespacedResource(t *testing.T) {
	mockClient := &test.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			if key.Name == mcName {
				o := obj.(*fleetv1alpha1.MemberCluster)
				*o = fleetv1alpha1.MemberCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: mcName,
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
		req               admission.Request
		resourceValidator fleetResourceValidator
		wantResponse      admission.Response
	}{
		"allow user not in system:masters group with create in non-fleet member cluster namespace": {
			req: admission.Request{
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
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed("namespace name doesn't begin with fleet-member prefix so we allow all operations on these namespaces for the request object"),
		},
		"allow user in system:masters group with update in fleet member cluster namespace": {
			req: admission.Request{
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
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "testUser", []string{"system:masters"}, admissionv1.Update, "Work", "", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with update in fleet member cluster namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-mc",
					Namespace: "fleet-member-test-mc",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "InternalMemberCluster",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-identity",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-identity", []string{"system:authenticated"}, admissionv1.Update, "InternalMemberCluster", "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow hub-agent-sa in MC identity with create": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-mc",
					Namespace: "fleet-member-test-mc",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "InternalMemberCluster",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "system:serviceaccount:fleet-system:hub-agent-sa",
						Groups:   []string{"system:serviceaccounts"},
					},
					Operation: admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*fleetv1alpha1.MemberCluster)
						*o = fleetv1alpha1.MemberCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name: mcName,
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
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "system:serviceaccount:fleet-system:hub-agent-sa", []string{"system:serviceaccounts"}, admissionv1.Create, "InternalMemberCluster", "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow request if get MC failed with internal server error": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-work",
					Namespace: "fleet-member-test-mc1",
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
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedGetMCFailed, "testUser", []string{"testGroup"}, admissionv1.Update, "Work", "", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc1"})),
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleFleetMemberNamespacedResource(context.Background(), testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestHandleEvent(t *testing.T) {
	mockClient := &test.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			if key.Name == mcName {
				o := obj.(*fleetv1alpha1.MemberCluster)
				*o = fleetv1alpha1.MemberCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: mcName,
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
		req               admission.Request
		resourceValidator fleetResourceValidator
		wantResponse      admission.Response
	}{
		"allow user in MC identity to create in fleet member namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-event",
					Namespace: "fleet-member-test-mc",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Event",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-identity",
						Groups:   []string{"test-group"},
					},
					Operation: admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "test-identity", []string{"test-group"}, admissionv1.Create, "Event", "", types.NamespacedName{Name: "test-event", Namespace: "fleet-member-test-mc"})),
		},
		"allow hub-agent-sa in MC identity with create in fleet member namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-event",
					Namespace: "fleet-member-test-mc",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Event",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "system:serviceaccount:fleet-system:hub-agent-sa",
						Groups:   []string{"system:serviceaccounts"},
					},
					Operation: admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*fleetv1alpha1.MemberCluster)
						*o = fleetv1alpha1.MemberCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name: mcName,
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
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "system:serviceaccount:fleet-system:hub-agent-sa", []string{"system:serviceaccounts"}, admissionv1.Create, "Event", "", types.NamespacedName{Name: "test-event", Namespace: "fleet-member-test-mc"})),
		},
		"deny user in system:masters group with create in a fleet member cluster namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-event",
					Namespace: "fleet-member-test-mc",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Event",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation: admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf(resourceDeniedFormat, "testUser", []string{"system:masters"}, admissionv1.Create, "Event", "", types.NamespacedName{Name: "test-event", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in system:masters group with create in fleet-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-event",
					Namespace: "fleet-system",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Event",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "testUser", []string{"system:masters"}, admissionv1.Create, "Event", "", types.NamespacedName{Name: "test-event", Namespace: "fleet-system"})),
		},
		"deny user not is system:masters group with create in fleet-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-event",
					Namespace: "te",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Event",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed("namespace name for this event is not a reserved namespace so we allow all operations for events on these namespaces"),
		},
		"allow user in system:masters group with create in kube-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-event",
					Namespace: "kube-system",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Event",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedFormat, "testUser", []string{"system:masters"}, admissionv1.Create, "Event", "", types.NamespacedName{Name: "test-event", Namespace: "kube-system"})),
		},
		"allow user not in system:masters group with create in non reserved namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-event",
					Namespace: "test-ns",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Event",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed("namespace name for this event is not a reserved namespace so we allow all operations for events on these namespaces"),
		},
		"allow request if get MC failed with internal server error": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "test-event",
					Namespace: "fleet-member",
					RequestKind: &metav1.GroupVersionKind{
						Kind: "Event",
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation: admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(resourceAllowedGetMCFailed, "testUser", []string{"system:masters"}, admissionv1.Create, "Event", "", types.NamespacedName{Name: "test-event", Namespace: "fleet-member"})),
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleEvent(context.Background(), testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
