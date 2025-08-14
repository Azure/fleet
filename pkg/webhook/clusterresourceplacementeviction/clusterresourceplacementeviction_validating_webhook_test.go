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

package clusterresourceplacementeviction

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

func TestHandle(t *testing.T) {
	validCRPEObject := &placementv1beta1.ClusterResourcePlacementEviction{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crpe",
		},
		Spec: placementv1beta1.PlacementEvictionSpec{
			PlacementName: "test-crp",
		},
	}
	validCRPEObjectPlacementNameNotFound := &placementv1beta1.ClusterResourcePlacementEviction{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crpe",
		},
		Spec: placementv1beta1.PlacementEvictionSpec{
			PlacementName: "does-not-exist",
		},
	}
	invalidCRPEObjectCRPDeleting := &placementv1beta1.ClusterResourcePlacementEviction{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crpe",
		},
		Spec: placementv1beta1.PlacementEvictionSpec{
			PlacementName: "crp-deleting",
		},
	}
	invalidCRPEObjectInvalidPlacementType := &placementv1beta1.ClusterResourcePlacementEviction{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crpe",
		},
		Spec: placementv1beta1.PlacementEvictionSpec{
			PlacementName: "crp-pickfixed",
		},
	}

	validCRPEObjectBytes, err := json.Marshal(validCRPEObject)
	assert.Nil(t, err)
	validCRPEObjectPlacementNameNotFoundBytes, err := json.Marshal(validCRPEObjectPlacementNameNotFound)
	assert.Nil(t, err)
	invalidCRPEObjectCRPDeletingBytes, err := json.Marshal(invalidCRPEObjectCRPDeleting)
	assert.Nil(t, err)
	invalidCRPEObjectInvalidPlacementTypeBytes, err := json.Marshal(invalidCRPEObjectInvalidPlacementType)
	assert.Nil(t, err)

	validCRP := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
		},
	}
	invalidCRPDeleting := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "crp-deleting",
			DeletionTimestamp: &metav1.Time{
				Time: time.Now().Add(10 * time.Minute),
			},
			Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
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

	objects := []client.Object{validCRP, invalidCRPDeleting, invalidCRPPickFixed}
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
		resourceValidator clusterResourcePlacementEvictionValidator
		wantResponse      admission.Response
	}{
		"allow CRPE create": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crpe",
					Object: runtime.RawExtension{
						Raw:    validCRPEObjectBytes,
						Object: validCRPEObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementEvictionMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: clusterResourcePlacementEvictionValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Allowed("clusterResourcePlacementEviction has valid fields"),
		},
		"allow CRPE create - CRPE object with not found CRP": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crpe",
					Object: runtime.RawExtension{
						Raw:    validCRPEObjectPlacementNameNotFoundBytes,
						Object: validCRPEObjectPlacementNameNotFound,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: clusterResourcePlacementEvictionValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Allowed("Associated clusterResourcePlacement object for clusterResourcePlacementEviction is not found"),
		},
		"deny CRPE create - CRP is deleting": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crpe",
					Object: runtime.RawExtension{
						Raw:    invalidCRPEObjectCRPDeletingBytes,
						Object: invalidCRPEObjectCRPDeleting,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementEvictionMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: clusterResourcePlacementEvictionValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Denied("cluster resource placement crp-deleting is being deleted"),
		},
		"deny CRPE create - CRP with PickFixed placement type": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crpe",
					Object: runtime.RawExtension{
						Raw:    invalidCRPEObjectInvalidPlacementTypeBytes,
						Object: invalidCRPEObjectInvalidPlacementType,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementEvictionMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceValidator: clusterResourcePlacementEvictionValidator{
				decoder: decoder,
				client:  fakeClient,
			},
			wantResponse: admission.Denied("cluster resource placement policy type PickFixed is not supported"),
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.resourceValidator.Handle(context.Background(), testCase.req)
			if diff := cmp.Diff(testCase.wantResponse, gotResult); diff != "" {
				t.Errorf("ClusterResourcePlacementEvictionValidator Handle() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
