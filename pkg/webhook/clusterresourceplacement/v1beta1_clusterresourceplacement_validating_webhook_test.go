package clusterresourceplacement

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/validator"
	testinformer "github.com/kubefleet-dev/kubefleet/test/utils/informer"
)

var (
	resourceSelector = placementv1beta1.ResourceSelectorTerm{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "ClusterRole",
		Name:    "test-cluster-role",
	}
	errString = "the rollout Strategy field  is invalid: maxUnavailable must be greater than or equal to 0, got `-1`"
)

func TestHandle(t *testing.T) {
	invalidCRPObject := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-crp",
			Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
		},
		Spec: placementv1beta1.PlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: -1},
				},
			},
		},
	}

	invalidCRPObjectDeleting := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-crp",
			Finalizers:        []string{placementv1beta1.PlacementCleanupFinalizer},
			DeletionTimestamp: ptr.To(metav1.NewTime(time.Now())),
		},
		Spec: placementv1beta1.PlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: -1},
					MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
				},
			},
		},
	}

	invalidCRPObjectDeletingFinalizersRemoved := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-crp",
			Finalizers:        []string{},
			DeletionTimestamp: ptr.To(metav1.NewTime(time.Now())),
		},
		Spec: placementv1beta1.PlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: -1},
				},
			},
		},
	}

	invalidCRPObjectFinalizersRemoved := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-crp",
			Finalizers: []string{},
		},
		Spec: placementv1beta1.PlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: -1},
				},
			},
		},
	}

	validCRPObject := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-crp",
			Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
		},
		Spec: placementv1beta1.PlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
			},
		},
	}

	validCRPObjectWithTolerations := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
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
			Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
		},
		Spec: placementv1beta1.PlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 2},
				},
			},
		},
	}

	updatedValidSpecCRPObjectDeletingFinalizerRemoved := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-crp",
			Finalizers:        []string{},
			DeletionTimestamp: ptr.To(metav1.NewTime(time.Now())),
		},
		Spec: placementv1beta1.PlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 2},
				},
			},
		},
	}

	updatedLabelInvalidCRPObject := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-crp",
			Labels:     map[string]string{"key1": "value1"},
			Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
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
		Spec: placementv1beta1.PlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(2)),
			},
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
			},
		},
	}

	validCRPObjectBytes, err := json.Marshal(validCRPObject)
	assert.Nil(t, err)
	invalidCRPObjectBytes, err := json.Marshal(invalidCRPObject)
	assert.Nil(t, err)
	invalidCRPObjectDeletingFinalizersRemovedBytes, err := json.Marshal(invalidCRPObjectDeletingFinalizersRemoved)
	assert.Nil(t, err)
	invalidCRPObjectFinalizersRemovedBytes, err := json.Marshal(invalidCRPObjectFinalizersRemoved)
	assert.Nil(t, err)
	invalidCRPObjectDeletingBytes, err := json.Marshal(invalidCRPObjectDeleting)
	assert.Nil(t, err)
	updatedValidSpecCRPObjectBytes, err := json.Marshal(updatedValidSpecCRPObject)
	assert.Nil(t, err)
	updatedValidSpecCRPObjectDeletingFinalizerRemovedBytes, err := json.Marshal(updatedValidSpecCRPObjectDeletingFinalizerRemoved)
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
			wantResponse: admission.Allowed(fmt.Sprintf(validator.AllowModifyFmt, "CRP")),
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
			wantResponse: admission.Denied(fmt.Sprintf(validator.DenyCreateUpdateInvalidFmt, "CRP", errString)),
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
			wantResponse: admission.Allowed(fmt.Sprintf(validator.AllowModifyFmt, "CRP")),
		},
		"allow CRP update - invalid old CRP object, invalid new CRP is deleting, finalizer removed": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					OldObject: runtime.RawExtension{
						Raw:    invalidCRPObjectBytes,
						Object: invalidCRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    invalidCRPObjectDeletingFinalizersRemovedBytes,
						Object: invalidCRPObjectDeletingFinalizersRemoved,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validator.AllowUpdateOldInvalidFmt, "CRP")),
		},
		"allow CRP update - invalid old CRP object, valid new CRP is deleting, finalizer removed, spec updated": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					OldObject: runtime.RawExtension{
						Raw:    invalidCRPObjectBytes,
						Object: invalidCRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    updatedValidSpecCRPObjectDeletingFinalizerRemovedBytes,
						Object: updatedValidSpecCRPObjectDeletingFinalizerRemoved,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validator.AllowUpdateOldInvalidFmt, "CRP")),
		},
		"deny CRP update - invalid old CRP, invalid new CRP is not deleting, finalizer removed": {
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
			wantResponse: admission.Denied(fmt.Sprintf(validator.DenyUpdateOldInvalidFmt, "CRP", errString)),
		},
		"allow CRP update - invalid old CRP, invalid new CRP is deleting, finalizer not removed": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					OldObject: runtime.RawExtension{
						Raw:    invalidCRPObjectBytes,
						Object: invalidCRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    invalidCRPObjectDeletingBytes,
						Object: invalidCRPObjectDeleting,
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
			wantResponse: admission.Allowed(fmt.Sprintf(validator.AllowUpdateOldInvalidFmt, "CRP")),
		},
		"deny CRP update - invalid old CRP, invalid new CRP, label is updated": {
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
			wantResponse: admission.Denied(fmt.Sprintf(validator.DenyUpdateOldInvalidFmt, "CRP", errString)),
		},
		"deny CRP update - invalid old CRP, valid new CRP, spec updated": {
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
			wantResponse: admission.Denied(fmt.Sprintf(validator.DenyUpdateOldInvalidFmt, "CRP", errString)),
		},
		"deny CRP update - valid old CRP, invalid new CRP, spec updated": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp",
					OldObject: runtime.RawExtension{
						Raw:    validCRPObjectBytes,
						Object: validCRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    invalidCRPObjectBytes,
						Object: invalidCRPObject,
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
			wantResponse: admission.Denied(fmt.Sprintf(validator.DenyCreateUpdateInvalidFmt, "CRP", errString)),
		},
		"deny CRP update - new CRP immutable placement type": {
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
		"deny CRP update - new CRP tolerations updated": {
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
