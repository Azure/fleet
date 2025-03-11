package before

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/clusterresourceplacement"
	"go.goms.io/fleet/pkg/controllers/workapplier"
	scheduler "go.goms.io/fleet/pkg/scheduler/framework"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/test/e2e/framework"
)

func resourcePlacementRolloutCompletedConditions(generation int64, resourceIsTrackable bool, hasOverride bool) []metav1.Condition {
	availableConditionReason := condition.WorkNotAvailabilityTrackableReason
	if resourceIsTrackable {
		availableConditionReason = condition.AllWorkAvailableReason
	}
	overrideConditionReason := condition.OverrideNotSpecifiedReason
	if hasOverride {
		overrideConditionReason = condition.OverriddenSucceededReason
	}

	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ResourceScheduledConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.ScheduleSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceRolloutStartedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceOverriddenConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             overrideConditionReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceWorkSynchronizedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.AllWorkSyncedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourcesAppliedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.AllWorkAppliedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourcesAvailableConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             availableConditionReason,
			ObservedGeneration: generation,
		},
	}
}

func resourcePlacementScheduleFailedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ResourceScheduledConditionType),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: generation,
			Reason:             clusterresourceplacement.ResourceScheduleFailedReason,
		},
	}
}

func resourcePlacementApplyFailedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ResourceScheduledConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.ScheduleSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceRolloutStartedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceOverriddenConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.OverrideNotSpecifiedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceWorkSynchronizedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.AllWorkSyncedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourcesAppliedConditionType),
			Status:             metav1.ConditionFalse,
			Reason:             condition.WorkNotAppliedReason,
			ObservedGeneration: generation,
		},
	}
}

func resourcePlacementAvailabilityCheckFailedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ResourceScheduledConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.ScheduleSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceRolloutStartedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceOverriddenConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.OverrideNotSpecifiedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceWorkSynchronizedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.AllWorkSyncedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourcesAppliedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.AllWorkAppliedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourcesAvailableConditionType),
			Status:             metav1.ConditionFalse,
			Reason:             condition.WorkNotAvailableReason,
			ObservedGeneration: generation,
		},
	}
}

func crpRolloutCompletedConditions(generation int64, hasOverride bool) []metav1.Condition {
	overrideConditionReason := condition.OverrideNotSpecifiedReason
	if hasOverride {
		overrideConditionReason = condition.OverriddenSucceededReason
	}
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             scheduler.FullyScheduledReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             overrideConditionReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.WorkSynchronizedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.ApplySucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementAvailableConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.AvailableReason,
			ObservedGeneration: generation,
		},
	}
}

func crpSchedulePartiallyFailedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: generation,
			Reason:             scheduler.NotFullyScheduledReason,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.OverrideNotSpecifiedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.WorkSynchronizedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.ApplySucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementAvailableConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.AvailableReason,
			ObservedGeneration: generation,
		},
	}
}

func crpScheduleFailedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: generation,
			Reason:             scheduler.NotFullyScheduledReason,
		},
	}
}

func crpNotAvailableConditions(generation int64, hasOverride bool) []metav1.Condition {
	overrideConditionReason := condition.OverrideNotSpecifiedReason
	if hasOverride {
		overrideConditionReason = condition.OverriddenSucceededReason
	}
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             scheduler.FullyScheduledReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             overrideConditionReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.WorkSynchronizedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.ApplySucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementAvailableConditionType),
			Status:             metav1.ConditionFalse,
			Reason:             condition.NotAvailableYetReason,
			ObservedGeneration: generation,
		},
	}
}

func crpNotAppliedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             scheduler.FullyScheduledReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.OverrideNotSpecifiedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             condition.WorkSynchronizedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
			Status:             metav1.ConditionFalse,
			Reason:             condition.ApplyFailedReason,
			ObservedGeneration: generation,
		},
	}
}

func crpStatusUpdatedActual(crpName string, wantSelectedResourceIdentifiers []placementv1beta1.ResourceIdentifier, wantSelectedClusters, wantUnselectedClusters []string, wantObservedResourceIndex string) func() error {
	return customizedCRPStatusUpdatedActual(crpName, wantSelectedResourceIdentifiers, wantSelectedClusters, wantUnselectedClusters, wantObservedResourceIndex, true)
}

