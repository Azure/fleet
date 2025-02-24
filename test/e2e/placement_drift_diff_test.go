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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/workapplier"
	"go.goms.io/fleet/test/e2e/framework"
)

var (
	unmanagedLabelKey    = "foo"
	unmanagedLabelVal1   = "bar"
	managedDataFieldKey  = "data"
	managedDataFieldVal1 = "custom-1"
	managedDataFieldVal2 = "custom-2"
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
				unmanagedLabelKey: unmanagedLabelVal1,
			}
			existingNS = ns.DeepCopy()
			Expect(memberCluster1EastProdClient.Create(ctx, &ns)).To(Succeed())

			cm := appConfigMap()
			// Update the cinfigMap data (unmanaged field).
			cm.Data = map[string]string{
				managedDataFieldKey: managedDataFieldVal1,
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
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
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
				unmanagedLabelKey:      unmanagedLabelVal1,
				workNamespaceLabelName: fmt.Sprintf("%d", GinkgoParallelProcess()),
			}
			existingNS = ns.DeepCopy()
			Expect(memberCluster1EastProdClient.Create(ctx, &ns)).To(Succeed())

			cm := appConfigMap()
			// Update the cinfigMap data (unmanaged field).
			cm.Data = map[string]string{
				managedDataFieldKey: managedDataFieldVal1,
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
										ObservedGeneration: 0,
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
											ValueInMember: managedDataFieldVal1,
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
				managedDataFieldKey: managedDataFieldVal1,
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
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
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
				unmanagedLabelKey:      unmanagedLabelVal1,
				workNamespaceLabelName: fmt.Sprintf("%d", GinkgoParallelProcess()),
			}
			existingNS = ns.DeepCopy()
			Expect(memberCluster1EastProdClient.Create(ctx, &ns)).To(Succeed())

			cm := appConfigMap()
			// Update the cinfigMap data (unmanaged field).
			cm.Data = map[string]string{
				managedDataFieldKey: managedDataFieldVal1,
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
										ObservedGeneration: 0,
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
										ObservedGeneration: 0,
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
				managedDataFieldKey: managedDataFieldVal1,
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

			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
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
			ns.Labels[unmanagedLabelKey] = unmanagedLabelVal1
			Expect(memberCluster1EastProdClient.Update(ctx, ns)).To(Succeed(), "Failed to update namespace")

			cm := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: nsName}, cm)).To(Succeed(), "Failed to get configMap")

			if cm.Data == nil {
				cm.Data = make(map[string]string)
			}
			cm.Data[managedDataFieldKey] = managedDataFieldVal1
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
											ValueInMember: unmanagedLabelVal1,
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
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
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
			ns.Labels[unmanagedLabelKey] = unmanagedLabelVal1
			Expect(memberCluster1EastProdClient.Update(ctx, ns)).To(Succeed(), "Failed to update namespace")

			cm := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: nsName}, cm)).To(Succeed(), "Failed to get configMap")

			if cm.Data == nil {
				cm.Data = make(map[string]string)
			}
			cm.Data[managedDataFieldKey] = managedDataFieldVal1
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
										ObservedGeneration: 0,
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
											ValueInMember: managedDataFieldVal1,
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
			wantNS.Labels[unmanagedLabelKey] = unmanagedLabelVal1
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
			wantCM.Data[managedDataFieldKey] = managedDataFieldVal1
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
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("apply if no drift, full comparison", Ordered, func() {
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
			ns.Labels[unmanagedLabelKey] = unmanagedLabelVal1
			Expect(memberCluster1EastProdClient.Update(ctx, ns)).To(Succeed(), "Failed to update namespace")

			cm := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: nsName}, cm)).To(Succeed(), "Failed to get configMap")

			if cm.Data == nil {
				cm.Data = make(map[string]string)
			}
			cm.Data[managedDataFieldKey] = managedDataFieldVal1
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
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.ResourcesAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 0,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDrifts),
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
										ObservedGeneration: 0,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDrifts),
									},
								},
							},
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
			wantNS.Labels[unmanagedLabelKey] = unmanagedLabelVal1
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
			wantCM.Data[managedDataFieldKey] = managedDataFieldVal1
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
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
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
				unmanagedLabelKey:      unmanagedLabelVal1,
				workNamespaceLabelName: fmt.Sprintf("%d", GinkgoParallelProcess()),
			}
			Expect(memberCluster1EastProdClient.Create(ctx, &ns)).To(Succeed())

			cm := appConfigMap()
			// Update the configMap data (managed field).
			cm.Data = map[string]string{
				managedDataFieldKey: managedDataFieldVal1,
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
					Conditions:        crpDiffReportedConditions(crpGeneration, false),
					SelectedResources: workResourceIdentifiers(),
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{
							ClusterName:      memberCluster1EastProdName,
							Conditions:       resourcePlacementDiffReportedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{},
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
							ClusterName:      memberCluster2EastCanaryName,
							Conditions:       resourcePlacementDiffReportedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{},
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
							ClusterName:      memberCluster3WestProdName,
							Conditions:       resourcePlacementDiffReportedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{},
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
			wantNS.Labels[unmanagedLabelKey] = unmanagedLabelVal1
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
			wantCM.Data[managedDataFieldKey] = managedDataFieldVal1

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
						return fmt.Errorf("the namespace is placed or an unexpected error occurred: %w", err)
					}

					return nil
				}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Resources are placed unexpectedly")
			}
		})

		It("can edit out the diffed fields", func() {
			ns := &corev1.Namespace{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: nsName}, ns)).To(Succeed())

			delete(ns.Labels, unmanagedLabelKey)
			Expect(memberCluster1EastProdClient.Update(ctx, ns)).To(Succeed())

			cm := &corev1.ConfigMap{}
			Expect(memberCluster1EastProdClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, cm)).To(Succeed())
			cm.Data = map[string]string{
				managedDataFieldKey: "test",
			}
			Expect(memberCluster1EastProdClient.Update(ctx, cm)).To(Succeed())
		})

		It("should update CRP status as expected", func() {
			buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.ClusterResourcePlacementStatus {
				return &placementv1beta1.ClusterResourcePlacementStatus{
					Conditions:        crpDiffReportedConditions(crpGeneration, false),
					SelectedResources: workResourceIdentifiers(),
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{
							ClusterName:      memberCluster1EastProdName,
							Conditions:       resourcePlacementDiffReportedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{},
							DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{},
						},
						{
							ClusterName:      memberCluster2EastCanaryName,
							Conditions:       resourcePlacementDiffReportedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{},
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
							ClusterName:      memberCluster3WestProdName,
							Conditions:       resourcePlacementDiffReportedConditions(crpGeneration),
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{},
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

		AfterAll(func() {
			// The pre-existing namespace has not been taken over and must be deleted manually.
			cleanWorkResourcesOnCluster(memberCluster1EastProd)

			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})
})

var _ = FDescribe("mixed diff and drift reportings", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
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
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels[managedDataFieldKey] = managedDataFieldVal1
		ns1 := ns.DeepCopy()
		ns2 := ns.DeepCopy()
		Expect(hubClient.Create(ctx, &ns)).To(Succeed())

		deploy := appDeployment()
		deploy1 := deploy.DeepCopy()
		Expect(hubClient.Create(ctx, &deploy)).To(Succeed())

		// Add pre-existing resources.
		Expect(memberCluster1EastProdClient.Create(ctx, ns1)).To(Succeed())
		deploy1.Spec.Replicas = ptr.To(int32(2))
		Expect(memberCluster1EastProdClient.Create(ctx, deploy1)).To(Succeed())

		ns2.Labels[managedDataFieldKey] = managedDataFieldVal2
		Expect(memberCluster3WestProdClient.Create(ctx, ns2)).To(Succeed())

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(),
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
						MaxUnavailable:           ptr.To(intstr.FromString("100%")),
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
		Expect(hubClient.Create(ctx, crp)).To(Succeed())
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

	It("should update CRP status as expected", func() {
		buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.ClusterResourcePlacementStatus {
			return &placementv1beta1.ClusterResourcePlacementStatus{
				Conditions: crpAppliedFailedConditions(crpGeneration),
				SelectedResources: []placementv1beta1.ResourceIdentifier{
					{
						Kind:    "Namespace",
						Name:    nsName,
						Version: "v1",
					},
					{
						Kind:      "Deployment",
						Name:      deployName,
						Version:   "v1",
						Namespace: nsName,
						Group:     "apps",
					},
				},
				PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
					{
						ClusterName: memberCluster1EastProdName,
						Conditions:  resourcePlacementApplyFailedConditions(crpGeneration),
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
						ClusterName: memberCluster2EastCanaryName,
						Conditions:  resourcePlacementApplyFailedConditions(crpGeneration),
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
									Type:               string(placementv1beta1.ResourcesAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 2,
									Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDrifts),
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
								TargetClusterObservedGeneration: 2,
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
									ObservedGeneration: 0,
									Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
								},
							},
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Version:   "v1",
									Group:     "apps",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								Condition: metav1.Condition{
									Type:               string(placementv1beta1.ResourcesAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 2,
									Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDrifts),
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
										Path:          fmt.Sprintf("/metadata/labels/%s", managedDataFieldKey),
										ValueInMember: managedDataFieldVal2,
										ValueInHub:    managedDataFieldVal1,
									},
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
			crp := &placementv1beta1.ClusterResourcePlacement{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
				return err
			}
			wantCRPStatus := buildWantCRPStatus(crp.Generation)

			if diff := cmp.Diff(crp.Status, *wantCRPStatus, crpStatusCmpOptions...); diff != "" {
				return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
			}

			// Populate the timestamps.
			for _, placementStatus := range crp.Status.PlacementStatuses {
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
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")

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

	It("should update CRP status as expected", func() {
		var refreshedLastDeployDiffObservedTimeOnCluster1 metav1.Time
		var refreshedFirstDeployDiffObservedTimeOnCluster1 metav1.Time
		var refreshedLastDeployDriftObservedTimeOnCluster2 metav1.Time
		var refreshedFirstDeployDriftObservedTimeOnCluster2 metav1.Time

		buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.ClusterResourcePlacementStatus {
			return &placementv1beta1.ClusterResourcePlacementStatus{
				Conditions: crpAppliedFailedConditions(crpGeneration),
				SelectedResources: []placementv1beta1.ResourceIdentifier{
					{
						Kind:    "Namespace",
						Name:    nsName,
						Version: "v1",
					},
					{
						Group:     "apps",
						Kind:      "Deployment",
						Name:      deployName,
						Version:   "v1",
						Namespace: nsName,
					},
				},
				PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
					{
						ClusterName: memberCluster1EastProdName,
						Conditions:  resourcePlacementApplyFailedConditions(crpGeneration),
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
									Type:               string(placementv1beta1.ResourcesAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 2,
									Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
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
						ClusterName: memberCluster2EastCanaryName,
						Conditions:  resourcePlacementApplyFailedConditions(crpGeneration),
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
									Type:               string(placementv1beta1.ResourcesAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 3,
									Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDrifts),
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
									ObservedGeneration: 0,
									Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
								},
							},
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Version:   "v1",
									Group:     "apps",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								Condition: metav1.Condition{
									Type:               string(placementv1beta1.ResourcesAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 2,
									Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDrifts),
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
										Path:          fmt.Sprintf("/metadata/labels/%s", managedDataFieldKey),
										ValueInMember: managedDataFieldVal2,
										ValueInHub:    managedDataFieldVal1,
									},
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
			crp := &placementv1beta1.ClusterResourcePlacement{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
				return err
			}
			wantCRPStatus := buildWantCRPStatus(crp.Generation)

			if diff := cmp.Diff(crp.Status, *wantCRPStatus, crpStatusCmpOptions...); diff != "" {
				return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
			}

			// Populate the timestamps.
			for _, placementStatus := range crp.Status.PlacementStatuses {
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
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")

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

	It("should update CRP status as expected", func() {
		buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.ClusterResourcePlacementStatus {
			return &placementv1beta1.ClusterResourcePlacementStatus{
				Conditions: crpAppliedFailedConditions(crpGeneration),
				SelectedResources: []placementv1beta1.ResourceIdentifier{
					{
						Kind:    "Namespace",
						Name:    nsName,
						Version: "v1",
					},
					{
						Group:     "apps",
						Kind:      "Deployment",
						Name:      deployName,
						Version:   "v1",
						Namespace: nsName,
					},
				},
				PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
					{
						ClusterName:      memberCluster1EastProdName,
						Conditions:       resourcePlacementRolloutCompletedConditions(crpGeneration, true, false),
						FailedPlacements: []placementv1beta1.FailedResourcePlacement{},
						DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{},
					},
					{
						ClusterName:       memberCluster2EastCanaryName,
						Conditions:        resourcePlacementRolloutCompletedConditions(crpGeneration, true, false),
						FailedPlacements:  []placementv1beta1.FailedResourcePlacement{},
						DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{},
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
									ObservedGeneration: 0,
									Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
								},
							},
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Version:   "v1",
									Group:     "apps",
									Kind:      "Deployment",
									Name:      deployName,
									Namespace: nsName,
								},
								Condition: metav1.Condition{
									Type:               string(placementv1beta1.ResourcesAppliedConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 2,
									Reason:             string(workapplier.ManifestProcessingApplyResultTypeFoundDrifts),
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
										Path:          fmt.Sprintf("/metadata/labels/%s", managedDataFieldKey),
										ValueInMember: managedDataFieldVal2,
										ValueInHub:    managedDataFieldVal1,
									},
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
			crp := &placementv1beta1.ClusterResourcePlacement{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
				return err
			}
			wantCRPStatus := buildWantCRPStatus(crp.Generation)

			if diff := cmp.Diff(crp.Status, *wantCRPStatus, crpStatusCmpOptions...); diff != "" {
				return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
			}
			return nil
			// Give the system a bit more breathing room for the Deployment (nginx) to become
			// available; on certain environments it might take longer to pull the image and have
			// the container up and running.
		}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("can update manifests", func() {
		Eventually(func() error {
			ns := &corev1.Namespace{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err != nil {
				return fmt.Errorf("failed to get namespace: %w", err)
			}

			if ns.Labels == nil {
				ns.Labels = make(map[string]string)
			}
			ns.Labels[managedDataFieldKey] = managedDataFieldVal2
			if err := hubClient.Update(ctx, ns); err != nil {
				return fmt.Errorf("failed to update namespace: %w", err)
			}

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

	It("should update CRP status as expected", func() {
		buildWantCRPStatus := func(crpGeneration int64) *placementv1beta1.ClusterResourcePlacementStatus {
			return &placementv1beta1.ClusterResourcePlacementStatus{
				Conditions: crpRolloutCompletedConditions(crpGeneration, false),
				SelectedResources: []placementv1beta1.ResourceIdentifier{
					{
						Kind:    "Namespace",
						Name:    nsName,
						Version: "v1",
					},
					{
						Group:     "apps",
						Kind:      "Deployment",
						Name:      deployName,
						Version:   "v1",
						Namespace: nsName,
					},
				},
				PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
					{
						ClusterName:      memberCluster1EastProdName,
						Conditions:       resourcePlacementRolloutCompletedConditions(crpGeneration, true, false),
						FailedPlacements: []placementv1beta1.FailedResourcePlacement{},
						DiffedPlacements: []placementv1beta1.DiffedResourcePlacement{},
					},
					{
						ClusterName:       memberCluster2EastCanaryName,
						Conditions:        resourcePlacementRolloutCompletedConditions(crpGeneration, true, false),
						FailedPlacements:  []placementv1beta1.FailedResourcePlacement{},
						DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{},
					},
					{
						ClusterName:       memberCluster3WestProdName,
						Conditions:        resourcePlacementRolloutCompletedConditions(crpGeneration, true, false),
						FailedPlacements:  []placementv1beta1.FailedResourcePlacement{},
						DiffedPlacements:  []placementv1beta1.DiffedResourcePlacement{},
						DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{},
					},
				},
			}
		}

		Eventually(func() error {
			crp := &placementv1beta1.ClusterResourcePlacement{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
				return err
			}
			wantCRPStatus := buildWantCRPStatus(crp.Generation)

			// There is no guarantee on how many resource snapshots Fleet will create based
			// on the previous round of changes; consequently the test spec here drops the field
			// for comparison.
			wantCRPStatus.ObservedResourceIndex = crp.Status.ObservedResourceIndex

			if diff := cmp.Diff(crp.Status, *wantCRPStatus, crpStatusCmpOptions...); diff != "" {
				return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
			}
			return nil
			// Give the system a bit more breathing room for the Deployment (nginx) to become
			// available; on certain environments it might take longer to pull the image and have
			// the container up and running.
		}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterAll(func() {
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

		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})
})
