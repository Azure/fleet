/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"encoding/json"
	"fmt"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workapplier"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

var _ = Describe("handling errors and failures gracefully", func() {
	envelopeName := "wrapper"
	wrappedCMName1 := "app-1"
	wrappedCMName2 := "app-2"

	cmDataKey := "foo"
	cmDataVal1 := "bar"
	cmDataVal2 := "baz"

	// Many test specs below use envelopes for placement as it is a bit tricky to simulate
	// decoding errors with resources created directly in the hub cluster.
	//
	// TO-DO (chenyu1): reserve an API group exclusively on the hub cluster so that
	// envelopes do not need to be used for this test spec.

	Context("pre-processing failure in apply ops (decoding errors)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Use an envelope to create duplicate resource entries.
			ns := appNamespace()
			Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

			// Create an envelope resource to wrap the configMaps.
			resourceEnvelope := &placementv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      envelopeName,
					Namespace: ns.Name,
				},
				Data: map[string]runtime.RawExtension{},
			}

			// Create configMaps as wrapped resources.
			configMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: ns.Name,
					Name:      wrappedCMName1,
				},
				Data: map[string]string{
					cmDataKey: cmDataVal1,
				},
			}
			// Prepare a malformed config map.
			badConfigMap := configMap.DeepCopy()
			badConfigMap.TypeMeta = metav1.TypeMeta{
				APIVersion: "dummy/v10",
				Kind:       "Fake",
			}
			badCMBytes, err := json.Marshal(badConfigMap)
			Expect(err).To(BeNil(), "Failed to marshal configMap %s", badConfigMap.Name)
			resourceEnvelope.Data["cm1.yaml"] = runtime.RawExtension{Raw: badCMBytes}

			// Prepare a regular config map.
			wrappedCM2 := configMap.DeepCopy()
			wrappedCM2.Name = wrappedCMName2
			wrappedCM2.Data[cmDataKey] = cmDataVal2
			wrappedCM2Bytes, err := json.Marshal(wrappedCM2)
			Expect(err).To(BeNil(), "Failed to marshal configMap %s", wrappedCM2.Name)
			resourceEnvelope.Data["cm2.yaml"] = runtime.RawExtension{Raw: wrappedCM2Bytes}

			Expect(hubClient.Create(ctx, resourceEnvelope)).To(Succeed(), "Failed to create resource envelope %s", resourceEnvelope.Name)

			// Create a CRP.
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
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				wantStatus := placementv1beta1.PlacementStatus{
					Conditions: crpAppliedFailedConditions(crp.Generation),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Group:     "dummy",
										Version:   "v10",
										Kind:      "Fake",
										Namespace: workNamespaceName,
										Name:      wrappedCMName1,
										Envelope: &placementv1beta1.EnvelopeIdentifier{
											Name:      envelopeName,
											Namespace: workNamespaceName,
											Type:      placementv1beta1.ResourceEnvelopeType,
										},
									},
									Condition: metav1.Condition{
										Type:               placementv1beta1.WorkConditionTypeApplied,
										Status:             metav1.ConditionFalse,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeDecodingErred),
										ObservedGeneration: 0,
									},
								},
							},
							Conditions: perClusterApplyFailedConditions(crp.Generation),
						},
					},
					SelectedResources: []placementv1beta1.ResourceIdentifier{
						{
							Kind:    "Namespace",
							Name:    workNamespaceName,
							Version: "v1",
						},
						{
							Group:     placementv1beta1.GroupVersion.Group,
							Kind:      placementv1beta1.ResourceEnvelopeKind,
							Version:   placementv1beta1.GroupVersion.Version,
							Name:      envelopeName,
							Namespace: workNamespaceName,
						},
					},
					ObservedResourceIndex: "0",
				}
				if diff := cmp.Diff(crp.Status, wantStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place some manifests on member clusters", func() {
			Eventually(func() error {
				return validateWorkNamespaceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: workNamespaceName})
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Eventually(func() error {
				cm := &corev1.ConfigMap{}
				if err := memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: wrappedCMName2, Namespace: workNamespaceName}, cm); err != nil {
					return err
				}

				wantCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      wrappedCMName2,
						Namespace: workNamespaceName,
					},
					Data: map[string]string{
						cmDataKey: cmDataVal2,
					},
				}
				// Rebuild the configMap for ease of comparison.
				rebuiltGotCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cm.Name,
						Namespace: cm.Namespace,
					},
					Data: cm.Data,
				}

				if diff := cmp.Diff(wantCM, rebuiltGotCM); diff != "" {
					return fmt.Errorf("configMap diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the configMap object #1")
		})

		AfterAll(func() {
			// Remove the CRP and the namespace from the hub cluster.
			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{memberCluster1EastProd})
		})
	})

	Context("pre-processing failure in report diff mode (decoding errors)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Use an envelope to create duplicate resource entries.
			ns := appNamespace()
			Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

			// Create an envelope resource to wrap the configMaps.
			resourceEnvelope := &placementv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      envelopeName,
					Namespace: ns.Name,
				},
				Data: map[string]runtime.RawExtension{},
			}

			// Create a malformed config map as a wrapped resource.
			badConfigMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "malformed/v10",
					Kind:       "Unknown",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: ns.Name,
					Name:      wrappedCMName1,
				},
				Data: map[string]string{
					cmDataKey: cmDataVal1,
				},
			}
			badCMBytes, err := json.Marshal(badConfigMap)
			Expect(err).To(BeNil(), "Failed to marshal configMap %s", badConfigMap.Name)
			resourceEnvelope.Data["cm1.yaml"] = runtime.RawExtension{Raw: badCMBytes}
			Expect(hubClient.Create(ctx, resourceEnvelope)).To(Succeed(), "Failed to create resource envelope %s", resourceEnvelope.Name)

			// Create a CRP.
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
							Type: placementv1beta1.ApplyStrategyTypeReportDiff,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				wantStatus := placementv1beta1.PlacementStatus{
					Conditions: crpDiffReportingFailedConditions(crp.Generation, false),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterDiffReportingFailedConditions(crp.Generation),
						},
					},
					SelectedResources: []placementv1beta1.ResourceIdentifier{
						{
							Kind:    "Namespace",
							Name:    workNamespaceName,
							Version: "v1",
						},
						{
							Group:     placementv1beta1.GroupVersion.Group,
							Kind:      placementv1beta1.ResourceEnvelopeKind,
							Version:   placementv1beta1.GroupVersion.Version,
							Name:      envelopeName,
							Namespace: workNamespaceName,
						},
					},
					ObservedResourceIndex: "0",
				}
				if diff := cmp.Diff(crp.Status, wantStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration*20, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should not apply any resource", func() {
			Consistently(func() error {
				cm := &corev1.ConfigMap{}
				if err := memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: wrappedCMName1, Namespace: workNamespaceName}, cm); !errors.IsNotFound(err) {
					return fmt.Errorf("the config map exists, or an unexpected error has occurred: %w", err)
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The malformed configMap has been applied unexpectedly")

			Consistently(workNamespaceRemovedFromClusterActual(memberCluster1EastProd)).Should(Succeed(), "The namespace object has been applied unexpectedly")
		})

		AfterAll(func() {
			// Remove the CRP and the namespace from the hub cluster.
			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{memberCluster1EastProd})
		})
	})

	Context("pre-processing failure in apply ops (duplicated)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Use an envelope to create duplicate resource entries.
			ns := appNamespace()
			Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

			// Create an envelope resource to wrap the configMaps.
			resourceEnvelope := &placementv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      envelopeName,
					Namespace: ns.Name,
				},
				Data: map[string]runtime.RawExtension{},
			}

			// Create configMaps as wrapped resources.
			configMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: ns.Name,
					Name:      wrappedCMName1,
				},
				Data: map[string]string{
					cmDataKey: cmDataVal1,
				},
			}
			// Prepare a regular config map and a duplicate.
			wrappedCM := configMap.DeepCopy()
			wrappedCM.Name = wrappedCMName1
			wrappedCM.Data[cmDataKey] = cmDataVal1
			wrappedCMBytes, err := json.Marshal(wrappedCM)
			Expect(err).To(BeNil(), "Failed to marshal configMap %s", wrappedCM.Name)
			resourceEnvelope.Data["cm1.yaml"] = runtime.RawExtension{Raw: wrappedCMBytes}

			// Note: due to how work generator sorts manifests, the CM below will actually be
			// applied first.
			duplicatedCM := configMap.DeepCopy()
			duplicatedCM.Name = wrappedCMName1
			duplicatedCM.Data[cmDataKey] = cmDataVal2
			duplicatedCMBytes, err := json.Marshal(duplicatedCM)
			Expect(err).To(BeNil(), "Failed to marshal configMap %s", duplicatedCM.Name)
			resourceEnvelope.Data["cm2.yaml"] = runtime.RawExtension{Raw: duplicatedCMBytes}

			Expect(hubClient.Create(ctx, resourceEnvelope)).To(Succeed(), "Failed to create resource envelope %s", resourceEnvelope.Name)

			// Create a CRP.
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
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				wantStatus := placementv1beta1.PlacementStatus{
					Conditions: crpAppliedFailedConditions(crp.Generation),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Group:     "",
										Version:   "v1",
										Kind:      "ConfigMap",
										Namespace: workNamespaceName,
										Name:      wrappedCMName1,
										Envelope: &placementv1beta1.EnvelopeIdentifier{
											Name:      envelopeName,
											Namespace: workNamespaceName,
											Type:      placementv1beta1.ResourceEnvelopeType,
										},
									},
									Condition: metav1.Condition{
										Type:               placementv1beta1.WorkConditionTypeApplied,
										Status:             metav1.ConditionFalse,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeDuplicated),
										ObservedGeneration: 0,
									},
								},
							},
							Conditions: perClusterApplyFailedConditions(crp.Generation),
						},
					},
					SelectedResources: []placementv1beta1.ResourceIdentifier{
						{
							Kind:    "Namespace",
							Name:    workNamespaceName,
							Version: "v1",
						},
						{
							Group:     placementv1beta1.GroupVersion.Group,
							Kind:      placementv1beta1.ResourceEnvelopeKind,
							Version:   placementv1beta1.GroupVersion.Version,
							Name:      envelopeName,
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
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "CRP status has changed unexpectedly")
		})

		It("should place some manifests on member clusters", func() {
			Eventually(func() error {
				return validateWorkNamespaceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: workNamespaceName})
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Eventually(func() error {
				cm := &corev1.ConfigMap{}
				if err := memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: wrappedCMName1, Namespace: workNamespaceName}, cm); err != nil {
					return err
				}

				wantCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      wrappedCMName1,
						Namespace: workNamespaceName,
					},
					Data: map[string]string{
						cmDataKey: cmDataVal2,
					},
				}
				// Rebuild the configMap for ease of comparison.
				rebuiltGotCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cm.Name,
						Namespace: cm.Namespace,
					},
					Data: cm.Data,
				}

				if diff := cmp.Diff(rebuiltGotCM, wantCM); diff != "" {
					return fmt.Errorf("configMap diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the configMap object #1")
		})

		AfterAll(func() {
			// Remove the CRP and the namespace from the hub cluster.
			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{memberCluster1EastProd})
		})
	})
})