func customizedCRPStatusUpdatedActual(crpName string,
	wantSelectedResourceIdentifiers []placementv1beta1.ResourceIdentifier,
	wantSelectedClusters, wantUnselectedClusters []string,
	wantObservedResourceIndex string,
	resourceIsTrackable bool) func() error {
	return func() error {
		crp := &placementv1beta1.ClusterResourcePlacement{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return err
		}

		wantPlacementStatus := []placementv1beta1.ResourcePlacementStatus{}
		for _, name := range wantSelectedClusters {
			wantPlacementStatus = append(wantPlacementStatus, placementv1beta1.ResourcePlacementStatus{
				ClusterName: name,
				Conditions:  resourcePlacementRolloutCompletedConditions(crp.Generation, resourceIsTrackable, false),
			})
		}
		for i := 0; i < len(wantUnselectedClusters); i++ {
			wantPlacementStatus = append(wantPlacementStatus, placementv1beta1.ResourcePlacementStatus{
				Conditions: resourcePlacementScheduleFailedConditions(crp.Generation),
			})
		}

		var wantCRPConditions []metav1.Condition
		if len(wantSelectedClusters) > 0 {
			wantCRPConditions = crpRolloutCompletedConditions(crp.Generation, false)
		} else {
			wantCRPConditions = []metav1.Condition{
				// we don't set the remaining resource conditions.
				{
					Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             scheduler.FullyScheduledReason,
					ObservedGeneration: crp.Generation,
				},
			}
		}

		if len(wantUnselectedClusters) > 0 {
			if len(wantSelectedClusters) > 0 {
				wantCRPConditions = crpSchedulePartiallyFailedConditions(crp.Generation)
			} else {
				// we don't set the remaining resource conditions if there is no clusters to select
				wantCRPConditions = crpScheduleFailedConditions(crp.Generation)
			}
		}

		// Note that the CRP controller will only keep decisions regarding unselected clusters for a CRP if:
		//
		// * The CRP is of the PickN placement type and the required N count cannot be fulfilled; or
		// * The CRP is of the PickFixed placement type and the list of target clusters specified cannot be fulfilled.
		wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
			Conditions:            wantCRPConditions,
			PlacementStatuses:     wantPlacementStatus,
			SelectedResources:     wantSelectedResourceIdentifiers,
			ObservedResourceIndex: wantObservedResourceIndex,
		}
		if diff := cmp.Diff(crp.Status, wantStatus, crpStatusCmpOptions...); diff != "" {
			return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
		}
		return nil
	}
}

func crpWithOneFailedAvailabilityCheckStatusUpdatedActual(
	crpName string,
	wantSelectedResourceIdentifiers []placementv1beta1.ResourceIdentifier,
	wantFailedClusters []string,
	wantFailedWorkloadResourceIdentifier placementv1beta1.ResourceIdentifier,
	wantFailedResourceObservedGeneration int64,
	wantAvailableClusters []string,
	wantObservedResourceIndex string,
) func() error {
	return func() error {
		crp := &placementv1beta1.ClusterResourcePlacement{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return err
		}

		var wantPlacementStatus []placementv1beta1.ResourcePlacementStatus

		for _, name := range wantFailedClusters {
			wantPlacementStatus = append(wantPlacementStatus, placementv1beta1.ResourcePlacementStatus{
				ClusterName: name,
				Conditions:  resourcePlacementAvailabilityCheckFailedConditions(crp.Generation),
				FailedPlacements: []placementv1beta1.FailedResourcePlacement{
					{
						ResourceIdentifier: wantFailedWorkloadResourceIdentifier,
						Condition: metav1.Condition{
							Type:   string(placementv1beta1.ResourcesAvailableConditionType),
							Status: metav1.ConditionFalse,
							// The new and old applier uses the same reason string to make things
							// a bit easier.
							Reason:             string(workapplier.ManifestProcessingAvailabilityResultTypeNotYetAvailable),
							ObservedGeneration: wantFailedResourceObservedGeneration,
						},
					},
				},
			})
		}

		for _, name := range wantAvailableClusters {
			wantPlacementStatus = append(wantPlacementStatus, placementv1beta1.ResourcePlacementStatus{
				ClusterName: name,
				Conditions:  resourcePlacementRolloutCompletedConditions(crp.Generation, true, false),
			})
		}

		wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
			Conditions:            crpNotAvailableConditions(crp.Generation, false),
			PlacementStatuses:     wantPlacementStatus,
			SelectedResources:     wantSelectedResourceIdentifiers,
			ObservedResourceIndex: wantObservedResourceIndex,
		}

		if diff := cmp.Diff(crp.Status, wantStatus, crpStatusCmpOptions...); diff != "" {
			return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
		}
		return nil
	}
}

