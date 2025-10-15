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

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workapplier"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

var _ = Describe("take over existing resources using RP", Label("resourceplacement"), func() {
	var crpName, nsName, cm1Name, cm2Name string

	BeforeEach(OncePerOrdered, func() {
		nsName = fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cm1Name = fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
		cm2Name = fmt.Sprintf(appConfigMapNameTemplate+"-%d", GinkgoParallelProcess(), 2)

		By("Create the resources on the hub cluster")
		createWorkResources()
		cm2 := appConfigMap()
		cm2.Name = cm2Name
		Expect(hubClient.Create(ctx, &cm2)).To(Succeed())

		By("Create the resources on one of the member clusters")
		ns := appNamespace()
		Expect(memberCluster1EastProdClient.Create(ctx, &ns)).To(Succeed())

		cm1 := appConfigMap()
		// Update the configMap data (managed field).
		cm1.Data = map[string]string{
			managedDataFieldKey: managedDataFieldVal1,
		}
		Expect(memberCluster1EastProdClient.Create(ctx, &cm1)).To(Succeed())

		// Update the configMap labels (unmanaged field).
		cm2 = appConfigMap()
		cm2.Name = cm2Name
		cm2.Labels = map[string]string{
			unmanagedLabelKey: unmanagedLabelVal1,
		}
		Expect(memberCluster1EastProdClient.Create(ctx, &cm2)).To(Succeed())

		By("Create the CRP with Namespace-only selector")
		// Note that this CRP will take over pre-existing namespaces on member clusters.
		crpName = fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: namespaceOnlySelector(),
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

		By("Validate CRP status is as expected")
		crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		// No need to clean up pre-existing resources as the namespace has been taken over.
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("always take over", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the RP in the same namespace selecting namespaced resources
			// with apply strategy.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  nsName,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: multipleConfigMapsSelector(cm1Name, cm2Name),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
							WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(multipleAppConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should take over the existing resources on clusters", func() {
			expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, rpName, nsName)

			cm1 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm1Name, Namespace: nsName}, cm1)).To(Succeed())

			// The difference has been overwritten.
			wantCM := appConfigMap()
			wantCM.OwnerReferences = []metav1.OwnerReference{*expectedOwnerRef}

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff := cmp.Diff(
				cm1, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)

			cm2 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm2Name, Namespace: nsName}, cm2)).To(Succeed())

			wantCM = appConfigMap()
			wantCM.Name = cm2Name
			wantCM.OwnerReferences = []metav1.OwnerReference{*expectedOwnerRef}
			wantCM.Labels = map[string]string{
				unmanagedLabelKey: unmanagedLabelVal1,
			}

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff = cmp.Diff(
				cm2, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)
		})

		It("should place the resources on the rest of the member clusters", func() {
			targetClusters := []*framework.Cluster{
				memberCluster2EastCanary,
				memberCluster3WestProd,
			}
			for idx := range targetClusters {
				memberCluster := targetClusters[idx]

				// Validate namespace and configMap are placed.
				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)

				// Validate second configmap is placed.
				secondConfigMapPlacedActual := validateConfigMapOnCluster(memberCluster, types.NamespacedName{Name: cm2Name, Namespace: nsName})
				Eventually(secondConfigMapPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place second configMap on member cluster %s", memberCluster.ClusterName)
			}
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: nsName}, allMemberClusters)
		})
	})

	Context("take over if no diff, partial comparison", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the RP in the same namespace selecting namespaced resources
			// with apply strategy.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  nsName,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: multipleConfigMapsSelector(cm1Name, cm2Name),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
							WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeIfNoDiff,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        rpAppliedFailedConditions(rpGeneration),
					SelectedResources: multipleAppConfigMapIdentifiers(),
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
										Name:      cm1Name,
										Namespace: nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.PerClusterAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 0,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
									},
								},
							},
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm1Name,
										Namespace: nsName,
									},
									TargetClusterObservedGeneration: ptr.To(int64(0)),
									ObservedDiffs: []placementv1beta1.PatchDetail{
										{
											Path:          "/data/data",
											ValueInMember: managedDataFieldVal1,
											ValueInHub:    "test",
										},
									},
								},
							},
						},
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						},
						{
							ClusterName:           memberCluster3WestProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						},
					},
					ObservedResourceIndex: "0",
				}
			}

			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: nsName}, rp); err != nil {
					return err
				}
				wantRPStatus := buildWantRPStatus(rp.Generation)

				if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("RP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should not take over existing resources with diff on clusters", func() {
			cm1 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm1Name, Namespace: nsName}, cm1)).To(Succeed())

			// The difference has been overwritten.
			wantCM := appConfigMap()
			// Owner references should be unset (nil).
			wantCM.Data = map[string]string{
				managedDataFieldKey: managedDataFieldVal1,
			}

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff := cmp.Diff(
				cm1, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)
		})

		It("should take over existing resources with no diff on clusters", func() {
			expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, rpName, nsName)
			cm2 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm2Name, Namespace: nsName}, cm2)).To(Succeed())

			wantCM := appConfigMap()
			wantCM.Name = cm2Name
			// The owner reference should be set.
			wantCM.OwnerReferences = []metav1.OwnerReference{*expectedOwnerRef}
			wantCM.Labels = map[string]string{
				unmanagedLabelKey: unmanagedLabelVal1,
			}

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff := cmp.Diff(
				cm2, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)
		})

		It("should place the resources on the rest of the member clusters", func() {
			targetClusters := []*framework.Cluster{
				memberCluster2EastCanary,
				memberCluster3WestProd,
			}
			for idx := range targetClusters {
				memberCluster := targetClusters[idx]

				// Validate namespace and configMap are placed.
				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)

				// Validate second configmap is placed.
				secondConfigMapPlacedActual := validateConfigMapOnCluster(memberCluster, types.NamespacedName{Name: cm2Name, Namespace: nsName})
				Eventually(secondConfigMapPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place second configMap on member cluster %s", memberCluster.ClusterName)
			}
		})

		AfterAll(func() {
			cleanupConfigMapOnCluster(memberCluster1EastProd)
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: nsName}, allMemberClusters)
		})
	})

	Context("take over if no diff, full comparison", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the RP in the same namespace selecting namespaced resources
			// with apply strategy.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  nsName,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: multipleConfigMapsSelector(cm1Name, cm2Name),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
							WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeIfNoDiff,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        rpAppliedFailedConditions(rpGeneration),
					SelectedResources: multipleAppConfigMapIdentifiers(),
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
										Name:      cm1Name,
										Namespace: nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.PerClusterAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 0,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
									},
								},
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm2Name,
										Namespace: nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.PerClusterAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 0,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
									},
								},
							},
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm1Name,
										Namespace: nsName,
									},
									TargetClusterObservedGeneration: ptr.To(int64(0)),
									ObservedDiffs: []placementv1beta1.PatchDetail{
										{
											Path:          "/data/data",
											ValueInMember: managedDataFieldVal1,
											ValueInHub:    "test",
										},
									},
								},
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm2Name,
										Namespace: nsName,
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
							Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						},
						{
							ClusterName:           memberCluster3WestProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						},
					},
					ObservedResourceIndex: "0",
				}
			}

			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: nsName}, rp); err != nil {
					return err
				}
				wantRPStatus := buildWantRPStatus(rp.Generation)

				if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("RP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should not take over existing resources with diff on clusters", func() {
			cm1 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm1Name, Namespace: nsName}, cm1)).To(Succeed())

			wantCM := appConfigMap()
			// Owner references should be unset (nil).
			wantCM.Data = map[string]string{
				managedDataFieldKey: managedDataFieldVal1,
			}

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff := cmp.Diff(
				cm1, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)

			cm2 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm2Name, Namespace: nsName}, cm2)).To(Succeed())

			wantCM = appConfigMap()
			wantCM.Name = cm2Name
			// Owner references should be unset (nil).
			wantCM.Labels = map[string]string{
				unmanagedLabelKey: unmanagedLabelVal1,
			}

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff = cmp.Diff(
				cm2, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)
		})

		It("should place the resources on the rest of the member clusters", func() {
			targetClusters := []*framework.Cluster{
				memberCluster2EastCanary,
				memberCluster3WestProd,
			}
			for idx := range targetClusters {
				memberCluster := targetClusters[idx]

				// Validate namespace and configMap are placed.
				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)

				// Validate second configmap is placed.
				secondConfigMapPlacedActual := validateConfigMapOnCluster(memberCluster, types.NamespacedName{Name: cm2Name, Namespace: nsName})
				Eventually(secondConfigMapPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place second configMap on member cluster %s", memberCluster.ClusterName)
			}
		})

		AfterAll(func() {
			cleanupConfigMapOnCluster(memberCluster1EastProd)
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: nsName}, allMemberClusters)
		})
	})
})

