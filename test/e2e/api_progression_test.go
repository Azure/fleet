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
	"time"

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

	placementv1 "go.goms.io/fleet/apis/placement/v1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/workapplier"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/test/e2e/framework"
	testutilseviction "go.goms.io/fleet/test/utils/eviction"
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

	placementStatusCmpOptionsOnCreateV1 = append(
		cmp.Options{
			ignorePlacementStatusObservedResourceIndexFieldV1,
			ignorePerClusterPlacementStatusObservedResourceIndexFieldV1,
		},
		placementStatusCmpOptionsV1...,
	)
)

// The helpers below are v1 API counterparts of the shared (v1beta1) E2E utilities; they read and
// write exclusively through the v1 API so that the API progression specs never fall back to v1beta1.

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

func retrievePlacementV1(placementKey types.NamespacedName) (placementv1.PlacementObj, error) {
	var placement placementv1.PlacementObj
	if placementKey.Namespace == "" {
		placement = &placementv1.ClusterResourcePlacement{}
	} else {
		placement = &placementv1.ResourcePlacement{}
	}
	if err := hubClient.Get(ctx, placementKey, placement); err != nil {
		return nil, err
	}
	return placement, nil
}

func placementRemovedActualV1(placementKey types.NamespacedName) func() error {
	return func() error {
		if _, err := retrievePlacementV1(placementKey); !errors.IsNotFound(err) {
			return fmt.Errorf("placement %s still exists or an unexpected error occurred: %w", placementKey, err)
		}
		return nil
	}
}

func allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActualV1(placementKey types.NamespacedName) func() error {
	return func() error {
		placement, err := retrievePlacementV1(placementKey)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}

		wantFinalizers := []string{customDeletionBlockerFinalizer}
		if diff := cmp.Diff(placement.GetFinalizers(), wantFinalizers); diff != "" {
			return fmt.Errorf("placement finalizers diff (-got, +want): %s", diff)
		}
		return nil
	}
}

func crpEvictionRemovedActualV1(crpEvictionName string) func() error {
	return func() error {
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpEvictionName}, &placementv1.ClusterResourcePlacementEviction{}); !errors.IsNotFound(err) {
			return fmt.Errorf("CRP eviction still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

func crpDisruptionBudgetRemovedActualV1(crpDisruptionBudgetName string) func() error {
	return func() error {
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpDisruptionBudgetName}, &placementv1.ClusterResourcePlacementDisruptionBudget{}); !errors.IsNotFound(err) {
			return fmt.Errorf("CRP disruption budget still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

func cleanupPlacementV1(placementKey types.NamespacedName) {
	Eventually(func() error {
		placement, err := retrievePlacementV1(placementKey)
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}

		// Delete the placement (again, if applicable); this helps the After All node to run
		// successfully even if the steps above fail early.
		if err := hubClient.Delete(ctx, placement); err != nil {
			return err
		}

		placement.SetFinalizers([]string{})
		return hubClient.Update(ctx, placement)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete placement %s", placementKey)

	Eventually(placementRemovedActualV1(placementKey), workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove placement %s", placementKey)

	// Wait for the Work objects to be deleted as well; leftover Work objects (which are kept
	// around by a finalizer until all applied resources are gone) may lead to resource overlaps
	// and flakiness in subsequent specs.
	By("Check if work is deleted")
	workName := fmt.Sprintf("%s-work", placementKey.Name)
	if placementKey.Namespace != "" {
		workName = fmt.Sprintf("%s.%s", placementKey.Namespace, workName)
	}
	Eventually(func() error {
		for idx := range allMemberClusterNames {
			workNS := fmt.Sprintf(utils.NamespaceNameFormat, allMemberClusterNames[idx])
			if err := hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: workNS}, &placementv1.Work{}); !errors.IsNotFound(err) {
				return fmt.Errorf("work object %s/%s still exists or an unexpected error occurred: %w", workNS, workName, err)
			}
		}
		return nil
	}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work objects derived from placement %s", placementKey)
}

func ensureCRPEvictionDeletedV1(crpEvictionName string) {
	crpe := &placementv1.ClusterResourcePlacementEviction{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpEvictionName,
		},
	}
	Expect(hubClient.Delete(ctx, crpe)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}), "Failed to delete CRP eviction")
	Eventually(crpEvictionRemovedActualV1(crpEvictionName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "CRP eviction still exists")
}

func ensureCRPDisruptionBudgetDeletedV1(crpDisruptionBudgetName string) {
	crpdb := &placementv1.ClusterResourcePlacementDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpDisruptionBudgetName,
		},
	}
	Expect(hubClient.Delete(ctx, crpdb)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}), "Failed to delete CRP disruption budget")
	Eventually(crpDisruptionBudgetRemovedActualV1(crpDisruptionBudgetName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "CRP disruption budget still exists")
}

