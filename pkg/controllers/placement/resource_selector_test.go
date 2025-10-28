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

package placement

import (
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	testinformer "github.com/kubefleet-dev/kubefleet/test/utils/informer"
)

func makeIPFamilyPolicyTypePointer(policyType corev1.IPFamilyPolicyType) *corev1.IPFamilyPolicyType {
	return &policyType
}
func makeServiceInternalTrafficPolicyPointer(policyType corev1.ServiceInternalTrafficPolicyType) *corev1.ServiceInternalTrafficPolicyType {
	return &policyType
}

func TestGenerateResourceContent(t *testing.T) {
	tests := map[string]struct {
		resource     interface{}
		wantResource interface{}
	}{
		"should generate sanitized resource content for Kind: CustomResourceDefinition": {
			resource: apiextensionsv1.CustomResourceDefinition{
				TypeMeta: metav1.TypeMeta{
					Kind:       "CustomResourceDefinition",
					APIVersion: "apiextensions.k8s.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:                       "object-name",
					GenerateName:               "object-generateName",
					Namespace:                  "object-namespace",
					SelfLink:                   "object-selflink",
					UID:                        types.UID(utilrand.String(10)),
					ResourceVersion:            utilrand.String(10),
					Generation:                 int64(utilrand.Int()),
					CreationTimestamp:          metav1.Time{Time: time.Date(utilrand.IntnRange(0, 999), time.January, 1, 1, 1, 1, 1, time.UTC)},
					DeletionTimestamp:          &metav1.Time{Time: time.Date(utilrand.IntnRange(1000, 1999), time.January, 1, 1, 1, 1, 1, time.UTC)},
					DeletionGracePeriodSeconds: ptr.To(int64(9999)),
					Labels: map[string]string{
						"label-key": "label-value",
					},
					Annotations: map[string]string{
						corev1.LastAppliedConfigAnnotation: "svc-object-annotation-lac-value",
						"svc-annotation-key":               "svc-object-annotation-key-value",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "svc-ownerRef-api/v1",
							Kind:       "svc-owner-kind",
							Name:       "svc-owner-name",
							UID:        "svc-owner-uid",
						},
					},
					Finalizers: []string{"object-finalizer"},
					ManagedFields: []metav1.ManagedFieldsEntry{
						{
							Manager:    utilrand.String(10),
							Operation:  metav1.ManagedFieldsOperationApply,
							APIVersion: utilrand.String(10),
						},
					},
				},
			},
			wantResource: apiextensionsv1.CustomResourceDefinition{
				TypeMeta: metav1.TypeMeta{
					Kind:       "CustomResourceDefinition",
					APIVersion: "apiextensions.k8s.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:                       "object-name",
					GenerateName:               "object-generateName",
					Namespace:                  "object-namespace",
					DeletionGracePeriodSeconds: ptr.To(int64(9999)),
					Labels: map[string]string{
						"label-key": "label-value",
					},
					Annotations: map[string]string{
						"svc-annotation-key": "svc-object-annotation-key-value",
					},
					Finalizers: []string{"object-finalizer"},
				},
			},
		},
		"should generate sanitized resource content for Kind: Service": {
			resource: corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:              "svc-name",
					Namespace:         "svc-namespace",
					SelfLink:          utilrand.String(10),
					DeletionTimestamp: &metav1.Time{Time: time.Date(00002, time.January, 1, 1, 1, 1, 1, time.UTC)},
					ManagedFields: []metav1.ManagedFieldsEntry{
						{
							Manager:    "svc-manager",
							Operation:  metav1.ManagedFieldsOperationApply,
							APIVersion: "svc-manager-api/v1",
						},
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "svc-ownerRef-api/v1",
							Kind:       "svc-owner-kind",
							Name:       "svc-owner-name",
							UID:        "svc-owner-uid",
						},
					},
					Annotations: map[string]string{
						corev1.LastAppliedConfigAnnotation: "svc-object-annotation-lac-value",
						"svc-annotation-key":               "svc-object-annotation-key-value",
					},
					ResourceVersion:   "svc-object-resourceVersion",
					Generation:        int64(utilrand.Int()),
					CreationTimestamp: metav1.Time{Time: time.Date(00001, time.January, 1, 1, 1, 1, 1, time.UTC)},
					UID:               types.UID(utilrand.String(10)),
				},
				Spec: corev1.ServiceSpec{
					ClusterIP:           utilrand.String(10),
					ClusterIPs:          []string{},
					HealthCheckNodePort: rand.Int31(),
					Selector:            map[string]string{"svc-spec-selector-key": "svc-spec-selector-value"},
					Ports: []corev1.ServicePort{
						{
							Name:        "svc-port",
							Protocol:    corev1.ProtocolTCP,
							AppProtocol: ptr.To("svc.com/my-custom-protocol"),
							Port:        9001,
							NodePort:    rand.Int31(),
						},
					},
					Type:                     corev1.ServiceType("svc-spec-type"),
					ExternalIPs:              []string{"svc-spec-externalIps-1"},
					SessionAffinity:          corev1.ServiceAffinity("svc-spec-sessionAffinity"),
					LoadBalancerIP:           "192.168.1.3",
					LoadBalancerSourceRanges: []string{"192.168.1.1"},
					ExternalName:             "svc-spec-externalName",
					ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyType("svc-spec-externalTrafficPolicy"),
					PublishNotReadyAddresses: false,
					SessionAffinityConfig:    &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: ptr.To(int32(60))}},
					IPFamilies: []corev1.IPFamily{
						corev1.IPv4Protocol,
						corev1.IPv6Protocol,
					},
					IPFamilyPolicy:                makeIPFamilyPolicyTypePointer(corev1.IPFamilyPolicySingleStack),
					AllocateLoadBalancerNodePorts: ptr.To(false),
					LoadBalancerClass:             ptr.To("svc-spec-loadBalancerClass"),
					InternalTrafficPolicy:         makeServiceInternalTrafficPolicyPointer(corev1.ServiceInternalTrafficPolicyCluster),
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP:       "192.168.1.1",
								Hostname: "loadbalancer-ingress-hostname",
								Ports: []corev1.PortStatus{
									{
										Port:     9003,
										Protocol: corev1.ProtocolTCP,
									},
								},
							},
						},
					},
				},
			},
			wantResource: corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-name",
					Namespace: "svc-namespace",
					Annotations: map[string]string{
						"svc-annotation-key": "svc-object-annotation-key-value",
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"svc-spec-selector-key": "svc-spec-selector-value"},
					Ports: []corev1.ServicePort{
						{
							Name:        "svc-port",
							Protocol:    corev1.ProtocolTCP,
							AppProtocol: ptr.To("svc.com/my-custom-protocol"),
							Port:        9001,
						},
					},
					Type:                     corev1.ServiceType("svc-spec-type"),
					ExternalIPs:              []string{"svc-spec-externalIps-1"},
					SessionAffinity:          corev1.ServiceAffinity("svc-spec-sessionAffinity"),
					LoadBalancerIP:           "192.168.1.3",
					LoadBalancerSourceRanges: []string{"192.168.1.1"},
					ExternalName:             "svc-spec-externalName",
					ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyType("svc-spec-externalTrafficPolicy"),
					PublishNotReadyAddresses: false,
					SessionAffinityConfig:    &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: ptr.To(int32(60))}},
					IPFamilies: []corev1.IPFamily{
						corev1.IPv4Protocol,
						corev1.IPv6Protocol,
					},
					IPFamilyPolicy:                makeIPFamilyPolicyTypePointer(corev1.IPFamilyPolicySingleStack),
					AllocateLoadBalancerNodePorts: ptr.To(false),
					LoadBalancerClass:             ptr.To("svc-spec-loadBalancerClass"),
					InternalTrafficPolicy:         makeServiceInternalTrafficPolicyPointer(corev1.ServiceInternalTrafficPolicyCluster),
				},
			},
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&tt.resource)
			if err != nil {
				t.Fatalf("ToUnstructured failed: %v", err)
			}
			got, err := generateResourceContent(&unstructured.Unstructured{Object: object})
			if err != nil {
				t.Fatalf("failed to generateResourceContent(): %v", err)
			}
			wantResourceContent := createResourceContentForTest(t, &tt.wantResource)
			if diff := cmp.Diff(wantResourceContent, got); diff != "" {
				t.Errorf("generateResourceContent() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func createResourceContentForTest(t *testing.T, obj interface{}) *fleetv1beta1.ResourceContent {
	want, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&obj)
	if err != nil {
		t.Fatalf("ToUnstructured failed: %v", err)
	}
	delete(want["metadata"].(map[string]interface{}), "creationTimestamp")
	delete(want, "status")

	uWant := unstructured.Unstructured{Object: want}
	rawWant, err := uWant.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	return &fleetv1beta1.ResourceContent{
		RawExtension: runtime.RawExtension{
			Raw: rawWant,
		},
	}
}

