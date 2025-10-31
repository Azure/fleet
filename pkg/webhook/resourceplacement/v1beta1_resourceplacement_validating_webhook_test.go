package resourceplacement

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

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
	"github.com/stretchr/testify/assert"
)

var (
	resourceSelector = placementv1beta1.ResourceSelectorTerm{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
		Name:    "test-deployment",
	}
	errString = "the rollout Strategy field  is invalid: maxUnavailable must be greater than or equal to 0, got `-1`"
)

func TestHandle(t *testing.T) {
	invalidRPObject := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-rp",
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

	invalidRPObjectDeletingFinalizersRemoved := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-rp",
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

	invalidRPObjectFinalizersRemoved := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-rp",
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

	validRPObject := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-rp",
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

	validRPObjectWithTolerations := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-rp",
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

	updatedPlacementTypeRPObject := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-rp",
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

	validRPObjectBytes, err := json.Marshal(validRPObject)
	assert.Nil(t, err)
	invalidRPObjectBytes, err := json.Marshal(invalidRPObject)
	assert.Nil(t, err)
	invalidRPObjectDeletingFinalizersRemovedBytes, err := json.Marshal(invalidRPObjectDeletingFinalizersRemoved)
	assert.Nil(t, err)
	invalidRPObjectFinalizersRemovedBytes, err := json.Marshal(invalidRPObjectFinalizersRemoved)
	assert.Nil(t, err)
	updatedPlacementTypeRPObjectBytes, err := json.Marshal(updatedPlacementTypeRPObject)
	assert.Nil(t, err)
	validRPObjectWithTolerationsBytes, err := json.Marshal(validRPObjectWithTolerations)
	assert.Nil(t, err)

	scheme := runtime.NewScheme()
	err = placementv1beta1.AddToScheme(scheme)
	assert.Nil(t, err)
	decoder := admission.NewDecoder(scheme)
	assert.Nil(t, err)

	testCases := map[string]struct {
		req               admission.Request
		resourceValidator resourcePlacementValidator
		resourceInformer  informer.Manager
		wantResponse      admission.Response
	}{
		"allow RP create": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-rp",
					Object: runtime.RawExtension{
						Raw:    validRPObjectBytes,
						Object: validRPObject,
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
				APIResources:            map[schema.GroupVersionKind]bool{utils.DeploymentGVK: true},
				IsClusterScopedResource: false,
			},
			resourceValidator: resourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validator.AllowModifyFmt, "RP")),
		},
		"deny RP create - invalid RP object": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-rp",
					Object: runtime.RawExtension{
						Raw:    invalidRPObjectBytes,
						Object: invalidRPObject,
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
				APIResources:            map[schema.GroupVersionKind]bool{utils.DeploymentGVK: true},
				IsClusterScopedResource: false,
			},
			resourceValidator: resourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validator.DenyCreateUpdateInvalidFmt, "RP", errString)),
		},
		"allow RP update - invalid old RP object, invalid new RP is deleting, finalizer removed": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-rp",
					OldObject: runtime.RawExtension{
						Raw:    invalidRPObjectBytes,
						Object: invalidRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    invalidRPObjectDeletingFinalizersRemovedBytes,
						Object: invalidRPObjectDeletingFinalizersRemoved,
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
				APIResources:            map[schema.GroupVersionKind]bool{utils.DeploymentGVK: true},
				IsClusterScopedResource: false,
			},
			resourceValidator: resourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed(fmt.Sprintf(validator.AllowUpdateOldInvalidFmt, "RP")),
		},
		"deny RP update - invalid old RP, invalid new RP is not deleting, finalizer removed": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-rp",
					OldObject: runtime.RawExtension{
						Raw:    invalidRPObjectBytes,
						Object: invalidRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    invalidRPObjectFinalizersRemovedBytes,
						Object: invalidRPObjectFinalizersRemoved,
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
				APIResources:            map[schema.GroupVersionKind]bool{utils.DeploymentGVK: true},
				IsClusterScopedResource: false,
			},
			resourceValidator: resourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validator.DenyUpdateOldInvalidFmt, "RP", errString)),
		},
		"deny RP update - valid old RP, invalid new RP, spec updated": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-rp",
					OldObject: runtime.RawExtension{
						Raw:    validRPObjectBytes,
						Object: validRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    invalidRPObjectBytes,
						Object: invalidRPObject,
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
				APIResources:            map[schema.GroupVersionKind]bool{utils.DeploymentGVK: true},
				IsClusterScopedResource: false,
			},
			resourceValidator: resourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied(fmt.Sprintf(validator.DenyCreateUpdateInvalidFmt, "RP", errString)),
		},
		"deny RP update - new RP immutable placement type": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-rp",
					OldObject: runtime.RawExtension{
						Raw:    validRPObjectBytes,
						Object: validRPObject,
					},
					Object: runtime.RawExtension{
						Raw:    updatedPlacementTypeRPObjectBytes,
						Object: updatedPlacementTypeRPObject,
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
				APIResources:            map[schema.GroupVersionKind]bool{utils.DeploymentGVK: true},
				IsClusterScopedResource: false,
			},
			resourceValidator: resourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied("placement type is immutable"),
		},
		"deny RP update - new RP tolerations updated": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-rp",
					OldObject: runtime.RawExtension{
						Raw:    validRPObjectWithTolerationsBytes,
						Object: validRPObjectWithTolerations,
					},
					Object: runtime.RawExtension{
						Raw:    validRPObjectBytes,
						Object: validRPObject,
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
				APIResources:            map[schema.GroupVersionKind]bool{utils.DeploymentGVK: true},
				IsClusterScopedResource: false,
			},
			resourceValidator: resourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Denied("tolerations have been updated/deleted, only additions to tolerations are allowed"),
		},
		"decode error for main object": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-rp",
					Object: runtime.RawExtension{
						Raw: []byte(`{"invalid": "json"`), // Invalid JSON
					},
					Operation: admissionv1.Create,
				},
			},
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.DeploymentGVK: true},
				IsClusterScopedResource: false,
			},
			resourceValidator: resourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Errored(http.StatusBadRequest, fmt.Errorf("couldn't get version/kind; json parse error: unexpected end of JSON input")),
		},
		"decode error for old object during update": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-rp",
					Object: runtime.RawExtension{
						Raw: func() []byte {
							validRP := &placementv1beta1.ResourcePlacement{
								ObjectMeta: metav1.ObjectMeta{Name: "test-rp"},
								Spec: placementv1beta1.PlacementSpec{
									ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
										{Group: "apps", Version: "v1", Kind: "Deployment", Name: "test"},
									},
								},
							}
							validRPBytes, _ := json.Marshal(validRP)
							return validRPBytes
						}(),
					},
					OldObject: runtime.RawExtension{
						Raw: []byte(`{"invalid": "json"`), // Invalid JSON for old object
					},
					Operation: admissionv1.Update,
				},
			},
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.DeploymentGVK: true},
				IsClusterScopedResource: false,
			},
			resourceValidator: resourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Errored(http.StatusBadRequest, fmt.Errorf("couldn't get version/kind; json parse error: unexpected end of JSON input")),
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
