/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package integration

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

var _ = Describe("Test Cluster Resource Placement Controller", func() {
	var clusterA, clusterB fleetv1alpha1.MemberCluster
	var clustarANamespace, clustarBNamespace corev1.Namespace
	var crp *fleetv1alpha1.ClusterResourcePlacement
	const ClusterRoleKind = "ClusterRole"
	var endpointSlice discoveryv1.EndpointSlice

	BeforeEach(func() {
		endpointSlice = discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nginx-export",
				Namespace: testService.GetNamespace(),
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{
				{
					Name: pointer.StringPtr("https"),
					Port: pointer.Int32Ptr(443),
				},
			},
		}
		By("Create member clusters", func() {
			// create a new cluster everytime since namespace deletion doesn't work in testenv
			clusterA = fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-a-" + utils.RandStr(),
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateJoin,
					Identity: rbacv1.Subject{
						Kind:      rbacv1.UserKind,
						Name:      "hub-access",
						Namespace: "app",
					},
				},
			}
			Expect(k8sClient.Create(ctx, &clusterA)).Should(Succeed())
		})

		By("Check if the member cluster namespace created")
		nsName := fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name: nsName,
			}, &clustarANamespace)
		}, timeout, interval).Should(Succeed())
		By(fmt.Sprintf("Cluster namespace %s created", nsName))
	})

	AfterEach(func() {
		By("Delete member clusters", func() {
			Expect(k8sClient.Delete(ctx, &clusterA)).Should(Succeed())
		})
	})

	Context("Test select resources functionality", func() {
		BeforeEach(func() {
			clusterB = fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-b-" + utils.RandStr(),
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateJoin,
					Identity: rbacv1.Subject{
						Kind:      rbacv1.UserKind,
						Name:      "hub-access",
						Namespace: "app",
					},
				},
			}
			Expect(k8sClient.Create(ctx, &clusterB)).Should(Succeed())

			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name: fmt.Sprintf(utils.NamespaceNameFormat, clusterB.Name),
				}, &clustarBNamespace)
			}, timeout, interval).Should(Succeed())

			By("Cluster namespace created")
		})

		AfterEach(func() {
			By("Delete member clusters", func() {
				Expect(k8sClient.Delete(ctx, &clusterB)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, crp)).Should(Succeed())
			})
		})

		It("Test select the resources by name", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-list-resource",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   rbacv1.GroupName,
							Version: "v1",
							Kind:    ClusterRoleKind,
							Name:    "test-cluster-role",
						},
						{
							Group:   apiextensionsv1.GroupName,
							Version: "v1",
							Kind:    "CustomResourceDefinition",
							Name:    "clonesets.apps.kruise.io",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select named resource clusterResourcePlacement created")

			// verify that we have created work objects that contain the resource selected
			verifyWorkObjects(crp, []string{ClusterRoleKind, "CustomResourceDefinition"}, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})

			var cloneSetCRD apiextensionsv1.CustomResourceDefinition
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "clonesets.apps.kruise.io",
			}, &cloneSetCRD)).Should(Succeed())
			placement, exist := cloneSetCRD.Annotations[utils.AnnotationPlacementList]
			Expect(controllerutil.ContainsFinalizer(&cloneSetCRD, utils.PlacementFinalizer)).Should(BeTrue())
			Expect(exist).Should(BeTrue())
			Expect(strings.Contains(placement, crp.Name))
		})

		It("Test select the resources by label", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resource-label-selector",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   rbacv1.GroupName,
							Version: "v1",
							Kind:    ClusterRoleKind,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"fleet.azure.com/name": "test",
								},
							},
						},
						{
							Group:   apiextensionsv1.GroupName,
							Version: "v1",
							Kind:    "CustomResourceDefinition",
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "fleet.azure.com/name",
										Operator: metav1.LabelSelectorOpExists,
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select resource by label clusterResourcePlacement created")

			// verify that we have created work objects that contain the resource selected
			verifyWorkObjects(crp, []string{ClusterRoleKind, "CustomResourceDefinition"}, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})

			var cloneSetCRD apiextensionsv1.CustomResourceDefinition
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "clonesets.apps.kruise.io",
			}, &cloneSetCRD)).Should(Succeed())
			placement, exist := cloneSetCRD.Annotations[utils.AnnotationPlacementList]
			Expect(controllerutil.ContainsFinalizer(&cloneSetCRD, utils.PlacementFinalizer)).Should(BeTrue())
			Expect(exist).Should(BeTrue())
			Expect(strings.Contains(placement, crp.Name))
		})

		It("Test select all the resources in a namespace", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-select-namespace",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"fleet.azure.com/name": "test",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select all the resources in a namespace clusterResourcePlacement created")

			By("Verify that only the valid resources in a namespace clusterResourcePlacement are selected")
			// we should not get anything else like the endpoints and endpointSlice
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})

			By("Make sure that the reconciler is stopped")
			waitForPlacementScheduleStopped(crp.Name)

			By("Create one more resources in the namespace")
			// this is a user created endpointSlice
			extraResource := endpointSlice.DeepCopy()
			Expect(k8sClient.Create(ctx, extraResource)).Should(Succeed())
			DeferCleanup(func() {
				By("Delete the extra resources in the namespace")
				Expect(k8sClient.Delete(ctx, extraResource)).Should(Succeed())
			})

			By("verify that new resources in a namespace are selected")
			waitForPlacementScheduleStopped(crp.Name)
			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(utils.WorkNameFormat, crp.Name),
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			By(fmt.Sprintf("validate work resource for cluster %s. It should contain %d manifests", clusterA.Name, len(namespacedResource)+1))
			Expect(len(clusterWork.Spec.Workload.Manifests)).Should(BeIdenticalTo(len(namespacedResource) + 1))
		})

		It("Test select only the propagated resources in a namespace", func() {
			By("Create a lease resource in the namespace")
			lease := coordv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-lease",
					Namespace: testNameSpace.Name,
				},
			}
			Expect(k8sClient.Create(ctx, &lease)).Should(Succeed())

			By("Create an endpoint resource in the namespace of the same name as the service")
			endpoints := corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testService.GetName(),
					Namespace: testService.GetNamespace(),
				},
			}
			Expect(k8sClient.Create(ctx, &endpoints)).Should(Succeed())

			By("Create an endpointSlice resource has a managed by label")
			mangedEPS := endpointSlice.DeepCopy()
			mangedEPS.Labels = map[string]string{discoveryv1.LabelManagedBy: "test-controller"}
			Expect(k8sClient.Create(ctx, mangedEPS)).Should(Succeed())

			By("Create an event in the namespace of the same name as the service")
			event := corev1.Event{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testService.GetName(),
					Namespace: testService.GetNamespace(),
				},
				Reason:  "test",
				Message: "test",
				InvolvedObject: corev1.ObjectReference{
					Name:      "test-obj",
					Namespace: testService.GetNamespace(),
				},
				EventTime:           metav1.NewMicroTime(time.Now()),
				ReportingController: "test-controller",
				ReportingInstance:   "test-instance",
				Action:              "normal",
			}
			Expect(k8sClient.Create(ctx, &event)).Should(Succeed())

			DeferCleanup(func() {
				By("Delete the extra resources in the namespace")
				Expect(k8sClient.Delete(ctx, &lease)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, &endpoints)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, mangedEPS)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, &event)).Should(Succeed())
			})

			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-select-namespace-check",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"fleet.azure.com/name": "test",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select all the resources in a namespace clusterResourcePlacement created")

			By("Verify that only the valid resources in a namespace clusterResourcePlacement are selected")
			// we should not select the endpoints and lease we created
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})
		})

		It("Test namespace scoped resource change picked up by placement", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-select-namespace-change",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"fleet.azure.com/name": "test",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select all the resources in a namespace clusterResourcePlacement created")

			By("Verify that only the valid resources in a namespace clusterResourcePlacement are selected")
			// we should not select the endpoints and lease we created
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})

			By("Create a new role resource in the namespace")
			newRoleName := "test-role-crud"
			role := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      newRoleName,
					Namespace: testService.GetNamespace(),
				},
				Rules: []rbacv1.PolicyRule{utils.FleetRule},
			}
			Expect(k8sClient.Create(ctx, &role)).Should(Succeed())

			By("Verify that we pick up the role")
			waitForPlacementScheduleStopped(crp.Name)
			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(utils.WorkNameFormat, crp.Name),
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())

			By(fmt.Sprintf("validate work resource for cluster %s. It should contain %d manifests", clusterA.Name, len(namespacedResource)+1))
			Expect(len(clusterWork.Spec.Workload.Manifests)).Should(BeIdenticalTo(len(namespacedResource) + 1))
			findRole := false
			for i, manifest := range clusterWork.Spec.Workload.Manifests {
				By(fmt.Sprintf("validate the %d uObj in the work resource in cluster A", i))
				var uObj unstructured.Unstructured
				GetObjectFromRawExtension(manifest.Raw, &uObj)
				if uObj.GroupVersionKind().Kind == "Role" && uObj.GroupVersionKind().Group == rbacv1.GroupName {
					var selectedRole rbacv1.Role
					GetObjectFromRawExtension(manifest.Raw, &selectedRole)
					Expect(len(selectedRole.Rules)).Should(BeEquivalentTo(1))
					if reflect.DeepEqual(selectedRole.Rules[0], utils.FleetRule) {
						findRole = true
						break
					}
				}
			}
			Expect(findRole).Should(BeTrue())
			By("Verified that the correct role is scheduled")

			By("Update the role resource in the namespace")
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      newRoleName,
				Namespace: testService.GetNamespace(),
			}, &role)).Should(Succeed())
			_, exist := role.Annotations[utils.AnnotationPlacementList]
			Expect(exist).Should(BeFalse())
			role.Rules = []rbacv1.PolicyRule{utils.FleetRule, utils.WorkRule}
			Expect(k8sClient.Update(ctx, &role)).Should(Succeed())

			By("Verify that we pick up the role change")
			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(utils.WorkNameFormat, crp.Name),
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			Expect(len(clusterWork.Spec.Workload.Manifests)).Should(BeIdenticalTo(len(namespacedResource) + 1))
			findRole = false
			for i, manifest := range clusterWork.Spec.Workload.Manifests {
				By(fmt.Sprintf("validate the %d uObj in the work resource in cluster A", i))
				var uObj unstructured.Unstructured
				GetObjectFromRawExtension(manifest.Raw, &uObj)
				if uObj.GroupVersionKind().Kind == "Role" && uObj.GroupVersionKind().Group == rbacv1.GroupName {
					var selectedRole rbacv1.Role
					GetObjectFromRawExtension(manifest.Raw, &selectedRole)
					Expect(len(selectedRole.Rules)).Should(BeEquivalentTo(2))
					if reflect.DeepEqual(selectedRole.Rules[0], utils.FleetRule) &&
						reflect.DeepEqual(selectedRole.Rules[1], utils.WorkRule) {
						findRole = true
						break
					}
				}
			}
			Expect(findRole).Should(BeTrue())
			By("Verified that the role change is picked")

			By("Remove the role resource in the namespace")
			Expect(k8sClient.Delete(ctx, &role)).Should(Succeed())

			By("Verify that we pick up the role delete")
			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(utils.WorkNameFormat, crp.Name),
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})
			By("Verified that the deleted role is removed from the work")
		})

		It("Test cluster scoped resource change picked up by placement", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resource-test-change",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   rbacv1.GroupName,
							Version: "v1",
							Kind:    ClusterRoleKind,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"fleet.azure.com/name": "test",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select resource by label clusterResourcePlacement created")

			// verify that we have created work objects that contain the resource selected
			verifyWorkObjects(crp, []string{ClusterRoleKind}, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})

			By("Create a new clusterRole resource")
			newClusterRoleName := "test-clusterRole-crud"
			clusterRole := rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-clusterRole-crud",
					Labels: map[string]string{"fleet.azure.com/name": "test"},
				},
				Rules: []rbacv1.PolicyRule{utils.FleetRule},
			}
			Expect(k8sClient.Create(ctx, &clusterRole)).Should(Succeed())

			By("Verify that we pick up the clusterRole")
			waitForPlacementScheduleStopped(crp.Name)
			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(utils.WorkNameFormat, crp.Name),
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			By(fmt.Sprintf("validate work resource for cluster %s. It should contain %d manifests", clusterA.Name, 2))
			Expect(len(clusterWork.Spec.Workload.Manifests)).Should(BeIdenticalTo(2))
			findClusterRole := false
			for i, manifest := range clusterWork.Spec.Workload.Manifests {
				By(fmt.Sprintf("validate the %d uObj in the work resource in cluster A", i))
				var uObj unstructured.Unstructured
				GetObjectFromRawExtension(manifest.Raw, &uObj)
				if uObj.GroupVersionKind().Kind == ClusterRoleKind && uObj.GroupVersionKind().Group == rbacv1.GroupName {
					var selectedRole rbacv1.ClusterRole
					GetObjectFromRawExtension(manifest.Raw, &selectedRole)
					Expect(len(selectedRole.Rules)).Should(BeEquivalentTo(1))
					if reflect.DeepEqual(selectedRole.Rules[0], utils.FleetRule) {
						findClusterRole = true
						break
					}
				}
			}
			Expect(findClusterRole).Should(BeTrue())
			By("Verified that the correct clusterRole is scheduled")

			By("Update the clusterRole resource")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: newClusterRoleName}, &clusterRole)).Should(Succeed())
			_, exist := clusterRole.Annotations[utils.AnnotationPlacementList]
			Expect(exist).Should(BeTrue())
			clusterRole.Rules = []rbacv1.PolicyRule{utils.FleetRule, utils.WorkRule}
			Expect(k8sClient.Update(ctx, &clusterRole)).Should(Succeed())

			By("Verify that we pick up the clusterRole change")
			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(utils.WorkNameFormat, crp.Name),
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			Expect(len(clusterWork.Spec.Workload.Manifests)).Should(BeIdenticalTo(2))
			findClusterRole = false
			for i, manifest := range clusterWork.Spec.Workload.Manifests {
				By(fmt.Sprintf("validate the %d uObj in the work resource in cluster A", i))
				var uObj unstructured.Unstructured
				GetObjectFromRawExtension(manifest.Raw, &uObj)
				if uObj.GroupVersionKind().Kind == ClusterRoleKind && uObj.GroupVersionKind().Group == rbacv1.GroupName {
					var selectedRole rbacv1.ClusterRole
					GetObjectFromRawExtension(manifest.Raw, &selectedRole)
					Expect(len(selectedRole.Rules)).Should(BeEquivalentTo(2))
					if reflect.DeepEqual(selectedRole.Rules[0], utils.FleetRule) &&
						reflect.DeepEqual(selectedRole.Rules[1], utils.WorkRule) {
						findClusterRole = true
						break
					}
				}
			}
			Expect(findClusterRole).Should(BeTrue())
			By("Verified that the clusterRole change is picked")

			By("Delete the clusterRole resources")
			Expect(k8sClient.Delete(ctx, &clusterRole)).Should(Succeed())

			By("Verify that we pick up the clusterRole delete")
			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(utils.WorkNameFormat, crp.Name),
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			verifyWorkObjects(crp, []string{ClusterRoleKind}, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})
			By("Verified that the deleted clusterRole is removed from the work")

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: newClusterRoleName}, &clusterRole)).Should(utils.NotFoundMatcher{})
		})

		It("Test delete the entire namespace resource", func() {
			nsLabel := map[string]string{"fleet.azure.com/name": "test-delete"}
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-select-namespace-change",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: nsLabel,
							},
						},
						{
							Group:   rbacv1.GroupName,
							Version: "v1",
							Kind:    ClusterRoleKind,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"fleet.azure.com/name": "test",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select all the resources in a namespace clusterResourcePlacement created")

			By("Create a new namespace")
			newSpace := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-delete-namespace" + utils.RandStr(),
					Labels: nsLabel,
				},
			}
			Expect(k8sClient.Create(ctx, &newSpace)).Should(Succeed())
			By(fmt.Sprintf("Create a new namespace %s clusterResourcePlacement will select", newSpace.Name))

			By("Create a new Pdb in the new namespace")
			newPdb := testPdb.DeepCopy()
			newPdb.Namespace = newSpace.Name
			newPdb.SetResourceVersion("")
			newPdb.SetGeneration(0)
			Expect(k8sClient.Create(ctx, newPdb)).Should(Succeed())

			By("Create a new CloneSet in the new namespace")
			newCloneSet := testCloneset.DeepCopy()
			newCloneSet.Namespace = newSpace.Name
			newCloneSet.SetResourceVersion("")
			newCloneSet.SetGeneration(0)
			Expect(k8sClient.Create(ctx, newCloneSet)).Should(Succeed())

			By("Verify that we pick up the clusterRole and all the resources we created in the new namespace")
			verifyPartialWorkObjects(crp, []string{"PodDisruptionBudget", "CloneSet", ClusterRoleKind}, 4, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})

			By("Remove the namespace resource")
			Expect(k8sClient.Delete(ctx, &newSpace)).Should(Succeed())

			By("Verify that we pick up the namespace delete")
			verifyWorkObjects(crp, []string{ClusterRoleKind}, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})
		})
	})

	Context("Test select cluster functionality", func() {
		BeforeEach(func() {
			clusterB = fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-b-" + utils.RandStr(),
					Labels: map[string]string{
						"fleet.azure.com/name": "test-b",
					},
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateJoin,
					Identity: rbacv1.Subject{
						Kind:      rbacv1.UserKind,
						Name:      "hub-access",
						Namespace: "app",
					},
				},
			}
			Expect(k8sClient.Create(ctx, &clusterB)).Should(Succeed())
			nsName := fmt.Sprintf(utils.NamespaceNameFormat, clusterB.Name)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name: nsName,
				}, &clustarBNamespace)
			}, timeout, interval).Should(Succeed())
			By(fmt.Sprintf("Cluster namespace %s created", nsName))
		})

		AfterEach(func() {
			By("Delete member clusters", func() {
				Expect(k8sClient.Delete(ctx, &clusterB)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, crp)).Should(Succeed())
			})
		})

		It("Test select named cluster resources", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-list-resource",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   rbacv1.GroupName,
							Version: "v1",
							Kind:    ClusterRoleKind,
							Name:    "test-cluster-role",
						},
						{
							Group:   apiextensionsv1.GroupName,
							Version: "v1",
							Kind:    "CustomResourceDefinition",
							Name:    "clonesets.apps.kruise.io",
						},
					},
					Policy: &fleetv1alpha1.PlacementPolicy{
						ClusterNames: []string{clusterA.Name},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select named cluster clusterResourcePlacement created")

			// verify that we have created work objects that contain the resource selected
			verifyWorkObjects(crp, []string{ClusterRoleKind, "CustomResourceDefinition"}, []*fleetv1alpha1.MemberCluster{&clusterA})

			var cloneSetCRD apiextensionsv1.CustomResourceDefinition
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "clonesets.apps.kruise.io",
			}, &cloneSetCRD)).Should(Succeed())
			placement, exist := cloneSetCRD.Annotations[utils.AnnotationPlacementList]
			Expect(controllerutil.ContainsFinalizer(&cloneSetCRD, utils.PlacementFinalizer)).Should(BeTrue())
			Expect(exist).Should(BeTrue())
			Expect(strings.Contains(placement, crp.Name))

			By("Verify that work is not created in cluster B")
			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(utils.WorkNameFormat, crp.Name),
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterB.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})
		})

		It("Test select cluster resources by label", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-list-resource",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"fleet.azure.com/name": "test",
								},
							},
						},
					},
					Policy: &fleetv1alpha1.PlacementPolicy{
						Affinity: &fleetv1alpha1.Affinity{
							ClusterAffinity: &fleetv1alpha1.ClusterAffinity{
								ClusterSelectorTerms: []fleetv1alpha1.ClusterSelectorTerm{
									{
										LabelSelector: metav1.LabelSelector{
											MatchLabels: clusterB.Labels,
										},
									},
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select label cluster clusterResourcePlacement created")

			// verify that we have created work objects that contain the resource selected
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterB})

			By("Verify that work is not created in cluster A")
			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(utils.WorkNameFormat, crp.Name),
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})

			By("Add the matching label to cluster A")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterA.GetName()}, &clusterA)).Should(Succeed())
			clusterA.Labels = clusterB.Labels
			Expect(k8sClient.Update(ctx, &clusterA)).Should(Succeed())

			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterB, &clusterA})
			By("Verified that the work is also propagated to cluster A")

			By("Remove the matching label from cluster A")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterA.GetName()}, &clusterA)).Should(Succeed())
			clusterA.Labels = map[string]string{"random": "test"}
			Expect(k8sClient.Update(ctx, &clusterA)).Should(Succeed())

			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterB})
			By("Verify that work is removed from cluster A")
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(utils.WorkNameFormat, crp.Name),
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})

			By("Verified that the work is removed from cluster A")
		})

		It("Test  member cluster create/delete trigger placement", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-list-resource",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"fleet.azure.com/name": "test",
								},
							},
						},
					},
					Policy: &fleetv1alpha1.PlacementPolicy{
						Affinity: &fleetv1alpha1.Affinity{
							ClusterAffinity: &fleetv1alpha1.ClusterAffinity{
								ClusterSelectorTerms: []fleetv1alpha1.ClusterSelectorTerm{
									{
										LabelSelector: metav1.LabelSelector{
											MatchLabels: clusterB.Labels,
										},
									},
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select label cluster clusterResourcePlacement created")

			// verify that we have created work objects that contain the resource selected
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterB})
			By("Verified that the work is propagated to cluster B")

			By("Create a new member cluster C")
			// create a new cluster everytime since namespace deletion doesn't work in testenv
			clusterC := fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-c-" + utils.RandStr(),
					Labels: clusterB.Labels,
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateJoin,
					Identity: rbacv1.Subject{
						Kind:      rbacv1.UserKind,
						Name:      "hub-access-c",
						Namespace: "app",
					},
				},
			}
			Expect(k8sClient.Create(ctx, &clusterC)).Should(Succeed())

			// verify that we have created work objects that contain the resource selected
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterB, &clusterC})
			By("Verified that the work is also propagated to cluster C")

			By("Delete the member cluster C")
			Expect(k8sClient.Delete(ctx, &clusterC)).Should(Succeed())
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterB})
			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf(utils.WorkNameFormat, crp.Name),
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})
			By("Verified that the work is removed from cluster C")
		})
	})
})
