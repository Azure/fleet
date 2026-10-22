/*
Copyright 2026 The KubeFleet Authors.

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

package admissionpolicymanager

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	placementAnnotationValue    = "env=prod"
	altPlacementAnnotationValue = "env=staging"

	testDataKey      = "foo"
	testDataValue    = "bar"
	altTestDataValue = "baz"
)

var (
	placementPolicyAPIGroup = kfplacementv1alpha1.GroupVersion.Group

	objectWriterVerbs = []string{"get", authzVerbCreate, authzVerbUpdate, "patch", "delete"}
)

func placementAnnotation(value string) map[string]string {
	return map[string]string{
		kfplacementv1alpha1.ClusterSelectorsAnnotation: value,
	}
}

func newConfigMap(namespace, name string, annotations map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Annotations: annotations,
		},
		Data: map[string]string{
			testDataKey: testDataValue,
		},
	}
}

func newTestResource(namespace, name string, annotations map[string]string) *unstructured.Unstructured {
	testResource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				testDataKey: testDataValue,
			},
		},
	}
	testResource.SetAPIVersion("test.kubernetes-fleet.io/v1alpha1")
	testResource.SetKind("TestResource")
	testResource.SetNamespace(namespace)
	testResource.SetName(name)
	testResource.SetAnnotations(annotations)
	return testResource
}

func newNamespace(name string, annotations map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
	}
}

// newClusterRole returns a cluster role with no rules, which any user who can create cluster roles can create
// (i.e., the RBAC privilege escalation prevention does not apply).
func newClusterRole(name string, annotations map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
	}
}

var _ = Describe("deny placement annotation if no placement policy API access", Ordered, func() {
	suffix := utils.RandStr()
	workNSName := fmt.Sprintf("work-%s", suffix)
	otherNSName := fmt.Sprintf("other-%s", suffix)

	// The users that the test cases impersonate. All of them can write the objects that the test cases
	// annotate, but they differ in their permissions on the placement policy APIs.
	//
	// The usernames are randomized so that permissions granted in other test containers never apply.
	noPermUsername := fmt.Sprintf("no-perm-%s", suffix)
	// Can create and update placement policies in the work namespace only.
	ppInNSUsername := fmt.Sprintf("pp-ns-%s", suffix)
	// Can create (but not update) placement policies in the work namespace only.
	ppCreateOnlyInNSUsername := fmt.Sprintf("pp-ns-create-only-%s", suffix)
	// Can create and update placement policies in all namespaces, but not cluster placement policies.
	ppClusterWideUsername := fmt.Sprintf("pp-cluster-wide-%s", suffix)
	// Can create and update cluster placement policies, but not placement policies.
	cppUsername := fmt.Sprintf("cpp-%s", suffix)

	objWriterRBACName := fmt.Sprintf("object-writer-%s", suffix)
	ppInNSRBACName := fmt.Sprintf("pp-ns-%s", suffix)
	ppCreateOnlyInNSRBACName := fmt.Sprintf("pp-ns-create-only-%s", suffix)
	ppClusterWideRBACName := fmt.Sprintf("pp-cluster-wide-%s", suffix)
	cppRBACName := fmt.Sprintf("cpp-%s", suffix)

	var noPermClient, ppInNSClient, ppCreateOnlyInNSClient, ppClusterWideClient, cppClient client.Client

	// The names of the cluster roles that the test cases create as annotated objects; they are cleaned up
	// after all the test cases have run.
	var annotatedClusterRoleNames []string

	BeforeAll(func() {
		createNamespace(workNSName)
		createNamespace(otherNSName)

		grantClusterWidePermissions(objWriterRBACName, []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps", "namespaces", "namespaces/status", "namespaces/finalize"},
				Verbs:     objectWriterVerbs,
			},
			{
				APIGroups: []string{appsv1.GroupName},
				Resources: []string{"deployments", "deployments/status", "deployments/scale"},
				Verbs:     objectWriterVerbs,
			},
			{
				APIGroups: []string{"test.kubernetes-fleet.io"},
				Resources: []string{"testresources"},
				Verbs:     objectWriterVerbs,
			},
			{
				APIGroups: []string{rbacv1.GroupName},
				Resources: []string{"clusterroles"},
				Verbs:     objectWriterVerbs,
			},
		},
			userSubject(noPermUsername),
			userSubject(ppInNSUsername),
			userSubject(ppCreateOnlyInNSUsername),
			userSubject(ppClusterWideUsername),
			userSubject(cppUsername),
		)
		grantPermissionsInNamespace(workNSName, ppInNSRBACName, []rbacv1.PolicyRule{
			{
				APIGroups: []string{placementPolicyAPIGroup},
				Resources: []string{placementPoliciesResourceName},
				Verbs:     []string{authzVerbCreate, authzVerbUpdate},
			},
		}, userSubject(ppInNSUsername))
		grantPermissionsInNamespace(workNSName, ppCreateOnlyInNSRBACName, []rbacv1.PolicyRule{
			{
				APIGroups: []string{placementPolicyAPIGroup},
				Resources: []string{placementPoliciesResourceName},
				Verbs:     []string{authzVerbCreate},
			},
		}, userSubject(ppCreateOnlyInNSUsername))
		grantClusterWidePermissions(ppClusterWideRBACName, []rbacv1.PolicyRule{
			{
				APIGroups: []string{placementPolicyAPIGroup},
				Resources: []string{placementPoliciesResourceName},
				Verbs:     []string{authzVerbCreate, authzVerbUpdate},
			},
		}, userSubject(ppClusterWideUsername))
		grantClusterWidePermissions(cppRBACName, []rbacv1.PolicyRule{
			{
				APIGroups: []string{placementPolicyAPIGroup},
				Resources: []string{clusterPlacementPoliciesResourceName},
				Verbs:     []string{authzVerbCreate, authzVerbUpdate},
			},
		}, userSubject(cppUsername))

		noPermClient = impersonatedClient(noPermUsername)
		ppInNSClient = impersonatedClient(ppInNSUsername)
		ppCreateOnlyInNSClient = impersonatedClient(ppCreateOnlyInNSUsername)
		ppClusterWideClient = impersonatedClient(ppClusterWideUsername)
		cppClient = impersonatedClient(cppUsername)

		By("waiting for the admission policy to take effect")
		waitUntilPolicyIsEnforced(func() error {
			probeCM := newConfigMap(workNSName, fmt.Sprintf("probe-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			return noPermClient.Create(ctx, probeCM)
		}, annotatedForPlacementObjsVAPPolicyName, wantNamespacedObjDenialMsg)
	})

	Context("namespaced objects", func() {
		It("should allow creating an object without the annotation", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), nil)
			Expect(noPermClient.Create(ctx, cm)).To(Succeed(), "Failed to create a config map without the annotation")
		})

		It("should deny creating an annotated object if the caller cannot create or update placement policies", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			err := noPermClient.Create(ctx, cm)
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantNamespacedObjDenialMsg)).To(Succeed())
		})

		It("should deny creating an annotated object if the caller can create but not update placement policies", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			err := ppCreateOnlyInNSClient.Create(ctx, cm)
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantNamespacedObjDenialMsg)).To(Succeed())
		})

		It("should allow creating an annotated object if the caller can create and update placement policies in the namespace", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			Expect(ppInNSClient.Create(ctx, cm)).To(Succeed(), "Failed to create an annotated config map")
		})

		It("should deny creating an annotated object in a namespace where the caller cannot create or update placement policies", func() {
			cm := newConfigMap(otherNSName, fmt.Sprintf("cm-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			err := ppInNSClient.Create(ctx, cm)
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantNamespacedObjDenialMsg)).To(Succeed())
		})

		It("should allow creating an annotated object if the caller can create and update placement policies in all namespaces", func() {
			cm := newConfigMap(otherNSName, fmt.Sprintf("cm-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			Expect(ppClusterWideClient.Create(ctx, cm)).To(Succeed(), "Failed to create an annotated config map")
		})

		It("should deny creating an annotated object if the caller can only create and update cluster placement policies", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			err := cppClient.Create(ctx, cm)
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantNamespacedObjDenialMsg)).To(Succeed())
		})

		It("should allow creating an annotated object as a cluster admin", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			Expect(hubUncachedClient.Create(ctx, cm)).To(Succeed(), "Failed to create an annotated config map")
		})

		It("should deny adding the annotation to an existing object if the caller cannot create or update placement policies", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), nil)
			Expect(hubUncachedClient.Create(ctx, cm)).To(Succeed(), "Failed to create a config map without the annotation")

			err := getAndUpdate(noPermClient, cm, func(cm *corev1.ConfigMap) {
				cm.Annotations = placementAnnotation(placementAnnotationValue)
			})
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantNamespacedObjDenialMsg)).To(Succeed())
		})

		It("should deny adding the annotation to an existing object with a patch if the caller cannot create or update placement policies", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), nil)
			Expect(hubUncachedClient.Create(ctx, cm)).To(Succeed(), "Failed to create a config map without the annotation")

			// This is how kubectl annotate adds an annotation.
			patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, kfplacementv1alpha1.ClusterSelectorsAnnotation, placementAnnotationValue)
			err := noPermClient.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(patch)))
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantNamespacedObjDenialMsg)).To(Succeed())
		})

		It("should deny changing the annotation value if the caller cannot create or update placement policies", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			Expect(hubUncachedClient.Create(ctx, cm)).To(Succeed(), "Failed to create an annotated config map")

			err := getAndUpdate(noPermClient, cm, func(cm *corev1.ConfigMap) {
				cm.Annotations[kfplacementv1alpha1.ClusterSelectorsAnnotation] = altPlacementAnnotationValue
			})
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantNamespacedObjDenialMsg)).To(Succeed())
		})

		It("should allow updating an annotated object without changing the annotation", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			Expect(hubUncachedClient.Create(ctx, cm)).To(Succeed(), "Failed to create an annotated config map")

			err := getAndUpdate(noPermClient, cm, func(cm *corev1.ConfigMap) {
				cm.Data = map[string]string{testDataKey: altTestDataValue}
				cm.Labels = map[string]string{testDataKey: testDataValue}
				cm.Annotations[testDataKey] = testDataValue
			})
			Expect(err).To(Succeed(), "Failed to update an annotated config map without changing the annotation")
		})

		It("should allow removing the annotation", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			Expect(hubUncachedClient.Create(ctx, cm)).To(Succeed(), "Failed to create an annotated config map")

			err := getAndUpdate(noPermClient, cm, func(cm *corev1.ConfigMap) {
				delete(cm.Annotations, kfplacementv1alpha1.ClusterSelectorsAnnotation)
			})
			Expect(err).To(Succeed(), "Failed to remove the annotation")
		})

		It("should allow adding and changing the annotation if the caller can create and update placement policies in the namespace", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), nil)
			Expect(hubUncachedClient.Create(ctx, cm)).To(Succeed(), "Failed to create a config map without the annotation")

			err := getAndUpdate(ppInNSClient, cm, func(cm *corev1.ConfigMap) {
				cm.Annotations = placementAnnotation(placementAnnotationValue)
			})
			Expect(err).To(Succeed(), "Failed to add the annotation")

			err = getAndUpdate(ppInNSClient, cm, func(cm *corev1.ConfigMap) {
				cm.Annotations[kfplacementv1alpha1.ClusterSelectorsAnnotation] = altPlacementAnnotationValue
			})
			Expect(err).To(Succeed(), "Failed to change the annotation value")
		})

		It("should allow deleting an annotated object and removing its finalizers (as the garbage collector would)", func() {
			cm := newConfigMap(workNSName, fmt.Sprintf("cm-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			cm.Finalizers = []string{testFinalizer}
			Expect(hubUncachedClient.Create(ctx, cm)).To(Succeed(), "Failed to create an annotated config map with a finalizer")

			Expect(noPermClient.Delete(ctx, cm)).To(Succeed(), "Failed to delete the annotated config map")

			err := getAndUpdate(noPermClient, cm, func(cm *corev1.ConfigMap) {
				cm.Finalizers = nil
			})
			Expect(err).To(Succeed(), "Failed to remove the finalizer from the annotated config map")

			Eventually(func() error {
				err := hubUncachedClient.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{})
				if !apierrors.IsNotFound(err) {
					return fmt.Errorf("config map still exists or an unexpected error has occurred: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the annotated config map")
		})
	})

	Context("namespaced custom resources", func() {
		It("should deny creating an annotated object if the caller cannot create or update placement policies", func() {
			testResource := newTestResource(workNSName, fmt.Sprintf("tr-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			err := noPermClient.Create(ctx, testResource)
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantNamespacedObjDenialMsg)).To(Succeed())
		})

		It("should allow creating an annotated object if the caller can create and update placement policies in the namespace", func() {
			testResource := newTestResource(workNSName, fmt.Sprintf("tr-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			Expect(ppInNSClient.Create(ctx, testResource)).To(Succeed(), "Failed to create an annotated test resource")
		})
	})

	Context("cluster-scoped objects", func() {
		It("should deny creating an annotated namespace if the caller cannot create or update cluster placement policies", func() {
			ns := newNamespace(fmt.Sprintf("annotated-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			err := noPermClient.Create(ctx, ns)
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantClusterScopedObjDenialMsg)).To(Succeed())
		})

		// For namespaces, the API server sets the namespace of the admission request to the name of the namespace;
		// the admission policy must still check them as cluster-scoped objects.
		It("should deny creating an annotated namespace if the caller can only create and update placement policies (in all namespaces)", func() {
			ns := newNamespace(fmt.Sprintf("annotated-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			err := ppClusterWideClient.Create(ctx, ns)
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantClusterScopedObjDenialMsg)).To(Succeed())
		})

		It("should allow creating an annotated namespace if the caller can create and update cluster placement policies", func() {
			ns := newNamespace(fmt.Sprintf("annotated-%s", utils.RandStr()), placementAnnotation(placementAnnotationValue))
			Expect(cppClient.Create(ctx, ns)).To(Succeed(), "Failed to create an annotated namespace")
		})

		It("should deny adding the annotation to an existing namespace if the caller cannot create or update cluster placement policies, and allow it otherwise", func() {
			nsName := fmt.Sprintf("plain-%s", utils.RandStr())
			createNamespace(nsName)

			err := getAndUpdate(noPermClient, newNamespace(nsName, nil), func(ns *corev1.Namespace) {
				ns.Annotations = placementAnnotation(placementAnnotationValue)
			})
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantClusterScopedObjDenialMsg)).To(Succeed())

			err = getAndUpdate(cppClient, newNamespace(nsName, nil), func(ns *corev1.Namespace) {
				ns.Annotations = placementAnnotation(placementAnnotationValue)
			})
			Expect(err).To(Succeed(), "Failed to add the annotation to the namespace")
		})

		It("should deny creating an annotated cluster role if the caller cannot create or update cluster placement policies, and allow it otherwise", func() {
			clusterRoleName := fmt.Sprintf("annotated-%s", utils.RandStr())
			annotatedClusterRoleNames = append(annotatedClusterRoleNames, clusterRoleName)

			err := noPermClient.Create(ctx, newClusterRole(clusterRoleName, placementAnnotation(placementAnnotationValue)))
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantClusterScopedObjDenialMsg)).To(Succeed())

			Expect(cppClient.Create(ctx, newClusterRole(clusterRoleName, placementAnnotation(placementAnnotationValue)))).To(Succeed(), "Failed to create an annotated cluster role")
		})
	})

	// Some built-in resources (e.g., Deployments and Namespaces) keep the annotation changes sent to some of their
	// subresources, such as status and finalize.
	Context("subresources", func() {
		It("should deny adding the annotation through the status subresource if the caller cannot create or update placement policies", func() {
			deploy := newDeployment(workNSName, fmt.Sprintf("deploy-%s", utils.RandStr()))
			Expect(hubUncachedClient.Create(ctx, deploy)).To(Succeed(), "Failed to create a deployment without the annotation")

			err := getAndUpdateSubResource(noPermClient, deploy, "status", func(deploy *appsv1.Deployment) {
				deploy.Annotations = placementAnnotation(placementAnnotationValue)
			})
			Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantNamespacedObjDenialMsg)).To(Succeed())
		})

		It("should allow updating the status of an annotated object without changing the annotation", func() {
			deploy := newDeployment(workNSName, fmt.Sprintf("deploy-%s", utils.RandStr()))
			deploy.Annotations = placementAnnotation(placementAnnotationValue)
			Expect(hubUncachedClient.Create(ctx, deploy)).To(Succeed(), "Failed to create an annotated deployment")

			err := getAndUpdateSubResource(noPermClient, deploy, "status", func(deploy *appsv1.Deployment) {
				deploy.Status.ObservedGeneration = deploy.Generation
			})
			Expect(err).To(Succeed(), "Failed to update the status of the annotated deployment")
		})

		// The scale subresource carries a Scale object rather than the Deployment itself.
		It("should allow scaling an annotated object", func() {
			deploy := newDeployment(workNSName, fmt.Sprintf("deploy-%s", utils.RandStr()))
			deploy.Annotations = placementAnnotation(placementAnnotationValue)
			Expect(hubUncachedClient.Create(ctx, deploy)).To(Succeed(), "Failed to create an annotated deployment")

			scale := &autoscalingv1.Scale{}
			Expect(noPermClient.SubResource("scale").Get(ctx, deploy, scale)).To(Succeed(), "Failed to get the scale of the annotated deployment")
			scale.Spec.Replicas = 3
			Expect(noPermClient.SubResource("scale").Update(ctx, deploy, client.WithSubResourceBody(scale))).To(Succeed(), "Failed to scale the annotated deployment")
		})

		DescribeTable("should deny adding the annotation to a namespace through a subresource if the caller cannot create or update cluster placement policies, and allow it otherwise",
			func(subResource string) {
				nsName := fmt.Sprintf("plain-%s", utils.RandStr())
				createNamespace(nsName)

				err := getAndUpdateSubResource(noPermClient, newNamespace(nsName, nil), subResource, func(ns *corev1.Namespace) {
					ns.Annotations = placementAnnotation(placementAnnotationValue)
				})
				Expect(wantDeniedByPolicy(err, annotatedForPlacementObjsVAPPolicyName, wantClusterScopedObjDenialMsg)).To(Succeed())

				err = getAndUpdateSubResource(cppClient, newNamespace(nsName, nil), subResource, func(ns *corev1.Namespace) {
					ns.Annotations = placementAnnotation(placementAnnotationValue)
				})
				Expect(err).To(Succeed(), "Failed to add the annotation to the namespace through the %s subresource", subResource)
			},
			Entry("status", "status"),
			Entry("finalize", "finalize"),
		)
	})

	AfterAll(func() {
		for _, name := range annotatedClusterRoleNames {
			Expect(client.IgnoreNotFound(hubUncachedClient.Delete(ctx, newClusterRole(name, nil)))).To(Succeed(), "Failed to delete cluster role %s", name)
		}
		revokeClusterWidePermissions(objWriterRBACName)
		revokeClusterWidePermissions(ppClusterWideRBACName)
		revokeClusterWidePermissions(cppRBACName)
	})
})
