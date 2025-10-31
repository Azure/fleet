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

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/validation"
)

const (
	mcName                 = "test-mc"
	testClusterResourceID1 = "test-cluster-resource-id-1"
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
					Name: "memberclusters.cluster.kubernetes-fleet.io",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.CRDMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{},
			wantResponse:      admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &utils.CRDMetaGVK, "", types.NamespacedName{Name: "memberclusters.cluster.kubernetes-fleet.io"})),
		},
		"allow white listed user to modify fleet CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "memberclusters.cluster.kubernetes-fleet.io",
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
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Delete, &utils.CRDMetaGVK, "", types.NamespacedName{Name: "memberclusters.cluster.kubernetes-fleet.io"})),
		},
		"deny non system user to modify fleet CRD": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "memberclusters.cluster.kubernetes-fleet.io",
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &utils.CRDMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"test-group"}), admissionv1.Create, &utils.CRDMetaGVK, "", types.NamespacedName{Name: "memberclusters.cluster.kubernetes-fleet.io"})),
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
					Type:   string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
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
					Type:   string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
					Status: metav1.ConditionTrue,
				},
			},
		},
	})
	assert.Nil(t, err)

	scheme := runtime.NewScheme()
	err = clusterv1beta1.AddToScheme(scheme)
	assert.Nil(t, err)
	decoder := admission.NewDecoder(scheme)
	assert.Nil(t, err)

	testCases := map[string]struct {
		req               admission.Request
		resourceValidator fleetResourceValidator
		wantResponse      admission.Response
	}{
		"allow create upstream fleet MC by any user": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: mcObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"test-group"},
					},
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(allowedMessageMemberCluster),
		},
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

		"allow label modification by system:masters when flag is set to true": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: func() []byte {
							updatedMC := &clusterv1beta1.MemberCluster{
								ObjectMeta: metav1.ObjectMeta{
									Name:   "test-mc",
									Labels: map[string]string{"key1": "value1"},
									Annotations: map[string]string{
										"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
									},
								},
							}
							raw, _ := json.Marshal(updatedMC)
							return raw
						}(),
					},
					OldObject: runtime.RawExtension{
						Raw: fleetMCObjectBytes,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "mastersUser",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:                       decoder,
				denyModifyMemberClusterLabels: true,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "mastersUser", utils.GenerateGroupString([]string{"system:masters"}), admissionv1.Update, &utils.MCMetaGVK, "", types.NamespacedName{Name: "test-mc"})),
		},
		"deny label modification by non system:masters user when flag is set to true": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: func() []byte {
							updatedMC := &clusterv1beta1.MemberCluster{
								ObjectMeta: metav1.ObjectMeta{
									Name:   "test-mc",
									Labels: map[string]string{"key1": "value1"},
									Annotations: map[string]string{
										"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
									},
								},
							}
							raw, _ := json.Marshal(updatedMC)
							return raw
						}(),
					},
					OldObject: runtime.RawExtension{
						Raw: func() []byte {
							oldMC := &clusterv1beta1.MemberCluster{
								ObjectMeta: metav1.ObjectMeta{
									Name:   "test-mc",
									Labels: map[string]string{"key1": "value2"},
									Annotations: map[string]string{
										"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
									},
								},
							}
							raw, _ := json.Marshal(oldMC)
							return raw
						}(),
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "nonSystemMastersUser",
						Groups:   []string{"system:authenticated"},
					},
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:                       decoder,
				denyModifyMemberClusterLabels: true,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.DeniedModifyMemberClusterLabels)),
		},
		"deny label modification by fleet agent when flag is set to true": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: func() []byte {
							updatedMC := &clusterv1beta1.MemberCluster{
								ObjectMeta: metav1.ObjectMeta{
									Name:   "test-mc",
									Labels: map[string]string{"key1": "value1"},
									Annotations: map[string]string{
										"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
									},
								},
							}
							raw, _ := json.Marshal(updatedMC)
							return raw
						}(),
					},
					OldObject: runtime.RawExtension{
						Raw: func() []byte {
							oldMC := &clusterv1beta1.MemberCluster{
								ObjectMeta: metav1.ObjectMeta{
									Name:   "test-mc",
									Labels: map[string]string{"key1": "value2"},
									Annotations: map[string]string{
										"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
									},
								},
							}
							raw, _ := json.Marshal(oldMC)
							return raw
						}(),
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "system:serviceaccount:fleet-system:hub-agent-sa",
						Groups:   []string{"system:serviceaccounts"},
					},
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:                       decoder,
				denyModifyMemberClusterLabels: true,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.DeniedModifyMemberClusterLabels)),
		},
		"allow label modification by non system:masters resources when flag is set to false": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-mc",
					Object: runtime.RawExtension{
						Raw: func() []byte {
							updatedMC := &clusterv1beta1.MemberCluster{
								ObjectMeta: metav1.ObjectMeta{
									Name:   "test-mc",
									Labels: map[string]string{"key1": "value1"},
									Annotations: map[string]string{
										"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
									},
								},
							}
							raw, _ := json.Marshal(updatedMC)
							return raw
						}(),
					},
					OldObject: runtime.RawExtension{
						Raw: func() []byte {
							oldMC := &clusterv1beta1.MemberCluster{
								ObjectMeta: metav1.ObjectMeta{
									Name:   "test-mc",
									Labels: map[string]string{"key1": "value2"},
									Annotations: map[string]string{
										"fleet.azure.com/cluster-resource-id": "test-cluster-resource-id",
									},
								},
							}
							raw, _ := json.Marshal(oldMC)
							return raw
						}(),
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "nonMastersUser",
						Groups:   []string{"system:authenticated"},
					},
					RequestKind: &utils.MCMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: fleetResourceValidator{
				decoder:                       decoder,
				denyModifyMemberClusterLabels: false,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat,
				"nonMastersUser", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.MCMetaGVK, "",
				types.NamespacedName{Name: "test-mc"})),
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
				client: mockClient,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validation.ResourceAllowedFormat, "test-identity", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Create, &utils.InternalServiceExportMetaGVK, "", types.NamespacedName{Name: "test-ise", Namespace: "fleet-member-test-mc"})),
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
				client: mockClient,
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
				client: mockClient,
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
				client: mockClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString([]string{"system:authenticated"}), admissionv1.Update, &utils.IMCMetaGVK, "", types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"})),
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
				client: mockClient,
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
				client: mockClient,
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
		"allow any user to modify non-reserved namespace": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-namespace",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &utils.NamespaceMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Allowed(allowedMessageNonReservedNamespace),
		},
		"allow any user to modify non-reserved namespace without fleet- prefix": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "fleettest",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &utils.NamespaceMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Allowed(allowedMessageNonReservedNamespace),
		},
		"allow any user to modify non-reserved namespace without kube- prefix": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "kubetest",
					UserInfo: authenticationv1.UserInfo{
						Username: "testUser",
						Groups:   []string{"testGroup"},
					},
					RequestKind: &utils.NamespaceMetaGVK,
					Operation:   admissionv1.Delete,
				},
			},
			wantResponse: admission.Allowed(allowedMessageNonReservedNamespace),
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