var _ = Describe("detect drifts on placed resources using RP", Ordered, Label("resourceplacement"), func() {
	var crpName, nsName, cm1Name, cm2Name string

	BeforeEach(OncePerOrdered, func() {
		nsName = fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cm1Name = fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
		cm2Name = fmt.Sprintf(appConfigMapNameTemplate+"-%d", GinkgoParallelProcess(), 2)

		By("Create the resources on the hub cluster")
		createWorkResources()
		cm2 := appConfigMap()
		cm2.Name = cm2Name
		Expect(hubClient.Create(ctx, &cm2)).To(Succeed())

		By("Create the CRP with Namespace-only selector")
		crpName = fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: namespaceOnlySelector(),
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

		By("Validate CRP status is as expected")
		crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		// Clean up second configMap on the hub cluster.
		cleanupAnotherConfigMap(types.NamespacedName{Name: cm2Name, Namespace: nsName})

		// The CRP owns the namespace; do not verify if the namespace has been deleted until the CRP is gone.
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("always apply, full comparison", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the RP in the same namespace selecting namespaced resources
			// with apply strategy.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  nsName,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: multipleConfigMapsSelector(cm1Name, cm2Name),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
							WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(multipleAppConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("can edit the placed configmaps on the cluster", func() {
			cm1 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: cm1Name, Namespace: nsName}, cm1)).To(Succeed(), "Failed to get configMap")

			if cm1.Data == nil {
				cm1.Data = make(map[string]string)
			}
			cm1.Data[managedDataFieldKey] = managedDataFieldVal1
			Expect(memberCluster1EastProdClient.Update(ctx, cm1)).To(Succeed(), "Failed to update configMap")

			cm2 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: cm2Name, Namespace: nsName}, cm2)).To(Succeed(), "Failed to get configMap")
			if cm2.Labels == nil {
				cm2.Labels = make(map[string]string)
			}
			cm2.Labels[unmanagedLabelKey] = unmanagedLabelVal1
			Expect(memberCluster1EastProdClient.Update(ctx, cm2)).To(Succeed(), "Failed to update configMap")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(multipleAppConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should overwrite drifts on managed fields", func() {
			expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, rpName, nsName)
			Eventually(func() error {
				cm1 := &corev1.ConfigMap{}
				Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm1Name, Namespace: nsName}, cm1)).To(Succeed())

				// The difference has been overwritten.
				wantCM := appConfigMap()
				wantCM.OwnerReferences = []metav1.OwnerReference{
					*expectedOwnerRef,
				}

				// No need to use an Eventually block as this spec runs after the RP status has been verified.
				diff := cmp.Diff(
					cm1, &wantCM,
					ignoreObjectMetaAutoGenExceptOwnerRefFields,
					ignoreObjectMetaAnnotationField,
				)
				if diff != "" {
					return fmt.Errorf("ConfigMap diff (-got +want):\n%s", diff)
				}
				return nil
			}, eventuallyDuration*3, eventuallyInterval).Should(Succeed(), "Failed to update ConfigMap as expected")
		})

		It("should place the resources on the rest of the member clusters", func() {
			targetClusters := []*framework.Cluster{
				memberCluster2EastCanary,
				memberCluster3WestProd,
			}
			for idx := range targetClusters {
				memberCluster := targetClusters[idx]

				// Validate namespace and configMap are placed.
				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)

				// Validate second configmap is placed.
				secondConfigMapPlacedActual := validateConfigMapOnCluster(memberCluster, types.NamespacedName{Name: cm2Name, Namespace: nsName})
				Eventually(secondConfigMapPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place second configMap on member cluster %s", memberCluster.ClusterName)
			}
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: nsName}, allMemberClusters)
		})
	})

	Context("apply if no drift, partial comparison", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the RP in the same namespace selecting namespaced resources
			// with apply strategy.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  nsName,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: multipleConfigMapsSelector(cm1Name, cm2Name),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
							WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(multipleAppConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("can edit the placed resources on clusters", func() {
			cm1 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: cm1Name, Namespace: nsName}, cm1)).To(Succeed(), "Failed to get configMap")

			if cm1.Data == nil {
				cm1.Data = make(map[string]string)
			}
			cm1.Data[managedDataFieldKey] = managedDataFieldVal1
			Expect(memberCluster1EastProdClient.Update(ctx, cm1)).To(Succeed(), "Failed to update configMap")

			cm2 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: cm2Name, Namespace: nsName}, cm2)).To(Succeed(), "Failed to get configMap")

			if cm2.Labels == nil {
				cm2.Labels = make(map[string]string)
			}
			cm2.Labels[unmanagedLabelKey] = unmanagedLabelVal1
			Expect(memberCluster1EastProdClient.Update(ctx, cm2)).To(Succeed(), "Failed to update configMap")
		})

		It("should update RP status as expected", func() {
			buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        rpAppliedFailedConditions(rpGeneration),
					SelectedResources: multipleAppConfigMapIdentifiers(),
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
										Name:      cm1Name,
										Namespace: nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.PerClusterAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 0,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeFoundDrifts),
									},
								},
							},
							DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm1Name,
										Namespace: nsName,
									},
									TargetClusterObservedGeneration: 0,
									ObservedDrifts: []placementv1beta1.PatchDetail{
										{
											Path:          fmt.Sprintf("/data/%s", managedDataFieldKey),
											ValueInMember: managedDataFieldVal1,
											ValueInHub:    "test",
										},
									},
								},
							},
						},
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						},
						{
							ClusterName:           memberCluster3WestProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						},
					},
					ObservedResourceIndex: "0",
				}
			}

			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: nsName}, rp); err != nil {
					return err
				}
				wantRPStatus := buildWantRPStatus(rp.Generation)

				if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("RP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration*3, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should not overwrite drifts on fields", func() {
			expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, rpName, nsName)

			cm1 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm1Name, Namespace: nsName}, cm1)).To(Succeed())

			wantCM := appConfigMap()
			if wantCM.Data == nil {
				wantCM.Data = make(map[string]string)
			}
			wantCM.Data[managedDataFieldKey] = managedDataFieldVal1
			wantCM.OwnerReferences = []metav1.OwnerReference{
				*expectedOwnerRef,
			}

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff := cmp.Diff(
				cm1, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)

			cm2 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm2Name, Namespace: nsName}, cm2)).To(Succeed())

			wantCM = appConfigMap()
			wantCM.Name = cm2Name
			if wantCM.Labels == nil {
				wantCM.Labels = make(map[string]string)
			}
			wantCM.Labels[unmanagedLabelKey] = unmanagedLabelVal1
			wantCM.OwnerReferences = []metav1.OwnerReference{
				*expectedOwnerRef,
			}

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff = cmp.Diff(
				cm2, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)
		})

		It("should place the resources on the rest of the member clusters", func() {
			targetClusters := []*framework.Cluster{
				memberCluster2EastCanary,
				memberCluster3WestProd,
			}
			for idx := range targetClusters {
				memberCluster := targetClusters[idx]

				// Validate namespace and configMap are placed.
				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)

				// Validate second configmap is placed.
				secondConfigMapPlacedActual := validateConfigMapOnCluster(memberCluster, types.NamespacedName{Name: cm2Name, Namespace: nsName})
				Eventually(secondConfigMapPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place second configMap on member cluster %s", memberCluster.ClusterName)
			}
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: nsName}, allMemberClusters)
		})
	})

	Context("apply if no drift, full comparison", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the RP in the same namespace selecting namespaced resources
			// with apply strategy.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  nsName,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: multipleConfigMapsSelector(cm1Name, cm2Name),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
							WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(multipleAppConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("can edit the placed resources on clusters", func() {
			cm1 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: cm1Name, Namespace: nsName}, cm1)).To(Succeed(), "Failed to get configMap")

			if cm1.Data == nil {
				cm1.Data = make(map[string]string)
			}
			cm1.Data[managedDataFieldKey] = managedDataFieldVal1
			Expect(memberCluster1EastProdClient.Update(ctx, cm1)).To(Succeed(), "Failed to update configMap")

			cm2 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: cm2Name, Namespace: nsName}, cm2)).To(Succeed(), "Failed to get configMap")

			if cm2.Labels == nil {
				cm2.Labels = make(map[string]string)
			}
			cm2.Labels[unmanagedLabelKey] = unmanagedLabelVal1
			Expect(memberCluster1EastProdClient.Update(ctx, cm2)).To(Succeed(), "Failed to update configMap")
		})

		It("should update RP status as expected", func() {
			buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        rpAppliedFailedConditions(rpGeneration),
					SelectedResources: multipleAppConfigMapIdentifiers(),
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
										Name:      cm1Name,
										Namespace: nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.PerClusterAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 0,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeFoundDrifts),
									},
								},
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm2Name,
										Namespace: nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.PerClusterAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 0,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeFoundDrifts),
									},
								},
							},
							DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm1Name,
										Namespace: nsName,
									},
									TargetClusterObservedGeneration: 0,
									ObservedDrifts: []placementv1beta1.PatchDetail{
										{
											Path:          fmt.Sprintf("/data/%s", managedDataFieldKey),
											ValueInMember: managedDataFieldVal1,
											ValueInHub:    "test",
										},
									},
								},
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm2Name,
										Namespace: nsName,
									},
									TargetClusterObservedGeneration: 0,
									ObservedDrifts: []placementv1beta1.PatchDetail{
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
							Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						},
						{
							ClusterName:           memberCluster3WestProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						},
					},
					ObservedResourceIndex: "0",
				}
			}

			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: nsName}, rp); err != nil {
					return err
				}
				wantRPStatus := buildWantRPStatus(rp.Generation)

				if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("RP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration*3, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should not overwrite drifts on fields", func() {
			expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, rpName, nsName)

			cm1 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm1Name, Namespace: nsName}, cm1)).To(Succeed())

			// The difference has been overwritten.
			wantCM := appConfigMap()
			if wantCM.Data == nil {
				wantCM.Data = make(map[string]string)
			}
			wantCM.Data[managedDataFieldKey] = managedDataFieldVal1
			wantCM.OwnerReferences = []metav1.OwnerReference{
				*expectedOwnerRef,
			}

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff := cmp.Diff(
				cm1, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)

			cm2 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm2Name, Namespace: nsName}, cm2)).To(Succeed())

			// The difference has been overwritten.
			wantCM = appConfigMap()
			wantCM.Name = cm2Name
			if wantCM.Labels == nil {
				wantCM.Labels = make(map[string]string)
			}
			wantCM.Labels[unmanagedLabelKey] = unmanagedLabelVal1
			wantCM.OwnerReferences = []metav1.OwnerReference{
				*expectedOwnerRef,
			}

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff = cmp.Diff(
				cm2, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)
		})

		It("should place the resources on the rest of the member clusters", func() {
			targetClusters := []*framework.Cluster{
				memberCluster2EastCanary,
				memberCluster3WestProd,
			}
			for idx := range targetClusters {
				memberCluster := targetClusters[idx]

				// Validate namespace and configMap are placed.
				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)

				// Validate second configmap is placed.
				secondConfigMapPlacedActual := validateConfigMapOnCluster(memberCluster, types.NamespacedName{Name: cm2Name, Namespace: nsName})
				Eventually(secondConfigMapPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place second configMap on member cluster %s", memberCluster.ClusterName)
			}
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: nsName}, allMemberClusters)
		})
	})
})

