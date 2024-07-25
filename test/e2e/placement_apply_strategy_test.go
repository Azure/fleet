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
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/work"
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
			createCRP(crpName, strategy)
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
			createCRP(crpName, strategy)
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
			createCRP(crpName, strategy)
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
			createCRP(crpName, strategy)
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
										Reason:             work.ManifestsAlreadyOwnedByOthersReason,
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

	Context("Test a CRP fail to apply namespace when the conflicted annotation is managed by others (server-side-apply and allow co-own)", Ordered, func() {
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
				ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: false},
				AllowCoOwnership:      true,
			}
			createCRP(crpName, strategy)
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
							ClusterName: allMemberClusters[0].ClusterName,
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
										Reason:             work.ManifestApplyFailedReason,
										ObservedGeneration: 0,
									},
								},
							},
							Conditions: resourcePlacementApplyFailedConditions(crp.Generation),
						},
						{
							ClusterName: allMemberClusters[1].ClusterName,
							Conditions:  resourcePlacementRolloutCompletedConditions(crp.Generation, true, false),
						},
						{
							ClusterName: allMemberClusters[2].ClusterName,
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

		// This check will ignore the annotation of resources.
		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should have original annotation on the namespace of first cluster", func() {
			want := map[string]string{annotationKey: annotationValue}
			Expect(validateAnnotationOfWorkNamespaceOnCluster(allMemberClusters[0], want)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", allMemberClusters[0].ClusterName)
		})

		It("should have updated annotations on the namespace of all clusters excluding the first one", func() {
			want := map[string]string{annotationKey: annotationUpdatedValue}
			for _, c := range allMemberClusters[1:] {
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
			createCRP(crpName, strategy)
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
})

var _ = Describe("validating two CRP selecting the same resources", Ordered, func() {
	BeforeAll(func() {
		By("creating work resources on hub cluster")
		createWorkResources()
	})

	AfterAll(func() {
		By("deleting created work resources on hub cluster")
		cleanupWorkResources()
	})

	Context("Test multiple CRPs (with same apply strategy) can place objects successfully", Ordered, func() {
		BeforeAll(func() {
			for i := 0; i < 2; i++ {
				crpName := fmt.Sprintf(crpNameWithSubIndexTemplate, GinkgoParallelProcess(), i)
				createCRP(crpName, nil)
			}
		})

		AfterAll(func() {
			// The test will create 3 CRPs in total.
			for i := 0; i < 3; i++ {
				crpName := fmt.Sprintf(crpNameWithSubIndexTemplate, GinkgoParallelProcess(), i)
				By(fmt.Sprintf("deleting placement %s", crpName))
				cleanupCRP(crpName)
			}
		})

		It("should update CRP status as expected", func() {
			for i := 0; i < 2; i++ {
				crpName := fmt.Sprintf(crpNameWithSubIndexTemplate, GinkgoParallelProcess(), i)
				crpStatusUpdatedActual := customizedCRPStatusUpdatedActual(crpName, workResourceIdentifiers(), allMemberClusterNames, nil, "0", true)
				Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
			}
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("can delete the CRP", func() {
			// Delete the CRP.
			crpName := fmt.Sprintf(crpNameWithSubIndexTemplate, GinkgoParallelProcess(), 0)
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
			}
			Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
		})

		It("add a new CRP and selecting the same resources", func() {
			crpName := fmt.Sprintf(crpNameWithSubIndexTemplate, GinkgoParallelProcess(), 2)
			createCRP(crpName, nil)
		})

		It("should update CRP status as expected", func() {
			crpName := fmt.Sprintf(crpNameWithSubIndexTemplate, GinkgoParallelProcess(), 2)
			crpStatusUpdatedActual := customizedCRPStatusUpdatedActual(crpName, workResourceIdentifiers(), allMemberClusterNames, nil, "0", true)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClustersConsistently)

		It("can delete the CRP", func() {
			// Delete the CRP.
			for i := 1; i < 3; i++ {
				crpName := fmt.Sprintf(crpNameWithSubIndexTemplate, GinkgoParallelProcess(), i)
				crp := &placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
				}
				Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
			}
		})

		It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

		It("should remove controller finalizers from CRP", func() {
			for i := 0; i < 3; i++ {
				crpName := fmt.Sprintf(crpNameWithSubIndexTemplate, GinkgoParallelProcess(), i)
				finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual(crpName)
				Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
			}
		})
	})

	Context("Test placement should fail when the resource is owned by other CRP with different strategy", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		conflictCRPName := fmt.Sprintf(crpNameWithSubIndexTemplate, GinkgoParallelProcess(), 0)
		BeforeAll(func() {
			createCRP(crpName, nil)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s", crpName))
			cleanupCRP(crpName)

			// in case fail in the middle
			By(fmt.Sprintf("deleting placement %s", conflictCRPName))
			cleanupCRP(conflictCRPName)
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("create another CRP with different apply strategy", func() {
			createCRP(conflictCRPName, &placementv1beta1.ApplyStrategy{AllowCoOwnership: true})
		})

		It("should update conflicted CRP status as expected", func() {
			crpStatusUpdatedActual := func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: conflictCRPName}, crp); err != nil {
					return err
				}

				workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
				appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
				wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
					Conditions:        crpAppliedFailedConditions(crp.Generation),
					PlacementStatuses: buildApplyConflictFailedPlacements(crp.Generation, allMemberClusterNames),
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

		It("can delete the CRP", func() {
			// Delete the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
			}
			Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)

			// Cleanup the conflict CRP
			cleanupCRP(conflictCRPName)
		})

		It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

		It("should remove controller finalizers from CRP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual(crpName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
		})
	})
})

func createCRP(crpName string, applyStrategy *placementv1beta1.ApplyStrategy) {
	crp := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpName,
			// Add a custom finalizer; this would allow us to better observe
			// the behavior of the controllers.
			Finalizers: []string{customDeletionBlockerFinalizer},
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			ResourceSelectors: workResourceSelector(),
			Strategy: placementv1beta1.RolloutStrategy{
				ApplyStrategy: applyStrategy,
			},
		},
	}
	By(fmt.Sprintf("creating placement %s", crpName))
	Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
}

func buildApplyConflictFailedPlacements(generation int64, cluster []string) []placementv1beta1.ResourcePlacementStatus {
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	res := make([]placementv1beta1.ResourcePlacementStatus, 0, len(cluster))
	for _, c := range cluster {
		res = append(res, placementv1beta1.ResourcePlacementStatus{
			ClusterName: c,
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
						Reason:             work.ApplyConflictBetweenPlacementsReason,
						ObservedGeneration: 0,
					},
				},
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Kind:      "ConfigMap",
						Name:      fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess()),
						Namespace: workNamespaceName,
						Version:   "v1",
					},
					Condition: metav1.Condition{
						Type:               placementv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						Reason:             work.ApplyConflictBetweenPlacementsReason,
						ObservedGeneration: 0,
					},
				},
			},
			Conditions: resourcePlacementApplyFailedConditions(generation),
		})
	}
	return res
}
