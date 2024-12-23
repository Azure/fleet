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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/workapplier"
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
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		var existingNS *corev1.Namespace

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			createWorkResources()

			// Create the resources on one of the member clusters.
			ns := appNamespace()
			// Add a label (unmanaged field) to the namespace.
			ns.Labels = map[string]string{
				unmanagedLabelKey:      unmanagedLabelVal,
				workNamespaceLabelName: fmt.Sprintf("%d", GinkgoParallelProcess()),
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
							ClusterName: memberCluster1EastProdName,
							Conditions:  resourcePlacementApplyFailedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cmName,
										Namespace: nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.ResourcesAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 1,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
									},
								},
							},
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
											ValueInMember: "custom",
											ValueInHub:    "test",
										},
									},
								},
							},
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
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm)).To(Succeed())

			// The difference has been overwritten.
			wantCM := appConfigMap()
			// Owner references should be unset (nil).
			wantCM.Data = map[string]string{
				managedDataFieldKey: managedDataFieldVal,
			}

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

	Context("take over if no diff, full comparison", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		var existingNS *corev1.Namespace

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			createWorkResources()

			// Create the resources on one of the member clusters.
			ns := appNamespace()
			// Add a label (unmanaged field) to the namespace.
			ns.Labels = map[string]string{
				unmanagedLabelKey:      unmanagedLabelVal,
				workNamespaceLabelName: fmt.Sprintf("%d", GinkgoParallelProcess()),
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
							ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
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
										Type:               string(placementv1beta1.ResourcesAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 1,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
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
										Type:               string(placementv1beta1.ResourcesAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 1,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
									},
								},
							},
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
											ValueInMember: unmanagedLabelVal,
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
									TargetClusterObservedGeneration: ptr.To(int64(0)),
									ObservedDiffs: []placementv1beta1.PatchDetail{
										{
											Path:          "/data/data",
											ValueInMember: "custom",
											ValueInHub:    "test",
										},
									},
								},
							},
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

		It("should not take over existing resources w/ diff on clusters", func() {
			ns := &corev1.Namespace{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: nsName}, ns)).To(Succeed())

			wantNS := existingNS.DeepCopy()
			if wantNS.Labels == nil {
				wantNS.Labels = make(map[string]string)
			}
			wantNS.Labels["kubernetes.io/metadata.name"] = nsName
			wantNS.Labels[workNamespaceLabelName] = fmt.Sprintf("%d", GinkgoParallelProcess())

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
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm)).To(Succeed())

			// The difference has been overwritten.
			wantCM := appConfigMap()
			// Owner references should be unset (nil).
			wantCM.Data = map[string]string{
				managedDataFieldKey: managedDataFieldVal,
			}

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
			// The pre-existing namespace has not been taken over and must be deleted manually.
			cleanWorkResourcesOnCluster(memberCluster1EastProd)

			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
		})
	})
})

var _ = Describe("detect drifts on placed resources", func() {
	Context("always apply, full comparison", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			createWorkResources()

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
							ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
							WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
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

		It("can edit the placed resources on clusters", func() {
			ns := &corev1.Namespace{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: nsName}, ns)).To(Succeed(), "Failed to get namespace")

			if ns.Labels == nil {
				ns.Labels = make(map[string]string)
			}
			ns.Labels[unmanagedLabelKey] = unmanagedLabelVal
			Expect(memberCluster1EastProdClient.Update(ctx, ns)).To(Succeed(), "Failed to update namespace")

			cm := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: nsName}, cm)).To(Succeed(), "Failed to get configMap")

			if cm.Data == nil {
				cm.Data = make(map[string]string)
			}
			cm.Data[managedDataFieldKey] = managedDataFieldVal
			Expect(memberCluster1EastProdClient.Update(ctx, cm)).To(Succeed(), "Failed to update configMap")
		})

		It("should update CRP status as expected", func() {
			buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.ClusterResourcePlacementStatus {
				return &placementv1beta1.ClusterResourcePlacementStatus{
					Conditions:        crpRolloutCompletedConditions(crpGeneration, false),
					SelectedResources: workResourceIdentifiers(),
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{
							ClusterName:      memberCluster1EastProdName,
							Conditions:       resourcePlacementRolloutCompletedConditions(crpGeneration, true, false),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{},
							DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									TargetClusterObservedGeneration: 0,
									ObservedDrifts: []placementv1beta1.PatchDetail{
										{
											Path:          fmt.Sprintf("/metadata/labels/%s", unmanagedLabelKey),
											ValueInMember: unmanagedLabelVal,
										},
									},
								},
							},
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

		It("should overwrite drifts on managed fields", func() {
			expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, crpName)

			cm := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm)).To(Succeed())

			// The difference has been overwritten.
			wantCM := appConfigMap()
			wantCM.OwnerReferences = []metav1.OwnerReference{
				*expectedOwnerRef,
			}

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

	Context("apply if no drift, partial comparison", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			createWorkResources()

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
							WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
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

		It("can edit the placed resources on clusters", func() {
			ns := &corev1.Namespace{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: nsName}, ns)).To(Succeed(), "Failed to get namespace")

			if ns.Labels == nil {
				ns.Labels = make(map[string]string)
			}
			ns.Labels[unmanagedLabelKey] = unmanagedLabelVal
			Expect(memberCluster1EastProdClient.Update(ctx, ns)).To(Succeed(), "Failed to update namespace")

			cm := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: nsName}, cm)).To(Succeed(), "Failed to get configMap")

			if cm.Data == nil {
				cm.Data = make(map[string]string)
			}
			cm.Data[managedDataFieldKey] = managedDataFieldVal
			Expect(memberCluster1EastProdClient.Update(ctx, cm)).To(Succeed(), "Failed to update configMap")
		})

		It("should update CRP status as expected", func() {
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
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cmName,
										Namespace: nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.ResourcesAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 1,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDrifts),
									},
								},
							},
							DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version:   "v1",
										Kind:      "ConfigMap",
										Name:      cmName,
										Namespace: nsName,
									},
									TargetClusterObservedGeneration: 0,
									ObservedDrifts: []placementv1beta1.PatchDetail{
										{
											Path:          fmt.Sprintf("/data/%s", managedDataFieldKey),
											ValueInMember: managedDataFieldVal,
											ValueInHub:    "test",
										},
									},
								},
							},
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

		It("should not overwrite drifts on fields", func() {
			expectedOwnerRef := buildOwnerReference(memberCluster1EastProd, crpName)

			ns := &corev1.Namespace{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: nsName}, ns)).To(Succeed())

			wantNS := appNamespace()
			if wantNS.Labels == nil {
				wantNS.Labels = make(map[string]string)
			}
			wantNS.Labels[unmanagedLabelKey] = unmanagedLabelVal
			wantNS.Labels["kubernetes.io/metadata.name"] = nsName
			wantNS.Labels[workNamespaceLabelName] = fmt.Sprintf("%d", GinkgoParallelProcess())
			wantNS.OwnerReferences = []metav1.OwnerReference{
				*expectedOwnerRef,
			}

			// No need to use an Eventually block as this spec runs after the CRP status has been verified.
			diff := cmp.Diff(
				ns, &wantNS,
				ignoreNamespaceSpecField,
				ignoreNamespaceStatusField,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "Namespace diff (-got +want):\n%s", diff)

			cm := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm)).To(Succeed())

			// The difference has been overwritten.
			wantCM := appConfigMap()
			if wantCM.Data == nil {
				wantCM.Data = make(map[string]string)
			}
			wantCM.Data[managedDataFieldKey] = managedDataFieldVal
			wantCM.OwnerReferences = []metav1.OwnerReference{
				*expectedOwnerRef,
			}

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
})