var _ = Describe("report diff mode using RP", Label("resourceplacement"), func() {
	Context("do not touch anything", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cm1Name := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
		cm2Name := fmt.Sprintf(appConfigMapNameTemplate+"-%d", GinkgoParallelProcess(), 2)

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			createWorkResources()
			cm2 := appConfigMap()
			cm2.Name = cm2Name
			Expect(hubClient.Create(ctx, &cm2)).To(Succeed())

			// Create the resources on one of the member clusters.
			ns := appNamespace()
			Expect(memberCluster1EastProdClient.Create(ctx, &ns)).To(Succeed())

			cm1 := appConfigMap()
			// Update the configMap data (managed field).
			cm1.Data = map[string]string{
				managedDataFieldKey: managedDataFieldVal1,
			}
			Expect(memberCluster1EastProdClient.Create(ctx, &cm1)).To(Succeed())

			// Update the configMap labels (unmanaged field).
			cm2 = appConfigMap()
			cm2.Name = cm2Name
			cm2.Labels = map[string]string{
				unmanagedLabelKey: unmanagedLabelVal1,
			}
			Expect(memberCluster1EastProdClient.Create(ctx, &cm2)).To(Succeed())

			// Create the CRP with Namespace-only selector.
			//
			// Note that this CRP will take over existing namespaces.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: namespaceOnlySelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Create the RP in the same namespace selecting namespaced resources
			// with apply strategy.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  nsName,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: multipleConfigMapsSelector(cm1Name, cm2Name),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
							Type:             placementv1beta1.ApplyStrategyTypeReportDiff,
							WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeNever,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should update RP status as expected", func() {
			buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        rpDiffReportedConditions(rpGeneration, false),
					SelectedResources: multipleAppConfigMapIdentifiers(),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterDiffReportedConditions(rpGeneration),
							FailedPlacements:      []placementv1beta1.FailedResourcePlacement{},
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm1Name,
										Namespace: nsName,
									},
									TargetClusterObservedGeneration: ptr.To(int64(0)),
									ObservedDiffs: []placementv1beta1.PatchDetail{
										{
											Path:          "/data/data",
											ValueInMember: managedDataFieldVal1,
											ValueInHub:    "test",
										},
									},
								},
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm2Name,
										Namespace: nsName,
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
							FailedPlacements:      []placementv1beta1.FailedResourcePlacement{},
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm1Name,
										Namespace: nsName,
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
										Name:      cm2Name,
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
							ClusterName:           memberCluster3WestProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterDiffReportedConditions(rpGeneration),
							FailedPlacements:      []placementv1beta1.FailedResourcePlacement{},
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm1Name,
										Namespace: nsName,
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
										Name:      cm2Name,
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
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: nsName}, rp); err != nil {
					return err
				}
				wantRPStatus := buildWantRPStatus(rp.Generation)

				if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("RP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should not touch pre-existing resources", func() {
			cm1 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm1Name, Namespace: nsName}, cm1)).To(Succeed())

			// Managed field is not changed.
			wantCM := appConfigMap()
			if wantCM.Data == nil {
				wantCM.Data = make(map[string]string)
			}
			wantCM.Data[managedDataFieldKey] = managedDataFieldVal1

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff := cmp.Diff(
				cm1, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)

			cm2 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm2Name, Namespace: nsName}, cm2)).To(Succeed())

			// Unmanaged field is not changed.
			wantCM = appConfigMap()
			wantCM.Name = cm2Name
			if wantCM.Labels == nil {
				wantCM.Labels = make(map[string]string)
			}
			wantCM.Labels[unmanagedLabelKey] = unmanagedLabelVal1

			// No need to use an Eventually block as this spec runs after the RP status has been verified.
			diff = cmp.Diff(
				cm2, &wantCM,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "ConfigMap diff (-got +want):\n%s", diff)
		})

		It("should not place resources on clusters", func() {
			targetClusters := []*framework.Cluster{
				memberCluster2EastCanary,
				memberCluster3WestProd,
			}

			for idx := range targetClusters {
				cluster := targetClusters[idx]

				Consistently(func() error {
					cm1 := &corev1.ConfigMap{}
					if err := cluster.KubeClient.Get(ctx, client.ObjectKey{Name: cm1Name, Namespace: nsName}, cm1); !errors.IsNotFound(err) {
						return fmt.Errorf("the configmap %s is placed or an unexpected error occurred: %w", cm1Name, err)
					}

					cm2 := &corev1.ConfigMap{}
					if err := cluster.KubeClient.Get(ctx, client.ObjectKey{Name: cm2Name, Namespace: nsName}, cm2); !errors.IsNotFound(err) {
						return fmt.Errorf("the configmap %s is placed or an unexpected error occurred: %w", cm2Name, err)
					}

					return nil
				}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Resources are placed unexpectedly")
			}
		})

		It("can edit out the diffed fields", func() {
			cm1 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm1Name, Namespace: nsName}, cm1)).To(Succeed())
			cm1.Data = map[string]string{
				managedDataFieldKey: "test",
			}
			Expect(memberCluster1EastProdClient.Update(ctx, cm1)).To(Succeed())

			cm2 := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cm2Name, Namespace: nsName}, cm2)).To(Succeed())
			cm2.Labels = nil
			Expect(memberCluster1EastProdClient.Update(ctx, cm2)).To(Succeed())
		})

		It("should update RP status as expected", func() {
			buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
				return &placementv1beta1.PlacementStatus{
					Conditions:        rpDiffReportedConditions(rpGeneration, false),
					SelectedResources: multipleAppConfigMapIdentifiers(),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterDiffReportedConditions(rpGeneration),
							FailedPlacements:      []placementv1beta1.FailedResourcePlacement{},
							DiffedPlacements:      []placementv1beta1.DiffedResourcePlacement{},
						},
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterDiffReportedConditions(rpGeneration),
							FailedPlacements:      []placementv1beta1.FailedResourcePlacement{},
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm1Name,
										Namespace: nsName,
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
										Name:      cm2Name,
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
							ClusterName:           memberCluster3WestProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterDiffReportedConditions(rpGeneration),
							FailedPlacements:      []placementv1beta1.FailedResourcePlacement{},
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cm1Name,
										Namespace: nsName,
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
										Name:      cm2Name,
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
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: nsName}, rp); err != nil {
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
			// Clean up the created resources.

			// For the RP-related resources, ignore the first member cluster as it has pre-existing resources.
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: nsName}, []*framework.Cluster{
				memberCluster2EastCanary,
				memberCluster3WestProd,
			})
			// Note that the pre-existing namespace (not the configMaps) has been taken over by the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})
})