func TestGatherSelectedResource(t *testing.T) {
	// Common test deployment object used across multiple test cases.
	testDeployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "test-deployment",
				"namespace": "test-ns",
			},
		},
	}
	testDeployment.SetGroupVersionKind(utils.DeploymentGVK)

	// Common test configmap object used across multiple test cases.
	testConfigMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-configmap",
				"namespace": "test-ns",
			},
		},
	}
	testConfigMap.SetGroupVersionKind(utils.ConfigMapGVK)

	// Common test endpoints object used across multiple test cases.
	testEndpoints := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Endpoints",
			"metadata": map[string]interface{}{
				"name":      "test-endpoints",
				"namespace": "test-ns",
			},
		},
	}
	testEndpoints.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Endpoints",
	})

	kubeRootCAConfigMap := &unstructured.Unstructured{ // reserved configmap object
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "kube-root-ca.crt",
				"namespace": "test-ns",
			},
		},
	}
	kubeRootCAConfigMap.SetGroupVersionKind(utils.ConfigMapGVK)

	// Common test deployment object in deleting state.
	testDeletingDeployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":              "test-deleting-deployment",
				"namespace":         "test-ns",
				"deletionTimestamp": "2025-01-01T00:00:00Z",
				"labels": map[string]interface{}{
					"tier": "api",
					"app":  "frontend",
				},
			},
		},
	}
	testDeletingDeployment.SetGroupVersionKind(utils.DeploymentGVK)

	// Common test deployment with app=frontend label.
	testFrontendDeployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "frontend-deployment",
				"namespace": "test-ns",
				"labels": map[string]interface{}{
					"app":  "frontend",
					"tier": "web",
				},
			},
		},
	}
	testFrontendDeployment.SetGroupVersionKind(utils.DeploymentGVK)

	// Common test deployment with app=backend label.
	testBackendDeployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "backend-deployment",
				"namespace": "test-ns",
				"labels": map[string]interface{}{
					"app":  "backend",
					"tier": "api",
				},
			},
		},
	}
	testBackendDeployment.SetGroupVersionKind(utils.DeploymentGVK)

	// Common test namespace object (cluster-scoped).
	testNamespace := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name": "test-ns",
				"labels": map[string]interface{}{
					"environment": "test",
				},
			},
		},
	}
	testNamespace.SetGroupVersionKind(utils.NamespaceGVK)

	testDeletingNamespace := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name": "deleting-ns",
				"labels": map[string]interface{}{
					"environment": "test",
				},
				"deletionTimestamp": "2025-01-01T00:00:00Z",
			},
		},
	}
	testDeletingNamespace.SetGroupVersionKind(utils.NamespaceGVK)

	prodNamespace := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name": "prod-ns",
				"labels": map[string]interface{}{
					"environment": "production",
				},
			},
		},
	}
	prodNamespace.SetGroupVersionKind(utils.NamespaceGVK)

	// Common test cluster role object (cluster-scoped).
	testClusterRole := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name": "test-cluster-role",
			},
		},
	}
	testClusterRole.SetGroupVersionKind(utils.ClusterRoleGVK)

	// Common test cluster role object #2 (cluster-scoped).
	testClusterRole2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name": "test-cluster-role-2",
			},
		},
	}
	testClusterRole2.SetGroupVersionKind(utils.ClusterRoleGVK)

	kubeSystemNamespace := &unstructured.Unstructured{ // reserved namespace object
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name": "kube-system",
				"labels": map[string]interface{}{
					"environment": "test",
				},
			},
		},
	}
	kubeSystemNamespace.SetGroupVersionKind(utils.NamespaceGVK)

	tests := []struct {
		name            string
		placementName   types.NamespacedName
		selectors       []fleetv1beta1.ResourceSelectorTerm
		resourceConfig  *utils.ResourceConfig
		informerManager *testinformer.FakeManager
		want            []*unstructured.Unstructured
		wantError       error
	}{
		{
			name:          "should handle empty selectors",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors:     []fleetv1beta1.ResourceSelectorTerm{},
			want:          nil,
		},
		{
			name:          "should skip disabled resources",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deployment",
				},
			},
			resourceConfig: utils.NewResourceConfig(true), // make this allow list - nothing is allowed
			want:           nil,
		},
		{
			name:          "should skip disabled resources for resource placement",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deployment",
				},
			},
			resourceConfig: utils.NewResourceConfig(true), // make this allow list - nothing is allowed
			want:           nil,
		},
		{
			name:          "should return error for cluster-scoped resource",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "test-clusterrole",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: &testinformer.FakeManager{
				IsClusterScopedResource: false,
				Listers:                 map[schema.GroupVersionResource]*testinformer.FakeLister{},
			},
			want:      nil,
			wantError: controller.ErrUserError,
		},
		{
			name:          "should handle single resource selection successfully",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deployment",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment}},
					},
				}
			}(),
			want:      []*unstructured.Unstructured{testDeployment},
			wantError: nil,
		},
		{
			name:          "should return empty result when informer manager returns not found error",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deployment",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.DeploymentGVR: {
							Objects: []runtime.Object{},
							Err:     apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "test-deployment"),
						},
					},
				}
			}(),
			want: nil, // should return nil when informer returns not found error
		},
		{
			name:          "should return error when informer manager returns non-NotFound error",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deployment",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.DeploymentGVR: {
							Objects: []runtime.Object{},
							Err:     errors.New("connection timeout"),
						},
					},
				}
			}(),
			wantError: controller.ErrUnexpectedBehavior,
		},
		{
			name:          "should return error using label selector when informer manager returns error",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.DeploymentGVR: {
							Objects: []runtime.Object{},
							Err:     apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "test-deployment"),
						},
					},
				}
			}(),
			wantError: controller.ErrAPIServerError,
		},
		{
			name:          "should return only non-deleting resources when mixed with deleting resources",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deployment", // non-deleting deployment
				},
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deleting-deployment", // deleting deployment
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment, testDeletingDeployment}},
					},
				}
			}(),
			want:      []*unstructured.Unstructured{testDeployment},
			wantError: nil,
		},
		{
			name:          "should handle resource selection successfully by using label selector",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "frontend",
						},
					},
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.DeploymentGVR: {Objects: []runtime.Object{testFrontendDeployment, testBackendDeployment, testDeployment, testDeletingDeployment}},
					},
				}
			}(),
			want:      []*unstructured.Unstructured{testFrontendDeployment},
			wantError: nil,
		},
		{
			name:          "should handle label selector with MatchExpressions",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "tier",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"web", "api"},
							},
						},
					},
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.DeploymentGVR: {Objects: []runtime.Object{testFrontendDeployment, testBackendDeployment, testDeployment, testDeletingDeployment}},
					},
				}
			}(),
			want:      []*unstructured.Unstructured{testBackendDeployment, testFrontendDeployment}, // should return both deployments (order may vary)
			wantError: nil,
		},
		{
			name:          "should detect duplicate resources",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deployment",
				},
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deployment", // same deployment selected twice
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment}},
					},
				}
			}(),
			wantError: controller.ErrUserError,
		},
		{
			name:          "should sort resources according to apply order",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deployment",
				},
				{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
					Name:    "test-configmap",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment}},
						utils.ConfigMapGVR:  {Objects: []runtime.Object{testConfigMap}},
					},
				}
			}(),
			// ConfigMap should come first according to apply order.
			want: []*unstructured.Unstructured{testConfigMap, testDeployment},
		},
		// tests for cluster-scoped placements
		{
			name:          "should return error for namespace-scoped resource for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deployment",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: &testinformer.FakeManager{
				IsClusterScopedResource: true,
				Listers:                 map[schema.GroupVersionResource]*testinformer.FakeLister{},
			},
			want:      nil,
			wantError: controller.ErrUserError,
		},
		{
			name:          "should sort resources for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					// Empty name means select all ClusterRoles (or use label selector).
				},
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-ns",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: false,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.ClusterRoleGVR: {Objects: []runtime.Object{testClusterRole, testClusterRole2}},
						utils.NamespaceGVR:   {Objects: []runtime.Object{testNamespace}},
					},
				}
			}(),
			// Namespace should come first according to apply order (namespace comes before ClusterRole).
			// Both ClusterRoles should be included since we're selecting all ClusterRoles with empty name.
			want: []*unstructured.Unstructured{testNamespace, testClusterRole, testClusterRole2},
		},
		{
			name:          "should select resources by name for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "test-cluster-role",
				},
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-ns",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: false,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.ClusterRoleGVR: {Objects: []runtime.Object{testClusterRole, testClusterRole2}},
						utils.NamespaceGVR:   {Objects: []runtime.Object{testNamespace}},
					},
				}
			}(),
			// Namespace should come first according to apply order (namespace comes before ClusterRole).
			want: []*unstructured.Unstructured{testNamespace, testClusterRole},
		},
		{
			name:          "should select namespaces and its children resources by using label selector for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "test",
						},
					},
					SelectionScope: fleetv1beta1.NamespaceWithResources,
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: false,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR:  {Objects: []runtime.Object{testNamespace, prodNamespace, testDeletingNamespace}},
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment, testDeletingDeployment}},
						utils.ConfigMapGVR:  {Objects: []runtime.Object{testConfigMap, kubeRootCAConfigMap}},
					},
					NamespaceScopedResources: []schema.GroupVersionResource{utils.DeploymentGVR, utils.ConfigMapGVR},
				}
			}(),
			// Should select only non-reserved namespaces with matching labels and their children resources
			want: []*unstructured.Unstructured{testNamespace, testConfigMap, testDeployment},
		},
		{
			name:          "should skip the resource for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "test",
						},
					},
					SelectionScope: fleetv1beta1.NamespaceWithResources,
				},
			},
			resourceConfig: func() *utils.ResourceConfig {
				cfg := utils.NewResourceConfig(false)
				cfg.AddGroupVersionKind(utils.DeploymentGVK)
				return cfg
			}(),
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: false,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR:  {Objects: []runtime.Object{testNamespace, prodNamespace, testDeletingNamespace}},
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment, testDeletingDeployment}},
						utils.ConfigMapGVR:  {Objects: []runtime.Object{testConfigMap, kubeRootCAConfigMap}},
					},
					NamespaceScopedResources: []schema.GroupVersionResource{utils.DeploymentGVR, utils.ConfigMapGVR},
				}
			}(),
			// should skip the deployment resource since it is not allowed by resource config
			want: []*unstructured.Unstructured{testNamespace, testConfigMap},
		},
		{
			name:          "should select namespaces using nil label selector for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:          "",
					Version:        "v1",
					Kind:           "Namespace",
					SelectionScope: fleetv1beta1.NamespaceWithResources,
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: false,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR:  {Objects: []runtime.Object{testNamespace, prodNamespace, testDeletingNamespace}},
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment, testDeletingDeployment}},
						utils.ConfigMapGVR:  {Objects: []runtime.Object{testConfigMap, kubeRootCAConfigMap}},
					},
					NamespaceScopedResources: []schema.GroupVersionResource{utils.DeploymentGVR, utils.ConfigMapGVR},
				}
			}(),
			// Should select only non-reserved namespaces with matching labels and their child resources
			want: []*unstructured.Unstructured{prodNamespace, testNamespace, testConfigMap, testDeployment},
		},
		{
			name:          "should select only namespaces for namespace only scope for a namespace",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:          "",
					Version:        "v1",
					Kind:           "Namespace",
					Name:           "test-ns",
					SelectionScope: fleetv1beta1.NamespaceOnly,
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: false,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR:  {Objects: []runtime.Object{testNamespace, prodNamespace, testDeletingNamespace}},
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment, testDeletingDeployment}},
						utils.ConfigMapGVR:  {Objects: []runtime.Object{testConfigMap, kubeRootCAConfigMap}},
					},
					NamespaceScopedResources: []schema.GroupVersionResource{utils.DeploymentGVR, utils.ConfigMapGVR},
				}
			}(),
			// Should select only the namespace with name "test-ns" and none of its child resources
			want: []*unstructured.Unstructured{testNamespace},
		},
		{
			name:          "should select only namespaces for namespace only scope for namespaces with labels",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:          "",
					Version:        "v1",
					Kind:           "Namespace",
					SelectionScope: fleetv1beta1.NamespaceOnly,
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: false,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR:  {Objects: []runtime.Object{testNamespace, prodNamespace, testDeletingNamespace}},
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment, testDeletingDeployment}},
						utils.ConfigMapGVR:  {Objects: []runtime.Object{testConfigMap, kubeRootCAConfigMap}},
					},
					NamespaceScopedResources: []schema.GroupVersionResource{utils.DeploymentGVR, utils.ConfigMapGVR},
				}
			}(),
			// Should select only non-deleting namespaces with matching labels and none of their child resources
			want: []*unstructured.Unstructured{prodNamespace, testNamespace},
		},
		{
			name:          "should return error if a resourceplacement selects namespaces even for namespace only scope",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:          "",
					Version:        "v1",
					Kind:           "Namespace",
					Name:           "test-ns",
					SelectionScope: fleetv1beta1.NamespaceOnly,
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: false,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR:  {Objects: []runtime.Object{testNamespace, prodNamespace, testDeletingNamespace}},
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment, testDeletingDeployment}},
						utils.ConfigMapGVR:  {Objects: []runtime.Object{testConfigMap, kubeRootCAConfigMap}},
					},
					NamespaceScopedResources: []schema.GroupVersionResource{utils.DeploymentGVR, utils.ConfigMapGVR},
				}
			}(),
			wantError: controller.ErrUserError,
		},
		{
			name:          "should return error when selecting a reserved namespace for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "test",
						},
					},
					SelectionScope: fleetv1beta1.NamespaceWithResources,
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: false,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR:  {Objects: []runtime.Object{testNamespace, prodNamespace, testDeletingNamespace, kubeSystemNamespace}},
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment, testDeletingDeployment}},
						utils.ConfigMapGVR:  {Objects: []runtime.Object{testConfigMap}},
					},
					NamespaceScopedResources: []schema.GroupVersionResource{utils.DeploymentGVR, utils.ConfigMapGVR},
				}
			}(),
			wantError: controller.ErrUserError,
		},
		{
			name:          "should return empty result when informer manager returns not found error for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:          "",
					Version:        "v1",
					Kind:           "Namespace",
					Name:           "test-ns",
					SelectionScope: fleetv1beta1.NamespaceWithResources,
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR: {
							Objects: []runtime.Object{},
							Err:     apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "namespaces"}, "test-ns"),
						},
					},
				}
			}(),
			want: nil, // should return nil when informer returns not found error
		},
		{
			name:          "should return error when informer manager returns non-NotFound error (getting namespace) for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:          "",
					Version:        "v1",
					Kind:           "Namespace",
					Name:           "test-ns",
					SelectionScope: fleetv1beta1.NamespaceWithResources,
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR: {
							Objects: []runtime.Object{},
							Err:     errors.New("connection timeout"),
						},
					},
				}
			}(),
			wantError: controller.ErrUnexpectedBehavior,
		},
		{
			name:          "should return error using label selector when informer manager returns error (getting namespace) for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:          "",
					Version:        "v1",
					Kind:           "Namespace",
					SelectionScope: fleetv1beta1.NamespaceWithResources,
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR: {
							Objects: []runtime.Object{},
							Err:     apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "namespaces"}, "test-ns"),
						},
					},
				}
			}(),
			wantError: controller.ErrAPIServerError,
		},
		{
			name:          "should return error when informer manager returns non-NotFound error (getting deployment) for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:          "",
					Version:        "v1",
					Kind:           "Namespace",
					Name:           "test-ns",
					SelectionScope: fleetv1beta1.NamespaceWithResources,
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR: {Objects: []runtime.Object{testNamespace, prodNamespace, testDeletingNamespace, kubeSystemNamespace}},
						utils.DeploymentGVR: {
							Objects: []runtime.Object{},
							Err:     errors.New("connection timeout"),
						},
					},
					NamespaceScopedResources: []schema.GroupVersionResource{utils.DeploymentGVR},
				}
			}(),
			wantError: controller.ErrUnexpectedBehavior,
		},
		{
			name:          "should skip reserved resources for namespaced placement",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
					Name:    "kube-root-ca.crt",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.ConfigMapGVR: {Objects: []runtime.Object{kubeRootCAConfigMap}},
					},
				}
			}(),
			want: nil, // should not propagate reserved configmap
		},
		{
			name:          "should skip reserved resources for namespaced placement when selecting all the configMaps",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.ConfigMapGVR: {Objects: []runtime.Object{kubeRootCAConfigMap, testConfigMap}},
					},
				}
			}(),
			want: []*unstructured.Unstructured{testConfigMap}, // should not propagate reserved configmap
		},
		{
			name:          "should return error when informer cache is not synced for namespaced placement",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "test-deployment",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment}},
					},
					InformerSynced: ptr.To(false),
				}
			}(),
			wantError: controller.ErrExpectedBehavior,
		},
		{
			name:          "should return error when informer cache is not synced for cluster scoped placement",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "test-cluster-role",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: false,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.ClusterRoleGVR: {Objects: []runtime.Object{testClusterRole}},
					},
					InformerSynced: ptr.To(false),
				}
			}(),
			wantError: controller.ErrExpectedBehavior,
		},
		{
			name:          "should return error when informer cache is not synced for cluster scoped placement with namespace resources",
			placementName: types.NamespacedName{Name: "test-placement"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:          "",
					Version:        "v1",
					Kind:           "Namespace",
					Name:           "test-ns",
					SelectionScope: fleetv1beta1.NamespaceWithResources,
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					APIResources: map[schema.GroupVersionKind]bool{
						utils.NamespaceGVK: true,
					},
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.NamespaceGVR:  {Objects: []runtime.Object{testNamespace}},
						utils.DeploymentGVR: {Objects: []runtime.Object{testDeployment}},
					},
					NamespaceScopedResources: []schema.GroupVersionResource{utils.DeploymentGVR},
					InformerSynced:           ptr.To(false),
				}
			}(),
			wantError: controller.ErrExpectedBehavior,
		},
		{
			name:          "should return error when shouldPropagateObj returns error",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Endpoints",
					Name:    "test-endpoints",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						{Group: "", Version: "v1", Resource: "endpoints"}: {
							Objects: []runtime.Object{testEndpoints},
						},
						utils.ServiceGVR: {
							Objects: []runtime.Object{},
							Err:     errors.New("connection timeout"),
						},
					},
				}
			}(),
			wantError: controller.ErrUnexpectedBehavior,
		},
		{
			name:          "should return error by selecting all the endpoints when shouldPropagateObj returns error",
			placementName: types.NamespacedName{Name: "test-placement", Namespace: "test-ns"},
			selectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Endpoints",
				},
			},
			resourceConfig: utils.NewResourceConfig(false), // default deny list
			informerManager: func() *testinformer.FakeManager {
				return &testinformer.FakeManager{
					IsClusterScopedResource: true,
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						{Group: "", Version: "v1", Resource: "endpoints"}: {
							Objects: []runtime.Object{testEndpoints},
						},
						utils.ServiceGVR: {
							Objects: []runtime.Object{},
							Err:     errors.New("connection timeout"),
						},
					},
				}
			}(),
			wantError: controller.ErrUnexpectedBehavior,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Reconciler{
				ResourceConfig:  tt.resourceConfig,
				InformerManager: tt.informerManager,
				RestMapper:      newFakeRESTMapper(),
			}

			got, err := r.gatherSelectedResource(tt.placementName, tt.selectors)
			if gotErr, wantErr := err != nil, tt.wantError != nil; gotErr != wantErr || !errors.Is(err, tt.wantError) {
				t.Fatalf("gatherSelectedResource() = %v, want error %v", err, tt.wantError)
			}
			if tt.wantError != nil {
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("gatherSelectedResource() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// fakeRESTMapper is a minimal RESTMapper implementation for testing
type fakeRESTMapper struct {
	mappings map[schema.GroupKind]*meta.RESTMapping
}

// newFakeRESTMapper creates a new fakeRESTMapper with default mappings
func newFakeRESTMapper() *fakeRESTMapper {
	return &fakeRESTMapper{
		mappings: map[schema.GroupKind]*meta.RESTMapping{
			{Group: "", Kind: "Namespace"}: {
				Resource: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"},
			},
			{Group: "apps", Kind: "Deployment"}: {
				Resource: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			},
			{Group: "", Kind: "ConfigMap"}: {
				Resource: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
			},
			{Group: "", Kind: "Node"}: {
				Resource: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"},
			},
			{Group: "rbac.authorization.k8s.io", Kind: "ClusterRole"}: {
				Resource: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"},
			},
			{Group: "", Kind: "Endpoints"}: {
				Resource: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "endpoints"},
			},
		},
	}
}

func (f *fakeRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	if mapping, exists := f.mappings[gk]; exists {
		return mapping, nil
	}
	return nil, errors.New("resource not found")
}

func (f *fakeRESTMapper) RESTMappings(gk schema.GroupKind, versions ...string) ([]*meta.RESTMapping, error) {
	mapping, err := f.RESTMapping(gk, versions...)
	if err != nil {
		return nil, err
	}
	return []*meta.RESTMapping{mapping}, nil
}

func (f *fakeRESTMapper) ResourceFor(input schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return input, nil
}

func (f *fakeRESTMapper) ResourcesFor(input schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return []schema.GroupVersionResource{input}, nil
}

func (f *fakeRESTMapper) KindFor(resource schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	switch {
	case resource.Group == "" && resource.Resource == "namespaces":
		return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}, nil
	case resource.Group == "apps" && resource.Resource == "deployments":
		return schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, nil
	case resource.Group == "" && resource.Resource == "configmaps":
		return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, nil
	case resource.Group == "" && resource.Resource == "nodes":
		return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}, nil
	case resource.Group == "" && resource.Resource == "endpoints":
		return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Endpoints"}, nil
	}
	return schema.GroupVersionKind{}, errors.New("kind not found")
}

