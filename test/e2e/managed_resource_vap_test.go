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
package e2e

import (
	"errors"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/test/e2e/framework"
)

const (
	managedByLabel      = "fleet.azure.com/managed-by"
	managedByLabelValue = "arm"
	vapName             = "aks-fleet-managed-by-arm"
	k8sVersionWithVAP   = "v1.30.0"
)

var managedByLabelMap = map[string]string{
	managedByLabel: managedByLabelValue,
}

var _ = Describe("ValidatingAdmissionPolicy for Managed Resources", Label("managedresource"), Ordered, func() {
	BeforeEach(func() {
		discoveryClient := framework.GetDiscoveryClient(hubCluster)
		clusterVersionWithVAP, err := isAPIServerVersionAtLeast(discoveryClient, k8sVersionWithVAP)
		Expect(err).To(BeNil(), "Error checking API server version")
		if !clusterVersionWithVAP {
			Skip(fmt.Sprintf("Skipping VAP tests as the cluster is running Kubernetes version < %s", k8sVersionWithVAP))
		}
	})

	Context("Baseline: Unmanaged resources", func() {
		DescribeTable("should allow all operations on unmanaged resources",
			func(resourceType string, clientFunc func() client.Client, suffix string) {
				client := clientFunc() // Call the function to get the client at runtime

				namespace := "default"
				switch resourceType {
				case "namespace", "clusterrole", "crp":
					namespace = ""
				}
				resource := createResource(resourceType, "test-unmanaged-"+suffix, namespace, false)
				testResourceOperations(client, resource, true, resourceType)
			},
			Entry("allowed: namespace by non-aksService user", "namespace", getUserClient, "ns-user"),
			Entry("allowed: namespace by aksService user", "namespace", getAksServiceClient, "ns-aks"),
			Entry("allowed: configmap by non-aksService user", "configmap", getUserClient, "cm-user"),
			Entry("allowed: configmap by aksService user", "configmap", getAksServiceClient, "cm-aks"),
			Entry("allowed: resourcequota by non-aksService user", "resourcequota", getUserClient, "rq-user"),
			Entry("allowed: resourcequota by aksService user", "resourcequota", getAksServiceClient, "rq-aks"),
			Entry("allowed: networkpolicy by non-aksService user", "networkpolicy", getUserClient, "np-user"),
			Entry("allowed: networkpolicy by aksService user", "networkpolicy", getAksServiceClient, "np-aks"),
			Entry("allowed: clusterrole by non-aksService user", "clusterrole", getUserClient, "cr-user"),
			Entry("allowed: clusterrole by aksService user", "clusterrole", getAksServiceClient, "cr-aks"),
			Entry("allowed: crp by non-aksService user", "crp", getUserClient, "crp-user"),
			Entry("allowed: crp by aksService user", "crp", getAksServiceClient, "crp-aks"),
		)
	})

	Context("Core resource validation", func() {
		describeResourceContexts([]struct{ resourceType, testPrefix, namespace, contextName string }{
			{"namespace", "test-namespace", "", "Cluster-scoped resources"},
			{"configmap", "test-configmap", "default", "Namespaced resources"},
			{"clusterrole", "test-clusterrole", "", "RBAC resources"},
		})
	})

	Context("Resources for Fleet managed namespaces", func() {
		describeResourceContexts([]struct{ resourceType, testPrefix, namespace, contextName string }{
			{"crp", "test-crp", "", "ClusterResourcePlacement"},
			{"namespace", "test-fleet-ns", "", "Namespace"},
			{"resourcequota", "test-quota", "default", "ResourceQuota"},
			{"networkpolicy", "test-netpol", "default", "NetworkPolicy"},
		})
	})

	Context("Dynamic resource types", func() {
		Context("Custom Resource Definitions", Ordered, func() {
			var testCRD *apiextensionsv1.CustomResourceDefinition

			BeforeAll(func() {
				testCRD = createTestCRD()
				authorizedClient := hubCluster.SystemMastersClient
				Expect(authorizedClient.Create(ctx, testCRD)).To(Succeed())
				// Wait for CRD to be established
				Eventually(func() bool {
					var crd apiextensionsv1.CustomResourceDefinition
					err := authorizedClient.Get(ctx, types.NamespacedName{Name: testCRD.Name}, &crd)
					if err != nil {
						return false
					}
					for _, condition := range crd.Status.Conditions {
						if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
							return true
						}
					}
					return false
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
			})

			DescribeTable("customresource operations",
				func(userType string, clientFunc func() client.Client, shouldSucceed bool, suffix string) {
					client := clientFunc() // Call the function to get the client at runtime
					resourceName := fmt.Sprintf("test-cr-%s", suffix)
					resource := createResource("customresource", resourceName, "default", true)
					testResourceOperations(client, resource, shouldSucceed, "customresource")
				},
				Entry("non-aksService user", "unauthorized", getUserClient, false, "deny"),
				Entry("aksService user", "authorized", getAksServiceClient, true, "allow"),
			)

			AfterAll(func() {
				if testCRD != nil {
					authorizedClient := hubCluster.SystemMastersClient
					Expect(authorizedClient.Delete(ctx, testCRD)).To(Succeed())
				}
			})
		})

	})

	Context("Policy infrastructure validation", func() {
		It("should have proper VAP and binding configuration", func() {
			checkVAPAndBindingExistence(hubCluster)
		})
	})
})

// Helper functions for creating managed resources
func createManagedNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: managedByLabelMap,
		},
	}
}