var _ = Describe("mixed diff and drift reportings using RP", Ordered, Label("resourceplacement"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	deployName := fmt.Sprintf(appDeploymentNameTemplate, GinkgoParallelProcess())

	startTime := metav1.Now()
	var lastDeployDiffObservedTimeOnCluster1 metav1.Time
	var firstDeployDiffObservedTimeOnCluster1 metav1.Time
	var lastDeployDriftObservedTimeOnCluster2 metav1.Time
	var firstDeployDriftObservedTimeOnCluster2 metav1.Time

	BeforeAll(func() {
		// Create the resources on the hub cluster.
		//
		// This test spec uses a Deployment instead of a ConfigMap to better
		// verify certain system behaviors (generation changes, etc.).
		ns := appNamespace()
		ns1 := ns.DeepCopy()
		Expect(hubClient.Create(ctx, &ns)).To(Succeed())

		deploy := appDeployment()
		if deploy.Labels == nil {
			deploy.Labels = make(map[string]string)
		}
		ns.Labels[managedDataFieldKey] = managedDataFieldVal1
		deploy1 := deploy.DeepCopy()
		Expect(hubClient.Create(ctx, &deploy)).To(Succeed())

		// Add pre-existing resources.
		Expect(memberCluster1EastProdClient.Create(ctx, ns1)).To(Succeed())
		deploy1.Spec.Replicas = ptr.To(int32(2))
		Expect(memberCluster1EastProdClient.Create(ctx, deploy1)).To(Succeed())

		// Create the CRP with Namespace-only selector
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: namespaceOnlySelector(),
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

		// Create the RP in the same namespace selecting namespaced resources
		// with apply strategy.
		rp := &placementv1beta1.ResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name:       rpName,
				Namespace:  nsName,
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Kind:    "Deployment",
						Name:    deployName,
						Version: "v1",
						Group:   "apps",
					},
				},
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
						WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
						Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
						WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeIfNoDiff,
						ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{
							ForceConflicts: true,
						},
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("can introduce drifts", func() {
		Eventually(func() error {
			deploy := &appsv1.Deployment{}
			if err := memberCluster2EastCanaryClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: nsName}, deploy); err != nil {
				return fmt.Errorf("failed to get deployment: %w", err)
			}

			deploy.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(int64(30))
			if err := memberCluster2EastCanaryClient.Update(ctx, deploy); err != nil {
				return fmt.Errorf("failed to update deployment: %w", err)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to introduce drifts")

		Eventually(func() error {
			deploy := &appsv1.Deployment{}
			if err := memberCluster3WestProdClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: nsName}, deploy); err != nil {
				return fmt.Errorf("failed to get deployment: %w", err)
			}

			deploy.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(int64(10))
			if err := memberCluster3WestProdClient.Update(ctx, deploy); err != nil {
				return fmt.Errorf("failed to update deployment: %w", err)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to introduce drifts")
	})

	It("should update RP status as expected", func() {
		buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
			return &placementv1beta1.PlacementStatus{
				Conditions: rpAppliedFailedConditions(rpGeneration),
				SelectedResources: []placementv1beta1.ResourceIdentifier{
					{
						Kind:      "Deployment",
						Name:      deployName,
						Version:   "v1",
						Namespace: nsName,
						Group:     "apps",
					},
				},
				PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           memberCluster1EastProdName,
						ObservedResourceIndex: "0",
						Conditions:            perClusterApplyFailedConditions(rpGeneration),
						FailedPlacements: []placementv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Version:   "v1",
									Group:     "apps",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								Condition: metav1.Condition{
									Type:               string(placementv1beta1.PerClusterAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 1,
									Reason:             string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
								},
							},
						},
						DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Group:     "apps",
									Version:   "v1",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								TargetClusterObservedGeneration: ptr.To(int64(1)),
								ObservedDiffs: []placementv1beta1.PatchDetail{
									{
										Path:          "/spec/replicas",
										ValueInMember: "2",
										ValueInHub:    "1",
									},
								},
							},
						},
					},
					{
						ClusterName:           memberCluster2EastCanaryName,
						ObservedResourceIndex: "0",
						Conditions:            perClusterApplyFailedConditions(rpGeneration),
						FailedPlacements: []placementv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Version:   "v1",
									Group:     "apps",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								Condition: metav1.Condition{
									Type:               string(placementv1beta1.PerClusterAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 2,
									Reason:             string(workapplier.ApplyOrReportDiffResTypeFoundDrifts),
								},
							},
						},
						DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Group:     "apps",
									Version:   "v1",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								TargetClusterObservedGeneration: int64(2),
								ObservedDrifts: []placementv1beta1.PatchDetail{
									{
										Path:          "/spec/template/spec/terminationGracePeriodSeconds",
										ValueInMember: "30",
										ValueInHub:    "60",
									},
								},
							},
						},
					},
					{
						ClusterName:           memberCluster3WestProdName,
						ObservedResourceIndex: "0",
						Conditions:            perClusterApplyFailedConditions(rpGeneration),
						FailedPlacements: []placementv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Version:   "v1",
									Group:     "apps",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								Condition: metav1.Condition{
									Type:               string(placementv1beta1.PerClusterAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 2,
									Reason:             string(workapplier.ApplyOrReportDiffResTypeFoundDrifts),
								},
							},
						},
						DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{},
						DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Group:     "apps",
									Version:   "v1",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								TargetClusterObservedGeneration: 2,
								ObservedDrifts: []placementv1beta1.PatchDetail{
									{
										Path:          "/spec/template/spec/terminationGracePeriodSeconds",
										ValueInMember: "10",
										ValueInHub:    "60",
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
			if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: nsName}, rp); err != nil {
				return err
			}
			wantRPStatus := buildWantRPStatus(rp.Generation)

			if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
				return fmt.Errorf("RP status diff (-got, +want): %s", diff)
			}

			// Populate the timestamps.
			for _, placementStatus := range rp.Status.PerClusterPlacementStatuses {
				switch placementStatus.ClusterName {
				case memberCluster1EastProdName:
					lastDeployDiffObservedTimeOnCluster1 = placementStatus.DiffedPlacements[0].ObservationTime
					firstDeployDiffObservedTimeOnCluster1 = placementStatus.DiffedPlacements[0].FirstDiffedObservedTime
				case memberCluster2EastCanaryName:
					lastDeployDriftObservedTimeOnCluster2 = placementStatus.DriftedPlacements[0].ObservationTime
					firstDeployDriftObservedTimeOnCluster2 = placementStatus.DriftedPlacements[0].FirstDriftedObservedTime
				}
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")

		// Verify the timestamps.
		Expect(lastDeployDiffObservedTimeOnCluster1.After(startTime.Time)).To(BeTrue(), "The diff observation time on cluster 1 is before the test spec start time")
		Expect(firstDeployDiffObservedTimeOnCluster1.After(startTime.Time)).To(BeTrue(), "The first diff observation time on cluster 1 is before the test spec start time")
		Expect(lastDeployDriftObservedTimeOnCluster2.After(startTime.Time)).To(BeTrue(), "The drift observation time on cluster 2 is before the test spec start time")
		Expect(firstDeployDriftObservedTimeOnCluster2.After(startTime.Time)).To(BeTrue(), "The first drift observation time on cluster 2 is before the test spec start time")
		// It is OK for the current diff/drift observation time to be after the first
		// diff/drift observation time as the current timestamps are refreshed periodically.
		Expect(lastDeployDiffObservedTimeOnCluster1.Before(&firstDeployDiffObservedTimeOnCluster1)).To(BeFalse(), "The first diff observation time on cluster 1 is after the last diff observation time on cluster 1")
		Expect(lastDeployDriftObservedTimeOnCluster2.Before(&firstDeployDriftObservedTimeOnCluster2)).To(BeFalse(), "The first drift observation time on cluster 2 is after the last drift observation time on cluster 2")

	})

	It("can introduce new diffs and drifts", func() {
		Eventually(func() error {
			deploy := &appsv1.Deployment{}
			if err := memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: nsName}, deploy); err != nil {
				return fmt.Errorf("failed to get deployment: %w", err)
			}

			deploy.Spec.Replicas = ptr.To(int32(3))
			if err := memberCluster1EastProdClient.Update(ctx, deploy); err != nil {
				return fmt.Errorf("failed to update deployment: %w", err)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to introduce new diffs and drifts")

		Eventually(func() error {
			deploy := &appsv1.Deployment{}
			if err := memberCluster2EastCanaryClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: nsName}, deploy); err != nil {
				return fmt.Errorf("failed to get deployment: %w", err)
			}

			deploy.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(int64(20))
			if err := memberCluster2EastCanaryClient.Update(ctx, deploy); err != nil {
				return fmt.Errorf("failed to update deployment: %w", err)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to introduce new diffs and drifts")
	})

	It("should update RP status as expected", func() {
		var refreshedLastDeployDiffObservedTimeOnCluster1 metav1.Time
		var refreshedFirstDeployDiffObservedTimeOnCluster1 metav1.Time
		var refreshedLastDeployDriftObservedTimeOnCluster2 metav1.Time
		var refreshedFirstDeployDriftObservedTimeOnCluster2 metav1.Time

		buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
			return &placementv1beta1.PlacementStatus{
				Conditions: rpAppliedFailedConditions(rpGeneration),
				SelectedResources: []placementv1beta1.ResourceIdentifier{
					{
						Group:     "apps",
						Kind:      "Deployment",
						Name:      deployName,
						Version:   "v1",
						Namespace: nsName,
					},
				},
				PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           memberCluster1EastProdName,
						ObservedResourceIndex: "0",
						Conditions:            perClusterApplyFailedConditions(rpGeneration),
						FailedPlacements: []placementv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Group:     "apps",
									Version:   "v1",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								Condition: metav1.Condition{
									Type:               string(placementv1beta1.PerClusterAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 2,
									Reason:             string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
								},
							},
						},
						DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Group:     "apps",
									Version:   "v1",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								TargetClusterObservedGeneration: ptr.To(int64(2)),
								ObservedDiffs: []placementv1beta1.PatchDetail{
									{
										Path:          "/spec/replicas",
										ValueInMember: "3",
										ValueInHub:    "1",
									},
								},
							},
						},
					},
					{
						ClusterName:           memberCluster2EastCanaryName,
						ObservedResourceIndex: "0",
						Conditions:            perClusterApplyFailedConditions(rpGeneration),
						FailedPlacements: []placementv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Version:   "v1",
									Group:     "apps",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								Condition: metav1.Condition{
									Type:               string(placementv1beta1.PerClusterAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 3,
									Reason:             string(workapplier.ApplyOrReportDiffResTypeFoundDrifts),
								},
							},
						},
						DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Group:     "apps",
									Version:   "v1",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								TargetClusterObservedGeneration: 3,
								ObservedDrifts: []placementv1beta1.PatchDetail{
									{
										Path:          "/spec/template/spec/terminationGracePeriodSeconds",
										ValueInMember: "20",
										ValueInHub:    "60",
									},
								},
							},
						},
					},
					{
						ClusterName:           memberCluster3WestProdName,
						ObservedResourceIndex: "0",
						Conditions:            perClusterApplyFailedConditions(rpGeneration),
						FailedPlacements: []placementv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Version:   "v1",
									Group:     "apps",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								Condition: metav1.Condition{
									Type:               string(placementv1beta1.PerClusterAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 2,
									Reason:             string(workapplier.ApplyOrReportDiffResTypeFoundDrifts),
								},
							},
						},
						DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{},
						DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Group:     "apps",
									Version:   "v1",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								TargetClusterObservedGeneration: 2,
								ObservedDrifts: []placementv1beta1.PatchDetail{
									{
										Path:          "/spec/template/spec/terminationGracePeriodSeconds",
										ValueInMember: "10",
										ValueInHub:    "60",
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
			if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: nsName}, rp); err != nil {
				return err
			}
			wantRPStatus := buildWantRPStatus(rp.Generation)

			if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
				return fmt.Errorf("RP status diff (-got, +want): %s", diff)
			}

			// Populate the timestamps.
			for _, placementStatus := range rp.Status.PerClusterPlacementStatuses {
				switch placementStatus.ClusterName {
				case memberCluster1EastProdName:
					refreshedLastDeployDiffObservedTimeOnCluster1 = placementStatus.DiffedPlacements[0].ObservationTime
					refreshedFirstDeployDiffObservedTimeOnCluster1 = placementStatus.DiffedPlacements[0].FirstDiffedObservedTime
				case memberCluster2EastCanaryName:
					refreshedLastDeployDriftObservedTimeOnCluster2 = placementStatus.DriftedPlacements[0].ObservationTime
					refreshedFirstDeployDriftObservedTimeOnCluster2 = placementStatus.DriftedPlacements[0].FirstDriftedObservedTime
				}
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")

		// Verify the timestamps.
		Expect(refreshedLastDeployDiffObservedTimeOnCluster1.After(lastDeployDiffObservedTimeOnCluster1.Time)).To(BeTrue(), "The diff observation time on cluster 1 is not refreshed")
		Expect(refreshedFirstDeployDiffObservedTimeOnCluster1.Equal(&firstDeployDiffObservedTimeOnCluster1)).To(BeTrue(), "The first diff observation time on cluster 1 is refreshed")
		Expect(refreshedLastDeployDriftObservedTimeOnCluster2.After(lastDeployDriftObservedTimeOnCluster2.Time)).To(BeTrue(), "The drift observation time on cluster 2 is not refreshed")
		Expect(refreshedFirstDeployDriftObservedTimeOnCluster2.Equal(&firstDeployDriftObservedTimeOnCluster2)).To(BeTrue(), "The first drift observation time on cluster 2 is refreshed")
	})

	It("can drop some diffs and drifts", func() {
		Eventually(func() error {
			deploy := &appsv1.Deployment{}
			if err := memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: nsName}, deploy); err != nil {
				return fmt.Errorf("failed to get deployment: %w", err)
			}

			deploy.Spec.Replicas = ptr.To(int32(1))
			if err := memberCluster1EastProdClient.Update(ctx, deploy); err != nil {
				return fmt.Errorf("failed to update deployment: %w", err)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to drop some diffs and drifts")

		Eventually(func() error {
			deploy := &appsv1.Deployment{}
			if err := memberCluster2EastCanaryClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: nsName}, deploy); err != nil {
				return fmt.Errorf("failed to get deployment: %w", err)
			}

			deploy.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(int64(60))
			if err := memberCluster2EastCanaryClient.Update(ctx, deploy); err != nil {
				return fmt.Errorf("failed to update deployment: %w", err)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to drop some diffs and drifts")
	})

	It("should update RP status as expected", func() {
		buildWantRPStatus := func(rpGeneration int64) *placementv1beta1.PlacementStatus {
			return &placementv1beta1.PlacementStatus{
				Conditions: rpAppliedFailedConditions(rpGeneration),
				SelectedResources: []placementv1beta1.ResourceIdentifier{
					{
						Group:     "apps",
						Kind:      "Deployment",
						Name:      deployName,
						Version:   "v1",
						Namespace: nsName,
					},
				},
				PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           memberCluster1EastProdName,
						ObservedResourceIndex: "0",
						Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						FailedPlacements:      []placementv1beta1.FailedResourcePlacement{},
						DiffedPlacements:      []placementv1beta1.DiffedResourcePlacement{},
					},
					{
						ClusterName:           memberCluster2EastCanaryName,
						ObservedResourceIndex: "0",
						Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						FailedPlacements:      []placementv1beta1.FailedResourcePlacement{},
						DriftedPlacements:     []placementv1beta1.DriftedResourcePlacement{},
					},
					{
						ClusterName:           memberCluster3WestProdName,
						ObservedResourceIndex: "0",
						Conditions:            perClusterApplyFailedConditions(rpGeneration),
						FailedPlacements: []placementv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Version:   "v1",
									Group:     "apps",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								Condition: metav1.Condition{
									Type:               string(placementv1beta1.PerClusterAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 2,
									Reason:             string(workapplier.ApplyOrReportDiffResTypeFoundDrifts),
								},
							},
						},
						DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{},
						DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Group:     "apps",
									Version:   "v1",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								TargetClusterObservedGeneration: 2,
								ObservedDrifts: []placementv1beta1.PatchDetail{
									{
										Path:          "/spec/template/spec/terminationGracePeriodSeconds",
										ValueInMember: "10",
										ValueInHub:    "60",
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
			if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: nsName}, rp); err != nil {
				return err
			}
			wantRPStatus := buildWantRPStatus(rp.Generation)

			if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
				return fmt.Errorf("RP status diff (-got, +want): %s", diff)
			}
			return nil
			// Give the system a bit more breathing room for the Deployment (nginx) to become
			// available; on certain environments it might take longer to pull the image and have
			// the container up and running.
		}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
	})

	It("can update manifests", func() {
		Eventually(func() error {
			deploy := &appsv1.Deployment{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: nsName}, deploy); err != nil {
				return fmt.Errorf("failed to get deployment: %w", err)
			}
			deploy.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(int64(10))
			if err := hubClient.Update(ctx, deploy); err != nil {
				return fmt.Errorf("failed to update deployment: %w", err)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update manifests")
	})

	It("should update RP status as expected", func() {
		buildWantRPStatus := func(rpGeneration int64, observedResourceIndex string) *placementv1beta1.PlacementStatus {
			return &placementv1beta1.PlacementStatus{
				ObservedResourceIndex: observedResourceIndex,
				Conditions:            rpRolloutCompletedConditions(rpGeneration, false),
				SelectedResources: []placementv1beta1.ResourceIdentifier{
					{
						Group:     "apps",
						Kind:      "Deployment",
						Name:      deployName,
						Version:   "v1",
						Namespace: nsName,
					},
				},
				PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           memberCluster1EastProdName,
						ObservedResourceIndex: observedResourceIndex,
						Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						FailedPlacements:      []placementv1beta1.FailedResourcePlacement{},
						DiffedPlacements:      []placementv1beta1.DiffedResourcePlacement{},
					},
					{
						ClusterName:           memberCluster2EastCanaryName,
						ObservedResourceIndex: observedResourceIndex,
						Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						FailedPlacements:      []placementv1beta1.FailedResourcePlacement{},
						DriftedPlacements:     []placementv1beta1.DriftedResourcePlacement{},
					},
					{
						ClusterName:           memberCluster3WestProdName,
						ObservedResourceIndex: observedResourceIndex,
						Conditions:            perClusterRolloutCompletedConditions(rpGeneration, true, false),
						FailedPlacements:      []placementv1beta1.FailedResourcePlacement{},
						DiffedPlacements:      []placementv1beta1.DiffedResourcePlacement{},
						DriftedPlacements:     []placementv1beta1.DriftedResourcePlacement{},
					},
				},
			}
		}

		Eventually(func() error {
			rp := &placementv1beta1.ResourcePlacement{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: nsName}, rp); err != nil {
				return err
			}

			// There is no guarantee on how many resource snapshots Fleet will create based
			// on the previous round of changes; consequently the test spec here drops the field
			// for comparison.
			wantRPStatus := buildWantRPStatus(rp.Generation, rp.Status.ObservedResourceIndex)

			if diff := cmp.Diff(rp.Status, *wantRPStatus, placementStatusCmpOptions...); diff != "" {
				return fmt.Errorf("RP status diff (-got, +want): %s", diff)
			}
			return nil
			// Give the system a bit more breathing room for the Deployment (nginx) to become
			// available; on certain environments it might take longer to pull the image and have
			// the container up and running.
		}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
	})

	AfterAll(func() {
		// Must delete RP first, otherwise resources might get re-created.
		rp := &placementv1beta1.ResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpName,
				Namespace: nsName,
			},
		}
		Expect(client.IgnoreNotFound(hubClient.Delete(ctx, rp))).To(Succeed(), "Failed to delete RP")

		// Must delete CRP first, otherwise resources might get re-created.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
		}
		Expect(client.IgnoreNotFound(hubClient.Delete(ctx, crp))).To(Succeed(), "Failed to delete CRP")

		// Attempt to clean up resources manually, even if they could have been taken over by
		// Fleet during the course of the test spec.
		ns := appNamespace()
		Expect(client.IgnoreNotFound(memberCluster1EastProdClient.Delete(ctx, &ns))).To(Succeed())
		Expect(client.IgnoreNotFound(memberCluster2EastCanaryClient.Delete(ctx, &ns))).To(Succeed())
		Expect(client.IgnoreNotFound(memberCluster3WestProdClient.Delete(ctx, &ns))).To(Succeed())

		ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: nsName}, allMemberClusters)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})
})
