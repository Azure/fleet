/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// Note that this container will run in parallel with other containers.
var _ = Describe("creating CRP and selecting resources by name", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(),
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s", crpName))
		cleanupCRP(crpName)

		By("deleting created work resources")
		cleanupWorkResources()
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual()
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

	It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

var _ = Describe("creating CRP and selecting resources by label", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
					{
						Group:   "",
						Kind:    "Namespace",
						Version: "v1",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								workNamespaceLabelName: strconv.Itoa(GinkgoParallelProcess()),
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s", crpName))
		cleanupCRP(crpName)

		By("deleting created work resources")
		cleanupWorkResources()
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual()
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

	It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

var _ = Describe("validating CRP when cluster-scoped resources become selected after the updates", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
					{
						Group:   "",
						Kind:    "Namespace",
						Version: "v1",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								workNamespaceLabelName: fmt.Sprintf("test-%d", GinkgoParallelProcess()),
							},
						},
					},
				},
				Strategy: placementv1beta1.RolloutStrategy{
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: pointer.Int(5),
					},
				},
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s", crpName))
		cleanupCRP(crpName)

		By("deleting created work resources")
		cleanupWorkResources()
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := func() error {
			return validateCRPStatus(types.NamespacedName{Name: crpName}, nil)
		}
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should not place work resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("updating the resources on the hub and the namespace becomes selected", func() {
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		ns := &corev1.Namespace{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns)).Should(Succeed(), "Failed to get the namespace %s", workNamespaceName)
		ns.Labels = map[string]string{
			workNamespaceLabelName: fmt.Sprintf("test-%d", GinkgoParallelProcess()),
		}
		Expect(hubClient.Update(ctx, ns)).Should(Succeed(), "Failed to update namespace %s", workNamespaceName)
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual()
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

	It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

var _ = Describe("validating CRP when cluster-scoped resources become unselected after the updates", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
					{
						Group:   "",
						Kind:    "Namespace",
						Version: "v1",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								workNamespaceLabelName: strconv.Itoa(GinkgoParallelProcess()),
							},
						},
					},
				},
				Strategy: placementv1beta1.RolloutStrategy{
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: pointer.Int(5),
					},
				},
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s", crpName))
		cleanupCRP(crpName)

		By("deleting created work resources")
		cleanupWorkResources()
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual()
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("updating the resources on the hub and the namespace becomes unselected", func() {
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		ns := &corev1.Namespace{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns)).Should(Succeed(), "Failed to get the namespace %s", workNamespaceName)
		ns.Labels = map[string]string{
			workNamespaceLabelName: fmt.Sprintf("test-%d", GinkgoParallelProcess()),
		}
		Expect(hubClient.Update(ctx, ns)).Should(Succeed(), "Failed to update namespace %s", workNamespaceName)
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := func() error {
			return validateCRPStatus(types.NamespacedName{Name: crpName}, nil)
		}
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should remove the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("can delete the CRP", func() {
		// Delete the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
		}
		Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
	})

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

var _ = Describe("validating CRP when cluster-scoped and namespace-scoped resources are updated", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
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
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: pointer.Int(5),
					},
				},
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s", crpName))
		cleanupCRP(crpName)

		By("deleting created work resources")
		cleanupWorkResources()
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual()
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("updating the resources on the hub", func() {
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		ns := &corev1.Namespace{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns)).Should(Succeed(), "Failed to get the namespace %s", workNamespaceName)
		ns.Labels["foo"] = "bar"
		Expect(hubClient.Update(ctx, ns)).Should(Succeed(), "Failed to update namespace %s", workNamespaceName)

		appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
		configMap := &corev1.ConfigMap{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: workNamespaceName, Name: appConfigMapName}, configMap)).Should(Succeed(), "Failed to get the config map %s", appConfigMapName)

		configMap.Data = map[string]string{
			"data": "test-1",
		}
		Expect(hubClient.Update(ctx, configMap)).Should(Succeed(), "Failed to update config map %s", appConfigMapName)
	})

	It("should update the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual()
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
	})

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

var _ = Describe("validating CRP when adding resources in a matching namespace", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating namespace")
		ns := workNamespace()
		Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Namespace)

		// Create the CRP.
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
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: pointer.Int(5),
					},
				},
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s", crpName))
		cleanupCRP(crpName)

		By("created work resources")
		cleanupWorkResources()
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := func() error {
			wantSelectedResources := []placementv1beta1.ResourceIdentifier{
				{
					Kind:    "Namespace",
					Name:    fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
					Version: "v1",
				},
			}
			return validateCRPStatus(types.NamespacedName{Name: crpName}, wantSelectedResources)
		}
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected namespace on member clusters", func() {
		workNamespaceName := types.NamespacedName{Name: fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())}

		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]
			workResourcesPlacedActual := func() error {
				return validateWorkNamespaceOnCluster(memberCluster, workNamespaceName)
			}
			Expect(workResourcesPlacedActual()).Should(Succeed(), "Failed to place work namespace %s on member cluster %s", workNamespaceName, memberCluster.ClusterName)
		}
	})

	It("creating config map on the hub", func() {
		deploy := appConfigMap()
		Expect(hubClient.Create(ctx, &deploy)).To(Succeed(), "Failed to create deployment %s", deploy.Name)
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual()
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should update the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("can delete the CRP", func() {
		// Delete the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
		}
		Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
	})

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

var _ = Describe("validating CRP when deleting resources in a matching namespace", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
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
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: pointer.Int(5),
					},
				},
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s", crpName))
		cleanupCRP(crpName)

		By("deleting created work resources")
		cleanupWorkResources()
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual()
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("deleting config map on the hub", func() {
		configMap := appConfigMap()
		Expect(hubClient.Delete(ctx, &configMap)).To(Succeed(), "Failed to delete config map %s", configMap.Name)
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := func() error {
			wantSelectedResources := []placementv1beta1.ResourceIdentifier{
				{
					Kind:    "Namespace",
					Name:    fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
					Version: "v1",
				},
			}
			return validateCRPStatus(types.NamespacedName{Name: crpName}, wantSelectedResources)
		}
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should delete config map on member clusters", func() {
		appConfigMapName := types.NamespacedName{
			Namespace: fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
			Name:      fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess()),
		}

		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]
			workResourcesPlacedActual := func() error {
				if err := memberCluster.KubeClient.Get(ctx, appConfigMapName, &corev1.ConfigMap{}); !errors.IsNotFound(err) {
					return fmt.Errorf("app configMap still exists or an unexpected error occurred: %w", err)
				}
				return nil
			}
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete config map %s on member cluster %s", appConfigMapName, memberCluster.ClusterName)
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

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})
