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
	corev1 "k8s.io/api/core/v1"
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
					RequestKind: &utils.CRDMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{},
			wantResponse:      admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Create, &utils.CRDMetaGVK, "", types.NamespacedName{Name: "test-crd"})),
		},
		"allow user in system:masters group to modify fleet CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "memberclusters.fleet.azure.com",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.CRDMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{},
			wantResponse:      admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &utils.CRDMetaGVK, "", types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
		},
		"allow white listed user to modify fleet CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "memberclusters.fleet.azure.com",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &utils.CRDMetaGVK,
					Operation:   admissionv1.Delete,
				},
			},
			resourceValidator: fleetResourceValidator{
				whiteListedUsers: []string{"test-user"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Delete, &utils.CRDMetaGVK, "", types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
		},
		"deny user not in system:masters group to modify fleet CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "memberclusters.fleet.azure.com",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &utils.CRDMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Create, &utils.CRDMetaGVK, "", types.NamespacedName{Name: "memberclusters.fleet.azure.com"})),
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
					RequestKind: &utils.V1Alpha1MCMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Create, &utils.V1Alpha1MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.V1Alpha1MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.V1Alpha1MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.V1Alpha1MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.V1Alpha1MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.V1Alpha1MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &utils.V1Alpha1MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.V1Alpha1MCMetaGVK,
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &utils.V1Alpha1MCMetaGVK, "status", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.V1Alpha1MCMetaGVK,
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.V1Alpha1MCMetaGVK, "status", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.V1Alpha1MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.V1Alpha1MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.V1Alpha1MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user1"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.V1Alpha1MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
	taintUpdatedMCObject := &clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Taints: []clusterv1beta1.Taint{
				{
					Key:    "key1",
					Value:  "value1",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
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
	taintUpdatedMCObjectBytes, err := json.Marshal(taintUpdatedMCObject)
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
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Create, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
		"allow any user to modify MC taints": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw:    taintUpdatedMCObjectBytes,
						Object: taintUpdatedMCObject,
					},
					OldObject: runtime.RawExtension{
						Raw:    MCObjectBytes,
						Object: MCObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &utils.MCMetaGVK, "status", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.MCMetaGVK, "status", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user1"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleMemberCluster(testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}

func TestHandleFleetReservedNamespacedResource(t *testing.T) {
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
					RequestKind: &utils.V1Alpha1IMCMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					Operation: admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed("namespace name doesn't begin with fleet/kube prefix so we allow all operations on these namespaces for the request object"),
		},
		"allow hub-agent-sa in MC identity with create with v1alpha1 IMC": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.V1Alpha1IMCMetaGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "system:serviceaccount:fleet-system:hub-agent-sa", utils.GenerateGroupString([]string{"system:serviceaccounts"}), admissionv1.Create, &utils.V1Alpha1IMCMetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with create in fleet member cluster namespace with internalServiceExport with v1alpha1 client": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-ise",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.InternalServiceExportMetaGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Create, &utils.InternalServiceExportMetaGVK, "", types.NamespacedName{Name: "test-ise", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with create in fleet member cluster namespace with internalServiceExport with v1beta1 client": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-ise",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.InternalServiceExportMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-identity",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				client:            mockClient,
				isFleetV1Beta1API: true,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Create, &utils.InternalServiceExportMetaGVK, "", types.NamespacedName{Name: "test-ise", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in system:masters group with update in fleet member cluster namespace with v1alpha1 Work": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-work",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.V1Alpha1WorkMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation: admissionv1.Update,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "testUser", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &utils.V1Alpha1WorkMetaGVK, "", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with update in fleet member cluster namespace with v1alpha1 IMC": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.V1Alpha1IMCMetaGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.V1Alpha1IMCMetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with update in fleet member cluster namespace with v1alpha1 Work": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.V1Alpha1WorkMetaGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.V1Alpha1WorkMetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow request if get MC failed with internal server error with v1alpha1 Work": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-work",
					Namespace:   "fleet-member-test-mc1",
					RequestKind: &utils.V1Alpha1WorkMetaGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedGetMCFailed, "testUser", utils.GenerateGroupString([]string{"testGroup"}), admissionv1.Update, &utils.V1Alpha1WorkMetaGVK, "", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc1"})),
		},
		"allow user in MC identity with update in fleet member cluster namespace with v1beta1 IMC": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.IMCMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-identity",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client:            mockClient,
				isFleetV1Beta1API: true,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.IMCMetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with update in fleet member cluster namespace with v1beta1 Work": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.WorkV1Beta1MetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-identity",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client:            mockClient,
				isFleetV1Beta1API: true,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.WorkV1Beta1MetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"deny user not in MC identity with update in fleet member cluster namespace with v1beta1 IMC": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.IMCMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client:            mockClient,
				isFleetV1Beta1API: true,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.IMCMetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow request if get MC failed with internal server error with v1beta1 Work": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-work",
					Namespace:   "fleet-member-test-mc1",
					RequestKind: &utils.V1Alpha1WorkMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					Operation: admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				client:            mockClient,
				isFleetV1Beta1API: true,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedGetMCFailed, "testUser", utils.GenerateGroupString([]string{"testGroup"}), admissionv1.Update, &utils.V1Alpha1WorkMetaGVK, "", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc1"})),
		},
		"deny request to create in fleet-system if user is not validated user with endPointSliceExport": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-net-eps",
					Namespace:   "fleet-system",
					RequestKind: &utils.EndpointSliceExportMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					Operation: admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				client:            mockClient,
				isFleetV1Beta1API: true,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "testUser", utils.GenerateGroupString([]string{"testGroup"}), admissionv1.Create, &utils.EndpointSliceExportMetaGVK, "", types.NamespacedName{Name: "test-net-eps", Namespace: "fleet-system"})),
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleFleetReservedNamespacedResource(context.Background(), testCase.req)
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
					RequestKind: &utils.NamespaceMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "testUser", utils.GenerateGroupString([]string{"testGroup"}), admissionv1.Update, &utils.NamespaceMetaGVK, "", types.NamespacedName{Name: "fleet-system"})),
		},
		"deny user not in system:masters group to modify kube-system namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "kube-system",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &utils.NamespaceMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "testUser", utils.GenerateGroupString([]string{"testGroup"}), admissionv1.Update, &utils.NamespaceMetaGVK, "", types.NamespacedName{Name: "kube-system"})),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.handleNamespace(testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
