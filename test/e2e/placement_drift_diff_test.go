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
	"go.goms.io/fleet/test/e2e/framework"
)

var (
	unmanagedLabelKey   = "foo"
	unmanagedLabelVal   = "bar"
	managedDataFieldKey = "data"
	managedDataFieldVal = "custom"
)

var _ = Describe("take over existing resources", func() {
	Context("always take over", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		var existingNS *corev1.Namespace

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			createWorkResources()

			// Create the resources on one of the member clusters.
			ns := appNamespace()
			// Add a label (unmanaged field) to the namespace.
			ns.Labels = map[string]string{
				unmanagedLabelKey: unmanagedLabelVal,
			}
			existingNS = ns.DeepCopy()
			Expect(memberCluster1EastProdClient.Create(ctx, &ns)).To(Succeed())

			cm := appConfigMap()
			// Update the cinfigMap data (unmanaged field).
			cm.Data = map[string]string{
				managedDataFieldKey: managedDataFieldVal,
			}
			Expect(memberCluster1EastProdClient.Create(ctx, &cm)).To(Succeed())

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
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should take over the existing resources on clusters", func() {
			expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, crpName)

			ns := &corev1.Namespace{}
			nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: nsName}, ns)).To(Succeed())

			wantNS := existingNS.DeepCopy()
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

		It("should place the resources on the rest of the member clusters", func() {
			targetClusters := []*framework.Cluster{
				memberCluster2EastCanary,
				memberCluster3WestProd,
			}
			for idx := range targetClusters {
				memberCluster := targetClusters[idx]

				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
		})
	})

	Context("take over if no diff, partial comparison", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		var existingNS *corev1.Namespace

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			createWorkResources()

			// Create the resources on one of the member clusters.
			ns := appNamespace()
			// Add a label (unmanaged field) to the namespace.
			ns.Labels = map[string]string{
				unmanagedLabelKey: unmanagedLabelVal,
			}
			existingNS = ns.DeepCopy()
			Expect(memberCluster1EastProdClient.Create(ctx, &ns)).To(Succeed())

			cm := appConfigMap()
			// Update the cinfigMap data (unmanaged field).
			cm.Data = map[string]string{
				managedDataFieldKey: managedDataFieldVal,
			}
			Expect(memberCluster1EastProdClient.Create(ctx, &cm)).To(Succeed())

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
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should update CRP status as expected", func() {
			buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.ClusterResourcePlacementStatus {
				return &placementv1beta1.ClusterResourcePlacementStatus{
					Conditions:        crpAppliedFailedConditions(crpGeneration),
					SelectedResources: workResourceIdentifiers(),
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{
							ClusterName:      memberCluster1EastProdName,
							Conditions:       resourcePlacementApplyFailedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{},
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{},
						},
						{
							ClusterName: memberCluster2EastCanaryName,
							Conditions:  resourcePlacementRolloutCompletedConditions(crpGeneration, true, false),
						},
						{
							ClusterName: memberCluster3WestProdName,
							Conditions:  resourcePlacementRolloutCompletedConditions(crpGeneration, true, false),
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

				if diff := cmp.Diff(crp.Status, *wantCRPStatus, crpStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should take over existing resources w/n diff on clusters", func() {
			expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, crpName)

			ns := &corev1.Namespace{}
			nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: nsName}, ns)).To(Succeed())

			wantNS := existingNS.DeepCopy()
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
		})

		It("should not take over existing resources w/ diff on clusters", func() {
			cm := &corev1.ConfigMap{}
			nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
			cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm)).To(Succeed())

			// The difference has been overwritten.
			wantCM := appConfigMap()
			wantCM.OwnerReferences = []metav1.OwnerReference{}

			// No need to use an Eventually block as this spec runs after the CRP status has been verified.
			diff := cmp.Diff(
				cm, &wantCM,
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

				workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
		})
	})

	Context("take over if no diff, full comparison", Ordered, func() {})
})

var _ = Describe("detect drifts on placed resources", func() {
	Context("always apply, full comparison", Ordered, func() {})

	Context("always apply, partial comparison", Ordered, func() {})

	Context("apply if no drift, full comparison", Ordered, func() {})

	Context("apply if no drift, partial comparison", Ordered, func() {})

	Context("manual drift resolution", Ordered, func() {})

	Context("resource template refresh", Ordered, func() {})
})

var _ = Describe("report diff mode", func() {
	Context("do not touch anything", Ordered, func() {})

	Context("partial comparison", Ordered, func() {})

	Context("full comparison", Ordered, func() {})
})