func ensureCRPAndRelatedResourcesDeletedV1(crpName string, memberClusters []*framework.Cluster) {
	crp := &placementv1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpName,
		},
	}
	Expect(hubClient.Delete(ctx, crp)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}), "Failed to delete CRP")

	// Verify that all resources placed have been removed from the specified member clusters.
	for idx := range memberClusters {
		memberCluster := memberClusters[idx]

		workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster)
		Eventually(workResourcesRemovedActual, workloadEventuallyDuration, time.Second*5).Should(Succeed(), "Failed to remove work resources from member cluster %s", memberCluster.ClusterName)
	}

	// Verify that related finalizers have been removed from the CRP.
	finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActualV1(types.NamespacedName{Name: crpName})
	Eventually(finalizerRemovedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP")

	// Remove the custom deletion blocker finalizer from the CRP.
	cleanupPlacementV1(types.NamespacedName{Name: crpName})

	// Delete the created resources.
	cleanupWorkResources()
}

func workResourceIdentifiersV1() []placementv1.ResourceIdentifier {
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

	return []placementv1.ResourceIdentifier{
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
	}
}

func crpStatusUpdatedActualV1(wantSelectedResourceIdentifiers []placementv1.ResourceIdentifier, wantSelectedClusters, wantUnselectedClusters []string, wantObservedResourceIndex string) func() error {
	crpKey := types.NamespacedName{Name: fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())}
	return func() error {
		placement, err := retrievePlacementV1(crpKey)
		if err != nil {
			return fmt.Errorf("failed to get placement %s: %w", crpKey, err)
		}

		wantStatus := buildWantPlacementStatusV1(crpKey, placement.GetGeneration(), wantSelectedResourceIdentifiers, wantSelectedClusters, wantUnselectedClusters, wantObservedResourceIndex)
		cmpOptions := placementStatusCmpOptionsV1
		if wantObservedResourceIndex == "0" {
			// The placement has just been created; the observed resource index might not have been
			// populated yet.
			cmpOptions = placementStatusCmpOptionsOnCreateV1
		}
		if diff := cmp.Diff(placement.GetPlacementStatus(), wantStatus, cmpOptions...); diff != "" {
			return fmt.Errorf("placement status diff (-got, +want): %s for placement %v", diff, crpKey)
		}
		return nil
	}
}

