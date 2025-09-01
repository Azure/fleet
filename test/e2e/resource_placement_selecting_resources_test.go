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
	"fmt"
	"strconv"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiResource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workapplier"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

var _ = Describe("testing RP selecting resources", Label("resourceplacement"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

	BeforeEach(OncePerOrdered, func() {
		By("creating work resources")
		createNamespace()

		// Create the CRP with Namespace-only selector.
		createNamespaceOnlyCRP(crpName)

		By("should update CRP status as expected")
		crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("selecting resources by name", Ordered, func() {
		BeforeAll(func() {
			createConfigMap()

			// Create the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
				},
			}
			By(fmt.Sprintf("creating placement %s/%s", rp.Namespace, rpName))
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s/%s", rp.Namespace, rpName)
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, allMemberClusters)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("can delete the RP", func() {
			// Delete the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
				},
			}
			Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s/%s", rp.Namespace, rpName)
		})

		It("should remove placed resources from all member clusters", checkIfRemovedConfigMapFromAllMemberClusters)

		It("should remove controller finalizers from RP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s/%s", appNamespace().Name, rpName)
		})
	})

	Context("selecting resources by label", Ordered, func() {
		BeforeAll(func() {
			// Create the configMap with a label so that it can be selected by the RP.
			configMap := appConfigMap()
			if configMap.Labels == nil {
				configMap.Labels = make(map[string]string)
			}
			configMap.Labels["app"] = strconv.Itoa(GinkgoParallelProcess())
			Expect(hubClient.Create(ctx, &configMap)).Should(Succeed(), "Failed to create config map %s/%s", configMap.Namespace, configMap.Name)

			// Create the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Kind:    "ConfigMap",
							Version: "v1",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": strconv.Itoa(GinkgoParallelProcess()),
								},
							},
						},
					},
				},
			}
			By(fmt.Sprintf("creating placement %s/%s", rp.Namespace, rpName))
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s/%s", rp.Namespace, rpName)
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, allMemberClusters)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("can delete the RP", func() {
			// Delete the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
				},
			}
			Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s/%s", rp.Namespace, rpName)
		})

		It("should remove placed resources from all member clusters", checkIfRemovedConfigMapFromAllMemberClusters)

		It("should remove controller finalizers from RP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s/%s", appNamespace().Name, rpName)
		})
	})

	Context("validating RP when namespaced resources become selected after the updates", Ordered, func() {
		BeforeAll(func() {
			createConfigMap()

			// Create the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Kind:    "ConfigMap",
							Version: "v1",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": fmt.Sprintf("test-%d", GinkgoParallelProcess()),
								},
							},
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(5),
						},
					},
				},
			}
			By(fmt.Sprintf("creating placement %s/%s", rp.Namespace, rpName))
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s/%s", rp.Namespace, rpName)
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, allMemberClusters)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should not place work resources on member clusters", checkIfRemovedConfigMapFromAllMemberClusters)

		It("updating the resources on the hub and the configMap becomes selected", func() {
			cm := appConfigMap()
			configMap := &corev1.ConfigMap{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: cm.Namespace, Name: cm.Name}, configMap)).Should(Succeed(), "Failed to get the config map %s/%s", cm.Namespace, cm.Name)

			if configMap.Labels == nil {
				configMap.Labels = make(map[string]string)
			}
			configMap.Labels["app"] = fmt.Sprintf("test-%d", GinkgoParallelProcess())
			Expect(hubClient.Update(ctx, configMap)).Should(Succeed(), "Failed to update config map %s/%s", cm.Namespace, cm.Name)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("can delete the RP", func() {
			// Delete the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
				},
			}
			Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s/%s", rp.Namespace, rpName)
		})

		It("should remove placed resources from all member clusters", checkIfRemovedConfigMapFromAllMemberClusters)

		It("should remove controller finalizers from RP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s/%s", appNamespace().Name, rpName)
		})
	})

	Context("validating RP when namespaced resources become unselected after the updates", Ordered, func() {
		BeforeAll(func() {
			// Create a configMap with a label so it can be selected.
			configMap := appConfigMap()
			if configMap.Labels == nil {
				configMap.Labels = make(map[string]string)
			}
			configMap.Labels["app"] = strconv.Itoa(GinkgoParallelProcess())
			Expect(hubClient.Create(ctx, &configMap)).Should(Succeed(), "Failed to update config map %s/%s", configMap.Namespace, configMap.Name)

			// Create the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Kind:    "ConfigMap",
							Version: "v1",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": strconv.Itoa(GinkgoParallelProcess()),
								},
							},
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(5),
						},
					},
				},
			}
			By(fmt.Sprintf("creating placement %s/%s", rp.Namespace, rpName))
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s/%s", rp.Namespace, rpName)
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, allMemberClusters)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("updating the resources on the hub and the configMap becomes unselected", func() {
			cm := appConfigMap()
			configMap := &corev1.ConfigMap{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: cm.Namespace, Name: cm.Name}, configMap)).Should(Succeed(), "Failed to get the config map %s/%s", cm.Namespace, cm.Name)

			if configMap.Labels == nil {
				configMap.Labels = make(map[string]string)
			}
			configMap.Labels["app"] = fmt.Sprintf("test-%d", GinkgoParallelProcess())
			Expect(hubClient.Update(ctx, configMap)).Should(Succeed(), "Failed to update config map %s/%s", cm.Namespace, cm.Name)
		})

		It("should remove the selected resources on member clusters", checkIfRemovedConfigMapFromAllMemberClusters)

		It("should update RP status as expected", func() {
			// If there are no resources selected, the available condition reason will become "AllWorkAreAvailable".
			rpStatusUpdatedActual := rpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "1")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("can delete the RP", func() {
			// Delete the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
				},
			}
			Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s/%s", rp.Namespace, rpName)
		})

		It("should remove controller finalizers from RP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s/%s", appNamespace().Name, rpName)
		})
	})

	Context("validating RP when selecting a reserved resource", Ordered, func() {
		BeforeAll(func() {
			// Create the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Kind:    "ConfigMap",
							Version: "v1",
							Name:    "kube-root-ca.crt",
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(5),
						},
					},
				},
			}
			By(fmt.Sprintf("creating placement %s/%s", rp.Namespace, rpName))
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s/%s", rp.Namespace, rpName)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s/%s", rpName, appNamespace().Name))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, allMemberClusters)
		})

		It("should update RP status as expected", func() {
			// The configMap should not be selected.
			rpStatusUpdatedActual := rpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("can delete the RP", func() {
			// Delete the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
				},
			}
			Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s/%s", rp.Namespace, rpName)
		})

		It("should remove controller finalizers from RP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s/%s", appNamespace().Name, rpName)
		})
	})

	Context("When creating a pickN RP with duplicated resources", Ordered, func() {
		BeforeAll(func() {
			By("creating configMap resource")
			createConfigMap()

			By("Create a rp that selects the same resource twice")
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    appConfigMap().Name,
						},
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    appConfigMap().Name,
						},
					},
				},
			}
			By(fmt.Sprintf("creating placement %s/%s", rp.Namespace, rpName))
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s/%s", rp.Namespace, rpName)
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, allMemberClusters)
		})

		It("should update RP status as expected", func() {
			Eventually(func() error {
				gotRP := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, gotRP); err != nil {
					return err
				}
				wantStatus := placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(placementv1beta1.ResourcePlacementScheduledConditionType),
							Reason:             condition.InvalidResourceSelectorsReason,
							ObservedGeneration: 1,
						},
					},
				}
				if diff := cmp.Diff(gotRP.Status, wantStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("RP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("updating the RP to select one configmap", func() {
			gotRP := &placementv1beta1.ResourcePlacement{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, gotRP)).Should(Succeed(), "Failed to get RP %s/%s", appNamespace().Name, rpName)
			// Just keep one configMap selector.
			gotRP.Spec.ResourceSelectors = gotRP.Spec.ResourceSelectors[:1]
			Expect(hubClient.Update(ctx, gotRP)).To(Succeed(), "Failed to update RP %s/%s", appNamespace().Name, rpName)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
	})

	Context("validating RP when failed to apply resources", Ordered, func() {
		var existingConfigMap corev1.ConfigMap
		BeforeAll(func() {
			By("creating configMap resource on hub cluster")
			createConfigMap()

			existingConfigMap = appConfigMap()
			existingConfigMap.SetOwnerReferences([]metav1.OwnerReference{
				{
					APIVersion: "another-api-version",
					Kind:       "another-kind",
					Name:       "another-owner",
					UID:        "another-uid",
				},
			})
			By(fmt.Sprintf("creating configMap %s on member cluster", existingConfigMap.Name))
			Expect(allMemberClusters[0].KubeClient.Create(ctx, &existingConfigMap)).Should(Succeed(), "Failed to create configMap %s", existingConfigMap.Name)

			// Create the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
				},
			}
			By(fmt.Sprintf("creating placement %s/%s", rp.Namespace, rpName))
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s/%s", rp.Namespace, rpName)
		})

		AfterAll(func() {
			By(fmt.Sprintf("garbage collect the %s", existingConfigMap.Name))
			propagage := metav1.DeletePropagationForeground
			Expect(allMemberClusters[0].KubeClient.Delete(ctx, &existingConfigMap, &client.DeleteOptions{PropagationPolicy: &propagage})).Should(Succeed(), "Failed to delete configMap %s", existingConfigMap.Name)

			By(fmt.Sprintf("garbage collect all things related to placement %s/%s", rpName, appNamespace().Name))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, allMemberClusters)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, rp); err != nil {
					return err
				}

				appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
				wantStatus := placementv1beta1.PlacementStatus{
					Conditions: rpAppliedFailedConditions(rp.Generation),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Kind:      "ConfigMap",
										Name:      appConfigMapName,
										Version:   "v1",
										Namespace: appNamespace().Name,
									},
									Condition: metav1.Condition{
										Type:               placementv1beta1.WorkConditionTypeApplied,
										Status:             metav1.ConditionFalse,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
										ObservedGeneration: 0,
									},
								},
							},
							Conditions: perClusterApplyFailedConditions(rp.Generation),
						},
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(rp.Generation, true, false),
						},
						{
							ClusterName:           memberCluster3WestProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(rp.Generation, true, false),
						},
					},
					SelectedResources: []placementv1beta1.ResourceIdentifier{
						{
							Kind:      "ConfigMap",
							Name:      appConfigMapName,
							Version:   "v1",
							Namespace: appNamespace().Name,
						},
					},
					ObservedResourceIndex: "0",
				}
				if diff := cmp.Diff(rp.Status, wantStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("RP status diff (-got, +want): %s", diff)
				}
				return nil
			}
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("can delete the RP", func() {
			// Delete the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
				},
			}
			Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s/%s", rp.Namespace, rpName)
		})

		It("should remove placed resources from member clusters excluding the first one", func() {
			checkIfRemovedConfigMapFromMemberClusters(allMemberClusters[1:])
		})

		It("should remove controller finalizers from RP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s/%s", appNamespace().Name, rpName)
		})

		It("configMap should be kept on member cluster", func() {
			Consistently(func() error {
				appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
				cm := &corev1.ConfigMap{}
				return allMemberClusters[0].KubeClient.Get(ctx, types.NamespacedName{Name: appConfigMapName, Namespace: appNamespace().Name}, cm)
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "ConfigMap which is not owned by the RP should not be deleted")
		})
	})

	Context("validating RP revision history allowing single revision when updating resource selector", Ordered, func() {
		workNamespace := appNamespace().Name
		BeforeAll(func() {
			By("creating configMap resource")
			createConfigMap()

			// Create the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: workNamespace,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Kind:    "ConfigMap",
							Version: "v1",
							Name:    "invalid-configmap",
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(5),
						},
					},
					RevisionHistoryLimit: ptr.To(int32(1)),
				},
			}
			By(fmt.Sprintf("creating placement %s/%s", rp.Namespace, rpName))
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s/%s", rp.Namespace, rpName)
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(nil, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("adding resource selectors", func() {
			updateFunc := func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespace}, rp); err != nil {
					return err
				}

				rp.Spec.ResourceSelectors = append(rp.Spec.ResourceSelectors, placementv1beta1.ResourceSelectorTerm{
					Group:   "",
					Kind:    "ConfigMap",
					Version: "v1",
					Name:    fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess()),
				})
				// may hit 409
				return hubClient.Update(ctx, rp)
			}
			Eventually(updateFunc, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the rp %s/%s", workNamespace, rpName)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should have one policy snapshot revision and one resource snapshot revision", func() {
			Expect(validateRPSnapshotRevisions(rpName, workNamespace, 1, 1)).Should(Succeed(), "Failed to validate the revision history")
		})

		It("can delete the RP", func() {
			// Delete the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: workNamespace,
				},
			}
			Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s/%s", workNamespace, rpName)
		})

		It("should remove placed resources from all member clusters", checkIfRemovedConfigMapFromAllMemberClusters)

		It("should remove controller finalizers from RP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: workNamespace})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s/%s", workNamespace, rpName)
		})
	})

	Context("validating RP revision history allowing multiple revisions when updating resource selector", Ordered, func() {
		workNamespace := appNamespace().Name
		BeforeAll(func() {
			By("creating configMap resource")
			createConfigMap()

			// Create the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: workNamespace,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Kind:    "ConfigMap",
							Version: "v1",
							Name:    "invalid-configmap",
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(5),
						},
					},
				},
			}
			By(fmt.Sprintf("creating placement %s/%s", rp.Namespace, rpName))
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s/%s", rp.Namespace, rpName)
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(nil, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("adding resource selectors", func() {
			updateFunc := func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespace}, rp); err != nil {
					return err
				}

				rp.Spec.ResourceSelectors = append(rp.Spec.ResourceSelectors, placementv1beta1.ResourceSelectorTerm{
					Group:   "",
					Kind:    "ConfigMap",
					Version: "v1",
					Name:    fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess()),
				})
				// may hit 409
				return hubClient.Update(ctx, rp)
			}
			Eventually(updateFunc, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the rp %s/%s", workNamespace, rpName)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should have one policy snapshot revision and two resource snapshot revisions", func() {
			Expect(validateRPSnapshotRevisions(rpName, workNamespace, 1, 2)).Should(Succeed(), "Failed to validate the revision history")
		})

		It("can delete the RP", func() {
			// Delete the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: workNamespace,
				},
			}
			Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s/%s", workNamespace, rpName)
		})

		It("should remove placed resources from all member clusters", checkIfRemovedConfigMapFromAllMemberClusters)

		It("should remove controller finalizers from RP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: workNamespace})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s/%s", workNamespace, rpName)
		})
	})

	// running spec in parallel with other specs causes timeouts.
	Context("validating RP when selected resources cross the 1MB limit", Ordered, Serial, func() {
		workNamespace := appNamespace().Name
		BeforeAll(func() {
			By("creating resources for multiple resource snapshots")
			createConfigMap()
			createResourcesForMultipleResourceSnapshots()

			// Create the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: workNamespace,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{memberCluster1EastProdName, memberCluster2EastCanaryName},
					},
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Kind:    "ConfigMap",
							Version: "v1",
							Name:    appConfigMap().Name,
						},
						// Select all the secrets.
						{
							Group:   "",
							Kind:    "Secret",
							Version: "v1",
						},
					},
				},
			}
			By(fmt.Sprintf("creating placement %s/%s", rp.Namespace, rpName))
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s/%s", rp.Namespace, rpName)
		})

		AfterAll(func() {
			secrets := make([]client.Object, 3)
			for i := 2; i >= 0; i-- {
				secrets[i] = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(appSecretNameTemplate, i),
						Namespace: workNamespace,
					},
				}
			}
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters, secrets...)
		})

		It("check if created resource snapshots are as expected", func() {
			Eventually(multipleRPResourceSnapshotsCreatedActual("2", "2", "0"), largeEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to check created resource snapshots", rpName)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(resourceIdentifiersForMultipleResourcesSnapshotsRP(), []string{memberCluster1EastProdName, memberCluster2EastCanaryName}, nil, "0")
			Eventually(rpStatusUpdatedActual, largeEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place the selected resources on member clusters", func() {
			targetMemberClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
			checkIfPlacedWorkResourcesOnTargetMemberClusters(targetMemberClusters)
			checkIfPlacedLargeSecretResourcesOnTargetMemberClusters(targetMemberClusters)
		})

		It("can delete the RP", func() {
			// Delete the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: workNamespace,
				},
			}
			Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s/%s", workNamespace, rpName)
		})

		It("should remove placed resources from all member clusters", checkIfRemovedConfigMapFromAllMemberClusters)

		It("should remove controller finalizers from RP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: workNamespace})
			Eventually(finalizerRemovedActual, largeEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s/%s", workNamespace, rpName)
		})
	})

	Context("creating RP and checking selected resources order", Ordered, func() {
		nsName := appNamespace().Name
		var configMap *corev1.ConfigMap
		var secret *corev1.Secret
		var pvc *corev1.PersistentVolumeClaim
		var role *rbacv1.Role

		BeforeAll(func() {
			// Create ConfigMap
			configMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-configmap-%d", GinkgoParallelProcess()),
					Namespace: nsName,
				},
				Data: map[string]string{
					"key1": "value1",
				},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create ConfigMap")

			// Create Secret
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-secret-%d", GinkgoParallelProcess()),
					Namespace: nsName,
				},
				StringData: map[string]string{
					"username": "test-user",
					"password": "test-password",
				},
				Type: corev1.SecretTypeOpaque,
			}
			Expect(hubClient.Create(ctx, secret)).To(Succeed(), "Failed to create Secret")

			// Create PersistentVolumeClaim
			pvc = &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-pvc-%d", GinkgoParallelProcess()),
					Namespace: nsName,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					StorageClassName: ptr.To("standard"),
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: apiResource.MustParse("1Gi"),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, pvc)).To(Succeed(), "Failed to create PVC")

			// Create Role
			role = &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-role-%d", GinkgoParallelProcess()),
					Namespace: nsName,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"configmaps"},
						Verbs:     []string{"get", "list"},
					},
				},
			}
			Expect(hubClient.Create(ctx, role)).To(Succeed(), "Failed to create Role")

			// Create the RP
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  nsName,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Kind:    "ConfigMap",
							Version: "v1",
						},
						{
							Group:   "",
							Kind:    "Secret",
							Version: "v1",
						},
						{
							Group:   "",
							Kind:    "PersistentVolumeClaim",
							Version: "v1",
						},
						{
							Group:   "rbac.authorization.k8s.io",
							Kind:    "Role",
							Version: "v1",
						},
					},
				},
			}
			By(fmt.Sprintf("creating placement %s/%s", rp.Namespace, rpName))
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s/%s", rp.Namespace, rpName)
		})

		AfterAll(func() {
			By(fmt.Sprintf("garbage collect all things related to placement %s/%s", rpName, nsName))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: nsName}, allMemberClusters, configMap, secret, pvc, role)
		})

		It("should update RP status with the correct order of the selected resources", func() {
			// Define the expected resources in order.
			expectedResources := []placementv1beta1.ResourceIdentifier{
				{Kind: "Secret", Name: secret.Name, Namespace: nsName, Version: "v1"},
				{Kind: "ConfigMap", Name: configMap.Name, Namespace: nsName, Version: "v1"},
				{Kind: "PersistentVolumeClaim", Name: pvc.Name, Namespace: nsName, Version: "v1"},
				{Group: "rbac.authorization.k8s.io", Kind: "Role", Name: role.Name, Namespace: nsName, Version: "v1"},
			}

			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: nsName}, rp); err != nil {
					return err
				}
				if diff := cmp.Diff(rp.Status.SelectedResources, expectedResources); diff != "" {
					return fmt.Errorf("RP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s selected resource status as expected", rpName)
		})
	})
})