func createManagedResourceQuota(ns, name string) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    managedByLabelMap,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				"pods": resource.MustParse("10"),
			},
		},
	}
}

func createManagedNetworkPolicy(ns, name string) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    managedByLabelMap,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
		},
	}
}

func createManagedCRP(name string) *placementv1beta1.ClusterResourcePlacement {
	return &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: managedByLabelMap,
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
				},
			},
		},
	}
}

func createManagedConfigMap(ns, name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    managedByLabelMap,
		},
		Data: map[string]string{"key": "value"},
	}
}

func createManagedClusterRole(name string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: managedByLabelMap,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list"},
			},
		},
	}
}

func createTestCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testresources.example.com",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"field": {Type: "string"},
									},
								},
							},
						},
					},
				},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "testresources",
				Singular: "testresource",
				Kind:     "TestResource",
			},
		},
	}
}

func createManagedCustomResource(ns, name string) *unstructured.Unstructured {
	cr := &unstructured.Unstructured{}
	cr.SetAPIVersion("example.com/v1")
	cr.SetKind("TestResource")
	cr.SetName(name)
	cr.SetNamespace(ns)
	cr.SetLabels(managedByLabelMap)
	cr.Object["spec"] = map[string]interface{}{
		"field": "test-value",
	}
	return cr
}

func expectDeniedByVAP(err error) {
	Expect(k8sErrors.IsForbidden(err)).To(BeTrue(), fmt.Sprintf("Expected Forbidden error, got error %s of type %s", err, reflect.TypeOf(err)))

	var statusErr *k8sErrors.StatusError
	if errors.As(err, &statusErr) {
		Expect(statusErr.ErrStatus.Message).To(SatisfyAny(
			ContainSubstring("ValidatingAdmissionPolicy"),
			ContainSubstring("denied the request"),
			ContainSubstring("violates policy"),
		), "Error should indicate policy violation")
	}
}

// Generic test helper for creating resources with optional managed labels
func createResource(resourceType string, name, namespace string, managed bool) client.Object {
	var resource client.Object

	// Always create the managed version first
	switch resourceType {
	case "namespace":
		resource = createManagedNamespace(name)
	case "configmap":
		resource = createManagedConfigMap(namespace, name)
	case "clusterrole":
		resource = createManagedClusterRole(name)
	case "resourcequota":
		resource = createManagedResourceQuota(namespace, name)
	case "networkpolicy":
		resource = createManagedNetworkPolicy(namespace, name)
	case "crp":
		resource = createManagedCRP(name)
	case "customresource":
		resource = createManagedCustomResource(namespace, name)
	default:
		panic(fmt.Sprintf("Unsupported resource type: %s", resourceType))
	}

	if !managed {
		resource.SetLabels(nil)
	}

	return resource
}

