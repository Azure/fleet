/*
Copyright 2026 The KubeFleet Authors.

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

package autorollout

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

// retrieveMinimumWaitPeriod returns the minimum wait period between consecutive auto-rollouts for the placement policy.
//
// The default period is used if the annotation is absent or cannot be parsed; a period outside of the
// allowed range is clamped to the closest bound.
func retrieveMinimumWaitPeriod(placementPolicy placementv1alpha1.PlacementPolicyAccessor) time.Duration {
	val, found := placementPolicy.GetAnnotations()[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey]
	if !found {
		return autoRolloutDefaultMinimumWaitPeriod
	}
	period, err := time.ParseDuration(val)
	if err != nil {
		klog.V(2).InfoS("Failed to parse the minimum wait period annotation; using the default",
			"placementPolicy", klog.KObj(placementPolicy),
			"observedValue", val, "defaultValue", autoRolloutDefaultMinimumWaitPeriod, "err", err,
			"controller", controllerName)
		return autoRolloutDefaultMinimumWaitPeriod
	}
	return min(max(period, autoRolloutMinimumWaitPeriodLowerBound), autoRolloutMinimumWaitPeriodUpperBound)
}

// remainingWaitPeriod returns how much longer the controller must wait before it can roll out the placement policy
// again; zero or a negative value means that the minimum wait period has passed since the last rollout.
//
// A missing or malformed last rollout attempt timestamp is treated as if the placement policy has never been rolled
// out. A timestamp in the future (e.g., set by hand, or written by a controller instance with a skewed clock) is
// treated as if the placement policy has just been rolled out, i.e., the remaining wait period never exceeds the
// minimum wait period.
func remainingWaitPeriod(placementPolicy placementv1alpha1.PlacementPolicyAccessor, minWaitPeriod time.Duration, now time.Time) time.Duration {
	val, found := placementPolicy.GetAnnotations()[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey]
	if !found {
		return 0
	}
	lastRolloutAttempt, err := time.Parse(time.RFC3339, val)
	if err != nil {
		klog.V(2).InfoS("Failed to parse the last rollout attempt timestamp annotation; treating the placement policy as never rolled out",
			"placementPolicy", klog.KObj(placementPolicy),
			"observedValue", val, "err", err,
			"controller", controllerName)
		return 0
	}
	return min(minWaitPeriod-now.Sub(lastRolloutAttempt), minWaitPeriod)
}

// isAutoRolloutEnabled returns true if auto-rollout is enabled for the placement policy.
func isAutoRolloutEnabled(placementPolicy placementv1alpha1.PlacementPolicyAccessor) bool {
	return placementPolicy.GetAnnotations()[rolloutv1alpha1.AutoRolloutEnabledAnnotationKey] == rolloutv1alpha1.AutoRolloutEnabledAnnotationValue
}

// shouldSkipProcessing checks if the placement policy should not be processed for an auto-rollout right now, which
// is the case if auto-rollout is not enabled, no rollout is needed, or the minimum wait period since the last
// rollout has not passed.
//
// If the processing should be skipped, a non-zero requeue-after duration is returned when the controller should come
// back to the placement policy after that duration (i.e., when only the minimum wait period is holding processing back).
func shouldSkipProcessing(placementPolicy placementv1alpha1.PlacementPolicyAccessor, minWaitPeriod time.Duration) (skip bool, requeueAfter time.Duration) {
	if !isAutoRolloutEnabled(placementPolicy) {
		klog.V(2).InfoS("Auto-rollout is not enabled for the placement policy, skipping processing",
			"placementPolicy", klog.KObj(placementPolicy), "controller", controllerName)
		return true, 0
	}

	if !isRolloutNeeded(placementPolicy) {
		klog.V(2).InfoS("No rollout is needed for the placement policy, skipping processing",
			"placementPolicy", klog.KObj(placementPolicy), "controller", controllerName)
		return true, 0
	}

	if remaining := remainingWaitPeriod(placementPolicy, minWaitPeriod, time.Now()); remaining > 0 {
		klog.V(2).InfoS("The minimum wait period since the last rollout has not passed, skipping processing",
			"placementPolicy", klog.KObj(placementPolicy), "minimumWaitPeriod", minWaitPeriod, "remaining", remaining, "controller", controllerName)
		return true, remaining
	}
	return false, 0
}

// isRolloutNeeded returns true if the placement policy has collected all the resources for its latest
// generation and has changed resources pending rollout or clusters still on an older resource revision.
// ResourceCollected must be true and Synchronized must be false for the latest generation.
func isRolloutNeeded(placementPolicy placementv1alpha1.PlacementPolicyAccessor) bool {
	conditions := placementPolicy.GetStatus().Conditions
	generation := placementPolicy.GetGeneration()

	resourceCollectedCond := meta.FindStatusCondition(conditions, placementv1alpha1.PlacementPolicyCondTypeResourceCollected)
	synchronizedCond := meta.FindStatusCondition(conditions, placementv1alpha1.PlacementPolicyCondTypeSynchronized)
	return condition.IsConditionStatusTrue(resourceCollectedCond, generation) &&
		condition.IsConditionStatusFalse(synchronizedCond, generation) &&
		(synchronizedCond.Reason == placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout ||
			synchronizedCond.Reason == placementv1alpha1.PlacementPolicySynchronizedCondReasonSomeClustersOnOlderResourceRevision)
}

// bookkeepLastRolloutAttemptTimestamp writes the current time to the last rollout attempt timestamp annotation of the
// placement policy, after a successful rollout or one that failed due to a user error.
//
// The annotation is written with a JSON merge patch that carries only this annotation (and no resource version),
// so that the write does not conflict with, or overwrite, changes made to the placement policy since it was
// retrieved; in particular, the status updates that the placement policy controller makes in reaction to the
// binding refreshes of the rollout.
func (r *Reconciler) bookkeepLastRolloutAttemptTimestamp(ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor, now time.Time) error {
	// Take the copy before the annotations are modified, as the annotation map is shared with the object.
	patch := client.MergeFrom(placementPolicy.DeepCopyObject().(placementv1alpha1.PlacementPolicyAccessor))

	annotations := placementPolicy.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}
	annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = now.UTC().Format(time.RFC3339Nano)
	placementPolicy.SetAnnotations(annotations)

	if err := r.hubClient.Patch(ctx, placementPolicy, patch); err != nil {
		return errors.NewAPIServerError(err, "failed to patch the last rollout attempt timestamp annotation", false,
			"placementPolicy", klog.KObj(placementPolicy))
	}
	return nil
}

func (r *Reconciler) rollout(ctx context.Context, placementPolicy placementv1alpha1.PlacementPolicyAccessor) error {
	// List all the bindings created for the placement policy.
	placementBindings, err := r.listAllPlacementBindings(ctx, placementPolicy)
	if err != nil {
		return err
	}
	klog.V(2).InfoS("Listed placement bindings",
		"placementPolicy", klog.KObj(placementPolicy), "bindingCount", len(placementBindings), "controller", controllerName)

	// Request a new placement resource snapshot for the placement policy.
	//
	// The snapshot manager returns the latest snapshot as it is if it is still up-to-date, or creates a new one
	// if the latest snapshot has become stale.
	resourceSnapshots, _, err := r.placementResourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
	if err != nil {
		return errors.Wraps(err, "failed to retrieve or create the latest resource snapshot")
	}
	if len(resourceSnapshots) == 0 {
		return errors.NewUnexpectedError(nil, "no resource snapshots returned")
	}
	// The first snapshot returned is always the primary snapshot.
	latestPrimarySnapshot := resourceSnapshots[0]
	klog.V(2).InfoS("Retrieved the latest placement resource snapshot",
		"placementPolicy", klog.KObj(placementPolicy), "resourceSnapshot", klog.KObj(latestPrimarySnapshot), "controller", controllerName)

	// Refresh the bindings in parallel, so that they all point to the latest primary resource snapshot.
	if err := r.refreshBindings(ctx, placementBindings, latestPrimarySnapshot.GetName()); err != nil {
		return errors.Wraps(err, "failed to refresh the placement bindings with the latest resource snapshot")
	}
	return nil
}

// refreshBindings updates, in parallel, all the placement bindings that do not point to the given primary resource
// snapshot so that they do.
func (r *Reconciler) refreshBindings(
	ctx context.Context,
	placementBindings []placementv1alpha1.PlacementBindingAccessor,
	wantResourceSnapshotName string,
) error {
	// Prepare a child context.
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	// Pre-allocate the slice to avoid contention.
	errs := make([]error, len(placementBindings))
	doWork := func(piece int) {
		binding := placementBindings[piece]
		if binding.GetSpec().ResourceSnapshotName == wantResourceSnapshotName {
			// The binding is already up-to-date; skip it.
			return
		}

		binding.GetSpec().ResourceSnapshotName = wantResourceSnapshotName
		if err := r.hubClient.Update(childCtx, binding); err != nil {
			errs[piece] = errors.NewAPIServerError(err,
				"failed to update the placement binding with the latest resource snapshot", false,
				"placementBinding", klog.KObj(binding), "resourceSnapshotName", wantResourceSnapshotName)
			// Fail fast.
			childCancel()
			return
		}
		klog.V(2).InfoS("Refreshed the placement binding with the latest resource snapshot",
			"placementBinding", klog.KObj(binding), "resourceSnapshotName", wantResourceSnapshotName, "controller", controllerName)
	}
	r.parallelizer.ParallelizeUntil(childCtx, len(placementBindings), doWork, "refreshBindings")

	if err := utilerrors.NewAggregate(errs); err != nil {
		return errors.Wraps(err, "aggregated errors occurred when refreshing placement bindings")
	}
	// Return the context error (nil if the context has not been cancelled) here to avoid silently skipping
	// bindings when the parent context is cancelled.
	return ctx.Err()
}