var _ = Describe("report diff mode", func() {
	Context("do not touch anything", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources on the hub cluster.
			createWorkResources()

			// Create the resources on one of the member clusters.
			ns := appNamespace()
			// Add a label (unmanaged field) to the namespace.
			ns.Labels = map[string]string{
				unmanagedLabelKey:      unmanagedLabelVal,
				workNamespaceLabelName: fmt.Sprintf("%d", GinkgoParallelProcess()),
			}
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
							ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
							Type:             placementv1beta1.ApplyStrategyTypeReportDiff,
							WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeNever,
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
										Type:               string(placementv1beta1.ResourcesAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 1,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDiffs),
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
										Type:               string(placementv1beta1.ResourcesAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 1,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDiffs),
									},
								},
							},
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
											ValueInMember: unmanagedLabelVal,
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
									TargetClusterObservedGeneration: ptr.To(int64(0)),
									ObservedDiffs: []placementv1beta1.PatchDetail{
										{
											Path:          "/data/data",
											ValueInMember: "custom",
											ValueInHub:    "test",
										},
									},
								},
							},
						},
						{
							ClusterName: memberCluster2EastCanaryName,
							Conditions:  resourcePlacementApplyFailedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.ResourcesAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 1,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDiffs),
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
										Type:               string(placementv1beta1.ResourcesAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 1,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDiffs),
									},
								},
							},
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
						{
							ClusterName: memberCluster3WestProdName,
							Conditions:  resourcePlacementApplyFailedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.ResourcesAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 1,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDiffs),
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
										Type:               string(placementv1beta1.ResourcesAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 1,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDiffs),
									},
								},
							},
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

				if diff := cmp.Diff(crp.Status, *wantCRPStatus, crpStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should not touch pre-existing resources", func() {
			ns := &corev1.Namespace{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: nsName}, ns)).To(Succeed())

			wantNS := appNamespace()
			if wantNS.Labels == nil {
				wantNS.Labels = make(map[string]string)
			}
			wantNS.Labels[unmanagedLabelKey] = unmanagedLabelVal
			wantNS.Labels["kubernetes.io/metadata.name"] = nsName
			wantNS.Labels[workNamespaceLabelName] = fmt.Sprintf("%d", GinkgoParallelProcess())

			// No need to use an Eventually block as this spec runs after the CRP status has been verified.
			diff := cmp.Diff(
				ns, &wantNS,
				ignoreNamespaceSpecField,
				ignoreNamespaceStatusField,
				ignoreObjectMetaAutoGenExceptOwnerRefFields,
				ignoreObjectMetaAnnotationField,
			)
			Expect(diff).To(BeEmpty(), "Namespace diff (-got +want):\n%s", diff)

			cm := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm)).To(Succeed())

			// The difference has been overwritten.
			wantCM := appConfigMap()
			if wantCM.Data == nil {
				wantCM.Data = make(map[string]string)
			}
			wantCM.Data[managedDataFieldKey] = managedDataFieldVal

			// No need to use an Eventually block as this spec runs after the CRP status has been verified.
			diff = cmp.Diff(
				cm, &wantCM,
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
					ns := &corev1.Namespace{}
					if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); !errors.IsNotFound(err) {
						return fmt.Errorf("the namespace is placed or an unexpected error occurred: %v", err)
					}

					return nil
				}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Resources are placed unexpectedly")
			}
		})

		AfterAll(func() {
			// The pre-existing namespace has not been taken over and must be deleted manually.
			cleanWorkResourcesOnCluster(memberCluster1EastProd)

			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
		})
	})
})
