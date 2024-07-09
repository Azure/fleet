package validating

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

	ClusterRoleGVK = schema.GroupVersionKind{
		Group:   rbacv1.GroupName,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    "ClusterRole",
	}

	ClusterRoleGVR = schema.GroupVersionResource{
		Group:    rbacv1.GroupName,
		Version:  rbacv1.SchemeGroupVersion.Version,
		Resource: "clusterroles",
	}
)

// This interface is needed for testMapper abstract class.
type testMapper struct {
	meta.RESTMapper
}

func (m testMapper) RESTMapping(gk schema.GroupKind, _ ...string) (*meta.RESTMapping, error) {
	if gk.Kind == "ClusterRole" {
		return &meta.RESTMapping{
			Resource:         ClusterRoleGVR,
			GroupVersionKind: ClusterRoleGVK,
			Scope:            nil,
		}, nil
	}
	return nil, errors.New("test error: mapping does not exist")
}

func TestHandle(t *testing.T) {
	//invalidCRPObject := &placementv1beta1.ClusterResourcePlacement{
	//	ObjectMeta: metav1.ObjectMeta{
	//		Name: "test-crp",
	//	},
	//	Spec: placementv1beta1.ClusterResourcePlacementSpec{
	//		ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
	//		Strategy: placementv1beta1.RolloutStrategy{
	//			Type: placementv1beta1.RollingUpdateRolloutStrategyType,
	//			RollingUpdate: &placementv1beta1.RollingUpdateConfig{
	//				MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
	//			},
	//		},
	//	},
	//}

	validCRPObject := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp",
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
			},
		},
	}

	//updatedCRPObject := &placementv1beta1.ClusterResourcePlacement{
	//	ObjectMeta: metav1.ObjectMeta{
	//		Name: "test-crp",
	//	},
	//	Spec: placementv1beta1.ClusterResourcePlacementSpec{
	//		ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
	//		Strategy: placementv1beta1.RolloutStrategy{
	//			Type: placementv1beta1.RollingUpdateRolloutStrategyType,
	//			RollingUpdate: &placementv1beta1.RollingUpdateConfig{
	//				MaxUnavailable: placementv1beta1.
	//			},
	//		},
	//	},
	//}

	validCRPObjectBytes, err := json.Marshal(validCRPObject)
	assert.Nil(t, err)

	//invalidCRPObjectBytes, err := json.Marshal(invalidCRPObject)
	//assert.Nil(t, err)

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
					Name: "test-cro",
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
				APIResources:            map[schema.GroupVersionKind]bool{ClusterRoleGVK: true},
				IsClusterScopedResource: true,
			},
			resourceValidator: clusterResourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed("any user is allowed to modify v1beta1 CRP"),
		},
		"allow CRP update": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-cro",
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
				APIResources:            map[schema.GroupVersionKind]bool{ClusterRoleGVK: true},
				IsClusterScopedResource: true,
			},
			resourceValidator: clusterResourcePlacementValidator{
				decoder: decoder,
			},
			wantResponse: admission.Allowed("any user is allowed to modify v1beta1 CRP"),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			validator.RestMapper = testMapper{}
			validator.ResourceInformer = testCase.resourceInformer
			gotResult := testCase.resourceValidator.Handle(context.Background(), testCase.req)
			assert.Equal(t, testCase.wantResponse, gotResult, utils.TestCaseMsg, testName)
		})
	}
}
