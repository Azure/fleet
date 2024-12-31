/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/workapplier"
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

	BeforeAll(func() {
		By("creating work resources on hub cluster")
		createWorkResources()
	})

	AfterAll(func() {
		By("deleting created work resources on hub cluster")
		cleanupWorkResources()
	})

	Context("Test a CRP place objects successfully (client-side-apply and allow co-own)", Ordered, func() {
		BeforeAll(func() {
			ns := appNamespace()
			ns.SetOwnerReferences([]metav1.OwnerReference{
				{
					APIVersion: "another-api-version",
					Kind:       "another-kind",
					Name:       "another-owner",
					UID:        "another-uid",
				},
			})
			ns.Annotations = map[string]string{
				annotationKey: annotationValue,
			}
			By(fmt.Sprintf("creating namespace %s on member cluster", ns.Name))
			Expect(allMemberClusters[0].KubeClient.Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace %s", ns.Name)

			// Create the CRP.
			strategy := &placementv1beta1.ApplyStrategy{AllowCoOwnership: true}
			createCRPWithApplyStrategy(crpName, strategy)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s", crpName))
			cleanupCRP(crpName)

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
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual(crpName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
		})

		It("namespace should be kept on member cluster", func() {
			Consistently(func() error {
				ns := &corev1.Namespace{}
				return allMemberClusters[0].KubeClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns)
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Namespace which is not owned by the CRP should not be deleted")
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
			createCRPWithApplyStrategy(crpName, strategy)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s", crpName))
			cleanupCRP(crpName)
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
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual(crpName)
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
			createCRPWithApplyStrategy(crpName, strategy)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s", crpName))
			cleanupCRP(crpName)
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
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual(crpName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
		})
	})

	Context("Test a CRP fail to apply namespace (server-side-apply and disallow co-own) and existing resource is owned by others", Ordered, func() {
		BeforeAll(func() {
			ns := appNamespace()
			ns.SetOwnerReferences([]metav1.OwnerReference{
				{
					APIVersion: "another-api-version",
					Kind:       "another-kind",
					Name:       "another-owner",
					UID:        "another-uid",
				},
			})
			By(fmt.Sprintf("creating namespace %s on member cluster", ns.Name))
			Expect(allMemberClusters[0].KubeClient.Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace %s", ns.Name)

			// Create the CRP.
			strategy := &placementv1beta1.ApplyStrategy{
				Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
				AllowCoOwnership: false,
			}
			createCRPWithApplyStrategy(crpName, strategy)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s", crpName))
			cleanupCRP(crpName)

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
				wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
					Conditions: crpAppliedFailedConditions(crp.Generation),
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{
							ClusterName: memberCluster1EastProdName,
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
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
										ObservedGeneration: 0,
									},
								},
							},
							Conditions: resourcePlacementApplyFailedConditions(crp.Generation),
						},
						{
							ClusterName: memberCluster2EastCanaryName,
							Conditions:  resourcePlacementRolloutCompletedConditions(crp.Generation, true, false),
						},
						{
							ClusterName: memberCluster3WestProdName,
							Conditions:  resourcePlacementRolloutCompletedConditions(crp.Generation, true, false),
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
				if diff := cmp.Diff(crp.Status, wantStatus, crpStatusCmpOptions...); diff != "" {
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
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual(crpName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
		})

		It("namespace should be kept on member cluster", func() {
			Consistently(func() error {
				workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
				ns := &corev1.Namespace{}
				return allMemberClusters[0].KubeClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns)
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Namespace which is not owned by the CRP should not be deleted")
		})
	})

	Context("Test a CRP able to apply namespace when the conflicted annotation is managed by others (force server-side-apply and allow co-own)", Ordered, func() {
		BeforeAll(func() {
			ns := appNamespace()
			ns.SetOwnerReferences([]metav1.OwnerReference{
				{
					APIVersion: "another-api-version",
					Kind:       "another-kind",
					Name:       "another-owner",
					UID:        "another-uid",
				},
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
			createCRPWithApplyStrategy(crpName, strategy)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s", crpName))
			cleanupCRP(crpName)

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
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual(crpName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
		})

		It("namespace should be kept on member cluster", func() {
			Consistently(func() error {
				ns := &corev1.Namespace{}
				return allMemberClusters[0].KubeClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns)
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Namespace which is not owned by the CRP should not be deleted")
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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
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
			buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.ClusterResourcePlacementStatus {
				return &placementv1beta1.ClusterResourcePlacementStatus{
					Conditions:        crpAppliedFailedConditions(crpGeneration),
					SelectedResources: workResourceIdentifiers(),
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{
							ClusterName: memberCluster1EastProdName,
							Conditions:  resourcePlacementApplyFailedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									Condition: metav1.Condition{
										Type:   string(placementv1beta1.ResourcesAppliedConditionType),
										Status: metav1.ConditionFalse,
										Reason: string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
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
										Type:   string(placementv1beta1.ResourcesAppliedConditionType),
										Status: metav1.ConditionFalse,
										Reason: string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
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

				if diff := cmp.Diff(conflictedCRP.Status, *wantCRPStatus, crpStatusCmpOptions...); diff != "" {
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
			expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, crpName)

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
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)

			ensureCRPAndRelatedResourcesDeletion(conflictedCRPName, allMemberClusters)
		})
	})
})