func crpWithOneFailedApplyOpStatusUpdatedActual(
	crpName string,
	wantSelectedResourceIdentifiers []placementv1beta1.ResourceIdentifier,
	wantFailedClusters []string,
	wantFailedWorkloadResourceIdentifier placementv1beta1.ResourceIdentifier,
	wantFailedResourceObservedGeneration int64,
	wantAvailableClusters []string,
	wantObservedResourceIndex string,
) func() error {
	return func() error {
		crp := &placementv1beta1.ClusterResourcePlacement{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return err
		}

		var wantPlacementStatus []placementv1beta1.ResourcePlacementStatus

		for _, name := range wantFailedClusters {
			wantPlacementStatus = append(wantPlacementStatus, placementv1beta1.ResourcePlacementStatus{
				ClusterName: name,
				Conditions:  resourcePlacementApplyFailedConditions(crp.Generation),
				FailedPlacements: []placementv1beta1.FailedResourcePlacement{
					{
						ResourceIdentifier: wantFailedWorkloadResourceIdentifier,
						Condition: metav1.Condition{
							Type:   string(placementv1beta1.ResourcesAppliedConditionType),
							Status: metav1.ConditionFalse,
							// The new and old applier uses the same reason string to make things
							// a bit easier.
							Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToApply),
							ObservedGeneration: wantFailedResourceObservedGeneration,
						},
					},
				},
			})
		}

		for _, name := range wantAvailableClusters {
			wantPlacementStatus = append(wantPlacementStatus, placementv1beta1.ResourcePlacementStatus{
				ClusterName: name,
				Conditions:  resourcePlacementRolloutCompletedConditions(crp.Generation, true, false),
			})
		}

		wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
			Conditions:            crpNotAppliedConditions(crp.Generation),
			PlacementStatuses:     wantPlacementStatus,
			SelectedResources:     wantSelectedResourceIdentifiers,
			ObservedResourceIndex: wantObservedResourceIndex,
		}

		if diff := cmp.Diff(crp.Status, wantStatus, crpStatusCmpOptions...); diff != "" {
			return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
		}
		return nil
	}
}

func crpWithStuckRolloutDueToOneFailedAvailabilityCheckStatusUpdatedActual(
	crpName string,
	wantSelectedResourceIdentifiers []placementv1beta1.ResourceIdentifier,
	failedWorkloadResourceIdentifier placementv1beta1.ResourceIdentifier,
	failedResourceObservedGeneration int64,
	wantSelectedClusters []string,
	wantObservedResourceIndex string,
) func() error {
	return func() error {
		crp := &placementv1beta1.ClusterResourcePlacement{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return err
		}

		var wantPlacementStatus []placementv1beta1.ResourcePlacementStatus
		// We only expect the deployment to not be available on one cluster.
		unavailableResourcePlacementStatus := placementv1beta1.ResourcePlacementStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.ScheduleSucceededReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourceRolloutStartedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourceOverriddenConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.OverrideNotSpecifiedReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourceWorkSynchronizedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkSyncedReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourcesAppliedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkAppliedReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourcesAvailableConditionType),
					Status:             metav1.ConditionFalse,
					Reason:             condition.WorkNotAvailableReason,
					ObservedGeneration: crp.Generation,
				},
			},
			FailedPlacements: []placementv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: failedWorkloadResourceIdentifier,
					Condition: metav1.Condition{
						Type:   string(placementv1beta1.ResourcesAvailableConditionType),
						Status: metav1.ConditionFalse,
						// The new and old applier uses the same reason string to make things
						// a bit easier.
						Reason:             string(workapplier.ManifestProcessingAvailabilityResultTypeNotYetAvailable),
						ObservedGeneration: failedResourceObservedGeneration,
					},
				},
			},
		}
		wantPlacementStatus = append(wantPlacementStatus, unavailableResourcePlacementStatus)

		// For all the other connected member clusters rollout will be blocked.
		rolloutBlockedPlacementStatus := placementv1beta1.ResourcePlacementStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.ScheduleSucceededReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourceRolloutStartedConditionType),
					Status:             metav1.ConditionFalse,
					Reason:             condition.RolloutNotStartedYetReason,
					ObservedGeneration: crp.Generation,
				},
			},
		}

		for i := 0; i < len(wantSelectedClusters)-1; i++ {
			wantPlacementStatus = append(wantPlacementStatus, rolloutBlockedPlacementStatus)
		}

		wantCRPConditions := []metav1.Condition{
			{
				Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
				Status:             metav1.ConditionTrue,
				Reason:             scheduler.FullyScheduledReason,
				ObservedGeneration: crp.Generation,
			},
			{
				Type:               string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
				Status:             metav1.ConditionFalse,
				Reason:             condition.RolloutNotStartedYetReason,
				ObservedGeneration: crp.Generation,
			},
		}

		wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
			Conditions:            wantCRPConditions,
			PlacementStatuses:     wantPlacementStatus,
			SelectedResources:     wantSelectedResourceIdentifiers,
			ObservedResourceIndex: wantObservedResourceIndex,
		}

		if diff := cmp.Diff(crp.Status, wantStatus, crpWithStuckRolloutStatusCmpOptions...); diff != "" {
			return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
		}
		return nil
	}
}

