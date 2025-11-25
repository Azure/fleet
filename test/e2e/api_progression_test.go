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
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	placementv1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workapplier"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var (
	placementStatusCmpOptionsV1 = cmp.Options{
		cmpopts.SortSlices(lessFuncCondition),
		cmpopts.SortSlices(lessFuncPlacementStatusV1),
		cmpopts.SortSlices(utils.LessFuncResourceIdentifierV1),
		cmpopts.SortSlices(utils.LessFuncFailedResourcePlacementsV1),
		cmpopts.SortSlices(utils.LessFuncDiffedResourcePlacementsV1),
		cmpopts.SortSlices(utils.LessFuncDriftedResourcePlacementsV1),
		utils.IgnoreConditionLTTAndMessageFields,
		ignorePlacementStatusDriftedPlacementsTimestampFieldsV1,
		ignorePlacementStatusDiffedPlacementsTimestampFieldsV1,
		cmpopts.EquateEmpty(),
	}
)

func ensureCRPRemovalV1(crpName string) {
	Eventually(func() error {
		crp := &placementv1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
		}
		if err := hubClient.Delete(ctx, crp); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete CRP object: %w", err)
		}

		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get CRP object: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to wait for CRP deletion")
}

// Test specs in this file help verify the progression from one API version to another (e.g., v1beta1 to v1);
// the logic is more focuses on API compatibility and is less focused on behavioral correctness for simplicity reasons.

// Note (chenyu1): in the test specs there are still sporadic references to the v1beta1 API package; this is needed
// as some of the constants (primarily condition types and reasons) are only available there.