func describeResourceContexts(resources []struct{ resourceType, testPrefix, namespace, contextName string }) {
	for _, r := range resources {
		Context(r.contextName, func() {
			DescribeTable(fmt.Sprintf("%s operations", r.resourceType),
				func(userType string, clientFunc func() client.Client, shouldSucceed bool, suffix string) {
					client := clientFunc() // Call the function to get the client at runtime
					resourceName := fmt.Sprintf("%s-%s", r.testPrefix, suffix)
					resource := createResource(r.resourceType, resourceName, r.namespace, true)
					testResourceOperations(client, resource, shouldSucceed, r.resourceType)
				},
				Entry("non-aksService user", "unauthorized", getUserClient, false, "deny"),
				Entry("aksService user", "authorized", getAksServiceClient, true, "allow"),
			)
		})
	}
}

func testResourceOperations(client client.Client, resource client.Object, shouldSucceed bool, resourceType string) {
	resourceKind := resource.GetObjectKind().GroupVersionKind().Kind
	if resourceKind == "" {
		// Fallback for resources that don't have GVK set
		resourceKind = fmt.Sprintf("%T", resource)
	}

	if shouldSucceed {
		By(fmt.Sprintf("Creating %s", resourceKind))
		Expect(client.Create(ctx, resource)).To(Succeed())

		By(fmt.Sprintf("Updating %s", resourceKind))
		Eventually(func() error {
			// Get latest version to avoid conflicts
			key := types.NamespacedName{
				Name:      resource.GetName(),
				Namespace: resource.GetNamespace(),
			}
			if err := client.Get(ctx, key, resource); err != nil {
				return err
			}

			annotations := resource.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations["test"] = "annotation"
			resource.SetAnnotations(annotations)
			return client.Update(ctx, resource)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())

		By(fmt.Sprintf("Deleting %s", resourceKind))
		Expect(client.Delete(ctx, resource)).To(Succeed())
	} else {
		By(fmt.Sprintf("Expecting denial of CREATE operation on %s", resourceKind))
		err := client.Create(ctx, resource)
		expectDeniedByVAP(err)

		// For UPDATE and DELETE tests, we need an existing resource first
		By(fmt.Sprintf("Creating %s with authorized client for UPDATE/DELETE tests", resourceKind))

		// Use a more unique name to avoid conflicts in parallel test execution
		resourceName := fmt.Sprintf("%s-for-update-delete-%d", resource.GetName(), GinkgoRandomSeed())
		existingResource := createResource(resourceType, resourceName, resource.GetNamespace(), true)
		authorizedClient := hubCluster.SystemMastersClient
		Expect(authorizedClient.Create(ctx, existingResource)).To(Succeed())

		// Test UPDATE denial
		By(fmt.Sprintf("Expecting denial of UPDATE operation on %s", resourceKind))
		Eventually(func() error {
			// Create a fresh object and get the latest version to have proper resourceVersion
			updateResource := createResource(resourceType, existingResource.GetName(), existingResource.GetNamespace(), true)
			key := types.NamespacedName{
				Name:      existingResource.GetName(),
				Namespace: existingResource.GetNamespace(),
			}
			if err := client.Get(ctx, key, updateResource); err != nil {
				return err
			}

			annotations := updateResource.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations["unauthorized-update"] = "should-fail"
			updateResource.SetAnnotations(annotations)

			err := client.Update(ctx, updateResource)

			// If it's a conflict error (409), retry
			if k8sErrors.IsConflict(err) {
				return err
			}

			// Otherwise, we expect it to be denied by VAP (403)
			expectDeniedByVAP(err)
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())

		By(fmt.Sprintf("Expecting denial of DELETE operation on %s", resourceKind))
		err = client.Delete(ctx, existingResource)
		expectDeniedByVAP(err)

		// Clean up the test resource using authorized client
		By(fmt.Sprintf("Cleaning up test %s with authorized client", resourceKind))
		Expect(authorizedClient.Delete(ctx, existingResource)).To(Succeed())
	}
}

func checkVAPAndBindingExistence(c *framework.Cluster) {
	client := c.KubeClient
	discoveryClient := framework.GetDiscoveryClient(c)

	serverVersionWithVAP, err := isAPIServerVersionAtLeast(discoveryClient, k8sVersionWithVAP)
	Expect(err).To(BeNil(), "Error checking API server version")

	var vap admissionregistrationv1.ValidatingAdmissionPolicy
	var vapBinding admissionregistrationv1.ValidatingAdmissionPolicyBinding
	if serverVersionWithVAP {
		By(fmt.Sprintf("Checking for the existence of ValidatingAdmissionPolicy: %s", vapName))
		Expect(client.Get(ctx, types.NamespacedName{Name: vapName}, &vap)).To(Succeed(),
			fmt.Sprintf("ValidatingAdmissionPolicy %s should exist", vapName))
		By(fmt.Sprintf("Checking for the existence of ValidatingAdmissionPolicyBinding: %s", vapName))
		Expect(client.Get(ctx, types.NamespacedName{Name: vapName}, &vapBinding)).To(Succeed(),
			fmt.Sprintf("ValidatingAdmissionPolicyBinding %s should exist", vapName))

		By("Verifying that the VAP has validations configured")
		Expect(vap.Spec.Validations).ToNot(BeEmpty(), "VAP should have validation rules")

		By("Verifying that the VAP binding references the correct policy")
		Expect(vapBinding.Spec.PolicyName).To(Equal(vapName),
			fmt.Sprintf("VAP binding should reference policy %s", vapName))

		By("Verifying that the VAP binding has validation actions configured")
		Expect(vapBinding.Spec.ValidationActions).ToNot(BeEmpty(), "VAP binding should have validation actions")
	} else {
		By("Verifying that the VAP resource type is not found")
		err := client.Get(ctx, types.NamespacedName{Name: vapName}, &vap)
		Expect(meta.IsNoMatchError(err)).To(BeTrue())
		By("Verifying that the VAP Binding resource type is not found")
		err = client.Get(ctx, types.NamespacedName{Name: vapName}, &vapBinding)
		Expect(meta.IsNoMatchError(err)).To(BeTrue())
	}
}

func checkVAPAndBindingAbsence(c *framework.Cluster) {
	client := c.KubeClient
	discoveryClient := framework.GetDiscoveryClient(c)
	clusterVersionWithVAP, err := isAPIServerVersionAtLeast(discoveryClient, k8sVersionWithVAP)
	Expect(err).To(BeNil(), "Error checking API server version")
	if !clusterVersionWithVAP {
		return // no check
	}
	By(fmt.Sprintf("Checking for the absence of ValidatingAdmissionPolicy: %s", vapName))
	var vap admissionregistrationv1.ValidatingAdmissionPolicy
	err = client.Get(ctx, types.NamespacedName{Name: vapName}, &vap)
	Expect(k8sErrors.IsNotFound(err)).To(BeTrue(),
		fmt.Sprintf("ValidatingAdmissionPolicy %s should not exist", vapName))

	By(fmt.Sprintf("Checking for the absence of ValidatingAdmissionPolicyBinding: %s", vapName))
	var vapBinding admissionregistrationv1.ValidatingAdmissionPolicyBinding
	err = client.Get(ctx, types.NamespacedName{Name: vapName}, &vapBinding)
	Expect(k8sErrors.IsNotFound(err)).To(BeTrue(),
		fmt.Sprintf("ValidatingAdmissionPolicyBinding %s should not exist", vapName))
}

// isAPIServerVersionAtLeast checks if the API server version is >= targetVersion (e.g., "v1.30.0")
func isAPIServerVersionAtLeast(disco discovery.DiscoveryInterface, targetVersion string) (bool, error) {
	serverVersion, err := disco.ServerVersion()
	if err != nil {
		return false, fmt.Errorf("failed to get server version: %w", err)
	}
	server, target := version.MustParseSemantic(serverVersion.GitVersion), version.MustParseSemantic(targetVersion)
	return server.AtLeast(target), nil
}

// Need this because Entry() evaluates parameters at definition time, not at runtime.
// Without this, the client value sent to Entry() would always be nil.
func getUserClient() client.Client {
	return hubCluster.ImpersonateKubeClient
}

func getAksServiceClient() client.Client {
	return hubCluster.SystemMastersClient
}
