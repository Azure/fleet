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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workapplier"
)

const (
	e2eTestFieldManager = "e2e-test-field-manager"
)

var _ = Describe("validating CRP when resources exists", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	annotationKey := "annotation-key"
	annotationValue := "annotation-value"
	annotationUpdatedValue := "annotation-updated-value"
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	anotherOwnerReference := metav1.OwnerReference{}

	BeforeAll(func() {
		By("creating work resources on hub cluster")
		createWorkResources()

		By("creating owner reference for the namespace")
		anotherOwnerReference = createAnotherValidOwnerReference(fmt.Sprintf("owner-namespace-%d", GinkgoParallelProcess()))
	})

	AfterAll(func() {
		By("deleting created work resources on hub cluster")
		cleanupWorkResources()

		By("deleting owner reference namespace")
		cleanupAnotherValidOwnerReference(anotherOwnerReference.Name)
	})

	Context("Test a CRP place objects successfully (client-side-apply and allow co-own)", Ordered, func() {
		BeforeAll(func() {
			ns := appNamespace()
			ns.SetOwnerReferences([]metav1.OwnerReference{
				anotherOwnerReference,
			})
			ns.Annotations = map[string]string{
				annotationKey: annotationValue,
			}
			By(fmt.Sprintf("creating namespace %s on member cluster", ns.Name))
			Expect(allMemberClusters[0].KubeClient.Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace %s", ns.Name)

			// Create the CRP.
			strategy := &placementv1beta1.ApplyStrategy{AllowCoOwnership: true}
			createCRPWithApplyStrategy(crpName, strategy, nil)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s", crpName))
			cleanupPlacement(types.NamespacedName{Name: crpName})

			By("deleting created work resources on member cluster")
			cleanWorkResourcesOnCluster(allMemberClusters[0])
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		// This check will ignore the annotation of resources.
		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should have annotations on the namespace", func() {
			want := map[string]string{annotationKey: annotationValue}
			Expect(validateAnnotationOfWorkNamespaceOnCluster(memberCluster1EastProd, want)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", memberCluster1EastProdName)
		})

		It("can delete the CRP", func() {
			// Delete the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
			}
			Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
		})

		It("should remove placed resources from member clusters excluding the first one", func() {
			checkIfRemovedWorkResourcesFromMemberClusters(allMemberClusters[1:])
		})

		It("should remove controller finalizers from CRP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: crpName})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
		})

		It("namespace should be kept on member cluster", func() {
			checkNamespaceExistsWithOwnerRefOnMemberCluster(workNamespaceName, crpName)
		})
	})

	Context("Test a CRP place objects successfully (client-side-apply and disallow co-own) and existing resource has no owner reference", Ordered, func() {
		BeforeAll(func() {
			ns := appNamespace()
			ns.Annotations = map[string]string{
				annotationKey: annotationValue,
			}
			By(fmt.Sprintf("creating namespace %s on member cluster", ns.Name))
			Expect(allMemberClusters[0].KubeClient.Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace %s", ns.Name)

			// Create the CRP.
			strategy := &placementv1beta1.ApplyStrategy{AllowCoOwnership: false}
			createCRPWithApplyStrategy(crpName, strategy, nil)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s", crpName))
			cleanupPlacement(types.NamespacedName{Name: crpName})
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		// This check will ignore the annotation of resources.
		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should have annotations on the namespace", func() {
			want := map[string]string{annotationKey: annotationValue}
			Expect(validateAnnotationOfWorkNamespaceOnCluster(memberCluster1EastProd, want)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", memberCluster1EastProdName)
		})

		It("can delete the CRP", func() {
			// Delete the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
			}
			Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
		})

		It("should remove the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

		It("should remove controller finalizers from CRP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: crpName})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
		})
	})

	Context("Test a CRP place objects successfully (server-side-apply and disallow co-own) and existing resource has no owner reference", Ordered, func() {
		BeforeAll(func() {
			ns := appNamespace()
			ns.Annotations = map[string]string{
				annotationKey: annotationValue,
			}
			By(fmt.Sprintf("creating namespace %s on member cluster", ns.Name))
			Expect(allMemberClusters[0].KubeClient.Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace %s", ns.Name)

			// Create the CRP.
			strategy := &placementv1beta1.ApplyStrategy{
				Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
				AllowCoOwnership: false,
			}
			createCRPWithApplyStrategy(crpName, strategy, nil)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s", crpName))
			cleanupPlacement(types.NamespacedName{Name: crpName})
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		// This check will ignore the annotation of resources.
		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should have annotations on the namespace", func() {
			want := map[string]string{annotationKey: annotationValue}
			Expect(validateAnnotationOfWorkNamespaceOnCluster(memberCluster1EastProd, want)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", memberCluster1EastProdName)
		})

		It("can delete the CRP", func() {
			// Delete the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
			}
			Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
		})

		It("should remove the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

		It("should remove controller finalizers from CRP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: crpName})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
		})
	})

	Context("Test a CRP fail to apply namespace (server-side-apply and disallow co-own) and existing resource is owned by others", Ordered, func() {
		BeforeAll(func() {
			ns := appNamespace()
			ns.SetOwnerReferences([]metav1.OwnerReference{
				anotherOwnerReference,
			})
			By(fmt.Sprintf("creating namespace %s on member cluster", ns.Name))
			Expect(allMemberClusters[0].KubeClient.Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace %s", ns.Name)

			// Create the CRP.
			strategy := &placementv1beta1.ApplyStrategy{
				Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
				AllowCoOwnership: false,
			}
			createCRPWithApplyStrategy(crpName, strategy, nil)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s", crpName))
			cleanupPlacement(types.NamespacedName{Name: crpName})

			By("deleting created work resources on member cluster")
			cleanWorkResourcesOnCluster(allMemberClusters[0])
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
				appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
				wantStatus := placementv1beta1.PlacementStatus{
					Conditions: crpAppliedFailedConditions(crp.Generation),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Kind:    "Namespace",
										Name:    workNamespaceName,
										Version: "v1",
									},
									Condition: metav1.Condition{
										Type:               placementv1beta1.WorkConditionTypeApplied,
										Status:             metav1.ConditionFalse,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
										ObservedGeneration: 0,
									},
								},
							},
							Conditions: perClusterApplyFailedConditions(crp.Generation),
						},
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(crp.Generation, true, false),
						},
						{
							ClusterName:           memberCluster3WestProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(crp.Generation, true, false),
						},
					},
					SelectedResources: []placementv1beta1.ResourceIdentifier{
						{
							Kind:    "Namespace",
							Name:    workNamespaceName,
							Version: "v1",
						},
						{
							Kind:      "ConfigMap",
							Name:      appConfigMapName,
							Version:   "v1",
							Namespace: workNamespaceName,
						},
					},
					ObservedResourceIndex: "0",
				}
				if diff := cmp.Diff(crp.Status, wantStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("can delete the CRP", func() {
			// Delete the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
			}
			Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
		})

		It("should remove placed resources from member clusters excluding the first one", func() {
			checkIfRemovedWorkResourcesFromMemberClusters(allMemberClusters[1:])
		})

		It("should remove controller finalizers from CRP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: crpName})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
		})

		It("namespace should be kept on member cluster", func() {
			checkNamespaceExistsWithOwnerRefOnMemberCluster(workNamespaceName, crpName)
		})
	})

	Context("Test a CRP able to apply namespace when the conflicted annotation is managed by others (force server-side-apply and allow co-own)", Ordered, func() {
		BeforeAll(func() {
			ns := appNamespace()
			ns.SetOwnerReferences([]metav1.OwnerReference{
				anotherOwnerReference,
			})
			ns.Annotations = map[string]string{
				annotationKey: annotationValue,
			}
			options := client.CreateOptions{FieldManager: e2eTestFieldManager}
			By(fmt.Sprintf("creating namespace %s on member cluster", ns.Name))
			Expect(allMemberClusters[0].KubeClient.Create(ctx, &ns, &options)).Should(Succeed(), "Failed to create namespace %s", ns.Name)

			By(fmt.Sprintf("updating namespace %s annotation on hub cluster", ns.Name))
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, &ns)).Should(Succeed(), "Failed to get namespace %s", workNamespaceName)
			ns.Annotations = map[string]string{
				annotationKey: annotationUpdatedValue,
			}
			Expect(hubClient.Update(ctx, &ns)).Should(Succeed(), "Failed to update namespace %s", workNamespaceName)

			// Create the CRP.
			strategy := &placementv1beta1.ApplyStrategy{
				Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
				ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: true},
				AllowCoOwnership:      true,
			}
			createCRPWithApplyStrategy(crpName, strategy, nil)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s", crpName))
			cleanupPlacement(types.NamespacedName{Name: crpName})

			By("deleting created work resources on member cluster")
			cleanWorkResourcesOnCluster(allMemberClusters[0])
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		// This check will ignore the annotation of resources.
		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should have updated annotations on the namespace of all clusters", func() {
			want := map[string]string{annotationKey: annotationUpdatedValue}
			for _, c := range allMemberClusters {
				Expect(validateAnnotationOfWorkNamespaceOnCluster(c, want)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", c.ClusterName)
			}
		})

		It("can delete the CRP", func() {
			// Delete the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
			}
			Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
		})

		It("should remove placed resources from member clusters excluding the first one", func() {
			checkIfRemovedWorkResourcesFromMemberClusters(allMemberClusters[1:])
		})

		It("should remove controller finalizers from CRP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: crpName})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
		})

		It("namespace should be kept on member cluster", func() {
			checkNamespaceExistsWithOwnerRefOnMemberCluster(workNamespaceName, crpName)
		})
	})

	Context("no dual placement", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		conflictedCRPName := "crp-conflicted"
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
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
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on member clusters", func() {
			workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProdName)

		})

		It("can create a conflicted CRP", func() {
			conflictedCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: conflictedCRPName,
					// No need for the custom deletion blocker finalizer.
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
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
			Expect(hubClient.Create(ctx, conflictedCRP)).To(Succeed())
		})

		It("should update conflicted CRP status as expected", func() {
			buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        crpAppliedFailedConditions(crpGeneration),
					SelectedResources: workResourceIdentifiers(),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterApplyFailedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									Condition: metav1.Condition{
										Type:   string(placementv1beta1.PerClusterAppliedConditionType),
										Status: metav1.ConditionFalse,
										Reason: string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
									},
								},
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cmName,
										Namespace: nsName,
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
				conflictedCRP := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: conflictedCRPName}, conflictedCRP); err != nil {
					return err
				}
				wantCRPStatus := buildWantCRPStatus(conflictedCRP.Generation)

				if diff := cmp.Diff(conflictedCRP.Status, *wantCRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should have no effect on previously created CRP", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should not add additional owner reference to affected resources", func() {
			expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, crpName, "")

			ns := &corev1.Namespace{}
			nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: nsName}, ns)).To(Succeed())

			wantNS := ptr.To(appNamespace()).DeepCopy()
			if wantNS.Labels == nil {
				wantNS.Labels = make(map[string]string)
			}
			wantNS.Labels["kubernetes.io/metadata.name"] = nsName
			wantNS.Labels[workNamespaceLabelName] = fmt.Sprintf("%d", GinkgoParallelProcess())
			wantNS.OwnerReferences = []metav1.OwnerReference{*expectedOwnerRef}

			// No need to use an Eventually block as this spec runs after the CRP status has been verified.
			diff := cmp.Diff(
				ns, wantNS,
				ignoreNamespaceSpecField,
				ignoreNamespaceStatusField,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "Namespace diff (-got +want):\n%s", diff)

			cm := &corev1.ConfigMap{}
			cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm)).To(Succeed())

			// The difference has been overwritten.
			wantCM := appConfigMap()
			wantCM.OwnerReferences = []metav1.OwnerReference{*expectedOwnerRef}

			// No need to use an Eventually block as this spec runs after the CRP status has been verified.
			diff = cmp.Diff(
				cm, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

			ensureCRPAndRelatedResourcesDeleted(conflictedCRPName, allMemberClusters)
		})
	})
})

