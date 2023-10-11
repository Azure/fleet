/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
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
		Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

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

// TODO should be blocked by the validation webhook
var _ = Describe("validating CRP when resource selector is not valid", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
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
						Kind:    "InvalidNamespace",
						Version: "v1",
						Name:    "invalid",
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
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := func() error {
			crp := &placementv1beta1.ClusterResourcePlacement{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
				return err
			}

			wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: crp.Generation,
					},
				},
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
	})

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

// TODO should be blocked by the validation webhook
var _ = Describe("validating CRP when selecting a reserved resource", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
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
						Name:    fleetSystemNS,
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
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := func() error {
			crp := &placementv1beta1.ClusterResourcePlacement{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
				return err
			}

			wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: crp.Generation,
					},
				},
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
	})

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

var _ = Describe("validating CRP when failed to apply resources", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources on hub cluster")
		createWorkResources()

		By("creating work namespace on member cluster")
		ns := workNamespace()

		Expect(memberCluster1.KubeClient.Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace %s", ns.Name)

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

		By("deleting created work resources on hub cluster")
		cleanupWorkResources()

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
				Conditions: []metav1.Condition{
					{
						Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
						Status:             metav1.ConditionTrue,
						ObservedGeneration: crp.Generation,
					},
					{
						Type:               string(placementv1beta1.ClusterResourcePlacementSynchronizedConditionType),
						Status:             metav1.ConditionTrue,
						ObservedGeneration: crp.Generation,
					},
					{
						Type:               string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: crp.Generation,
					},
				},
				PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
					{
						ClusterName: memberCluster1Name,
						FailedResourcePlacements: []placementv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Kind:    "Namespace",
									Name:    workNamespaceName,
									Version: "v1",
								},
								Condition: metav1.Condition{
									Type:               placementv1beta1.WorkConditionTypeApplied,
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 0,
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourceScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: crp.Generation,
							},
							{
								Type:               string(placementv1beta1.ResourceWorkSynchronizedConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: crp.Generation,
							},
							{
								Type:               string(placementv1beta1.ResourcesAppliedConditionType),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: crp.Generation,
							},
						},
					},
					{
						ClusterName: memberCluster2Name,
						Conditions:  resourcePlacementRolloutCompletedConditions(crp.Generation),
					},
					{
						ClusterName: memberCluster3Name,
						Conditions:  resourcePlacementRolloutCompletedConditions(crp.Generation),
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

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})

	It("namespace should be kept on member cluster", func() {
		Consistently(func() error {
			workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
			ns := &corev1.Namespace{}
			return memberCluster1.KubeClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns)
		}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Namespace which is not owned by the CRP should not be deleted")
	})
})

var _ = Describe("validating CRP when placing cluster scope resource (other than namespace)", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	clusterRoleName := fmt.Sprintf("reader-%d", GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating cluster role")
		clusterRole := rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterRoleName,
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "watch"},
					Resources: []string{"namespaces"},
					APIGroups: []string{""},
				},
			},
		}
		Expect(hubClient.Create(ctx, &clusterRole)).Should(Succeed(), "Failed to create the clusterRole %s", clusterRoleName)

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
						Group:   "rbac.authorization.k8s.io",
						Kind:    "ClusterRole",
						Version: "v1",
						Name:    clusterRoleName,
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
			wantSelectedResources := []placementv1beta1.ResourceIdentifier{
				{
					Group:   "rbac.authorization.k8s.io",
					Kind:    "ClusterRole",
					Version: "v1",
					Name:    clusterRoleName,
				},
			}
			return validateCRPStatus(types.NamespacedName{Name: crpName}, wantSelectedResources)
		}
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", func() {
		name := types.NamespacedName{Name: clusterRoleName}
		wantClusterRole := &rbacv1.ClusterRole{}
		Expect(hubClient.Get(ctx, name, wantClusterRole)).Should(Succeed(), "Failed to get the clusterRole on hub cluster")

		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]

			resourcesPlacedActual := func() error {
				clusterRole := &rbacv1.ClusterRole{}
				if err := memberCluster.KubeClient.Get(ctx, name, clusterRole); err != nil {
					return err
				}
				if diff := cmp.Diff(clusterRole, wantClusterRole, ignoreObjectMetaAutoGeneratedFields, ignoreObjectMetaAnnotationField); diff != "" {
					return fmt.Errorf("clusterRole diff (-got, +want): %s", diff)
				}
				return nil
			}
			Eventually(resourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on member cluster %s", memberCluster.ClusterName)
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

	It("should remove placed resources from all member clusters", func() {
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]

			resourcesRemovedActual := func() error {
				if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: clusterRoleName}, &rbacv1.ClusterRole{}); !errors.IsNotFound(err) {
					return fmt.Errorf("clusterRole %s still exists or an unexpected error occurred: %w", clusterRoleName, err)
				}
				return nil
			}
			Eventually(resourcesRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove resources from member cluster %s", memberCluster.ClusterName)
		}
	})

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})
