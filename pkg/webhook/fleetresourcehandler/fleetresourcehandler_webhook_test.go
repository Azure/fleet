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

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/webhook/validation"
)

const (
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
					RequestKind: &validation.CRDGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{},
			wantResponse:      admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Create, &validation.CRDGVK, "", types.NamespacedName{Name: "test-crd"})),
		},
		"allow user in system:masters group to modify fleet CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "memberclusters.fleet.azure.com",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &validation.CRDGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{},
			wantResponse:      admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &validation.CRDGVK, "", types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
		},
		"allow white listed user to modify fleet CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "memberclusters.fleet.azure.com",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &validation.CRDGVK,
					Operation:   admissionv1.Delete,
				},
			},
			resourceValidator: fleetResourceValidator{
				whiteListedUsers: []string{"test-user"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Delete, &validation.CRDGVK, "", types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
		},
		"deny user not in system:masters group to modify fleet CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "memberclusters.fleet.azure.com",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &validation.CRDGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Create, &validation.CRDGVK, "", types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleCRD(testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestHandleV1Alpha1MemberCluster(t *testing.T) {
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
					RequestKind: &validation.V1Alpha1MCGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Create, &validation.V1Alpha1MCGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.V1Alpha1MCGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &validation.V1Alpha1MCGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.V1Alpha1MCGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &validation.V1Alpha1MCGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.V1Alpha1MCGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &validation.V1Alpha1MCGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.V1Alpha1MCGVK,
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &validation.V1Alpha1MCGVK, "status", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.V1Alpha1MCGVK,
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &validation.V1Alpha1MCGVK, "status", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.V1Alpha1MCGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &validation.V1Alpha1MCGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.V1Alpha1MCGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user1"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &validation.V1Alpha1MCGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleV1Alpha1MemberCluster(testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestHandleMemberCluster(t *testing.T) {
	MCObject := &clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
		},
	}
	labelUpdatedMCObject := &clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-mc",
			Labels: map[string]string{"test-key": "test-value"},
		},
	}
	annotationUpdatedMCObject := &clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-mc",
			Annotations: map[string]string{"test-key": "test-value"},
		},
	}
	specUpdatedMCObject := &clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			HeartbeatPeriodSeconds: 30,
		},
	}
	statusUpdatedMCObject := &clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
		},
		Status: clusterv1beta1.MemberClusterStatus{
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
					RequestKind: &validation.MCGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Create, &validation.MCGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.MCGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &validation.MCGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.MCGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &validation.MCGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.MCGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &validation.MCGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.MCGVK,
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &validation.MCGVK, "status", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.MCGVK,
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &validation.MCGVK, "status", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.MCGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &validation.MCGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &validation.MCGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user1"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &validation.MCGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleMemberCluster(testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestHandleFleetMemberNamespacedResource(t *testing.T) {
	v1Alpha1MockClient := &test.MockClient{
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
	mockClient := &test.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			if key.Name == mcName {
				o := obj.(*clusterv1beta1.MemberCluster)
				*o = clusterv1beta1.MemberCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: mcName,
					},
					Spec: clusterv1beta1.MemberClusterSpec{
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
		"allow user not in system:masters group with create in non-fleet member cluster namespace with v1alpha1 IMC": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "test-ns",
					RequestKind: &validation.V1Alpha1IMCGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed("namespace name doesn't begin with fleet-member prefix so we allow all operations on these namespaces for the request object"),
		},
		"allow user in system:masters group with update in fleet member cluster namespace with v1alpha1 Work": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-work",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &validation.V1Alpha1WorkGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation: admissionv1.Update,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "testUser", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &validation.V1Alpha1WorkGVK, "", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"deny user in MC identity with create in fleet member cluster namespace with v1alpha1 IMC": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &validation.V1Alpha1IMCGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-identity",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: v1Alpha1MockClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Create, &validation.V1Alpha1IMCGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with update in fleet member cluster namespace with v1alpha1 IMC": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &validation.V1Alpha1IMCGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-identity",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: v1Alpha1MockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &validation.V1Alpha1IMCGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with update in fleet member cluster namespace with v1alpha1 Work": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &validation.V1Alpha1WorkGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-identity",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: v1Alpha1MockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &validation.V1Alpha1WorkGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with update in fleet member cluster namespace with v1beta1 IMC": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &validation.IMCGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &validation.IMCGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with update in fleet member cluster namespace with v1beta1 Work": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &validation.WorkGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &validation.WorkGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"deny user not in MC identity with update in fleet member cluster namespace with v1beta1 IMC": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &validation.IMCGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: mockClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &validation.IMCGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow hub-agent-sa in MC identity with create with v1alpha1 IMC": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &validation.V1Alpha1IMCGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "system:serviceaccount:fleet-system:hub-agent-sa", utils.GenerateGroupString([]string{"system:serviceaccounts"}), admissionv1.Create, &validation.V1Alpha1IMCGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow request if get MC failed with internal server error with v1alpha1 Work": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-work",
					Namespace:   "fleet-member-test-mc1",
					RequestKind: &validation.V1Alpha1WorkGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client: v1Alpha1MockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedGetMCFailed, "testUser", utils.GenerateGroupString([]string{"testGroup"}), admissionv1.Update, &validation.V1Alpha1WorkGVK, "", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc1"})),
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleFleetMemberNamespacedResource(context.Background(), testCase.req)
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
			wantResponse: admission.Allowed("namespace name doesn't begin with fleet/kube prefix so we allow all operations on these namespaces"),
		},
		"deny user not in system:masters group to modify fleet namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "fleet-system",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &validation.NamespaceGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "testUser", utils.GenerateGroupString([]string{"testGroup"}), admissionv1.Update, &validation.NamespaceGVK, "", types.NamespacedName{Name: "fleet-system"})),
		},
		"deny user not in system:masters group to modify kube-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "kube-system",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &validation.NamespaceGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "testUser", utils.GenerateGroupString([]string{"testGroup"}), admissionv1.Update, &validation.NamespaceGVK, "", types.NamespacedName{Name: "kube-system"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleNamespace(testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