func (f *fakeRESTMapper) KindsFor(resource schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	kind, err := f.KindFor(resource)
	if err != nil {
		return nil, err
	}
	return []schema.GroupVersionKind{kind}, nil
}

func (f *fakeRESTMapper) ResourceSingularizer(resource string) (singular string, err error) {
	return resource, nil
}

func TestSortResources(t *testing.T) {
	// Create the ingressClass object
	ingressClass := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking/v1",
			"kind":       "IngressClass",
			"metadata": map[string]interface{}{
				"name": "test",
			},
		},
	}

	// Create the Ingress object
	ingress := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking/v1",
			"kind":       "Ingress",
			"metadata": map[string]interface{}{
				"name":      "test-ingress",
				"namespace": "test",
			},
		},
	}

	// Create the NetworkPolicy object
	networkPolicy := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking/v1",
			"kind":       "NetworkPolicy",
			"metadata": map[string]interface{}{
				"name":      "test-networkpolicy",
				"namespace": "test",
			},
		},
	}

	// Create the first Namespace object
	namespace1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name": "test1",
			},
		},
	}

	// Create the second Namespace object
	namespace2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name": "test2",
			},
		},
	}

	// Create the LimitRange object
	limitRange := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "LimitRange",
			"metadata": map[string]interface{}{
				"name":      "test-limitrange",
				"namespace": "test",
			},
		},
	}

	// Create the pod object.
	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "test-pod",
				"namespace": "test",
			},
		},
	}

	// Create the ReplicationController object.
	replicationController := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ReplicationController",
			"metadata": map[string]interface{}{
				"name":      "test-replicationcontroller",
				"namespace": "test",
			},
		},
	}

	// Create the ResourceQuota object.
	resourceQuota := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ResourceQuota",
			"metadata": map[string]interface{}{
				"name":      "test-resourcequota",
				"namespace": "test",
			},
		},
	}

	// Create the Service object.
	service := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":      "test-service",
				"namespace": "test",
			},
		},
	}

	// Create the ServiceAccount object.
	serviceAccount := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ServiceAccount",
			"metadata": map[string]interface{}{
				"name":      "test-serviceaccount",
				"namespace": "test",
			},
		},
	}

	// Create the PodDisruptionBudget object.
	pdb := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "policy/v1",
			"kind":       "PodDisruptionBudget",
			"metadata": map[string]interface{}{
				"name":      "test-pdb",
				"namespace": "test",
			},
		},
	}

	// Create the Deployment object.
	deployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "test-nginx",
				"namespace": "test",
			},
		},
	}

	// Create the v1beta1 Deployment object.
	v1beta1Deployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1beta1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "test-nginx1",
				"namespace": "test",
			},
		},
	}

	// Create the DaemonSet object.
	daemonSet := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "DaemonSet",
			"metadata": map[string]interface{}{
				"name":      "test-daemonset",
				"namespace": "test",
			},
		},
	}

	// Create the ReplicaSet object.
	replicaSet := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "ReplicaSet",
			"metadata": map[string]interface{}{
				"name":      "test-replicaset",
				"namespace": "test",
			},
		},
	}

	// Create the StatefulSet object.
	statefulSet := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]interface{}{
				"name":      "test-statefulset",
				"namespace": "test",
			},
		},
	}

	// Create the StorageClass object.
	storageClass := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "storage.k8s.io/v1",
			"kind":       "StorageClass",
			"metadata": map[string]interface{}{
				"name": "test-storageclass",
			},
		},
	}

	// Create the APIService object.
	apiService := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apiregistration.k8s.io/v1",
			"kind":       "APIService",
			"metadata": map[string]interface{}{
				"name": "test-apiservice",
			},
		},
	}

	// Create the HorizontalPodAutoscaler object.
	hpa := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "autoscaling/v1",
			"kind":       "HorizontalPodAutoscaler",
			"metadata": map[string]interface{}{
				"name":      "test-hpa",
				"namespace": "test",
			},
		},
	}

	// Create the PriorityClass object.
	priorityClass := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "scheduling.k8s.io/v1",
			"kind":       "PriorityClass",
			"metadata": map[string]interface{}{
				"name": "test-priorityclass",
			},
		},
	}

	// Create the ValidatingWebhookConfiguration object.
	validatingWebhookConfiguration := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       "ValidatingWebhookConfiguration",
			"metadata": map[string]interface{}{
				"name": "test-validatingwebhookconfiguration",
			},
		},
	}

	// Create the MutatingWebhookConfiguration object.
	mutatingWebhookConfiguration := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       "MutatingWebhookConfiguration",
			"metadata": map[string]interface{}{
				"name": "test-mutatingwebhookconfiguration",
			},
		},
	}

	// Create the first CustomResourceDefinition object.
	crd1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]interface{}{
				"name": "test-crd1",
			},
		},
	}

	// Create the second CustomResourceDefinition object.
	crd2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]interface{}{
				"name": "test-crd2",
			},
		},
	}

	// Create the ClusterRole object.
	clusterRole := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name": "test-clusterrole",
			},
		},
	}

	// Create the ClusterRoleBinding object.
	clusterRoleBinindg := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRoleBinding",
			"metadata": map[string]interface{}{
				"name": "test-clusterrolebinding",
			},
		},
	}

	// Create the Role object.
	role := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "Role",
			"metadata": map[string]interface{}{
				"name":      "test-role",
				"namespace": "test",
			},
		},
	}

	// Create the RoleBinding object.
	roleBinding := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "RoleBinding",
			"metadata": map[string]interface{}{
				"name":      "test-rolebinding",
				"namespace": "test",
			},
		},
	}

	// Create the Secret object.
	secret1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "test-secret1",
				"namespace": "test",
			},
		},
	}

	// Create the Secret object.
	secret2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "test-secret2",
				"namespace": "test",
			},
		},
	}

	// Create the ConfigMap object.
	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-configmap",
				"namespace": "test",
			},
		},
	}

	// Create the CronJob object.
	cronJob := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "batch/v1",
			"kind":       "CronJob",
			"metadata": map[string]interface{}{
				"name":      "test-cronjob",
				"namespace": "test",
			},
		},
	}

	// Create the Job object.
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "batch/v1",
			"kind":       "Job",
			"metadata": map[string]interface{}{
				"name":      "test-job",
				"namespace": "test",
			},
		},
	}

	// Create the PersistentVolume object.
	pv := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "PersistentVolume",
			"metadata": map[string]interface{}{
				"name": "test-pv",
			},
		},
	}

	// Create the PersistentVolumeClaim object.
	pvc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "PersistentVolumeClaim",
			"metadata": map[string]interface{}{
				"name":      "test-pvc",
				"namespace": "test",
			},
		},
	}

	// Create the test resource.
	testResource1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "test.kubernetes-fleet.io/v1alpha1",
			"kind":       "TestResource",
			"metadata": map[string]interface{}{
				"name":      "test-resource1",
				"namespace": "test",
			},
		},
	}

	// Create the test resource.
	testResource2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "test.kubernetes-fleet.io/v1alpha1",
			"kind":       "TestResource",
			"metadata": map[string]interface{}{
				"name":      "test-resource2",
				"namespace": "test",
			},
		},
	}

	// Create another test resource.
	anotherTestResource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "test.kubernetes-fleet.io/v1alpha1",
			"kind":       "AnotherTestResource",
			"metadata": map[string]interface{}{
				"name":      "another-test-resource",
				"namespace": "test",
			},
		},
	}

	// Create v1beta1 another test resource.
	v1beta1AnotherTestResource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "test.kubernetes-fleet.io/v1beta1",
			"kind":       "AnotherTestResource",
			"metadata": map[string]interface{}{
				"name":      "another-test-resource",
				"namespace": "test",
			},
		},
	}

	tests := map[string]struct {
		resources []*unstructured.Unstructured
		want      []*unstructured.Unstructured
	}{
		"should handle empty resources list": {
			resources: []*unstructured.Unstructured{},
			want:      []*unstructured.Unstructured{},
		},
		"should handle single resource": {
			resources: []*unstructured.Unstructured{deployment},
			want:      []*unstructured.Unstructured{deployment},
		},
		"should handle multiple resources of all kinds": {
			resources: []*unstructured.Unstructured{ingressClass, clusterRole, clusterRoleBinindg, configMap, cronJob, crd1, daemonSet, deployment, testResource1, ingress, job, limitRange, namespace1, networkPolicy, pv, pvc, pod, pdb, replicaSet, replicationController, resourceQuota, role, roleBinding, secret1, service, serviceAccount, statefulSet, storageClass, apiService, hpa, priorityClass, validatingWebhookConfiguration, mutatingWebhookConfiguration},
			want:      []*unstructured.Unstructured{priorityClass, namespace1, networkPolicy, resourceQuota, limitRange, pdb, serviceAccount, secret1, configMap, storageClass, pv, pvc, crd1, clusterRole, clusterRoleBinindg, role, roleBinding, service, daemonSet, pod, replicationController, replicaSet, deployment, hpa, statefulSet, job, cronJob, ingressClass, ingress, apiService, mutatingWebhookConfiguration, validatingWebhookConfiguration, testResource1},
		},
		"should handle multiple known resources, different kinds": {
			resources: []*unstructured.Unstructured{crd2, crd1, secret2, namespace2, namespace1, secret1},
			want:      []*unstructured.Unstructured{namespace1, namespace2, secret1, secret2, crd1, crd2},
		},
		"should handle multiple known resources, same kinds with different versions": {
			resources: []*unstructured.Unstructured{v1beta1Deployment, deployment, limitRange},
			want:      []*unstructured.Unstructured{limitRange, deployment, v1beta1Deployment},
		},
		"should handle multiple unknown resources, same kinds": {
			resources: []*unstructured.Unstructured{testResource2, testResource1},
			want:      []*unstructured.Unstructured{testResource1, testResource2},
		},
		"should handle multiple unknown resources, different kinds": {
			resources: []*unstructured.Unstructured{testResource1, anotherTestResource},
			want:      []*unstructured.Unstructured{anotherTestResource, testResource1},
		},
		"should handle multiple unknown resources, same kinds with different versions": {
			resources: []*unstructured.Unstructured{v1beta1AnotherTestResource, anotherTestResource},
			want:      []*unstructured.Unstructured{anotherTestResource, v1beta1AnotherTestResource},
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			// run many times to make sure it's stable
			for i := 0; i < 10; i++ {
				sortResources(tt.resources)
				// Check that the returned resources match the expected resources
				diff := cmp.Diff(tt.want, tt.resources)
				if diff != "" {
					t.Errorf("sortResources() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}
