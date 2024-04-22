/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/clusterresourceplacement"
	"go.goms.io/fleet/pkg/controllers/work"
	scheduler "go.goms.io/fleet/pkg/scheduler/framework"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/test/e2e/framework"
)

var (
	// we are propagating large secrets from hub to member clusters the timeout needs to be large.
	largeEventuallyDuration = time.Minute * 5
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
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
						UnavailablePeriodSeconds: ptr.To(5),
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
		crpStatusUpdatedActual := crpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "0")
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "1")
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
						UnavailablePeriodSeconds: ptr.To(5),
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
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

	It("should remove the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "1")
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

	It("should remove the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

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
						UnavailablePeriodSeconds: ptr.To(5),
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("updating the namespace on the hub", func() {
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		ns := &corev1.Namespace{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns)).Should(Succeed(), "Failed to get the namespace %s", workNamespaceName)
		ns.Labels["foo"] = "bar"
		Expect(hubClient.Update(ctx, ns)).Should(Succeed(), "Failed to update namespace %s", workNamespaceName)
	})

	It("should update the selected resources on member clusters", checkIfPlacedNamespaceResourceOnAllMemberClusters)

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "1")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("updating the configmap on the hub", func() {
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "2")
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

	It("should remove the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

var _ = Describe("validating CRP when adding resources in a matching namespace", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		ns := appNamespace()
		By(fmt.Sprintf("creating namespace %s", ns.Name))
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
						UnavailablePeriodSeconds: ptr.To(5),
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
		wantSelectedResourceIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Kind:    "Namespace",
				Name:    fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
				Version: "v1",
			},
		}
		crpStatusUpdatedActual := crpStatusUpdatedActual(wantSelectedResourceIdentifiers, allMemberClusterNames, nil, "0")
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "1")
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

	It("should remove the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

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
						UnavailablePeriodSeconds: ptr.To(5),
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("deleting config map on the hub", func() {
		configMap := appConfigMap()
		Expect(hubClient.Delete(ctx, &configMap)).To(Succeed(), "Failed to delete config map %s", configMap.Name)
	})

	It("should update CRP status as expected", func() {
		wantSelectedResourceIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Kind:    "Namespace",
				Name:    fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
				Version: "v1",
			},
		}
		crpStatusUpdatedActual := crpStatusUpdatedActual(wantSelectedResourceIdentifiers, allMemberClusterNames, nil, "1")
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

	It("should remove the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

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
						UnavailablePeriodSeconds: ptr.To(5),
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
						Reason:             clusterresourceplacement.InvalidResourceSelectorsReason,
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

		ns := appNamespace()
		By(fmt.Sprintf("creating namespace %s on member cluster", ns.Name))
		Expect(memberCluster1EastProdClient.Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace %s", ns.Name)

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
						Reason:             scheduler.FullyScheduledReason,
						ObservedGeneration: crp.Generation,
					},
					{
						Type:               string(placementv1beta1.ClusterResourcePlacementSynchronizedConditionType),
						Status:             metav1.ConditionTrue,
						Reason:             clusterresourceplacement.SynchronizeSucceededReason,
						ObservedGeneration: crp.Generation,
					},
					{
						Type:               string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
						Status:             metav1.ConditionFalse,
						Reason:             clusterresourceplacement.ApplyFailedReason,
						ObservedGeneration: crp.Generation,
					},
				},
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
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourceScheduledConditionType),
								Status:             metav1.ConditionTrue,
								Reason:             clusterresourceplacement.ResourceScheduleSucceededReason,
								ObservedGeneration: crp.Generation,
							},
							{
								Type:               string(placementv1beta1.ResourceWorkSynchronizedConditionType),
								Status:             metav1.ConditionTrue,
								Reason:             clusterresourceplacement.WorkSynchronizeSucceededReason,
								ObservedGeneration: crp.Generation,
							},
							{
								Type:               string(placementv1beta1.ResourcesAppliedConditionType),
								Status:             metav1.ConditionFalse,
								Reason:             clusterresourceplacement.ResourceApplyFailedReason,
								ObservedGeneration: crp.Generation,
							},
						},
					},
					{
						ClusterName: memberCluster2EastCanaryName,
						Conditions:  resourcePlacementRolloutCompletedConditions(crp.Generation),
					},
					{
						ClusterName: memberCluster3WestProdName,
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

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})

	It("namespace should be kept on member cluster", func() {
		Consistently(func() error {
			workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
			ns := &corev1.Namespace{}
			return memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns)
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
		wantSelectedResourceIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Group:   "rbac.authorization.k8s.io",
				Kind:    "ClusterRole",
				Version: "v1",
				Name:    clusterRoleName,
			},
		}
		crpStatusUpdatedActual := crpStatusUpdatedActual(wantSelectedResourceIdentifiers, allMemberClusterNames, nil, "0")
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

var _ = Describe("validating CRP revision history allowing single revision when updating resource selector", Ordered, func() {
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
						Name:    "invalid-namespace",
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(nil, allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("adding resource selectors", func() {
		updateFunc := func() error {
			crp := &placementv1beta1.ClusterResourcePlacement{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
				return err
			}

			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors, placementv1beta1.ClusterResourceSelector{
				Group:   "",
				Kind:    "Namespace",
				Version: "v1",
				Name:    fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
			})
			// may hit 409
			return hubClient.Update(ctx, crp)
		}
		Eventually(updateFunc, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the crp %s", crpName)
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "1")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have one policy snapshot revision and one resource snapshot revision", func() {
		Expect(validateCRPSnapshotRevisions(crpName, 1, 1)).Should(Succeed(), "Failed to validate the revision history")
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

	It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

var _ = Describe("validating CRP revision history allowing multiple revisions when updating resource selector", Ordered, func() {
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
						Name:    "invalid-namespace",
					},
				},
				Strategy: placementv1beta1.RolloutStrategy{
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(5),
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(nil, allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("adding resource selectors", func() {
		updateFunc := func() error {
			crp := &placementv1beta1.ClusterResourcePlacement{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
				return err
			}

			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors, placementv1beta1.ClusterResourceSelector{
				Group:   "",
				Kind:    "Namespace",
				Version: "v1",
				Name:    fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
			})
			// may hit 409
			return hubClient.Update(ctx, crp)
		}
		Eventually(updateFunc, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the crp %s", crpName)
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "1")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have one policy snapshot revision and two resource snapshot revisions", func() {
		Expect(validateCRPSnapshotRevisions(crpName, 1, 2)).Should(Succeed(), "Failed to validate the revision history")
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

	It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

// running spec in parallel with other specs causes timeouts.
var _ = Describe("validating CRP when selected resources cross the 1MB limit", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	BeforeAll(func() {
		By("creating resources for multiple resource snapshots")
		createResourcesForMultipleResourceSnapshots()

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickFixedPlacementType,
					ClusterNames:  []string{memberCluster1EastProdName, memberCluster2EastCanaryName},
				},
				ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
					{
						Group:   "",
						Kind:    "Namespace",
						Version: "v1",
						Name:    fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
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

		By("deleting resources created for multiple resource snapshots")
		cleanupWorkResources()
	})

	It("check if created cluster resource snapshots are as expected", func() {
		Eventually(multipleResourceSnapshotsCreatedActual("2", "2", "0"), largeEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to check created cluster resource snapshots", crpName)
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(resourceIdentifiersForMultipleResourcesSnapshots(), []string{memberCluster1EastProdName, memberCluster2EastCanaryName}, nil, "0")
		Eventually(crpStatusUpdatedActual, largeEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", func() {
		targetMemberClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
		checkIfPlacedWorkResourcesOnTargetMemberClusters(targetMemberClusters)
		checkIfPlacedLargeSecretResourcesOnTargetMemberClusters(targetMemberClusters)
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
		targetMemberClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
		checkIfRemovedWorkResourcesFromTargetMemberClusters(targetMemberClusters)
	})

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, largeEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

func createResourcesForMultipleResourceSnapshots() {
	createWorkResources()

	for i := 0; i < 3; i++ {
		var secret corev1.Secret
		Expect(utils.GetObjectFromManifest("../integration/manifests/resources/test-large-secret.yaml", &secret)).Should(Succeed(), "Failed to read large secret from file")
		secret.Namespace = appNamespace().Name
		secret.Name = fmt.Sprintf(appSecretNameTemplate, i)
		Expect(hubClient.Create(ctx, &secret)).To(Succeed(), "Failed to create secret %s/%s", secret.Name, secret.Namespace)
	}

	// sleep for 5 seconds to ensure secrets exist to prevent flake.
	<-time.After(5 * time.Second)
}

func multipleResourceSnapshotsCreatedActual(wantTotalNumberOfResourceSnapshots, wantNumberOfMasterIndexedResourceSnapshots, wantResourceIndex string) func() error {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	return func() error {
		var resourceSnapshotList placementv1beta1.ClusterResourceSnapshotList
		masterResourceSnapshotLabels := client.MatchingLabels{
			placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
			placementv1beta1.CRPTrackingLabel:      crpName,
		}
		if err := hubClient.List(ctx, &resourceSnapshotList, masterResourceSnapshotLabels); err != nil {
			return err
		}
		// there should be only one master resource snapshot.
		if len(resourceSnapshotList.Items) != 1 {
			return fmt.Errorf("number of master cluster resource snapshots has unexpected value: got %d, want %d", len(resourceSnapshotList.Items), 1)
		}
		masterResourceSnapshot := resourceSnapshotList.Items[0]
		// labels to list all existing cluster resource snapshots.
		resourceSnapshotListLabels := client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crpName}
		if err := hubClient.List(ctx, &resourceSnapshotList, resourceSnapshotListLabels); err != nil {
			return err
		}
		// ensure total number of cluster resource snapshots equals to wanted number of cluster resource snapshots
		if strconv.Itoa(len(resourceSnapshotList.Items)) != wantTotalNumberOfResourceSnapshots {
			return fmt.Errorf("total number of cluster resource snapshots has unexpected value: got %s, want %s", strconv.Itoa(len(resourceSnapshotList.Items)), wantTotalNumberOfResourceSnapshots)
		}
		numberOfResourceSnapshots := masterResourceSnapshot.Annotations[placementv1beta1.NumberOfResourceSnapshotsAnnotation]
		if numberOfResourceSnapshots != wantNumberOfMasterIndexedResourceSnapshots {
			return fmt.Errorf("NumberOfResourceSnapshotsAnnotation in master cluster resource snapshot has unexpected value:  got %s, want %s", numberOfResourceSnapshots, wantNumberOfMasterIndexedResourceSnapshots)
		}
		masterResourceIndex := masterResourceSnapshot.Labels[placementv1beta1.ResourceIndexLabel]
		if masterResourceIndex != wantResourceIndex {
			return fmt.Errorf("resource index for master cluster resource snapshot %s has unexpected value: got %s, want %s", masterResourceSnapshot.Name, masterResourceIndex, wantResourceIndex)
		}
		// labels to list all cluster resource snapshots with master resource index.
		resourceSnapshotListLabels = client.MatchingLabels{
			placementv1beta1.ResourceIndexLabel: masterResourceIndex,
			placementv1beta1.CRPTrackingLabel:   crpName,
		}
		if err := hubClient.List(ctx, &resourceSnapshotList, resourceSnapshotListLabels); err != nil {
			return err
		}
		if strconv.Itoa(len(resourceSnapshotList.Items)) != wantNumberOfMasterIndexedResourceSnapshots {
			return fmt.Errorf("number of cluster resource snapshots with master resource index has unexpected value: got %s, want %s", strconv.Itoa(len(resourceSnapshotList.Items)), wantNumberOfMasterIndexedResourceSnapshots)
		}
		return nil
	}
}

func resourceIdentifiersForMultipleResourcesSnapshots() []placementv1beta1.ResourceIdentifier {
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
		Kind:    "Namespace",
		Name:    workNamespaceName,
		Version: "v1",
	})
	placementResourceIdentifiers = append(placementResourceIdentifiers, placementv1beta1.ResourceIdentifier{
		Kind:      "ConfigMap",
		Name:      fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess()),
		Version:   "v1",
		Namespace: workNamespaceName,
	})

	return placementResourceIdentifiers
}

func checkIfPlacedWorkResourcesOnTargetMemberClusters(targetMemberClusters []*framework.Cluster) {
	for idx := range targetMemberClusters {
		memberCluster := targetMemberClusters[idx]

		workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
		Eventually(workResourcesPlacedActual, largeEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
	}
}

func checkIfPlacedLargeSecretResourcesOnTargetMemberClusters(targetMemberClusters []*framework.Cluster) {
	for idx := range targetMemberClusters {
		memberCluster := targetMemberClusters[idx]

		secretsPlacedActual := secretsPlacedOnClusterActual(memberCluster)
		Eventually(secretsPlacedActual, largeEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place large secrets on member cluster %s", memberCluster.ClusterName)
	}
}

func checkIfRemovedWorkResourcesFromTargetMemberClusters(targetMemberClusters []*framework.Cluster) {
	for idx := range targetMemberClusters {
		memberCluster := targetMemberClusters[idx]

		workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster)
		Eventually(workResourcesRemovedActual, largeEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from member cluster %s", memberCluster.ClusterName)
	}
}

func secretsPlacedOnClusterActual(cluster *framework.Cluster) func() error {
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	return func() error {
		for i := 0; i < 3; i++ {
			if err := validateSecretOnCluster(cluster, types.NamespacedName{Name: fmt.Sprintf(appSecretNameTemplate, i), Namespace: workNamespaceName}); err != nil {
				return err
			}
		}
		return nil
	}
}

func validateSecretOnCluster(cluster *framework.Cluster, name types.NamespacedName) error {
	secret := &corev1.Secret{}
	if err := cluster.KubeClient.Get(ctx, name, secret); err != nil {
		return err
	}

	// Use the object created in the hub cluster as reference.
	wantSecret := &corev1.Secret{}
	if err := hubClient.Get(ctx, name, wantSecret); err != nil {
		return err
	}
	if diff := cmp.Diff(
		secret, wantSecret,
		ignoreObjectMetaAutoGeneratedFields,
		ignoreObjectMetaAnnotationField,
	); diff != "" {
		return fmt.Errorf("app secret %s diff (-got, +want): %s", name.Name, diff)
	}

	return nil
}
