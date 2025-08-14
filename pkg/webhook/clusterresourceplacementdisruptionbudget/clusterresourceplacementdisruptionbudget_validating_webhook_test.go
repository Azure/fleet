/*
Copyright 2025 The KubeFleet Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package clusterresourceplacementdisruptionbudget

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

func TestHandle(t *testing.T) {
	validCRPDBObject := &placementv1beta1.ClusterResourcePlacementDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pick-all-crp",
		},
		Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
			MaxUnavailable: nil,
			MinAvailable:   nil,
		},
	}
	validCRPDBObjectPickNCRP := &placementv1beta1.ClusterResourcePlacementDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: "crp-pickn",
		},
		Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
			MaxUnavailable: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 1,
			},
			MinAvailable: nil,
		},
	}
	invalidCRPDBObjectMinAvailablePercentage := &placementv1beta1.ClusterResourcePlacementDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pick-all-crp",
		},
		Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
			MaxUnavailable: nil,
			MinAvailable: &intstr.IntOrString{
				Type:   intstr.String,
				StrVal: "50%",
			},
		},
	}
	invalidCRPDBObjectMaxUnavailablePercentage := &placementv1beta1.ClusterResourcePlacementDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pick-all-crp",
		},
		Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
			MaxUnavailable: &intstr.IntOrString{
				Type:   intstr.String,
				StrVal: "50%",
			},
			MinAvailable: nil,
		},
	}
	invalidCRPDBObjectMaxUnavailableInteger := &placementv1beta1.ClusterResourcePlacementDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pick-all-crp",
		},
		Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
			MaxUnavailable: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 2,
			},
			MinAvailable: nil,
		},
	}
	validCRPDBObjectCRPNotFound := &placementv1beta1.ClusterResourcePlacementDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: "does-not-exist",
		},
		Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
			MaxUnavailable: nil,
			MinAvailable:   nil,
		},
	}

	validCRPDBObjectBytes, err := json.Marshal(validCRPDBObject)
	assert.Nil(t, err)
	validCRPDBObjectPickNCRPBytes, err := json.Marshal(validCRPDBObjectPickNCRP)
	assert.Nil(t, err)
	invalidCRPDBObjectMinAvailablePercentageBytes, err := json.Marshal(invalidCRPDBObjectMinAvailablePercentage)
	assert.Nil(t, err)
	invalidCRPDBObjectMaxUnavailablePercentageBytes, err := json.Marshal(invalidCRPDBObjectMaxUnavailablePercentage)
	assert.Nil(t, err)
	invalidCRPDBObjectMaxUnavailableIntegerBytes, err := json.Marshal(invalidCRPDBObjectMaxUnavailableInteger)
	assert.Nil(t, err)
	validCRPDBObjectCRPNotFoundBytes, err := json.Marshal(validCRPDBObjectCRPNotFound)
	assert.Nil(t, err)

	validCRP := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pick-all-crp",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
		},
	}
	validCRPPickN := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "crp-pickn",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(1)),
			},
		},
	}
	invalidCRPPickFixed := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "crp-pickfixed",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				ClusterNames:  []string{"cluster1", "cluster2"},
			},
		},
	}

	objects := []client.Object{validCRP, validCRPPickN, invalidCRPPickFixed}
	scheme := runtime.NewScheme()
	err = placementv1beta1.AddToScheme(scheme)
	assert.Nil(t, err)
	decoder := admission.NewDecoder(scheme)
	assert.Nil(t, err)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	testCases := map[string]struct {
		req               admission.Request
		resourceValidator clusterResourcePlacementDisruptionBudgetValidator
		wantResponse      admission.Response
	}{
		"allow CRPDB create": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "pick-all-crp",
					Object: runtime.RawExtension{
						Raw:    validCRPDBObjectBytes,
						Object: validCRPDBObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementDisruptionBudgetMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: clusterResourcePlacementDisruptionBudgetValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Allowed("clusterResourcePlacementDisruptionBudget has valid fields"),
		},
		"allow CRPDB create - PickN CRP": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "crp-pickn",
					Object: runtime.RawExtension{
						Raw:    validCRPDBObjectPickNCRPBytes,
						Object: validCRPDBObjectPickNCRP,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementDisruptionBudgetMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: clusterResourcePlacementDisruptionBudgetValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Allowed("clusterResourcePlacementDisruptionBudget has valid fields"),
		},
		"deny CRPDB create - MinAvailable as percentage": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "pick-all-crp",
					Object: runtime.RawExtension{
						Raw:    invalidCRPDBObjectMinAvailablePercentageBytes,
						Object: invalidCRPDBObjectMinAvailablePercentage,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: clusterResourcePlacementDisruptionBudgetValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf("cluster resource placement policy type PickAll is not supported with min available as a percentage %v", invalidCRPDBObjectMinAvailablePercentage.Spec.MinAvailable)),
		},
		"deny CRPDB create - MaxUnavailable as percentage": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "pick-all-crp",
					Object: runtime.RawExtension{
						Raw:    invalidCRPDBObjectMaxUnavailablePercentageBytes,
						Object: invalidCRPDBObjectMaxUnavailablePercentage,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementDisruptionBudgetMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: clusterResourcePlacementDisruptionBudgetValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf("cluster resource placement policy type PickAll is not supported with any specified max unavailable %v", invalidCRPDBObjectMaxUnavailablePercentage.Spec.MaxUnavailable)),
		},
		"deny CRPDB create - MaxUnavailable as integer": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "pick-all-crp",
					Object: runtime.RawExtension{
						Raw:    invalidCRPDBObjectMaxUnavailableIntegerBytes,
						Object: invalidCRPDBObjectMaxUnavailableInteger,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementDisruptionBudgetMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: clusterResourcePlacementDisruptionBudgetValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf("cluster resource placement policy type PickAll is not supported with any specified max unavailable %v", invalidCRPDBObjectMaxUnavailableInteger.Spec.MaxUnavailable)),
		},
		"allow CRPDB create - CRP not found": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "does-not-exist",
					Object: runtime.RawExtension{
						Raw:    validCRPDBObjectCRPNotFoundBytes,
						Object: validCRPDBObjectCRPNotFound,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementDisruptionBudgetMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: clusterResourcePlacementDisruptionBudgetValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Allowed("Associated clusterResourcePlacement object for clusterResourcePlacementDisruptionBudget is not found"),
		},
		"deny CRPDB update - CRPDB valid to invalid (MinAvailable as Percentage)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "pick-all-crp",
					Object: runtime.RawExtension{
						Raw:    invalidCRPDBObjectMinAvailablePercentageBytes,
						Object: invalidCRPDBObjectMinAvailablePercentage,
					},
					OldObject: runtime.RawExtension{
						Raw:    validCRPDBObjectBytes,
						Object: validCRPDBObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementDisruptionBudgetMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: clusterResourcePlacementDisruptionBudgetValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf("cluster resource placement policy type PickAll is not supported with min available as a percentage %v", invalidCRPDBObjectMinAvailablePercentage.Spec.MinAvailable)),
		},
		"deny CRPDB update - CRPDB valid to invalid (MaxUnavailable as Integer)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "pick-all-crp",
					Object: runtime.RawExtension{
						Raw:    invalidCRPDBObjectMaxUnavailableIntegerBytes,
						Object: invalidCRPDBObjectMaxUnavailableInteger,
					},
					OldObject: runtime.RawExtension{
						Raw:    validCRPDBObjectBytes,
						Object: validCRPDBObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementDisruptionBudgetMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: clusterResourcePlacementDisruptionBudgetValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf("cluster resource placement policy type PickAll is not supported with any specified max unavailable %v", invalidCRPDBObjectMaxUnavailableInteger.Spec.MaxUnavailable)),
		},
		"deny CRPDB update - CRPDB valid to invalid (MaxUnavailable as Percentage)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "pick-all-crp",
					Object: runtime.RawExtension{
						Raw:    invalidCRPDBObjectMaxUnavailablePercentageBytes,
						Object: invalidCRPDBObjectMaxUnavailablePercentage,
					},
					OldObject: runtime.RawExtension{
						Raw:    validCRPDBObjectBytes,
						Object: validCRPDBObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementDisruptionBudgetMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: clusterResourcePlacementDisruptionBudgetValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Denied(fmt.Sprintf("cluster resource placement policy type PickAll is not supported with any specified max unavailable %v", invalidCRPDBObjectMaxUnavailablePercentage.Spec.MaxUnavailable)),
		},
		"allow CRPDB update - CRPDB valid (PickAll CRP to PickN CRP)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "pick-n-crp",
					Object: runtime.RawExtension{
						Raw:    validCRPDBObjectPickNCRPBytes,
						Object: validCRPDBObjectPickNCRP,
					},
					OldObject: runtime.RawExtension{
						Raw:    validCRPDBObjectBytes,
						Object: validCRPDBObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementDisruptionBudgetMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceValidator: clusterResourcePlacementDisruptionBudgetValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Allowed("clusterResourcePlacementDisruptionBudget has valid fields"),
		},
		"allow CRPDB update - CRPDB valid (PickAll CRP to not found CRP)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "does-not-exist",
					Object: runtime.RawExtension{
						Raw:    validCRPDBObjectCRPNotFoundBytes,
						Object: validCRPDBObjectCRPNotFound,
					},
					OldObject: runtime.RawExtension{
						Raw:    validCRPDBObjectBytes,
						Object: validCRPDBObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementDisruptionBudgetMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: clusterResourcePlacementDisruptionBudgetValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Allowed("Associated clusterResourcePlacement object for clusterResourcePlacementDisruptionBudget is not found"),
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.Handle(context.Background(), testCase.req)
			if diff := cmp.Diff(testCase.wantResponse, gotResult); diff != "" {
				t.Errorf("ClusterResourcePlacementDisruptionBudgetValidator Handle() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
