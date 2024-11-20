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
	mcName                 = "test-mc"
	testClusterResourceID1 = "test-cluster-resource-id-1"
	testClusterResourceID2 = "test-cluster-resource-id-2"
	testLocation           = "test-location"

	fleetClusterResourceIsAnnotationKey = "fleet.azure.com/cluster-resource-id"
	fleetLocationAnnotationKey          = "fleet.azure.com/location"
)

func TestHandleCRD(t *testing.T) {
	testCases := map[string]struct {
		req               admission.Request
		resourceValidator fleetResourceValidator
		wantResponse      admission.Response
	}{
		"allow non system user to modify fleet unrelated CRD": {
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
		"deny non system user to modify fleet CRD": {
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
	MCObjectBytes, err := json.Marshal(&fleetv1alpha1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
		},
	})
	assert.Nil(t, err)
	labelUpdatedMCObjectBytes, err := json.Marshal(&fleetv1alpha1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-mc",
			Labels: map[string]string{"test-key": "test-value"},
		},
	})
	assert.Nil(t, err)
	annotationUpdatedMCObjectBytes, err := json.Marshal(&fleetv1alpha1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-mc",
			Annotations: map[string]string{"test-key": "test-value"},
		},
	})
	assert.Nil(t, err)
	specUpdatedMCObjectBytes, err := json.Marshal(&fleetv1alpha1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
		},
		Spec: fleetv1alpha1.MemberClusterSpec{
			State: fleetv1alpha1.ClusterStateLeave,
		},
	})
	assert.Nil(t, err)
	statusUpdatedMCObjectBytes, err := json.Marshal(&fleetv1alpha1.MemberCluster{
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
	})
	assert.Nil(t, err)

	scheme := runtime.NewScheme()
	err = fleetv1alpha1.AddToScheme(scheme)
	assert.Nil(t, err)
	decoder := admission.NewDecoder(scheme)
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
						Raw: labelUpdatedMCObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.MCV1Alpha1MetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Create, &utils.MCV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
		"allow non system user to modify MC labels": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: labelUpdatedMCObjectBytes,
					},
					OldObject: runtime.RawExtension{
						Raw: MCObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &utils.MCV1Alpha1MetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.MCV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
		"allow non system user to modify MC annotations": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: annotationUpdatedMCObjectBytes,
					},
					OldObject: runtime.RawExtension{
						Raw: MCObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &utils.MCV1Alpha1MetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.MCV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
		"allow system:masters group user to modify MC spec": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: specUpdatedMCObjectBytes,
					},
					OldObject: runtime.RawExtension{
						Raw: MCObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.MCV1Alpha1MetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &utils.MCV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
		"allow system:masters group user to modify MC status": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: statusUpdatedMCObjectBytes,
					},
					OldObject: runtime.RawExtension{
						Raw: MCObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.MCV1Alpha1MetaGVK,
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &utils.MCV1Alpha1MetaGVK, "status", types.NamespacedName{Name: "test-mc"})),
		},
		"allow whitelisted user to modify MC status": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: statusUpdatedMCObjectBytes,
					},
					OldObject: runtime.RawExtension{
						Raw: MCObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &utils.MCV1Alpha1MetaGVK,
					Operation:   admissionv1.Update,
					SubResource: "status",
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user"},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.MCV1Alpha1MetaGVK, "status", types.NamespacedName{Name: "test-mc"})),
		},
		"deny update of member cluster spec by non system user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: specUpdatedMCObjectBytes,
					},
					OldObject: runtime.RawExtension{
						Raw: MCObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &utils.MCV1Alpha1MetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.MCV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
		"deny update of member cluster spec by non whitelisted user ": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: specUpdatedMCObjectBytes,
					},
					OldObject: runtime.RawExtension{
						Raw: MCObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &utils.MCV1Alpha1MetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:          decoder,
				whiteListedUsers: []string{"test-user1"},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Update, &utils.MCV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
		// added as UT since testing this case as an E2E requires
		// creating a new user called aks-support in our test environment.
		"allow delete of member cluster by aks-support user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					OldObject: runtime.RawExtension{
						Raw: MCObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "aks-support",
						Groups:   []string{"system:authenticated"},
					},
					RequestKind: &utils.MCV1Alpha1MetaGVK,
					Operation:   admissionv1.Delete,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "aks-support", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Delete, &utils.MCV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
	// The UTs for this function are less because most of the cases are covered in E2Es in fleet_guard_rail_test.go.
	// The E2Es also cover actual behavior changes to the requests received by the webhook.
	fleetMCObjectBytes, err := json.Marshal(&clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
			Annotations: map[string]string{
				fleetClusterResourceIsAnnotationKey: testClusterResourceID1,
				fleetLocationAnnotationKey:          testLocation,
			},
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Kind: "User",
				Name: "test-user",
			},
		},
	})
	assert.Nil(t, err)
	mcObjectBytes, err := json.Marshal(&clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Kind: "User",
				Name: "test-user",
			},
		},
	})
	assert.Nil(t, err)
	specUpdatedFleetMCObjectBytes, err := json.Marshal(&clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
			Annotations: map[string]string{
				fleetClusterResourceIsAnnotationKey: testClusterResourceID1,
				fleetLocationAnnotationKey:          testLocation,
			},
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Kind: "User",
				Name: "test-user",
			},
			HeartbeatPeriodSeconds: 30,
		},
	})
	assert.Nil(t, err)
	statusUpdatedFleetMCObjectBytes, err := json.Marshal(&clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
			Annotations: map[string]string{
				fleetClusterResourceIsAnnotationKey: testClusterResourceID1,
				fleetLocationAnnotationKey:          testLocation,
			},
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Kind: "User",
				Name: "test-user",
			},
		},
		Status: clusterv1beta1.MemberClusterStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
					Status: metav1.ConditionTrue,
				},
			},
		},
	})
	assert.Nil(t, err)
	statusUpdatedMCObjectBytes, err := json.Marshal(&clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "MemberCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mc",
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Kind: "User",
				Name: "test-user",
			},
		},
		Status: clusterv1beta1.MemberClusterStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
					Status: metav1.ConditionTrue,
				},
			},
		},
	})
	assert.Nil(t, err)

	scheme := runtime.NewScheme()
	err = fleetv1alpha1.AddToScheme(scheme)
	assert.Nil(t, err)
	decoder := admission.NewDecoder(scheme)
	assert.Nil(t, err)

	testCases := map[string]struct {
		req               admission.Request
		resourceValidator fleetResourceValidator
		wantResponse      admission.Response
	}{
		"deny update of fleet MC spec by non whitelisted user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: specUpdatedFleetMCObjectBytes,
					},
					OldObject: runtime.RawExtension{
						Raw: fleetMCObjectBytes,
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
		"allow whitelisted user to modify fleet MC status": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: statusUpdatedFleetMCObjectBytes,
					},
					OldObject: runtime.RawExtension{
						Raw: fleetMCObjectBytes,
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
		"allow whitelisted user to modify upstream MC status": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: statusUpdatedMCObjectBytes,
					},
					OldObject: runtime.RawExtension{
						Raw: mcObjectBytes,
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
		// added as UT since testing this case as an E2E requires
		// creating a new user called aks-support in our test environment.
		"allow delete for fleet MC by aks-support user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					OldObject: runtime.RawExtension{
						Raw: fleetMCObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "aks-support",
						Groups:   []string{"system:authenticated"},
					},
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Delete,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "aks-support", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Delete, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
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
					RequestKind: &utils.IMCV1Alpha1MetaGVK,
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
					RequestKind: &utils.IMCV1Alpha1MetaGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "system:serviceaccount:fleet-system:hub-agent-sa", utils.GenerateGroupString([]string{"system:serviceaccounts"}), admissionv1.Create, &utils.IMCV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
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
					RequestKind: &utils.WorkV1Alpha1MetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"system:masters"},
					},
					Operation: admissionv1.Update,
				},
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "testUser", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &utils.WorkV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with update in fleet member cluster namespace with v1alpha1 IMC": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.IMCV1Alpha1MetaGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.IMCV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow user in MC identity with update in fleet member cluster namespace with v1alpha1 Work": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.WorkV1Alpha1MetaGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.WorkV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
		},
		"allow request if get MC failed with internal server error with v1alpha1 Work": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-work",
					Namespace:   "fleet-member-test-mc1",
					RequestKind: &utils.WorkV1Alpha1MetaGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedGetMCFailed, "testUser", utils.GenerateGroupString([]string{"testGroup"}), admissionv1.Update, &utils.WorkV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc1"})),
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
					RequestKind: &utils.WorkMetaGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.WorkMetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
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
					RequestKind: &utils.WorkV1Alpha1MetaGVK,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedGetMCFailed, "testUser", utils.GenerateGroupString([]string{"testGroup"}), admissionv1.Update, &utils.WorkV1Alpha1MetaGVK, "", types.NamespacedName{Name: "test-work", Namespace: "fleet-member-test-mc1"})),
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
		// added as UT since testing this case as an E2E requires
		// creating a new user called aks-support in our test environment.
		"allow delete on v1beta1 IMC in fleet-member namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:        "test-mc",
					Namespace:   "fleet-member-test-mc",
					RequestKind: &utils.IMCMetaGVK,
					UserInfo: authenticationv1.UserInfo{
						Username: "aks-support",
						Groups:   []string{"system:authenticated"},
					},
					Operation: admissionv1.Delete,
				},
			},
			resourceValidator: fleetResourceValidator{
				client:            mockClient,
				isFleetV1Beta1API: true,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "aks-support", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Delete, &utils.IMCMetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
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