func buildWantPlacementStatusV1(
	placementKey types.NamespacedName,
	placementGeneration int64,
	wantSelectedResourceIdentifiers []placementv1.ResourceIdentifier,
	wantSelectedClusters, wantUnselectedClusters []string,
	wantObservedResourceIndex string,
) *placementv1.PlacementStatus {
	wantPerClusterPlacementStatuses := []placementv1.PerClusterPlacementStatus{}
	for _, name := range wantSelectedClusters {
		wantPerClusterPlacementStatuses = append(wantPerClusterPlacementStatuses, placementv1.PerClusterPlacementStatus{
			ClusterName:           name,
			ObservedResourceIndex: wantObservedResourceIndex,
			Conditions:            perClusterRolloutCompletedConditions(placementGeneration, true, false),
		})
	}
	for i := 0; i < len(wantUnselectedClusters); i++ {
		wantPerClusterPlacementStatuses = append(wantPerClusterPlacementStatuses, placementv1.PerClusterPlacementStatus{
			Conditions: perClusterScheduleFailedConditions(placementGeneration),
		})
	}

	var wantPlacementConditions []metav1.Condition
	switch {
	case len(wantSelectedClusters) > 0 && len(wantUnselectedClusters) > 0:
		wantPlacementConditions = placementSchedulePartiallyFailedConditions(placementKey, placementGeneration)
	case len(wantSelectedClusters) > 0:
		wantPlacementConditions = placementRolloutCompletedConditions(placementKey, placementGeneration, false)
	case len(wantUnselectedClusters) > 0:
		// The remaining resource conditions are not set if there is no cluster to select.
		wantPlacementConditions = placementScheduleFailedConditions(placementKey, placementGeneration)
	default:
		wantPlacementConditions = placementScheduledConditions(placementKey, placementGeneration)
	}

	return &placementv1.PlacementStatus{
		Conditions:                  wantPlacementConditions,
		PerClusterPlacementStatuses: wantPerClusterPlacementStatuses,
		SelectedResources:           wantSelectedResourceIdentifiers,
		ObservedResourceIndex:       wantObservedResourceIndex,
	}
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
				Spec: placementv1.PlacementSpec{
					ResourceSelectors: []placementv1.ResourceSelectorTerm{
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
			buildWantCRPStatus := func(crpGeneration int64) *placementv1.PlacementStatus {
				return &placementv1.PlacementStatus{
					Conditions: crpAppliedFailedConditions(crpGeneration),
					SelectedResources: []placementv1.ResourceIdentifier{
						{
							Version: "v1",
							Kind:    "Namespace",
							Name:    nsName,
						},
					},
					PerClusterPlacementStatuses: []placementv1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterApplyFailedConditions(crpGeneration),
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
				Spec: placementv1.PlacementSpec{
					ResourceSelectors: []placementv1.ResourceSelectorTerm{
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
			buildWantCRPStatus := func(crpGeneration int64) *placementv1.PlacementStatus {
				return &placementv1.PlacementStatus{
					Conditions: crpAppliedFailedConditions(crpGeneration),
					SelectedResources: []placementv1.ResourceIdentifier{
						{
							Version: "v1",
							Kind:    "Namespace",
							Name:    nsName,
						},
					},
					PerClusterPlacementStatuses: []placementv1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterApplyFailedConditions(crpGeneration),
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
				Spec: placementv1.PlacementSpec{
					ResourceSelectors: []placementv1.ResourceSelectorTerm{
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
			buildWantCRPStatus := func(crpGeneration int64) *placementv1.PlacementStatus {
				return &placementv1.PlacementStatus{
					Conditions: crpDiffReportedConditions(crpGeneration, false),
					SelectedResources: []placementv1.ResourceIdentifier{
						{
							Version: "v1",
							Kind:    "Namespace",
							Name:    nsName,
						},
					},
					PerClusterPlacementStatuses: []placementv1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterDiffReportedConditions(crpGeneration),
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

var _ = Describe("eviction and disruption budget", func() {
	Context("eviction of a PickAll CRP protected by a disruption budget (read and write in v1)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			createWorkResources()

			crp := &placementv1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1.PlacementSpec{
					Policy: &placementv1.PlacementPolicy{
						PlacementType: placementv1.PickAllPlacementType,
					},
					ResourceSelectors: []placementv1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
		})

		AfterAll(func() {
			ensureCRPEvictionDeletedV1(crpEvictionName)
			ensureCRPDisruptionBudgetDeletedV1(crpName)
			ensureCRPAndRelatedResourcesDeletedV1(crpName, allMemberClusters)
		})

		It("should place resources on all available member clusters", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActualV1(workResourceIdentifiersV1(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should create a disruption budget that protects all placements", func() {
			crpdb := &placementv1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1.PlacementDisruptionBudgetSpec{
					MinAvailable: ptr.To(intstr.FromInt32(int32(len(allMemberClusterNames)))),
				},
			}
			Expect(hubClient.Create(ctx, crpdb)).To(Succeed(), "Failed to create CRP disruption budget %s", crpName)
		})

		It("should create an eviction targeting a bound cluster", func() {
			crpe := &placementv1.ClusterResourcePlacementEviction{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpEvictionName,
				},
				Spec: placementv1.PlacementEvictionSpec{
					PlacementName: crpName,
					ClusterName:   memberCluster1EastProdName,
				},
			}
			Expect(hubClient.Create(ctx, crpe)).To(Succeed(), "Failed to create CRP eviction %s", crpEvictionName)
		})

		It("should deny the disruption", func() {
			crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, hubClient, crpEvictionName,
				&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
				&testutilseviction.IsExecutedEviction{
					IsExecuted: false,
					Msg: fmt.Sprintf(
						condition.EvictionBlockedPDBSpecifiedMessageFmt,
						len(allMemberClusterNames),
						len(allMemberClusterNames),
					),
				},
			)
			Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to deny CRP eviction as expected")
		})
	})

	Context("eviction of a PickN CRP protected by a disruption budget (read and write in v1)", Ordered, Serial, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())
		taintClusterNames := []string{memberCluster1EastProdName}
		noTaintClusterNames := []string{memberCluster2EastCanaryName, memberCluster3WestProdName}

		BeforeAll(func() {
			createWorkResources()

			crp := &placementv1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1.PlacementSpec{
					Policy: &placementv1.PlacementPolicy{
						PlacementType:    placementv1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(len(allMemberClusterNames))),
					},
					ResourceSelectors: []placementv1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
		})

		AfterAll(func() {
			removeTaintsFromMemberClusters(taintClusterNames)
			ensureCRPEvictionDeletedV1(crpEvictionName)
			ensureCRPDisruptionBudgetDeletedV1(crpName)
			ensureCRPAndRelatedResourcesDeletedV1(crpName, allMemberClusters)
		})

		It("should place resources on all available member clusters", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActualV1(workResourceIdentifiersV1(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should create a disruption budget that allows one unavailable placement", func() {
			crpdb := &placementv1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: ptr.To(intstr.FromInt32(1)),
				},
			}
			Expect(hubClient.Create(ctx, crpdb)).To(Succeed(), "Failed to create CRP disruption budget %s", crpName)
		})

		It("should taint the target cluster to prevent it from being picked again", func() {
			addTaintsToMemberClusters(taintClusterNames, buildTaints(taintClusterNames))
		})

		It("should create an eviction targeting a bound cluster", func() {
			crpe := &placementv1.ClusterResourcePlacementEviction{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpEvictionName,
				},
				Spec: placementv1.PlacementEvictionSpec{
					PlacementName: crpName,
					ClusterName:   memberCluster1EastProdName,
				},
			}
			Expect(hubClient.Create(ctx, crpe)).To(Succeed(), "Failed to create CRP eviction %s", crpEvictionName)
		})

		It("should allow the disruption", func() {
			crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, hubClient, crpEvictionName,
				&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
				&testutilseviction.IsExecutedEviction{
					IsExecuted: true,
					Msg: fmt.Sprintf(
						condition.EvictionAllowedPDBSpecifiedMessageFmt,
						len(allMemberClusterNames),
						len(allMemberClusterNames),
					),
				},
			)
			Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to allow CRP eviction as expected")
		})

		It("should complete the disruption", func() {
			workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster1EastProd)
			Eventually(workResourcesRemovedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from evicted member cluster")

			crpStatusUpdatedActual := crpStatusUpdatedActualV1(workResourceIdentifiersV1(), noTaintClusterNames, taintClusterNames, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status after eviction")
		})
	})
})