var _ = Describe("SSA", Ordered, func() {
	Context("use server-side apply to place resources (with changes)", func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		// The key here should match the one used in the default config map.
		cmDataKey := "data"
		cmDataVal1 := "foobar"

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			createWorkResources()

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
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
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("can update the manifests", func() {
			Eventually(func() error {
				cm := &corev1.ConfigMap{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm); err != nil {
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

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "1")
			// Longer timeout is used to allow full rollouts.
			Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should refresh the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberClient := allMemberClusters[idx].KubeClient

				Eventually(func() error {
					cm := &corev1.ConfigMap{}
					if err := memberClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm); err != nil {
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
							Name:      cmName,
							Namespace: nsName,
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
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("fall back to server-side apply when client-side apply cannot be used", func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		cmDataKey := "randomBase64Str"

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			ns := appNamespace()
			Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: nsName,
				},
				Data: map[string]string{
					cmDataKey: "",
				},
			}
			Expect(hubClient.Create(ctx, cm)).To(Succeed(), "Failed to create configMap %s in namespace %s", cmName, nsName)

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
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
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
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
				if err := hubClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm); err != nil {
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

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName}, nil, "1")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on the selected member cluster", func() {
			workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProdName)
		})

		It("should fall back to server-side apply", func() {
			Eventually(func() error {
				cm := &corev1.ConfigMap{}
				if err := memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm); err != nil {
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
				if err := hubClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm); err != nil {
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

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName}, nil, "2")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on the selected member cluster", func() {
			workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProdName)
		})

		It("should use client-side apply", func() {
			Eventually(func() error {
				cm := &corev1.ConfigMap{}
				if err := memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm); err != nil {
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
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})
})

var _ = Describe("switching apply strategies", func() {
	Context("switch from client-side apply to report diff", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			createWorkResources()

			// Prepare the pre-existing resources.
			ns := appNamespace()
			Expect(memberCluster1EastProdClient.Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace")

			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
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
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should update CRP status as expected", func() {
			buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        crpAppliedFailedConditions(crpGeneration),
					SelectedResources: workResourceIdentifiers(),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterApplyFailedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
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
							Conditions:            perClusterRolloutCompletedConditions(crpGeneration, true, false),
						},
					},
					ObservedResourceIndex: "0",
				}
			}

			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				wantCRPStatus := buildWantCRPStatus(crp.Generation)

				if diff := cmp.Diff(crp.Status, *wantCRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can update the manifests", func() {
			Eventually(func() error {
				cm := &corev1.ConfigMap{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm); err != nil {
					return fmt.Errorf("failed to get configmap: %w", err)
				}

				cm.Data = map[string]string{"data": "bar"}
				if err := hubClient.Update(ctx, cm); err != nil {
					return fmt.Errorf("failed to update configmap: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the manifests")
		})

		It("should update CRP status as expected", func() {
			// The rollout of the previous change will be blocked due to the rollout
			// strategy configuration (1 member cluster has failed; 0 clusters are
			// allowed to become unavailable).
			buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        crpRolloutStuckConditions(crpGeneration),
					SelectedResources: workResourceIdentifiers(),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "1",
							Conditions:            perClusterApplyFailedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
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
							Conditions:            perClusterSyncPendingConditions(crpGeneration),
						},
					},
					ObservedResourceIndex: "1",
				}
			}

			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				wantCRPStatus := buildWantCRPStatus(crp.Generation)

				if diff := cmp.Diff(crp.Status, *wantCRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can update the apply strategy", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return fmt.Errorf("failed to get CRP: %w", err)
				}

				crp.Spec.Strategy.ApplyStrategy = &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeReportDiff,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeNever,
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
				}
				if err := hubClient.Update(ctx, crp); err != nil {
					return fmt.Errorf("failed to update CRP: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the apply strategy")
		})

		It("should update CRP status as expected", func() {
			// The rollout of the previous change will be blocked due to the rollout
			// strategy configuration (1 member cluster has failed; 0 clusters are
			// allowed to become unavailable).
			buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        crpDiffReportedConditions(crpGeneration, false),
					SelectedResources: workResourceIdentifiers(),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "1",
							Conditions:            perClusterDiffReportedConditions(crpGeneration),
						},
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "1",
							Conditions:            perClusterDiffReportedConditions(crpGeneration),
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cmName,
										Namespace: nsName,
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
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				wantCRPStatus := buildWantCRPStatus(crp.Generation)

				if diff := cmp.Diff(crp.Status, *wantCRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			cleanWorkResourcesOnCluster(memberCluster1EastProd)

			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("switch from report diff to server side apply", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			createWorkResources()

			// Prepare the pre-existing resources.
			ns := appNamespace()
			if ns.Labels == nil {
				ns.Labels = make(map[string]string)
			}
			ns.Labels[unmanagedLabelKey] = unmanagedLabelVal1

			Expect(memberCluster1EastProdClient.Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace")

			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
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
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should update CRP status as expected", func() {
			buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        crpDiffReportedConditions(crpGeneration, false),
					SelectedResources: workResourceIdentifiers(),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterDiffReportedConditions(crpGeneration),
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									TargetClusterObservedGeneration: ptr.To(int64(0)),
									ObservedDiffs: []placementv1beta1.PatchDetail{
										{
											Path:          fmt.Sprintf("/metadata/labels/%s", unmanagedLabelKey),
											ValueInMember: unmanagedLabelVal1,
										},
									},
								},
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cmName,
										Namespace: nsName,
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
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterDiffReportedConditions(crpGeneration),
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									ObservedDiffs: []placementv1beta1.PatchDetail{
										{
											Path:       "/",
											ValueInHub: "(the whole object)",
										},
									},
								},
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cmName,
										Namespace: nsName,
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
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				wantCRPStatus := buildWantCRPStatus(crp.Generation)

				if diff := cmp.Diff(crp.Status, *wantCRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can update the manifests", func() {
			Eventually(func() error {
				ns := &corev1.Namespace{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: nsName}, ns); err != nil {
					return fmt.Errorf("failed to get namespace: %w", err)
				}

				if ns.Labels == nil {
					ns.Labels = make(map[string]string)
				}
				ns.Labels[unmanagedLabelKey] = unmanagedLabelVal1
				if err := hubClient.Update(ctx, ns); err != nil {
					return fmt.Errorf("failed to update namespace: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the manifests")
		})

		It("should update CRP status as expected", func() {
			buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        crpDiffReportedConditions(crpGeneration, false),
					SelectedResources: workResourceIdentifiers(),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "1",
							Conditions:            perClusterDiffReportedConditions(crpGeneration),
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cmName,
										Namespace: nsName,
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
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "1",
							Conditions:            perClusterDiffReportedConditions(crpGeneration),
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									ObservedDiffs: []placementv1beta1.PatchDetail{
										{
											Path:       "/",
											ValueInHub: "(the whole object)",
										},
									},
								},
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cmName,
										Namespace: nsName,
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
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				wantCRPStatus := buildWantCRPStatus(crp.Generation)

				if diff := cmp.Diff(crp.Status, *wantCRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can update the apply strategy", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return fmt.Errorf("failed to get CRP: %w", err)
				}

				crp.Spec.Strategy.ApplyStrategy = &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{
						ForceConflicts: true,
					},
				}
				if err := hubClient.Update(ctx, crp); err != nil {
					return fmt.Errorf("failed to update CRP: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the apply strategy")
		})

		It("should update CRP status as expected", func() {
			buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        crpRolloutCompletedConditions(crpGeneration, false),
					SelectedResources: workResourceIdentifiers(),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "1",
							Conditions:            perClusterRolloutCompletedConditions(crpGeneration, true, false),
						},
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "1",
							Conditions:            perClusterRolloutCompletedConditions(crpGeneration, true, false),
						},
					},
					ObservedResourceIndex: "1",
				}
			}

			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				wantCRPStatus := buildWantCRPStatus(crp.Generation)

				if diff := cmp.Diff(crp.Status, *wantCRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})
})