func crpWithStuckRolloutDueToOneFailedApplyOpStatusUpdatedActual(
	crpName string,
	wantSelectedResourceIdentifiers []placementv1beta1.ResourceIdentifier,
	failedWorkloadResourceIdentifier placementv1beta1.ResourceIdentifier,
	failedResourceObservedGeneration int64,
	wantSelectedClusters []string,
	wantObservedResourceIndex string,
) func() error {
	return func() error {
		crp := &placementv1beta1.ClusterResourcePlacement{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return err
		}

		var wantPlacementStatus []placementv1beta1.ResourcePlacementStatus
		// We only expect the deployment to not be available on one cluster.
		unavailableResourcePlacementStatus := placementv1beta1.ResourcePlacementStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.ScheduleSucceededReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourceRolloutStartedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourceOverriddenConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.OverrideNotSpecifiedReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourceWorkSynchronizedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkSyncedReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourcesAppliedConditionType),
					Status:             metav1.ConditionFalse,
					Reason:             condition.WorkNotAppliedReason,
					ObservedGeneration: crp.Generation,
				},
			},
			FailedPlacements: []placementv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: failedWorkloadResourceIdentifier,
					Condition: metav1.Condition{
						Type:   string(placementv1beta1.ResourcesAppliedConditionType),
						Status: metav1.ConditionFalse,
						// The new and old applier uses the same reason string to make things
						// a bit easier.
						Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToApply),
						ObservedGeneration: failedResourceObservedGeneration,
					},
				},
			},
		}
		wantPlacementStatus = append(wantPlacementStatus, unavailableResourcePlacementStatus)

		// For all the other connected member clusters rollout will be blocked.
		rolloutBlockedPlacementStatus := placementv1beta1.ResourcePlacementStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.ScheduleSucceededReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourceRolloutStartedConditionType),
					Status:             metav1.ConditionFalse,
					Reason:             condition.RolloutNotStartedYetReason,
					ObservedGeneration: crp.Generation,
				},
			},
		}

		for i := 0; i < len(wantSelectedClusters)-1; i++ {
			wantPlacementStatus = append(wantPlacementStatus, rolloutBlockedPlacementStatus)
		}

		wantCRPConditions := []metav1.Condition{
			{
				Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
				Status:             metav1.ConditionTrue,
				Reason:             scheduler.FullyScheduledReason,
				ObservedGeneration: crp.Generation,
			},
			{
				Type:               string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
				Status:             metav1.ConditionFalse,
				Reason:             condition.RolloutNotStartedYetReason,
				ObservedGeneration: crp.Generation,
			},
		}

		wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
			Conditions:            wantCRPConditions,
			PlacementStatuses:     wantPlacementStatus,
			SelectedResources:     wantSelectedResourceIdentifiers,
			ObservedResourceIndex: wantObservedResourceIndex,
		}

		if diff := cmp.Diff(crp.Status, wantStatus, crpWithStuckRolloutStatusCmpOptions...); diff != "" {
			return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
		}
		return nil
	}
}

func crpWithStuckRolloutDueToUntrackableResourcesStatusUpdatedActual(
	crpName string,
	wantSelectedResourceIdentifiers []placementv1beta1.ResourceIdentifier,
	wantSelectedClusters []string,
	wantObservedResourceIndex string,
) func() error {
	return func() error {
		crp := &placementv1beta1.ClusterResourcePlacement{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return err
		}

		var wantPlacementStatus []placementv1beta1.ResourcePlacementStatus
		// For all the other connected member clusters rollout will be blocked.
		rolloutBlockedPlacementStatus := placementv1beta1.ResourcePlacementStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.ScheduleSucceededReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ResourceRolloutStartedConditionType),
					Status:             metav1.ConditionFalse,
					Reason:             condition.RolloutNotStartedYetReason,
					ObservedGeneration: crp.Generation,
				},
			},
		}

		for i := 0; i < len(wantSelectedClusters); i++ {
			wantPlacementStatus = append(wantPlacementStatus, rolloutBlockedPlacementStatus)
		}

		wantCRPConditions := []metav1.Condition{
			{
				Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
				Status:             metav1.ConditionTrue,
				Reason:             scheduler.FullyScheduledReason,
				ObservedGeneration: crp.Generation,
			},
			{
				Type:               string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
				Status:             metav1.ConditionFalse,
				Reason:             condition.RolloutNotStartedYetReason,
				ObservedGeneration: crp.Generation,
			},
		}

		wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
			Conditions:            wantCRPConditions,
			PlacementStatuses:     wantPlacementStatus,
			SelectedResources:     wantSelectedResourceIdentifiers,
			ObservedResourceIndex: wantObservedResourceIndex,
		}

		if diff := cmp.Diff(crp.Status, wantStatus, crpWithStuckRolloutStatusCmpOptions...); diff != "" {
			return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
		}
		return nil
	}
}

