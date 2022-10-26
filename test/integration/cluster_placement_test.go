/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package integration

import (
	"fmt"
	"reflect"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	adminv1 "k8s.io/api/admissionregistration/v1"
	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/pointer"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/controllers/clusterresourceplacement"
	workapi "go.goms.io/fleet/pkg/controllers/work"
	"go.goms.io/fleet/pkg/utils"
)

const ClusterRoleKind = "ClusterRole"

var _ = Describe("Test Cluster Resource Placement Controller", func() {
	var clusterA, clusterB fleetv1alpha1.MemberCluster
	var clustarANamespace, clustarBNamespace corev1.Namespace
	var crp *fleetv1alpha1.ClusterResourcePlacement
	var endpointSlice discoveryv1.EndpointSlice

	BeforeEach(func() {
		By("Create member cluster A ")
		// create a new cluster everytime since namespace deletion doesn't work in testenv
		clusterA = fleetv1alpha1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "cluster-a-" + utilrand.String(8),
				Labels: map[string]string{"clusterA": utilrand.String(10)},
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
		By("Check if the member cluster namespace is created")
		nsName := fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name: nsName,
			}, &clustarANamespace)
		}, timeout, interval).Should(Succeed())
		By(fmt.Sprintf("Cluster namespace %s created", nsName))

		By("Create member cluster B")
		// Create cluster B and wait for its namespace created
		clusterB = fleetv1alpha1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "cluster-b-" + utilrand.String(8),
				Labels: map[string]string{"clusterB": utilrand.String(10)},
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
		By("Check if the member cluster namespace is created")
		nsName = fmt.Sprintf(utils.NamespaceNameFormat, clusterB.Name)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name: fmt.Sprintf(utils.NamespaceNameFormat, clusterB.Name),
			}, &clustarBNamespace)
		}, timeout, interval).Should(Succeed())
		By(fmt.Sprintf("Cluster namespace %s created", nsName))
	})

	AfterEach(func() {
		By("Delete member clusters", func() {
			Expect(k8sClient.Delete(ctx, &clusterA)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			Expect(k8sClient.Delete(ctx, &clusterB)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			Expect(k8sClient.Delete(ctx, crp)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		})
	})

	Context("Test select resources functionality", func() {
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

			By("Mark member cluster A as joined")
			markInternalMCJoined(clusterA)

			By("Mark member cluster B as joined")
			markInternalMCJoined(clusterB)
		})

		It("Test select the resources by name happy path", func() {
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

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, 2, 2, metav1.ConditionTrue)
			verifyPlacementApplyStatus(crp, metav1.ConditionUnknown, clusterresourceplacement.ApplyPendingReason)

			By("Mimic work apply succeeded")
			markWorkAppliedStatusSuccess(crp, &clusterA)
			markWorkAppliedStatusSuccess(crp, &clusterB)

			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementApplyStatus(crp, metav1.ConditionTrue, clusterresourceplacement.ApplySucceededReason)
		})

		It("Test select the resources by name not found", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-list-resource",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   apiextensionsv1.GroupName,
							Version: "v1",
							Kind:    "CustomResourceDefinition",
							Name:    "doesnotexist",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select named resource clusterResourcePlacement created")

			// verify that we have created work objects that contain the resource selected
			waitForPlacementScheduled(crp.GetName())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, 0, 0, metav1.ConditionFalse)

			//add a valid cluster
			By("Select named cluster clusterResourcePlacement updated")
			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors, fleetv1alpha1.ClusterResourceSelector{
				Group:   rbacv1.GroupName,
				Version: "v1",
				Kind:    ClusterRoleKind,
				Name:    "test-cluster-role",
			})
			Expect(k8sClient.Update(ctx, crp)).Should(Succeed())
			By("verify that we have created work objects in the newly selected cluster")
			verifyWorkObjects(crp, []string{ClusterRoleKind}, []*fleetv1alpha1.MemberCluster{&clusterA})
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, 1, 2, metav1.ConditionTrue)
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

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, 2, 2, metav1.ConditionTrue)
			verifyPlacementApplyStatus(crp, metav1.ConditionUnknown, clusterresourceplacement.ApplyPendingReason)

			By("Mimic work apply succeeded")
			markWorkAppliedStatusSuccess(crp, &clusterA)
			markWorkAppliedStatusSuccess(crp, &clusterB)

			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementApplyStatus(crp, metav1.ConditionTrue, clusterresourceplacement.ApplySucceededReason)
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
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, len(namespacedResource)+1, 2, metav1.ConditionTrue)

			By("verify that new resources in a namespace are placed in the work")
			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			By(fmt.Sprintf("validate work resource for cluster %s. It should contain %d manifests", clusterA.Name, len(namespacedResource)+1))
			Expect(len(clusterWork.Spec.Workload.Manifests)).Should(BeIdenticalTo(len(namespacedResource) + 1))
		})

		It("Test select blocked namespace", func() {
			By("Create a select blocked namespace clusterResourcePlacement")
			blockedNameSpace := "fleet-" + utilrand.String(10)
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
							Name:    blockedNameSpace,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())

			By("Verify that the CPR failed with scheduling error")
			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			schedCond := crp.GetCondition(string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled))
			Expect(schedCond).ShouldNot(BeNil())
			Expect(schedCond.Status).Should(Equal(metav1.ConditionFalse))
			Expect(schedCond.Message).Should(ContainSubstring(fmt.Sprintf("namespace %s is not allowed to propagate", blockedNameSpace)))

			By("Update the CRP to place default namespace")
			crp.Spec.ResourceSelectors = []fleetv1alpha1.ClusterResourceSelector{
				{
					Group:   corev1.GroupName,
					Version: "v1",
					Kind:    "Namespace",
					Name:    "default",
				},
			}
			Expect(k8sClient.Update(ctx, crp)).Should(Succeed())

			By("Verify that the CPR failed with scheduling error")
			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			schedCond = crp.GetCondition(string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled))
			Expect(schedCond).ShouldNot(BeNil())
			Expect(schedCond.Status).Should(Equal(metav1.ConditionFalse))
			Expect(schedCond.Message).Should(ContainSubstring("namespace default is not allowed to propagate"))
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
				Name:      crp.Name,
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
			role.Rules = []rbacv1.PolicyRule{utils.FleetRule, utils.WorkRule}
			Expect(k8sClient.Update(ctx, &role)).Should(Succeed())

			By("Verify that we pick up the role change")
			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
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
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})
			By("Verified that the deleted role is removed from the work")
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
					Name:   "test-delete-namespace" + utilrand.String(10),
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
				Name:      crp.Name,
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
			clusterRole.Rules = []rbacv1.PolicyRule{utils.FleetRule, utils.WorkRule}
			Expect(k8sClient.Update(ctx, &clusterRole)).Should(Succeed())

			By("Verify that we pick up the clusterRole change")
			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
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
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			verifyWorkObjects(crp, []string{ClusterRoleKind}, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})
			By("Verified that the deleted clusterRole is removed from the work")

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: newClusterRoleName}, &clusterRole)).Should(utils.NotFoundMatcher{})
		})
	})

	Context("Test basic select cluster functionality, only cluster A is joined", func() {
		BeforeEach(func() {
			By("Mark member cluster A as joined")
			markInternalMCJoined(clusterA)
		})

		It("Test no matching cluster", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-select-cluster",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   rbacv1.GroupName,
							Version: "v1",
							Kind:    ClusterRoleKind,
							Name:    "test-cluster-role",
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
			By("Select named cluster clusterResourcePlacement created")

			waitForPlacementScheduled(crp.GetName())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, 0, 0, metav1.ConditionFalse)

			By("Verify that work is not created in any cluster")
			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterB.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})
		})

		It("Test select named cluster resources with status change", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-list-cluster",
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
						// Although both clusters are listed, only clusterA is selected as clusterB hasn't joined yet.
						ClusterNames: []string{clusterA.Name, clusterB.Name},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select named cluster clusterResourcePlacement created")

			// verify that we have created work objects that contain the resource selected
			verifyWorkObjects(crp, []string{ClusterRoleKind, "CustomResourceDefinition"}, []*fleetv1alpha1.MemberCluster{&clusterA})

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, 2, 1, metav1.ConditionTrue)

			By("Verify that work is not created in cluster B")
			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterB.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})

			By("Verify that work is created in cluster B after it joins")
			markInternalMCJoined(clusterB)
			markInternalMCLeft(clusterA)
			verifyWorkObjects(crp, []string{ClusterRoleKind, "CustomResourceDefinition"}, []*fleetv1alpha1.MemberCluster{&clusterB})

			By("Verify that work is removed from cluster A after it leaves")
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})

		})

		It("Test select named cluster does not exist", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-list-cluster",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   rbacv1.GroupName,
							Version: "v1",
							Kind:    ClusterRoleKind,
							Name:    "test-cluster-role",
						},
					},
					Policy: &fleetv1alpha1.PlacementPolicy{
						ClusterNames: []string{"doesnotexist"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			By("Select named cluster clusterResourcePlacement created")

			waitForPlacementScheduled(crp.GetName())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, 0, 0, metav1.ConditionFalse)

			//add a valid cluster
			By("Select named cluster clusterResourcePlacement updated")
			crp.Spec.Policy.ClusterNames = append(crp.Spec.Policy.ClusterNames, clusterA.Name)
			Expect(k8sClient.Update(ctx, crp)).Should(Succeed())
			waitForPlacementScheduled(crp.GetName())
			By("verify that we have created work objects in the newly selected cluster")
			verifyWorkObjects(crp, []string{ClusterRoleKind}, []*fleetv1alpha1.MemberCluster{&clusterA})
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, 1, 1, metav1.ConditionTrue)
		})

		It("Test select member cluster by label with change", func() {
			markInternalMCJoined(clusterB)
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-select-cluster",
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
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, len(namespacedResource), 1, metav1.ConditionTrue)
			verifyPlacementApplyStatus(crp, metav1.ConditionUnknown, clusterresourceplacement.ApplyPendingReason)

			By("Verify that work is not created in cluster A")
			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
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
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})
			By("Verified that the work is removed from cluster A")
		})

		It("Test member cluster join/leave trigger placement", func() {
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
											MatchLabels: clusterA.Labels,
										},
									},
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
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterA})
			By("Verified that the work is propagated to cluster A")

			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterB.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})
			By("Verified that the work is not scheduled to cluster B")

			By("mark the member cluster B joined")
			markInternalMCJoined(clusterB)

			// verify that we have created work objects that contain the resource selected
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})
			By("Verified that the work is also propagated to cluster B")

			By("mark the member cluster B left")
			markInternalMCLeft(clusterB)
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterA})

			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterB.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})
			By("Verified that the work is removed from cluster C")
		})
	})

	Context("Test advanced placement functionality", func() {
		BeforeEach(func() {
			By("Mark member cluster A as joined")
			markInternalMCJoined(clusterA)
		})

		It("Test cluster scoped resource change unpick by a placement", func() {
			By("create cluster role binding")
			crb := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-role-binding",
					Labels: map[string]string{
						"fleet.azure.com/name": "test",
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     ClusterRoleKind,
					Name:     "test-cluster-role",
				},
			}
			Expect(k8sClient.Create(ctx, crb)).Should(Succeed())

			By("create cluster resource placement")
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resource-select",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   rbacv1.GroupName,
							Version: "v1",
							Kind:    "ClusterRoleBinding",
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

			// verify that we have created the work object
			var clusterWork workv1alpha1.Work
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: crp.Name, Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name)}, &clusterWork); err != nil {
					return err
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to retrieve %s work", crp.Name)

			// Apply is Pending because work api controller is not being run for this test suite
			fleetResourceIdentifier := fleetv1alpha1.ResourceIdentifier{
				Group:   rbacv1.GroupName,
				Version: "v1",
				Kind:    "ClusterRoleBinding",
				Name:    "test-cluster-role-binding",
			}
			wantCRPStatus := fleetv1alpha1.ClusterResourcePlacementStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled),
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 1,
						Reason:             "ScheduleSucceeded",
					},
					{
						Type:               string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
						Status:             metav1.ConditionUnknown,
						ObservedGeneration: 1,
						Reason:             "ApplyPending",
					},
				},
				SelectedResources: []fleetv1alpha1.ResourceIdentifier{fleetResourceIdentifier},
				TargetClusters:    []string{clusterA.Name},
			}

			crpStatusCmpOptions := []cmp.Option{
				cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message"),
				cmpopts.SortSlices(func(ref1, ref2 metav1.Condition) bool { return ref1.Type < ref2.Type }),
			}

			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp); err != nil {
					return err
				}
				if diff := cmp.Diff(wantCRPStatus, crp.Status, crpStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status(%s) mismatch (-want +got):\n%s", crp.Name, diff)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to compare actual and expected CRP status in hub cluster")

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			By("Update cluster role binding such that CRP doesn't pick it up")
			crb.ObjectMeta.Labels = map[string]string{
				"fleet.azure.com/env": "prod",
			}
			Expect(k8sClient.Update(ctx, crb)).Should(Succeed())

			// verify that the work object created is not present anymore since we are not picking the cluster role binding
			nsName := fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name)
			Eventually(func() bool {
				var clusterWork workv1alpha1.Work
				return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name:      crp.Name,
					Namespace: nsName,
				}, &clusterWork))
			}, timeout, interval).Should(BeTrue())
			By("Verified the work object is removed")

			wantCRPStatus = fleetv1alpha1.ClusterResourcePlacementStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						Reason:             "ScheduleFailed",
					},
					{
						Type:               string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
						Status:             metav1.ConditionUnknown,
						ObservedGeneration: 1,
						Reason:             "ApplyPending",
					},
				},
			}

			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp); err != nil {
					return err
				}
				if diff := cmp.Diff(wantCRPStatus, crp.Status, crpStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status(%s) mismatch (-want +got):\n%s", crp.Name, diff)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to compare actual and expected CRP status in hub cluster")

			By("Delete cluster role binding")
			Expect(k8sClient.Delete(ctx, crb)).Should(Succeed())
		})

		It("Test a cluster scoped resource selected by multiple placements", func() {
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resource-select",
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
			verifyWorkObjects(crp, []string{ClusterRoleKind}, []*fleetv1alpha1.MemberCluster{&clusterA})

			By("Create another placement that can select the same resource")
			crp2 := &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resource-select-2",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   rbacv1.GroupName,
							Version: "v1",
							Kind:    ClusterRoleKind,
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
			Expect(k8sClient.Create(ctx, crp2)).Should(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, crp2)).Should(Succeed())
			})
			By("the second clusterResourcePlacement created")
			// verify that we have created work objects that contain the resource selected
			verifyWorkObjects(crp2, []string{ClusterRoleKind}, []*fleetv1alpha1.MemberCluster{&clusterA})

			By("Update the testClusterRole  resource")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testClusterRole.Name}, &testClusterRole)).Should(Succeed())
			testClusterRole.Rules = []rbacv1.PolicyRule{utils.FleetRule, utils.WorkRule}
			Expect(k8sClient.Update(ctx, &testClusterRole)).Should(Succeed())

			By("Verify that we pick up the clusterRole change")
			waitForPlacementScheduleStopped(crp.Name)
			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			Expect(len(clusterWork.Spec.Workload.Manifests)).Should(BeIdenticalTo(1))
			manifest := clusterWork.Spec.Workload.Manifests[0]
			var selectedRole rbacv1.ClusterRole
			GetObjectFromRawExtension(manifest.Raw, &selectedRole)
			Expect(len(selectedRole.Rules)).Should(BeEquivalentTo(2))
			Expect(reflect.DeepEqual(selectedRole.Rules[0], utils.FleetRule) &&
				reflect.DeepEqual(selectedRole.Rules[1], utils.WorkRule)).Should(BeTrue())
			By("Verified that the clusterRole change is picked by crp")

			waitForPlacementScheduleStopped(crp2.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp2.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			Expect(len(clusterWork.Spec.Workload.Manifests)).Should(BeIdenticalTo(1))
			manifest = clusterWork.Spec.Workload.Manifests[0]
			GetObjectFromRawExtension(manifest.Raw, &selectedRole)
			Expect(len(selectedRole.Rules)).Should(BeEquivalentTo(2))
			Expect(reflect.DeepEqual(selectedRole.Rules[0], utils.FleetRule) &&
				reflect.DeepEqual(selectedRole.Rules[1], utils.WorkRule)).Should(BeTrue())
			By("Verified that the clusterRole change is picked by crp2")
		})

		It("Test a placement select some clusters and then not any", func() {
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
											MatchLabels: clusterA.Labels,
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
			verifyWorkObjects(crp, namespacedResource, []*fleetv1alpha1.MemberCluster{&clusterA})
			By("Verified that the work is propagated to cluster A")

			By("mark the member cluster A left")
			markInternalMCLeft(clusterA)

			// verify that we have created work objects that contain the resource selected
			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, 0, 0, metav1.ConditionFalse)
			By("Verified that placement has nothing scheduled")

			var clusterWork workv1alpha1.Work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})
			By("Verified that the work is removed from cluster A")
		})

		It("Test a placement select some resources and then not any", func() {
			By("Create a webhook resource")
			webhookName := "test-mutating-webhook"
			sideEffect := adminv1.SideEffectClassNone
			mutatingWebhook := adminv1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      webhookName,
					Namespace: testService.GetNamespace(),
				},
				Webhooks: []adminv1.MutatingWebhook{
					{
						Name: "test.azure.com",
						Rules: []adminv1.RuleWithOperations{
							{
								Operations: []adminv1.OperationType{
									adminv1.OperationAll,
								},
								Rule: adminv1.Rule{
									APIGroups:   []string{"*"},
									APIVersions: []string{"*"},
									Resources:   []string{"pod"},
								},
							},
						},
						ClientConfig: adminv1.WebhookClientConfig{
							URL: pointer.String("https://test.azure.com/test-crp"),
						},
						AdmissionReviewVersions: []string{"v1"},
						SideEffects:             &sideEffect,
					},
				},
			}
			Expect(k8sClient.Create(ctx, &mutatingWebhook)).Should(Succeed())

			By("Select webhook clusterResourcePlacement created")
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resource-test-change",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   adminv1.GroupName,
							Version: "v1",
							Kind:    "MutatingWebhookConfiguration",
							Name:    webhookName,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())

			By("verify that we have created work objects that contain the resource selected")
			var clusterWork workv1alpha1.Work
			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())
			Expect(len(clusterWork.Spec.Workload.Manifests)).Should(Equal(1))
			var uObj unstructured.Unstructured
			GetObjectFromRawExtension(clusterWork.Spec.Workload.Manifests[0].Raw, &uObj)
			Expect(uObj.GroupVersionKind().Kind).Should(Equal("MutatingWebhookConfiguration"))

			By("Delete the webhook resources")
			Expect(k8sClient.Delete(ctx, &mutatingWebhook)).Should(Succeed())

			By("Verify that do not schedule anything")
			waitForPlacementScheduleStopped(crp.Name)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)).Should(Succeed())
			verifyPlacementScheduleStatus(crp, 0, 0, metav1.ConditionFalse)
			By("Verified that placement has nothing scheduled")

			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(utils.NotFoundMatcher{})
			By("Verified that the deleted webhook is removed from the work")
		})
	})

	Context("Test with simulated work api functionality", func() {
		BeforeEach(func() {
			By("Mark member cluster A as joined")
			markInternalMCJoined(clusterA)

			By("Mark member cluster B as joined")
			markInternalMCJoined(clusterB)
		})

		It("Test force delete member cluster after work agent lost connection/deleted", func() {
			By("create clusterResourcePlacement CR")
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

			By("verify that we have created work objects that contain the resource selected")
			verifyWorkObjects(crp, []string{ClusterRoleKind}, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})

			By("add finalizer to the work and mark it as applied")
			markWorkAppliedStatusSuccess(crp, &clusterA)

			By("mark the member cluster left")
			markInternalMCLeft(clusterA)

			By("delete the member cluster")
			Expect(k8sClient.Delete(ctx, &clusterA)).Should(Succeed())

			// the namespace won't be deleted as the GC controller does not run there
			By("verify that the work is deleted")
			nsName := fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name)
			Eventually(func() bool {
				var clusterWork workv1alpha1.Work
				return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name:      crp.Name,
					Namespace: nsName,
				}, &clusterWork))
			}, timeout, interval).Should(BeTrue())

			By("verify that the member cluster is deleted")
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name: clusterA.Name,
				}, &clusterA))
			}, timeout, interval).Should(BeTrue())
		})

		It("Test partial work failed to apply", func() {
			By("create clusterResourcePlacement CR")
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
							Name:    "test-cluster-role",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())

			By("verify that we have created work objects that contain the resource selected")
			verifyWorkObjects(crp, []string{ClusterRoleKind}, []*fleetv1alpha1.MemberCluster{&clusterA, &clusterB})

			var clusterWork workv1alpha1.Work
			workResourceIdentifier := workv1alpha1.ResourceIdentifier{
				Group:    rbacv1.GroupName,
				Kind:     ClusterRoleKind,
				Name:     "test-cluster-role",
				Ordinal:  0,
				Resource: "clusterroles",
				Version:  "v1",
			}

			// update work for clusterA to have applied condition as true for manifest and work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())

			appliedCondition := metav1.Condition{
				Type:               workapi.ConditionTypeApplied,
				Status:             metav1.ConditionTrue,
				Reason:             "appliedWorkComplete",
				ObservedGeneration: clusterWork.GetGeneration(),
				LastTransitionTime: metav1.Now(),
			}

			manifestCondition := workv1alpha1.ManifestCondition{
				Identifier: workResourceIdentifier,
				Conditions: []metav1.Condition{
					{
						Type:               workapi.ConditionTypeApplied,
						Status:             metav1.ConditionTrue,
						Reason:             "ManifestCreated",
						LastTransitionTime: metav1.Now(),
					},
				},
			}

			clusterWork.Status.Conditions = []metav1.Condition{appliedCondition}
			clusterWork.Status.ManifestConditions = []workv1alpha1.ManifestCondition{manifestCondition}
			Expect(k8sClient.Status().Update(ctx, &clusterWork)).Should(Succeed())

			// update work for clusterB to have applied condition as false for manifest and work
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterB.Name),
			}, &clusterWork)).Should(Succeed())

			appliedCondition = metav1.Condition{
				Type:               workapi.ConditionTypeApplied,
				Status:             metav1.ConditionFalse,
				Reason:             "appliedWorkFailed",
				ObservedGeneration: clusterWork.GetGeneration(),
				LastTransitionTime: metav1.Now(),
			}

			manifestCondition = workv1alpha1.ManifestCondition{
				Identifier: workResourceIdentifier,
				Conditions: []metav1.Condition{
					{
						Type:               workapi.ConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						Reason:             "appliedManifestFailed",
						LastTransitionTime: metav1.Now(),
					},
				},
			}

			clusterWork.Status.Conditions = []metav1.Condition{appliedCondition}
			clusterWork.Status.ManifestConditions = []workv1alpha1.ManifestCondition{manifestCondition}
			Expect(k8sClient.Status().Update(ctx, &clusterWork)).Should(Succeed())

			fleetResourceIdentifier := fleetv1alpha1.ResourceIdentifier{
				Group:   rbacv1.GroupName,
				Version: "v1",
				Kind:    ClusterRoleKind,
				Name:    "test-cluster-role",
			}
			wantCRPStatus := fleetv1alpha1.ClusterResourcePlacementStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled),
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 1,
						Reason:             "ScheduleSucceeded",
					},
					{
						Type:               string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						Reason:             "ApplyFailed",
					},
				},
				SelectedResources: []fleetv1alpha1.ResourceIdentifier{fleetResourceIdentifier},
				TargetClusters:    []string{clusterA.Name, clusterB.Name},
				FailedResourcePlacements: []fleetv1alpha1.FailedResourcePlacement{
					{
						ResourceIdentifier: fleetResourceIdentifier,
						ClusterName:        clusterB.Name,
						Condition: metav1.Condition{
							Type:               string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 0,
							Reason:             "appliedManifestFailed",
						},
					},
				},
			}

			crpStatusCmpOptions := []cmp.Option{
				cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message"),
				cmpopts.SortSlices(func(ref1, ref2 metav1.Condition) bool { return ref1.Type < ref2.Type }),
				cmpopts.SortSlices(func(ref1, ref2 string) bool { return ref1 < ref2 }),
			}

			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp); err != nil {
					return err
				}
				if diff := cmp.Diff(wantCRPStatus, crp.Status, crpStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status(%s) mismatch (-want +got):\n%s", crp.Name, diff)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to compare actual and expected CRP status in hub cluster")
		})

		It("Test partial manifest failed to apply", func() {
			By("create clusterResourcePlacement CR")
			crp = &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "partial-manifest-test",
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

			By("verify that we have created work objects that contain the resource selected")
			verifyWorkObjects(crp, []string{ClusterRoleKind, "CustomResourceDefinition"}, []*fleetv1alpha1.MemberCluster{&clusterA})

			var clusterWork workv1alpha1.Work
			workResourceIdentifier1 := workv1alpha1.ResourceIdentifier{
				Group:    rbacv1.GroupName,
				Kind:     ClusterRoleKind,
				Name:     "test-cluster-role",
				Ordinal:  0,
				Resource: "clusterroles",
				Version:  "v1",
			}
			workResourceIdentifier2 := workv1alpha1.ResourceIdentifier{
				Group:    apiextensionsv1.GroupName,
				Kind:     "CustomResourceDefinition",
				Name:     "clonesets.apps.kruise.io",
				Ordinal:  1,
				Resource: "clonesets",
				Version:  "v1",
			}

			// update work for clusterA to have applied condition as false, and have one manifest condition as false
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      crp.Name,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterA.Name),
			}, &clusterWork)).Should(Succeed())

			appliedCondition := metav1.Condition{
				Type:               workapi.ConditionTypeApplied,
				Status:             metav1.ConditionFalse,
				Reason:             "appliedWorkFailed",
				ObservedGeneration: clusterWork.GetGeneration(),
				LastTransitionTime: metav1.Now(),
			}

			manifestCondition1 := workv1alpha1.ManifestCondition{
				Identifier: workResourceIdentifier1,
				Conditions: []metav1.Condition{
					{
						Type:               workapi.ConditionTypeApplied,
						Status:             metav1.ConditionTrue,
						Reason:             "ManifestCreated",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			manifestCondition2 := workv1alpha1.ManifestCondition{
				Identifier: workResourceIdentifier2,
				Conditions: []metav1.Condition{
					{
						Type:               workapi.ConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						Reason:             "appliedManifestFailed",
						LastTransitionTime: metav1.Now(),
					},
				},
			}

			clusterWork.Status.Conditions = []metav1.Condition{appliedCondition}
			clusterWork.Status.ManifestConditions = []workv1alpha1.ManifestCondition{manifestCondition1, manifestCondition2}
			Expect(k8sClient.Status().Update(ctx, &clusterWork)).Should(Succeed())

			fleetResourceIdentifier1 := fleetv1alpha1.ResourceIdentifier{
				Group:   rbacv1.GroupName,
				Version: "v1",
				Kind:    ClusterRoleKind,
				Name:    "test-cluster-role",
			}
			fleetResourceIdentifier2 := fleetv1alpha1.ResourceIdentifier{
				Group:   apiextensionsv1.GroupName,
				Version: "v1",
				Kind:    "CustomResourceDefinition",
				Name:    "clonesets.apps.kruise.io",
			}
			wantCRPStatus := fleetv1alpha1.ClusterResourcePlacementStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled),
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 1,
						Reason:             "ScheduleSucceeded",
					},
					{
						Type:               string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						Reason:             "ApplyFailed",
					},
				},
				SelectedResources: []fleetv1alpha1.ResourceIdentifier{fleetResourceIdentifier1, fleetResourceIdentifier2},
				TargetClusters:    []string{clusterA.Name},
				FailedResourcePlacements: []fleetv1alpha1.FailedResourcePlacement{
					{
						ResourceIdentifier: fleetResourceIdentifier2,
						ClusterName:        clusterA.Name,
						Condition: metav1.Condition{
							Type:               string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 0,
							Reason:             "appliedManifestFailed",
						},
					},
				},
			}

			crpStatusCmpOptions := []cmp.Option{
				cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message"),
				cmpopts.SortSlices(func(ref1, ref2 metav1.Condition) bool { return ref1.Type < ref2.Type }),
				cmpopts.SortSlices(func(ref1, ref2 string) bool { return ref1 < ref2 }),
			}

			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp); err != nil {
					return err
				}
				if diff := cmp.Diff(wantCRPStatus, crp.Status, crpStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status(%s) mismatch (-want +got):\n%s", crp.Name, diff)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to compare actual and expected CRP status in hub cluster")
		})
	})
})
