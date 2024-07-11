package clusterresourceplacement

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/pkg/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/informer"
	"go.goms.io/fleet/pkg/utils/validator"
	testinformer "go.goms.io/fleet/test/utils/informer"
)

var (
	resourceSelector = placementv1beta1.ClusterResourceSelector{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "ClusterRole",
		Name:    "test-cluster-role",
	}
)

func TestHandle(t *testing.T) {
	invalidCRPObject := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-crp",
			Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
				},
			},
		},
	}

	invalidCRPObjectFinalizersRemoved := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-crp",
			Finalizers: []string{},
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
				},
			},
		},
	}

	validCRPObject := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp",
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
			},
		},
	}

	validCRPObjectWithTolerations := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp",
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Tolerations: []placementv1beta1.Toleration{
					{
						Key:   "key1",
						Value: "value1",
					},
				},
			},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
			},
		},
	}

	updatedValidSpecCRPObject := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-crp",
			Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
				},
			},
		},
	}

	updatedInvalidSpecCRPObject := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-crp",
			Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: -1},
				},
			},
		},
	}

	updatedLabelInvalidCRPObject := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-crp",
			Labels:     map[string]string{"key1": "value1"},
			Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
				},
			},
		},
	}

	updatedPlacementTypeCRPObject := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp",
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.Int32(2),
			},
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
			},
		},
	}

	validCRPObjectBytes, err := json.Marshal(validCRPObject)
	assert.Nil(t, err)

	invalidCRPObjectBytes, err := json.Marshal(invalidCRPObject)
	assert.Nil(t, err)

	invalidCRPObjectFinalizersRemovedBytes, err := json.Marshal(invalidCRPObjectFinalizersRemoved)
	assert.Nil(t, err)

	updatedValidSpecCRPObjectBytes, err := json.Marshal(updatedValidSpecCRPObject)
	assert.Nil(t, err)

	updatedInvalidSpecCRPObjectBytes, err := json.Marshal(updatedInvalidSpecCRPObject)
	assert.Nil(t, err)

	updatedLabelInvalidCRPObjectBytes, err := json.Marshal(updatedLabelInvalidCRPObject)
	assert.Nil(t, err)

	updatedPlacementTypeCRPObjectBytes, err := json.Marshal(updatedPlacementTypeCRPObject)
	assert.Nil(t, err)

	validCRPObjectWithTolerationsBytes, err := json.Marshal(validCRPObjectWithTolerations)
	assert.Nil(t, err)

	scheme := runtime.NewScheme()
	err = placementv1beta1.AddToScheme(scheme)
	assert.Nil(t, err)
	decoder := admission.NewDecoder(scheme)
	assert.Nil(t, err)

	testCases := map[string]struct {
		req               admission.Request
		resourceValidator clusterResourcePlacementValidator
		resourceInformer  informer.Manager
		wantResponse      admission.Response
	}{
		"allow CRP create": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					Object: runtime.RawExtension{
						Raw:    validCRPObjectBytes,
						Object: validCRPObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true,
			},
			resourceValidator: clusterResourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed("any user is allowed to modify v1beta1 CRP"),
		},
		"deny CRP create - invalid CRP object": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					Object: runtime.RawExtension{
						Raw:    invalidCRPObjectBytes,
						Object: invalidCRPObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true,
			},
			resourceValidator: clusterResourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied("the rollout Strategy field  is invalid: maxUnavailable must be greater than or equal to 1, got `0`"),
		},
		"allow CRP update - valid update": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					OldObject: runtime.RawExtension{
						Raw:    validCRPObjectBytes,
						Object: validCRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    updatedValidSpecCRPObjectBytes,
						Object: updatedValidSpecCRPObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true,
			},
			resourceValidator: clusterResourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed("any user is allowed to modify v1beta1 CRP"),
		},
		"allow CRP update - invalid old CRP object, finalizer removed": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					OldObject: runtime.RawExtension{
						Raw:    invalidCRPObjectBytes,
						Object: invalidCRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    invalidCRPObjectFinalizersRemovedBytes,
						Object: invalidCRPObjectFinalizersRemoved,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true,
			},
			resourceValidator: clusterResourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed("finalizer is removed"),
		},
		"deny CRP update - invalid old CRP object, finalizer not removed, spec update is valid": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					OldObject: runtime.RawExtension{
						Raw:    invalidCRPObjectBytes,
						Object: invalidCRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    updatedValidSpecCRPObjectBytes,
						Object: updatedValidSpecCRPObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true,
			},
			resourceValidator: clusterResourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied("the rollout Strategy field  is invalid: maxUnavailable must be greater than or equal to 1, got `0`"),
		},
		"deny CRP update - invalid old CRP object, finalizer not removed, spec update is invalid": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					OldObject: runtime.RawExtension{
						Raw:    invalidCRPObjectBytes,
						Object: invalidCRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    updatedInvalidSpecCRPObjectBytes,
						Object: updatedInvalidSpecCRPObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true,
			},
			resourceValidator: clusterResourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied("the rollout Strategy field  is invalid: maxUnavailable must be greater than or equal to 1, got `0`"),
		},
		"deny CRP update - invalid old CRP object, finalizer not removed, label is updated": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					OldObject: runtime.RawExtension{
						Raw:    invalidCRPObjectBytes,
						Object: invalidCRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    updatedLabelInvalidCRPObjectBytes,
						Object: updatedLabelInvalidCRPObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true,
			},
			resourceValidator: clusterResourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied("the rollout Strategy field  is invalid: maxUnavailable must be greater than or equal to 1, got `0`"),
		},
		"deny CRP update - immutable placement type": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					OldObject: runtime.RawExtension{
						Raw:    validCRPObjectBytes,
						Object: validCRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    updatedPlacementTypeCRPObjectBytes,
						Object: updatedPlacementTypeCRPObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true,
			},
			resourceValidator: clusterResourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied("placement type is immutable"),
		},
		"deny CRP update - tolerations updated": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					OldObject: runtime.RawExtension{
						Raw:    validCRPObjectWithTolerationsBytes,
						Object: validCRPObjectWithTolerations,
					},
					Object: runtime.RawExtension{
						Raw:    validCRPObjectBytes,
						Object: validCRPObject,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true,
			},
			resourceValidator: clusterResourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied("tolerations have been updated/deleted, only additions to tolerations are allowed"),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			validator.RestMapper = utils.TestMapper{}
			validator.ResourceInformer = testCase.resourceInformer
			gotResult := testCase.resourceValidator.Handle(context.Background(), testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