func multipleRPResourceSnapshotsCreatedActual(wantTotalNumberOfResourceSnapshots, wantNumberOfMasterIndexedResourceSnapshots, wantResourceIndex string) func() error {
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	workNamespace := appNamespace().Name

	return func() error {
		var resourceSnapshotList placementv1beta1.ResourceSnapshotList
		masterResourceSnapshotLabels := client.MatchingLabels{
			placementv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
			placementv1beta1.PlacementTrackingLabel: rpName,
		}
		if err := hubClient.List(ctx, &resourceSnapshotList, masterResourceSnapshotLabels, client.InNamespace(workNamespace)); err != nil {
			return err
		}
		// there should be only one master resource snapshot.
		if len(resourceSnapshotList.Items) != 1 {
			return fmt.Errorf("number of master resource snapshots has unexpected value: got %d, want %d", len(resourceSnapshotList.Items), 1)
		}
		masterResourceSnapshot := resourceSnapshotList.Items[0]
		// labels to list all existing resource snapshots.
		resourceSnapshotListLabels := client.MatchingLabels{placementv1beta1.PlacementTrackingLabel: rpName}
		if err := hubClient.List(ctx, &resourceSnapshotList, resourceSnapshotListLabels, client.InNamespace(workNamespace)); err != nil {
			return err
		}
		// ensure total number of resource snapshots equals to wanted number of resource snapshots
		if strconv.Itoa(len(resourceSnapshotList.Items)) != wantTotalNumberOfResourceSnapshots {
			return fmt.Errorf("total number of resource snapshots has unexpected value: got %s, want %s", strconv.Itoa(len(resourceSnapshotList.Items)), wantTotalNumberOfResourceSnapshots)
		}
		numberOfResourceSnapshots := masterResourceSnapshot.Annotations[placementv1beta1.NumberOfResourceSnapshotsAnnotation]
		if numberOfResourceSnapshots != wantNumberOfMasterIndexedResourceSnapshots {
			return fmt.Errorf("NumberOfResourceSnapshotsAnnotation in master resource snapshot has unexpected value:  got %s, want %s", numberOfResourceSnapshots, wantNumberOfMasterIndexedResourceSnapshots)
		}
		masterResourceIndex := masterResourceSnapshot.Labels[placementv1beta1.ResourceIndexLabel]
		if masterResourceIndex != wantResourceIndex {
			return fmt.Errorf("resource index for master resource snapshot %s has unexpected value: got %s, want %s", masterResourceSnapshot.Name, masterResourceIndex, wantResourceIndex)
		}
		// labels to list all resource snapshots with master resource index.
		resourceSnapshotListLabels = client.MatchingLabels{
			placementv1beta1.ResourceIndexLabel:     masterResourceIndex,
			placementv1beta1.PlacementTrackingLabel: rpName,
		}
		if err := hubClient.List(ctx, &resourceSnapshotList, resourceSnapshotListLabels, client.InNamespace(workNamespace)); err != nil {
			return err
		}
		if strconv.Itoa(len(resourceSnapshotList.Items)) != wantNumberOfMasterIndexedResourceSnapshots {
			return fmt.Errorf("number of resource snapshots with master resource index has unexpected value: got %s, want %s", strconv.Itoa(len(resourceSnapshotList.Items)), wantNumberOfMasterIndexedResourceSnapshots)
		}
		return nil
	}
}

func resourceIdentifiersForMultipleResourcesSnapshotsRP() []placementv1beta1.ResourceIdentifier {
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	var placementResourceIdentifiers []placementv1beta1.ResourceIdentifier

	for i := 2; i >= 0; i-- {
		placementResourceIdentifiers = append(placementResourceIdentifiers, placementv1beta1.ResourceIdentifier{
			Kind:      "Secret",
			Name:      fmt.Sprintf(appSecretNameTemplate, i),
			Namespace: workNamespaceName,
			Version:   "v1",
		})
	}

	placementResourceIdentifiers = append(placementResourceIdentifiers, placementv1beta1.ResourceIdentifier{
		Kind:      "ConfigMap",
		Name:      fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess()),
		Version:   "v1",
		Namespace: workNamespaceName,
	})

	return placementResourceIdentifiers
}
