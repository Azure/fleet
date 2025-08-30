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
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workapplier"
)

var _ = Describe("validating resource placement using different apply strategies", Label("resourceplacement"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	annotationKey := "annotation-key"
	annotationValue := "annotation-value"
	annotationUpdatedValue := "annotation-updated-value"
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	configMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
	anotherOwnerReference := metav1.OwnerReference{}

	BeforeEach(OncePerOrdered, func() {
		// Create the resources.
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

	Describe("validating RP when resources exists", Ordered, func() {
		BeforeAll(func() {
			By("creating configMap on hub cluster")
			createConfigMap()

			By("creating owner reference for the configmap")
			anotherOwnerReference = createAnotherValidOwnerReferenceForConfigMap(workNamespaceName, fmt.Sprintf("owner-configmap-%d", GinkgoParallelProcess()))
		})

		AfterAll(func() {
			By("deleting created configMap on hub cluster")
			cleanupConfigMap()

			By("deleting owner reference configmap")
			cleanupAnotherValidOwnerReferenceForConfigMap(workNamespaceName, anotherOwnerReference.Name)
		})

		Context("Test a RP place objects successfully (client-side-apply and allow co-own)", Ordered, func() {
			BeforeAll(func() {
				cm := appConfigMap()
				cm.SetOwnerReferences([]metav1.OwnerReference{
					anotherOwnerReference,
				})
				cm.Annotations = map[string]string{
					annotationKey: annotationValue,
				}
				By(fmt.Sprintf("creating configmap %s/%s on member cluster", cm.Namespace, cm.Name))
				Expect(allMemberClusters[0].KubeClient.Create(ctx, &cm)).Should(Succeed(), "Failed to create configmap %s/%s", cm.Namespace, cm.Name)

				// Create the RP.
				strategy := &placementv1beta1.ApplyStrategy{AllowCoOwnership: true}
				createRPWithApplyStrategy(workNamespaceName, rpName, strategy, nil)
			})

			AfterAll(func() {
				By(fmt.Sprintf("deleting placement %s/%s", workNamespaceName, rpName))
				cleanupPlacement(types.NamespacedName{Name: rpName, Namespace: workNamespaceName})

				By("deleting created config map on member cluster")
				cleanupConfigMapOnCluster(allMemberClusters[0])
			})

			It("should update RP status as expected", func() {
				rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
				Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
			})

			// This check will ignore the annotation of resources.
			It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

			It("should have annotations on the configmap", func() {
				want := map[string]string{annotationKey: annotationValue}
				Expect(validateAnnotationOfConfigMapOnCluster(memberCluster1EastProd, want)).Should(Succeed(), "Failed to override the annotation of work configmap on %s", memberCluster1EastProdName)
			})

			It("can delete the RP", func() {
				// Delete the RP.
				rp := &placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: workNamespaceName,
					},
				}
				Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s", rpName)
			})

			It("should remove placed resources from member clusters excluding the first one", func() {
				checkIfRemovedConfigMapFromMemberClusters(allMemberClusters[1:])
			})

			It("should remove controller finalizers from RP", func() {
				finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Namespace: workNamespaceName, Name: rpName})
				Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s", rpName)
			})

			It("configmap should be kept on member cluster", func() {
				checkConfigMapExistsWithOwnerRefOnMemberCluster(workNamespaceName, configMapName, rpName)
			})
		})

		Context("Test a RP place objects successfully (client-side-apply and disallow co-own) and existing resource has no owner reference", Ordered, func() {
			BeforeAll(func() {
				cm := appConfigMap()
				cm.Annotations = map[string]string{
					annotationKey: annotationValue,
				}
				By(fmt.Sprintf("creating configmap %s/%s on member cluster", cm.Namespace, cm.Name))
				Expect(allMemberClusters[0].KubeClient.Create(ctx, &cm)).Should(Succeed(), "Failed to create configmap %s/%s", cm.Namespace, cm.Name)

				// Create the RP.
				strategy := &placementv1beta1.ApplyStrategy{AllowCoOwnership: false}
				createRPWithApplyStrategy(workNamespaceName, rpName, strategy, nil)
			})

			AfterAll(func() {
				By(fmt.Sprintf("deleting placement %s/%s", workNamespaceName, rpName))
				cleanupPlacement(types.NamespacedName{Name: rpName, Namespace: workNamespaceName})
			})

			It("should update RP status as expected", func() {
				rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
				Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
			})

			// This check will ignore the annotation of resources.
			It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

			It("should have annotations on the configmap", func() {
				want := map[string]string{annotationKey: annotationValue}
				Expect(validateAnnotationOfConfigMapOnCluster(memberCluster1EastProd, want)).Should(Succeed(), "Failed to override the annotation of work configmap on %s", memberCluster1EastProdName)
			})

			It("can delete the RP", func() {
				// Delete the RP.
				rp := &placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: workNamespaceName,
					},
				}
				Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s", rpName)
			})

			It("should remove the selected resources on member clusters", checkIfRemovedConfigMapFromAllMemberClusters)

			It("should remove controller finalizers from RP", func() {
				finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: workNamespaceName})
				Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s", rpName)
			})
		})

		Context("Test a RP place objects successfully (server-side-apply and disallow co-own) and existing resource has no owner reference", Ordered, func() {
			BeforeAll(func() {
				cm := appConfigMap()
				cm.Annotations = map[string]string{
					annotationKey: annotationValue,
				}
				By(fmt.Sprintf("creating configmap %s/%s on member cluster", cm.Namespace, cm.Name))
				Expect(allMemberClusters[0].KubeClient.Create(ctx, &cm)).Should(Succeed(), "Failed to create configmap %s/%s", cm.Namespace, cm.Name)

				// Create the RP.
				strategy := &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
					AllowCoOwnership: false,
				}
				createRPWithApplyStrategy(workNamespaceName, rpName, strategy, nil)
			})

			AfterAll(func() {
				By(fmt.Sprintf("deleting placement %s/%s", workNamespaceName, rpName))
				cleanupPlacement(types.NamespacedName{Name: rpName, Namespace: workNamespaceName})
			})

			It("should update RP status as expected", func() {
				rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
				Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
			})

			// This check will ignore the annotation of resources.
			It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

			It("should have annotations on the configmap", func() {
				want := map[string]string{annotationKey: annotationValue}
				Expect(validateAnnotationOfConfigMapOnCluster(memberCluster1EastProd, want)).Should(Succeed(), "Failed to override the annotation of work configmap on %s", memberCluster1EastProdName)
			})

			It("can delete the RP", func() {
				// Delete the RP.
				rp := &placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: workNamespaceName,
					},
				}
				Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s", rpName)
			})

			It("should remove the selected resources on member clusters", checkIfRemovedConfigMapFromAllMemberClusters)

			It("should remove controller finalizers from RP", func() {
				finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: workNamespaceName})
				Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s", rpName)
			})
		})

		Context("Test a RP fail to apply configmap (server-side-apply and disallow co-own) and existing resource is owned by others", Ordered, func() {
			BeforeAll(func() {
				cm := appConfigMap()
				cm.SetOwnerReferences([]metav1.OwnerReference{
					anotherOwnerReference,
				})
				By(fmt.Sprintf("creating configmap %s/%s on member cluster", cm.Namespace, cm.Name))
				Expect(allMemberClusters[0].KubeClient.Create(ctx, &cm)).Should(Succeed(), "Failed to create configmap %s/%s", cm.Namespace, cm.Name)

				// Create the RP.
				strategy := &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
					AllowCoOwnership: false,
				}
				createRPWithApplyStrategy(workNamespaceName, rpName, strategy, nil)
			})

			AfterAll(func() {
				By(fmt.Sprintf("deleting placement %s/%s", workNamespaceName, rpName))
				cleanupPlacement(types.NamespacedName{Name: rpName, Namespace: workNamespaceName})

				By("deleting created config map on member cluster")
				cleanupConfigMapOnCluster(allMemberClusters[0])
			})

			It("should update RP status as expected", func() {
				rpStatusUpdatedActual := func() error {
					rp := &placementv1beta1.ResourcePlacement{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, rp); err != nil {
						return err
					}

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
											Name:      configMapName,
											Version:   "v1",
											Namespace: workNamespaceName,
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
						SelectedResources:     appConfigMapIdentifiers(),
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
						Namespace: workNamespaceName,
					},
				}
				Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s", rpName)
			})

			It("should remove placed resources from member clusters excluding the first one", func() {
				checkIfRemovedConfigMapFromMemberClusters(allMemberClusters[1:])
			})

			It("should remove controller finalizers from RP", func() {
				finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: workNamespaceName})
				Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s", rpName)
			})

			It("configmap should be kept on member cluster", func() {
				checkConfigMapExistsWithOwnerRefOnMemberCluster(workNamespaceName, configMapName, rpName)
			})
		})

		Context("Test a RP able to apply configmap when the conflicted annotation is managed by others (force server-side-apply and allow co-own)", Ordered, func() {
			BeforeAll(func() {
				cm := appConfigMap()
				cm.SetOwnerReferences([]metav1.OwnerReference{
					anotherOwnerReference,
				})
				cm.Annotations = map[string]string{
					annotationKey: annotationValue,
				}
				options := client.CreateOptions{FieldManager: e2eTestFieldManager}
				By(fmt.Sprintf("creating configmap %s/%s on member cluster", cm.Namespace, cm.Name))
				Expect(allMemberClusters[0].KubeClient.Create(ctx, &cm, &options)).Should(Succeed(), "Failed to create configmap %s/%s", cm.Namespace, cm.Name)

				By(fmt.Sprintf("updating configmap %s/%s annotation on hub cluster", cm.Namespace, cm.Name))
				Expect(hubClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: workNamespaceName}, &cm)).Should(Succeed(), "Failed to get configmap %s/%s", workNamespaceName, configMapName)
				cm.Annotations = map[string]string{
					annotationKey: annotationUpdatedValue,
				}
				Expect(hubClient.Update(ctx, &cm)).Should(Succeed(), "Failed to update configmap %s/%s", workNamespaceName, configMapName)

				// Create the RP.
				strategy := &placementv1beta1.ApplyStrategy{
					Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
					ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: true},
					AllowCoOwnership:      true,
				}
				createRPWithApplyStrategy(workNamespaceName, rpName, strategy, nil)
			})

			AfterAll(func() {
				By(fmt.Sprintf("deleting placement %s/%s", workNamespaceName, rpName))
				cleanupPlacement(types.NamespacedName{Name: rpName, Namespace: workNamespaceName})

				By("deleting created config map on member cluster")
				cleanupConfigMapOnCluster(allMemberClusters[0])
			})

			It("should update RP status as expected", func() {
				rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
				Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
			})

			// This check will ignore the annotation of resources.
			It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

			It("should have updated annotations on the configmap of all clusters", func() {
				want := map[string]string{annotationKey: annotationUpdatedValue}
				for _, c := range allMemberClusters {
					Expect(validateAnnotationOfConfigMapOnCluster(c, want)).Should(Succeed(), "Failed to override the annotation of work configmap on %s", c.ClusterName)
				}
			})

			It("can delete the RP", func() {
				// Delete the RP.
				rp := &placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: workNamespaceName,
					},
				}
				Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s", rpName)
			})

			It("should remove placed resources from member clusters excluding the first one", func() {
				checkIfRemovedConfigMapFromMemberClusters(allMemberClusters[1:])
			})

			It("should remove controller finalizers from RP", func() {
				finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: workNamespaceName})
				Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s", rpName)
			})

			It("configmap should be kept on member cluster", func() {
				checkConfigMapExistsWithOwnerRefOnMemberCluster(workNamespaceName, configMapName, rpName)
			})
		})

		Context("no dual placement", Ordered, func() {
			rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
			conflictedRPName := "rp-conflicted"
			workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
			configMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

			BeforeAll(func() {
				rp := &placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: workNamespaceName,
						// Add a custom finalizer; this would allow us to better observe
						// the behavior of the controllers.
						Finalizers: []string{customDeletionBlockerFinalizer},
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: configMapSelector(),
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickFixedPlacementType,
							ClusterNames: []string{
								memberCluster1EastProdName,
							},
						},
						Strategy: placementv1beta1.RolloutStrategy{
							Type: placementv1beta1.RollingUpdateRolloutStrategyType,
							RollingUpdate: &placementv1beta1.RollingUpdateConfig{
								UnavailablePeriodSeconds: ptr.To(2),
							},
							ApplyStrategy: &placementv1beta1.ApplyStrategy{
								AllowCoOwnership: true,
							},
						},
					},
				}
				Expect(hubClient.Create(ctx, rp)).To(Succeed())
			})

			It("should update RP status as expected", func() {
				rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
				Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("should place the resources on member clusters", func() {
				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProdName)
			})

			It("can create a conflicted RP", func() {
				conflictedRP := &placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      conflictedRPName,
						Namespace: workNamespaceName,
						// No need for the custom deletion blocker finalizer.
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: configMapSelector(),
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickFixedPlacementType,
							ClusterNames: []string{
								memberCluster1EastProdName,
							},
						},
						Strategy: placementv1beta1.RolloutStrategy{
							Type: placementv1beta1.RollingUpdateRolloutStrategyType,
							RollingUpdate: &placementv1beta1.RollingUpdateConfig{
								UnavailablePeriodSeconds: ptr.To(2),
							},
							ApplyStrategy: &placementv1beta1.ApplyStrategy{
								AllowCoOwnership: true,
							},
						},
					},
				}
				Expect(hubClient.Create(ctx, conflictedRP)).To(Succeed())
			})

			It("should update conflicted RP status as expected", func() {
				buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
					return &placementv1beta1.PlacementStatus{
						Conditions:        rpAppliedFailedConditions(rpGeneration),
						SelectedResources: appConfigMapIdentifiers(),
						PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
							{
								ClusterName:           memberCluster1EastProdName,
								ObservedResourceIndex: "0",
								Conditions:            perClusterApplyFailedConditions(rpGeneration),
								FailedPlacements: []placementv1beta1.FailedResourcePlacement{
									{
										ResourceIdentifier: placementv1beta1.ResourceIdentifier{
											Version:   "v1",
											Kind:      "ConfigMap",
											Name:      configMapName,
											Namespace: workNamespaceName,
										},
										Condition: metav1.Condition{
											Type:   string(placementv1beta1.PerClusterAppliedConditionType),
											Status: metav1.ConditionFalse,
											Reason: string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
										},
									},
								},
							},
						},
						ObservedResourceIndex: "0",
					}
				}

				Eventually(func() error {
					conflictedRP := &placementv1beta1.ResourcePlacement{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: conflictedRPName, Namespace: workNamespaceName}, conflictedRP); err != nil {
						return err
					}
					wantRPStatus := buildWantRPStatus(conflictedRP.Generation)

					if diff := cmp.Diff(conflictedRP.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
						return fmt.Errorf("RP status diff (-got, +want): %s", diff)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("should have no effect on previously created RP", func() {
				rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
				Consistently(rpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("should not add additional owner reference to affected resources", func() {
				expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, fmt.Sprintf("%s.%s", workNamespaceName, rpName))

				cm := &corev1.ConfigMap{}
				Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: workNamespaceName}, cm)).To(Succeed())

				// The difference has been overwritten.
				wantCM := appConfigMap()
				wantCM.OwnerReferences = []metav1.OwnerReference{*expectedOwnerRef}

				// No need to use an Eventually block as this spec runs after the RP status has been verified.
				diff := cmp.Diff(
					cm, &wantCM,
					ignoreObjectMetaAutoGenExceptOwnerRefFields,
					ignoreObjectMetaAnnotationField,
				)
				Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)
			})

			AfterAll(func() {
				ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, allMemberClusters)
				ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: conflictedRPName, Namespace: workNamespaceName}, allMemberClusters)
			})
		})
	})

	Describe("SSA", Ordered, func() {
		Context("use server-side apply to place resources (with changes)", func() {
			// The key here should match the one used in the default config map.
			cmDataKey := "data"
			cmDataVal1 := "foobar"

			BeforeAll(func() {
				createConfigMap()
				// Create the RP.
				rp := &placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: workNamespaceName,
						// Add a custom finalizer; this would allow us to better observe
						// the behavior of the controllers.
						Finalizers: []string{customDeletionBlockerFinalizer},
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: configMapSelector(),
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
						Strategy: placementv1beta1.RolloutStrategy{
							Type: placementv1beta1.RollingUpdateRolloutStrategyType,
							RollingUpdate: &placementv1beta1.RollingUpdateConfig{
								MaxUnavailable:           ptr.To(intstr.FromInt(1)),
								MaxSurge:                 ptr.To(intstr.FromInt(1)),
								UnavailablePeriodSeconds: ptr.To(2),
							},
							ApplyStrategy: &placementv1beta1.ApplyStrategy{
								Type: placementv1beta1.ApplyStrategyTypeServerSideApply,
							},
						},
					},
				}
				Expect(hubClient.Create(ctx, rp)).To(Succeed())
			})

			It("should update RP status as expected", func() {
				rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
				Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("should place the resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

			It("can update the manifests", func() {
				Eventually(func() error {
					cm := &corev1.ConfigMap{}
					if err := hubClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: workNamespaceName}, cm); err != nil {
						return fmt.Errorf("failed to get configmap: %w", err)
					}

					if cm.Data == nil {
						cm.Data = make(map[string]string)
					}
					cm.Data[cmDataKey] = cmDataVal1

					if err := hubClient.Update(ctx, cm); err != nil {
						return fmt.Errorf("failed to update configmap: %w", err)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the manifests")
			})

			It("should update RP status as expected", func() {
				rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "1")
				// Longer timeout is used to allow full rollouts.
				Eventually(rpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("should refresh the resources on all member clusters", func() {
				for idx := range allMemberClusters {
					memberClient := allMemberClusters[idx].KubeClient

					Eventually(func() error {
						cm := &corev1.ConfigMap{}
						if err := memberClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: workNamespaceName}, cm); err != nil {
							return fmt.Errorf("failed to get configmap: %w", err)
						}

						// To keep things simple, here the config map for comparison is
						// rebuilt from the retrieved data.
						rebuiltCM := &corev1.ConfigMap{
							ObjectMeta: metav1.ObjectMeta{
								Name:      cm.Name,
								Namespace: cm.Namespace,
							},
							Data: cm.Data,
						}
						wantCM := &corev1.ConfigMap{
							ObjectMeta: metav1.ObjectMeta{
								Name:      configMapName,
								Namespace: workNamespaceName,
							},
							Data: map[string]string{
								cmDataKey: cmDataVal1,
							},
						}
						if diff := cmp.Diff(rebuiltCM, wantCM); diff != "" {
							return fmt.Errorf("configMap diff (-got, +want):\n%s", diff)
						}
						return nil
					}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the resources on %s", allMemberClusters[idx].ClusterName)
				}
			})

			AfterAll(func() {
				ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, allMemberClusters)
			})
		})

		Context("fall back to server-side apply when client-side apply cannot be used", func() {
			cmDataKey := "randomBase64Str"

			BeforeAll(func() {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configMapName,
						Namespace: workNamespaceName,
					},
					Data: map[string]string{
						cmDataKey: "",
					},
				}
				Expect(hubClient.Create(ctx, cm)).To(Succeed(), "Failed to create configMap %s in namespace %s", configMapName, workNamespaceName)

				// Create the RP.
				rp := &placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: workNamespaceName,
						// Add a custom finalizer; this would allow us to better observe
						// the behavior of the controllers.
						Finalizers: []string{customDeletionBlockerFinalizer},
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: configMapSelector(),
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickFixedPlacementType,
							ClusterNames: []string{
								memberCluster1EastProdName,
							},
						},
						Strategy: placementv1beta1.RolloutStrategy{
							Type: placementv1beta1.RollingUpdateRolloutStrategyType,
							RollingUpdate: &placementv1beta1.RollingUpdateConfig{
								MaxUnavailable:           ptr.To(intstr.FromInt(1)),
								MaxSurge:                 ptr.To(intstr.FromInt(1)),
								UnavailablePeriodSeconds: ptr.To(2),
							},
						},
					},
				}
				Expect(hubClient.Create(ctx, rp)).To(Succeed())
			})

			It("should update RP status as expected", func() {
				rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
				Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("should place the resources on the selected member cluster", func() {
				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProdName)
			})

			It("can update the manifests", func() {
				// Update the configMap to add a large enough data piece so that
				// client-side apply is no longer possible.
				Eventually(func() error {
					cm := &corev1.ConfigMap{}
					if err := hubClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: workNamespaceName}, cm); err != nil {
						return fmt.Errorf("failed to get configMap: %w", err)
					}

					if cm.Data == nil {
						cm.Data = make(map[string]string)
					}
					// Generate a large bytes array.
					//
					// Kubernetes will reject configMaps larger than 1048576 bytes (~1 MB);
					// and when an object's spec size exceeds 262144 bytes, KubeFleet will not
					// be able to use client-side apply with the object as it cannot set
					// an last applied configuration annotation of that size. Consequently,
					// for this test case, it prepares a configMap object of 600000 bytes so
					// that Kubernetes will accept it but CSA cannot use it, forcing the
					// work applier to fall back to server-side apply.
					randomBytes := make([]byte, 600000)
					// Note that this method never returns an error and will always fill the given
					// slice completely.
					_, _ = rand.Read(randomBytes)
					// Encode the random bytes to a base64 string.
					randomBase64Str := base64.StdEncoding.EncodeToString(randomBytes)
					cm.Data[cmDataKey] = randomBase64Str

					if err := hubClient.Update(ctx, cm); err != nil {
						return fmt.Errorf("failed to update configMap: %w", err)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the manifests")
			})

			It("should update RP status as expected", func() {
				rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName}, nil, "1")
				Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("should place the resources on the selected member cluster", func() {
				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProdName)
			})

			It("should fall back to server-side apply", func() {
				Eventually(func() error {
					cm := &corev1.ConfigMap{}
					if err := memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: workNamespaceName}, cm); err != nil {
						return fmt.Errorf("failed to get configMap: %w", err)
					}

					lastAppliedConf, foundAnnotation := cm.Annotations[placementv1beta1.LastAppliedConfigAnnotation]
					if foundAnnotation && len(lastAppliedConf) > 0 {
						return fmt.Errorf("the configMap object has annotation %s (value: %s) in presence when SSA should be used", placementv1beta1.LastAppliedConfigAnnotation, lastAppliedConf)
					}

					foundFieldMgr := false
					fieldMgrs := cm.GetManagedFields()
					for _, fieldMgr := range fieldMgrs {
						// For simplicity reasons, here the test case verifies only against the field
						// manager name.
						if fieldMgr.Manager == "work-api-agent" {
							foundFieldMgr = true
						}
					}
					if !foundFieldMgr {
						return fmt.Errorf("the configMap object does not list the KubeFleet member agent as a field manager when SSA should be used")
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to fall back to server-side apply")
			})

			It("can update the manifests", func() {
				// Update the configMap to remove the large data piece so that
				// client-side apply can be used again.
				Eventually(func() error {
					cm := &corev1.ConfigMap{}
					if err := hubClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: workNamespaceName}, cm); err != nil {
						return fmt.Errorf("failed to get configMap: %w", err)
					}

					if cm.Data == nil {
						cm.Data = make(map[string]string)
					}
					cm.Data[cmDataKey] = ""

					if err := hubClient.Update(ctx, cm); err != nil {
						return fmt.Errorf("failed to update configMap: %w", err)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the manifests")
			})

			It("should update RP status as expected", func() {
				rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName}, nil, "2")
				Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("should place the resources on the selected member cluster", func() {
				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProdName)
			})

			It("should use client-side apply", func() {
				Eventually(func() error {
					cm := &corev1.ConfigMap{}
					if err := memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: workNamespaceName}, cm); err != nil {
						return fmt.Errorf("failed to get configMap: %w", err)
					}

					lastAppliedConf, foundAnnotation := cm.Annotations[placementv1beta1.LastAppliedConfigAnnotation]
					if !foundAnnotation || len(lastAppliedConf) == 0 {
						return fmt.Errorf("the configMap object does not have annotation %s in presence or its value is empty", placementv1beta1.LastAppliedConfigAnnotation)
					}

					// Field manager might still be set; this is an expected behavior.
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to fall back to server-side apply")
			})

			AfterAll(func() {
				ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, allMemberClusters)
			})
		})

	})

	Describe("switching apply strategies", func() {
		Context("switch from client-side apply to report diff", Ordered, func() {
			anotherConfigMapName := types.NamespacedName{Name: fmt.Sprintf("another-config-map-%d", GinkgoParallelProcess()), Namespace: workNamespaceName}
			selectedResources := []placementv1beta1.ResourceIdentifier{
				{
					Kind:      "ConfigMap",
					Name:      configMapName,
					Version:   "v1",
					Namespace: workNamespaceName,
				},
				{
					Kind:      "ConfigMap",
					Name:      anotherConfigMapName.Name,
					Version:   "v1",
					Namespace: workNamespaceName,
				},
			}

			BeforeAll(func() {
				// In the clusterResourcePlacement test, it selects two resources, ns and configMap.
				// Similarly, configMap maps to ns while anotherConfigMap maps to configMap.
				createConfigMap()
				createAnotherConfigMap(anotherConfigMapName)

				cm := appConfigMap()
				By(fmt.Sprintf("creating configmap %s/%s on member cluster", cm.Namespace, cm.Name))
				Expect(memberCluster1EastProdClient.Create(ctx, &cm)).Should(Succeed(), "Failed to create configmap")

				rp := &placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: workNamespaceName,
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
								Name:    configMapName,
							},
							{
								Group:   "",
								Kind:    "ConfigMap",
								Version: "v1",
								Name:    anotherConfigMapName.Name,
							},
						},
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickFixedPlacementType,
							ClusterNames: []string{
								memberCluster1EastProdName,
								memberCluster2EastCanaryName,
							},
						},
						Strategy: placementv1beta1.RolloutStrategy{
							Type: placementv1beta1.RollingUpdateRolloutStrategyType,
							RollingUpdate: &placementv1beta1.RollingUpdateConfig{
								MaxUnavailable:           ptr.To(intstr.FromInt(1)),
								MaxSurge:                 ptr.To(intstr.FromInt(1)),
								UnavailablePeriodSeconds: ptr.To(2),
							},
							ApplyStrategy: &placementv1beta1.ApplyStrategy{
								ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
								WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
								Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
								WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeNever,
							},
						},
					},
				}
				Expect(hubClient.Create(ctx, rp)).To(Succeed())
			})

			It("should update RP status as expected", func() {
				buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
					return &placementv1beta1.PlacementStatus{
						Conditions:        rpAppliedFailedConditions(rpGeneration),
						SelectedResources: selectedResources,
						PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
							{
								ClusterName:           memberCluster1EastProdName,
								ObservedResourceIndex: "0",
								Conditions:            perClusterApplyFailedConditions(rpGeneration),
								FailedPlacements: []placementv1beta1.FailedResourcePlacement{
									{
										ResourceIdentifier: placementv1beta1.ResourceIdentifier{
											Version:   "v1",
											Kind:      "ConfigMap",
											Name:      configMapName,
											Namespace: workNamespaceName,
										},
										Condition: metav1.Condition{
											Type:   string(placementv1beta1.PerClusterAppliedConditionType),
											Status: metav1.ConditionFalse,
											Reason: string(workapplier.ApplyOrReportDiffResTypeNotTakenOver),
										},
									},
								},
							},
							{
								ClusterName:           memberCluster2EastCanaryName,
								ObservedResourceIndex: "0",
								Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
							},
						},
						ObservedResourceIndex: "0",
					}
				}

				Eventually(func() error {
					rp := &placementv1beta1.ResourcePlacement{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, rp); err != nil {
						return err
					}
					wantRPStatus := buildWantRPStatus(rp.Generation)

					if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
						return fmt.Errorf("RP status diff (-got, +want): %s", diff)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("can update the manifests", func() {
				// updating the second configMap data
				Eventually(func() error {
					cm := &corev1.ConfigMap{}
					if err := hubClient.Get(ctx, anotherConfigMapName, cm); err != nil {
						return fmt.Errorf("failed to get configmap: %w", err)
					}

					cm.Data = map[string]string{"data": "bar"}
					if err := hubClient.Update(ctx, cm); err != nil {
						return fmt.Errorf("failed to update configmap: %w", err)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the manifests")
			})

			It("should update RP status as expected", func() {
				// The rollout of the previous change will be blocked due to the rollout
				// strategy configuration (1 member cluster has failed; 0 clusters are
				// allowed to become unavailable).
				buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
					return &placementv1beta1.PlacementStatus{
						Conditions:        rpRolloutStuckConditions(rpGeneration),
						SelectedResources: selectedResources,
						PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
							{
								ClusterName:           memberCluster1EastProdName,
								ObservedResourceIndex: "1",
								Conditions:            perClusterApplyFailedConditions(rpGeneration),
								FailedPlacements: []placementv1beta1.FailedResourcePlacement{
									{
										ResourceIdentifier: placementv1beta1.ResourceIdentifier{
											Version:   "v1",
											Kind:      "ConfigMap",
											Name:      configMapName,
											Namespace: workNamespaceName,
										},
										Condition: metav1.Condition{
											Type:   string(placementv1beta1.PerClusterAppliedConditionType),
											Status: metav1.ConditionFalse,
											Reason: string(workapplier.ApplyOrReportDiffResTypeNotTakenOver),
										},
									},
								},
							},
							{
								ClusterName:           memberCluster2EastCanaryName,
								ObservedResourceIndex: "1",
								Conditions:            perClusterSyncPendingConditions(rpGeneration),
							},
						},
						ObservedResourceIndex: "1",
					}
				}

				Eventually(func() error {
					rp := &placementv1beta1.ResourcePlacement{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, rp); err != nil {
						return err
					}
					wantRPStatus := buildWantRPStatus(rp.Generation)

					if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
						return fmt.Errorf("RP status diff (-got, +want): %s", diff)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("can update the apply strategy", func() {
				Eventually(func() error {
					rp := &placementv1beta1.ResourcePlacement{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, rp); err != nil {
						return fmt.Errorf("failed to get RP: %w", err)
					}

					rp.Spec.Strategy.ApplyStrategy = &placementv1beta1.ApplyStrategy{
						Type:             placementv1beta1.ApplyStrategyTypeReportDiff,
						WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeNever,
						ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					}
					if err := hubClient.Update(ctx, rp); err != nil {
						return fmt.Errorf("failed to update RP: %w", err)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the apply strategy")
			})

			It("should update RP status as expected", func() {
				// The rollout of the previous change will be blocked due to the rollout
				// strategy configuration (1 member cluster has failed; 0 clusters are
				// allowed to become unavailable).
				buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
					return &placementv1beta1.PlacementStatus{
						Conditions:        rpDiffReportedConditions(rpGeneration, false),
						SelectedResources: selectedResources,
						PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
							{
								ClusterName:           memberCluster1EastProdName,
								ObservedResourceIndex: "1",
								Conditions:            perClusterDiffReportedConditions(rpGeneration),
							},
							{
								ClusterName:           memberCluster2EastCanaryName,
								ObservedResourceIndex: "1",
								Conditions:            perClusterDiffReportedConditions(rpGeneration),
								DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
									{
										ResourceIdentifier: placementv1beta1.ResourceIdentifier{
											Version:   "v1",
											Kind:      "ConfigMap",
											Name:      anotherConfigMapName.Name,
											Namespace: workNamespaceName,
										},
										TargetClusterObservedGeneration: ptr.To(int64(0)),
										ObservedDiffs: []placementv1beta1.PatchDetail{
											{
												Path:          "/data/data",
												ValueInHub:    "bar",
												ValueInMember: "test",
											},
										},
									},
								},
							},
						},
						ObservedResourceIndex: "1",
					}
				}

				Eventually(func() error {
					rp := &placementv1beta1.ResourcePlacement{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, rp); err != nil {
						return err
					}
					wantRPStatus := buildWantRPStatus(rp.Generation)

					if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
						return fmt.Errorf("RP status diff (-got, +want): %s", diff)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			AfterAll(func() {
				cleanupConfigMapOnCluster(memberCluster1EastProd)
				cleanupAnotherConfigMap(anotherConfigMapName)

				ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, allMemberClusters)
			})
		})

		Context("switch from report diff to server side apply", Ordered, func() {
			BeforeAll(func() {
				createConfigMap()

				// Prepare the pre-existing resources.
				cm := appConfigMap()
				if cm.Labels == nil {
					cm.Labels = make(map[string]string)
				}
				cm.Labels[unmanagedLabelKey] = unmanagedLabelVal1

				By(fmt.Sprintf("creating configmap %s/%s on member cluster", cm.Namespace, cm.Name))
				Expect(memberCluster1EastProdClient.Create(ctx, &cm)).Should(Succeed(), "Failed to create configmap")

				rp := &placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: workNamespaceName,
						// Add a custom finalizer; this would allow us to better observe
						// the behavior of the controllers.
						Finalizers: []string{customDeletionBlockerFinalizer},
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: configMapSelector(),
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickFixedPlacementType,
							ClusterNames: []string{
								memberCluster1EastProdName,
								memberCluster2EastCanaryName,
							},
						},
						Strategy: placementv1beta1.RolloutStrategy{
							Type: placementv1beta1.RollingUpdateRolloutStrategyType,
							RollingUpdate: &placementv1beta1.RollingUpdateConfig{
								MaxUnavailable:           ptr.To(intstr.FromInt(1)),
								MaxSurge:                 ptr.To(intstr.FromInt(1)),
								UnavailablePeriodSeconds: ptr.To(2),
							},
							ApplyStrategy: &placementv1beta1.ApplyStrategy{
								ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
								Type:             placementv1beta1.ApplyStrategyTypeReportDiff,
							},
						},
					},
				}
				Expect(hubClient.Create(ctx, rp)).To(Succeed())
			})

			It("should update RP status as expected", func() {
				buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
					return &placementv1beta1.PlacementStatus{
						Conditions:        rpDiffReportedConditions(rpGeneration, false),
						SelectedResources: appConfigMapIdentifiers(),
						PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
							{
								ClusterName:           memberCluster1EastProdName,
								ObservedResourceIndex: "0",
								Conditions:            perClusterDiffReportedConditions(rpGeneration),
								DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
									{
										ResourceIdentifier: placementv1beta1.ResourceIdentifier{
											Version:   "v1",
											Kind:      "ConfigMap",
											Name:      configMapName,
											Namespace: workNamespaceName,
										},
										TargetClusterObservedGeneration: ptr.To(int64(0)),
										ObservedDiffs: []placementv1beta1.PatchDetail{
											{
												Path:          fmt.Sprintf("/metadata/labels/%s", unmanagedLabelKey),
												ValueInMember: unmanagedLabelVal1,
											},
										},
									},
								},
							},
							{
								ClusterName:           memberCluster2EastCanaryName,
								ObservedResourceIndex: "0",
								Conditions:            perClusterDiffReportedConditions(rpGeneration),
								DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
									{
										ResourceIdentifier: placementv1beta1.ResourceIdentifier{
											Version:   "v1",
											Kind:      "ConfigMap",
											Name:      configMapName,
											Namespace: workNamespaceName,
										},
										ObservedDiffs: []placementv1beta1.PatchDetail{
											{
												Path:       "/",
												ValueInHub: "(the whole object)",
											},
										},
									},
								},
							},
						},
						ObservedResourceIndex: "0",
					}
				}

				Eventually(func() error {
					rp := &placementv1beta1.ResourcePlacement{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, rp); err != nil {
						return err
					}
					wantRPStatus := buildWantRPStatus(rp.Generation)

					if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
						return fmt.Errorf("RP status diff (-got, +want): %s", diff)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("can update the manifests", func() {
				Eventually(func() error {
					cm := &corev1.ConfigMap{}
					if err := hubClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: workNamespaceName}, cm); err != nil {
						return fmt.Errorf("failed to get configmap: %w", err)
					}

					if cm.Labels == nil {
						cm.Labels = make(map[string]string)
					}
					cm.Labels[unmanagedLabelKey] = unmanagedLabelVal1
					if err := hubClient.Update(ctx, cm); err != nil {
						return fmt.Errorf("failed to update configmap: %w", err)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the manifests")
			})

			It("should update RP status as expected", func() {
				buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
					return &placementv1beta1.PlacementStatus{
						Conditions:        rpDiffReportedConditions(rpGeneration, false),
						SelectedResources: appConfigMapIdentifiers(),
						PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
							{
								ClusterName:           memberCluster1EastProdName,
								ObservedResourceIndex: "1",
								Conditions:            perClusterDiffReportedConditions(rpGeneration),
							},
							{
								ClusterName:           memberCluster2EastCanaryName,
								ObservedResourceIndex: "1",
								Conditions:            perClusterDiffReportedConditions(rpGeneration),
								DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
									{
										ResourceIdentifier: placementv1beta1.ResourceIdentifier{
											Version:   "v1",
											Kind:      "ConfigMap",
											Name:      configMapName,
											Namespace: workNamespaceName,
										},
										ObservedDiffs: []placementv1beta1.PatchDetail{
											{
												Path:       "/",
												ValueInHub: "(the whole object)",
											},
										},
									},
								},
							},
						},
						ObservedResourceIndex: "1",
					}
				}

				Eventually(func() error {
					rp := &placementv1beta1.ResourcePlacement{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, rp); err != nil {
						return err
					}
					wantRPStatus := buildWantRPStatus(rp.Generation)

					if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
						return fmt.Errorf("RP status diff (-got, +want): %s", diff)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			It("can update the apply strategy", func() {
				Eventually(func() error {
					rp := &placementv1beta1.ResourcePlacement{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, rp); err != nil {
						return fmt.Errorf("failed to get RP: %w", err)
					}

					rp.Spec.Strategy.ApplyStrategy = &placementv1beta1.ApplyStrategy{
						Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
						WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
						ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
						ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{
							ForceConflicts: true,
						},
					}
					if err := hubClient.Update(ctx, rp); err != nil {
						return fmt.Errorf("failed to update RP: %w", err)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the apply strategy")
			})

			It("should update RP status as expected", func() {
				buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
					return &placementv1beta1.PlacementStatus{
						Conditions:        rpRolloutCompletedConditions(rpGeneration, false),
						SelectedResources: appConfigMapIdentifiers(),
						PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
							{
								ClusterName:           memberCluster1EastProdName,
								ObservedResourceIndex: "1",
								Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
							},
							{
								ClusterName:           memberCluster2EastCanaryName,
								ObservedResourceIndex: "1",
								Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
							},
						},
						ObservedResourceIndex: "1",
					}
				}

				Eventually(func() error {
					rp := &placementv1beta1.ResourcePlacement{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, rp); err != nil {
						return err
					}
					wantRPStatus := buildWantRPStatus(rp.Generation)

					if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
						return fmt.Errorf("RP status diff (-got, +want): %s", diff)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
			})

			AfterAll(func() {
				ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, allMemberClusters)
			})
		})
	})
})

func createAnotherConfigMap(name types.NamespacedName) {
	configMap := buildAppConfigMap(name)
	Expect(hubClient.Create(ctx, &configMap)).To(Succeed(), "Failed to create config map %s", configMap.Name)
}

func cleanupAnotherConfigMap(name types.NamespacedName) {
	cm := &corev1.ConfigMap{}
	err := hubClient.Get(ctx, name, cm)
	if err != nil && apierrors.IsNotFound(err) {
		return
	}
	Expect(err).To(Succeed(), "Failed to get config map %s", name)

	Expect(hubClient.Delete(ctx, cm)).To(Succeed(), "Failed to delete config map %s", name)

	Eventually(func() error {
		cm := &corev1.ConfigMap{}
		err := hubClient.Get(ctx, name, cm)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return fmt.Errorf("config map %s still exists", name)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to wait for config map %s to be deleted", name)
}
