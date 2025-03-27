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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/workapplier"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("handling errors and failures gracefully", func() {
	// This test spec uses envelopes for placement as it is a bit tricky to simulate
	// decoding errors with resources created directly in the hub cluster.
	//
	// TO-DO (chenyu1): reserve an API group exclusively on the hub cluster so that
	// envelopes do not need to used for this test spec.
	Context("pre-processing failure (decoding errors)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		wrapperCMName := "wrapper"
		wrappedCMName1 := "app-1"
		wrappedCMName2 := "app-2"

		cmDataKey := "foo"
		cmDataVal1 := "bar"
		cmDataVal2 := "baz"

		BeforeAll(func() {
			// Use an envelope to create duplicate resource entries.
			ns := appNamespace()
			Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

			// Create an envelope config map.
			wrapperCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wrapperCMName,
					Namespace: ns.Name,
					Annotations: map[string]string{
						placementv1beta1.EnvelopeConfigMapAnnotation: "true",
					},
				},
				Data: map[string]string{},
			}

			// Create configMaps as wrapped resources.
			wrappedCM := &corev1.ConfigMap{
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
			// Given Fleet's current resource sorting logic, this configMap
			// will be considered as the duplicated resource entry.
			wrappedCM1 := wrappedCM.DeepCopy()
			wrappedCM1.TypeMeta = metav1.TypeMeta{
				APIVersion: "dummy/v10",
				Kind:       "Fake",
			}
			wrappedCM1Bytes, err := json.Marshal(wrappedCM1)
			Expect(err).To(BeNil(), "Failed to marshal configMap %s", wrappedCM1.Name)
			wrapperCM.Data["cm1.yaml"] = string(wrappedCM1Bytes)

			wrappedCM2 := wrappedCM.DeepCopy()
			wrappedCM2.Name = wrappedCMName2
			wrappedCM2.Data[cmDataKey] = cmDataVal2
			wrappedCM2Bytes, err := json.Marshal(wrappedCM2)
			Expect(err).To(BeNil(), "Failed to marshal configMap %s", wrappedCM2.Name)
			wrapperCM.Data["cm2.yaml"] = string(wrappedCM2Bytes)

			Expect(hubClient.Create(ctx, wrapperCM)).To(Succeed(), "Failed to create configMap %s", wrapperCM.Name)

			// Create a CRP.
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

				wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
					Conditions: crpAppliedFailedConditions(crp.Generation),
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{
							ClusterName: memberCluster1EastProdName,
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Group:     "dummy",
										Version:   "v10",
										Kind:      "Fake",
										Namespace: workNamespaceName,
										Name:      wrappedCMName1,
										Envelope: &placementv1beta1.EnvelopeIdentifier{
											Name:      wrapperCMName,
											Namespace: workNamespaceName,
											Type:      placementv1beta1.ConfigMapEnvelopeType,
										},
									},
									Condition: metav1.Condition{
										Type:               placementv1beta1.WorkConditionTypeApplied,
										Status:             metav1.ConditionFalse,
										Reason:             string(workapplier.ManifestProcessingApplyResultTypeDecodingErred),
										ObservedGeneration: 0,
									},
								},
							},
							Conditions: resourcePlacementApplyFailedConditions(crp.Generation),
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
							Name:      wrapperCMName,
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
})