func validateWorkNamespaceOnCluster(cluster *framework.Cluster, name types.NamespacedName) error {
	ns := &corev1.Namespace{}
	if err := cluster.KubeClient.Get(ctx, name, ns); err != nil {
		return err
	}

	// Use the object created in the hub cluster as reference; this helps to avoid the trouble
	// of having to ignore default fields in the spec.
	wantNS := &corev1.Namespace{}
	if err := hubClient.Get(ctx, name, wantNS); err != nil {
		return err
	}

	if diff := cmp.Diff(
		ns, wantNS,
		ignoreNamespaceStatusField,
		ignoreObjectMetaAutoGeneratedFields,
		ignoreObjectMetaAnnotationField,
	); diff != "" {
		return fmt.Errorf("work namespace diff (-got, +want): %s", diff)
	}
	return nil
}

func validateConfigMapOnCluster(cluster *framework.Cluster, name types.NamespacedName) error {
	configMap := &corev1.ConfigMap{}
	if err := cluster.KubeClient.Get(ctx, name, configMap); err != nil {
		return err
	}

	// Use the object created in the hub cluster as reference.
	wantConfigMap := &corev1.ConfigMap{}
	if err := hubClient.Get(ctx, name, wantConfigMap); err != nil {
		return err
	}

	if diff := cmp.Diff(
		configMap, wantConfigMap,
		ignoreObjectMetaAutoGeneratedFields,
		ignoreObjectMetaAnnotationField,
	); diff != "" {
		return fmt.Errorf("app config map diff (-got, +want): %s", diff)
	}

	return nil
}

func validateJobOnCluster(cluster *framework.Cluster, name types.NamespacedName) error {
	job := &batchv1.Job{}
	if err := cluster.KubeClient.Get(ctx, name, job); err != nil {
		return err
	}

	// Use the object created in the hub cluster as reference.
	wantJob := &batchv1.Job{}
	if err := hubClient.Get(ctx, name, wantJob); err != nil {
		return err
	}

	if diff := cmp.Diff(
		job, wantJob,
		ignoreObjectMetaAutoGeneratedFields,
		ignoreObjectMetaAnnotationField,
		ignoreJobSpecSelectorField,
		ignorePodTemplateSpecObjectMetaField,
		ignoreJobStatusField,
	); diff != "" {
		return fmt.Errorf("job diff (-got, +want): %s", diff)
	}

	return nil
}

func validateServiceOnCluster(cluster *framework.Cluster, name types.NamespacedName) error {
	service := &corev1.Service{}
	if err := cluster.KubeClient.Get(ctx, name, service); err != nil {
		return err
	}

	// Use the object created in the hub cluster as reference.
	wantService := &corev1.Service{}
	if err := hubClient.Get(ctx, name, wantService); err != nil {
		return err
	}

	if diff := cmp.Diff(
		service, wantService,
		ignoreObjectMetaAutoGeneratedFields,
		ignoreObjectMetaAnnotationField,
		ignoreServiceStatusField,
		ignoreServiceSpecIPAndPolicyFields,
		ignoreServicePortNodePortProtocolField,
	); diff != "" {
		return fmt.Errorf("service diff (-got, +want): %s", diff)
	}

	return nil
}

func workNamespaceAndConfigMapPlacedOnClusterActual(cluster *framework.Cluster, workNamespaceName, appConfigMapName string) func() error {
	return func() error {
		if err := validateWorkNamespaceOnCluster(cluster, types.NamespacedName{Name: workNamespaceName}); err != nil {
			return err
		}

		return validateConfigMapOnCluster(cluster, types.NamespacedName{Namespace: workNamespaceName, Name: appConfigMapName})
	}
}