var _ = Describe("takeover, drift detection, and reportDiff mode (v1beta1 to v1)", func() {
	Context("takeover with diff detection (CRP, read and write in v1)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		var existingNS *corev1.Namespace

		BeforeAll(func() {
			ns := appNamespace()
			// Add a label (managed field) to the namespace.
			ns.Labels = map[string]string{
				managedDataFieldKey:    managedDataFieldVal1,
				workNamespaceLabelName: fmt.Sprintf("%d", GinkgoParallelProcess()),
			}
			existingNS = ns.DeepCopy()

			// Create the resources on the hub cluster.
			Expect(hubClient.Create(ctx, &ns)).To(Succeed())

			// Create the resources on one of the member clusters.
			existingNS.Labels[managedDataFieldKey] = managedDataFieldVal2
			Expect(memberCluster1EastProdClient.Create(ctx, existingNS)).To(Succeed())

			crp := &placementv1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    nsName,
						},
					},
					Policy: &placementv1.PlacementPolicy{
						PlacementType: placementv1.PickFixedPlacementType,
						ClusterNames: []string{
							memberCluster1EastProdName,
						},
					},
					Strategy: placementv1.RolloutStrategy{
						Type: placementv1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("100%")),
							UnavailablePeriodSeconds: ptr.To(1),
						},
						ApplyStrategy: &placementv1.ApplyStrategy{
							ComparisonOption: placementv1.ComparisonOptionTypePartialComparison,
							WhenToTakeOver:   placementv1.WhenToTakeOverTypeIfNoDiff,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should update CRP status as expected", func() {
			buildWantCRPStatus := func(crpGeneration int64) *placementv1.ClusterResourcePlacementStatus {
				return &placementv1.ClusterResourcePlacementStatus{
					Conditions: crpAppliedFailedConditions(crpGeneration),
					SelectedResources: []placementv1.ResourceIdentifier{
						{
							Version: "v1",
							Kind:    "Namespace",
							Name:    nsName,
						},
					},
					PlacementStatuses: []placementv1.ResourcePlacementStatus{
						{
							ClusterName: memberCluster1EastProdName,
							Conditions:  perClusterApplyFailedConditions(crpGeneration),
							FailedPlacements: []placementv1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.PerClusterAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 0,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
									},
								},
							},
							DiffedPlacements: []placementv1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									TargetClusterObservedGeneration: ptr.To(int64(0)),
									ObservedDiffs: []placementv1.PatchDetail{
										{
											Path:          fmt.Sprintf("/metadata/labels/%s", managedDataFieldKey),
											ValueInMember: managedDataFieldVal2,
											ValueInHub:    managedDataFieldVal1,
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
				crp := &placementv1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				wantCRPStatus := buildWantCRPStatus(crp.Generation)

				if diff := cmp.Diff(crp.Status, *wantCRPStatus, placementStatusCmpOptionsV1...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPRemovalV1(crpName)

			// Delete the namespace from the hub cluster.
			cleanupWorkResources()

			// Verify that all resources placed have been removed from the specified member clusters.
			cleanWorkResourcesOnCluster(memberCluster1EastProd)
		})
	})

	Context("apply with drift detection (CRP, read and write in v1)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			ns := appNamespace()
			// Add a label (managed field) to the namespace.
			ns.Labels = map[string]string{
				managedDataFieldKey:    managedDataFieldVal1,
				workNamespaceLabelName: fmt.Sprintf("%d", GinkgoParallelProcess()),
			}

			// Create the resources on the hub cluster.
			Expect(hubClient.Create(ctx, &ns)).To(Succeed())

			crp := &placementv1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    nsName,
						},
					},
					Policy: &placementv1.PlacementPolicy{
						PlacementType: placementv1.PickFixedPlacementType,
						ClusterNames: []string{
							memberCluster1EastProdName,
						},
					},
					Strategy: placementv1.RolloutStrategy{
						Type: placementv1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("100%")),
							UnavailablePeriodSeconds: ptr.To(1),
						},
						ApplyStrategy: &placementv1.ApplyStrategy{
							ComparisonOption: placementv1.ComparisonOptionTypePartialComparison,
							WhenToApply:      placementv1.WhenToApplyTypeIfNotDrifted,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("can introduce a drift", func() {
			Eventually(func() error {
				ns := &corev1.Namespace{}
				if err := memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err != nil {
					return fmt.Errorf("failed to retrieve namespace: %w", err)
				}

				if ns.Labels == nil {
					ns.Labels = make(map[string]string)
				}
				ns.Labels[managedDataFieldKey] = managedDataFieldVal2
				if err := memberCluster1EastProdClient.Update(ctx, ns); err != nil {
					return fmt.Errorf("failed to update namespace: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to introduce a drift")
		})

		It("should update CRP status as expected", func() {
			buildWantCRPStatus := func(crpGeneration int64) *placementv1.ClusterResourcePlacementStatus {
				return &placementv1.ClusterResourcePlacementStatus{
					Conditions: crpAppliedFailedConditions(crpGeneration),
					SelectedResources: []placementv1.ResourceIdentifier{
						{
							Version: "v1",
							Kind:    "Namespace",
							Name:    nsName,
						},
					},
					PlacementStatuses: []placementv1.ResourcePlacementStatus{
						{
							ClusterName: memberCluster1EastProdName,
							Conditions:  perClusterApplyFailedConditions(crpGeneration),
							FailedPlacements: []placementv1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									Condition: metav1.Condition{
										Type:               string(placementv1beta1.PerClusterAppliedConditionType),
										Status:             metav1.ConditionFalse,
										ObservedGeneration: 0,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeFoundDrifts),
									},
								},
							},
							DriftedPlacements: []placementv1.DriftedResourcePlacement{
								{
									ResourceIdentifier: placementv1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									TargetClusterObservedGeneration: 0,
									ObservedDrifts: []placementv1.PatchDetail{
										{
											Path:          fmt.Sprintf("/metadata/labels/%s", managedDataFieldKey),
											ValueInMember: managedDataFieldVal2,
											ValueInHub:    managedDataFieldVal1,
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
				crp := &placementv1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				wantCRPStatus := buildWantCRPStatus(crp.Generation)

				if diff := cmp.Diff(crp.Status, *wantCRPStatus, placementStatusCmpOptionsV1...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPRemovalV1(crpName)

			// Delete the namespace from the hub cluster.
			cleanupWorkResources()

			// Verify that all resources placed have been removed from the specified member clusters.
			cleanWorkResourcesOnCluster(memberCluster1EastProd)
		})
	})

	Context("reportDiff mode (CRP, read and write in v1)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		var existingNS *corev1.Namespace

		BeforeAll(func() {
			ns := appNamespace()
			// Add a label (managed field) to the namespace.
			ns.Labels = map[string]string{
				managedDataFieldKey:    managedDataFieldVal1,
				workNamespaceLabelName: fmt.Sprintf("%d", GinkgoParallelProcess()),
			}
			existingNS = ns.DeepCopy()

			// Create the resources on the hub cluster.
			Expect(hubClient.Create(ctx, &ns)).To(Succeed())

			// Create the resources on one of the member clusters.
			existingNS.Labels[managedDataFieldKey] = managedDataFieldVal2
			Expect(memberCluster1EastProdClient.Create(ctx, existingNS)).To(Succeed())

			crp := &placementv1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    nsName,
						},
					},
					Policy: &placementv1.PlacementPolicy{
						PlacementType: placementv1.PickFixedPlacementType,
						ClusterNames: []string{
							memberCluster1EastProdName,
						},
					},
					Strategy: placementv1.RolloutStrategy{
						Type: placementv1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("100%")),
							UnavailablePeriodSeconds: ptr.To(1),
						},
						ApplyStrategy: &placementv1.ApplyStrategy{
							Type:             placementv1.ApplyStrategyTypeReportDiff,
							ComparisonOption: placementv1.ComparisonOptionTypePartialComparison,
							WhenToTakeOver:   placementv1.WhenToTakeOverTypeNever,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should update CRP status as expected", func() {
			buildWantCRPStatus := func(crpGeneration int64) *placementv1.ClusterResourcePlacementStatus {
				return &placementv1.ClusterResourcePlacementStatus{
					Conditions: crpDiffReportedConditions(crpGeneration, false),
					SelectedResources: []placementv1.ResourceIdentifier{
						{
							Version: "v1",
							Kind:    "Namespace",
							Name:    nsName,
						},
					},
					PlacementStatuses: []placementv1.ResourcePlacementStatus{
						{
							ClusterName: memberCluster1EastProdName,
							Conditions:  perClusterDiffReportedConditions(crpGeneration),
							DiffedPlacements: []placementv1.DiffedResourcePlacement{
								{
									ResourceIdentifier: placementv1.ResourceIdentifier{
										Version: "v1",
										Kind:    "Namespace",
										Name:    nsName,
									},
									TargetClusterObservedGeneration: ptr.To(int64(0)),
									ObservedDiffs: []placementv1.PatchDetail{
										{
											Path:          fmt.Sprintf("/metadata/labels/%s", managedDataFieldKey),
											ValueInMember: managedDataFieldVal2,
											ValueInHub:    managedDataFieldVal1,
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
				crp := &placementv1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				wantCRPStatus := buildWantCRPStatus(crp.Generation)

				if diff := cmp.Diff(crp.Status, *wantCRPStatus, placementStatusCmpOptionsV1...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPRemovalV1(crpName)

			// Delete the namespace from the hub cluster.
			cleanupWorkResources()

			// Verify that all resources placed have been removed from the specified member clusters.
			cleanWorkResourcesOnCluster(memberCluster1EastProd)
		})
	})
})
