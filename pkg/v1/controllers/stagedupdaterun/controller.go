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

package stagedupdaterun

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/parallelizer"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/managers/placementresourcesnapshot"
)

const (
	controllerName = "staged-update-run"

	requeueImmediatelyAfterPeriod = time.Millisecond * 200
)

const (
	minRefreshPeriod = time.Second * 30

	bindingManagerClaimRateLimiterBaseDelay      = time.Second * 5
	bindingManagerClaimRateLimiterMaxDelay       = time.Second * 180
	bindingAvailabilityCheckRateLimiterBaseDelay = time.Second * 5
	bindingAvailabilityCheckRateLimiterMaxDelay  = time.Second * 180
)

// Reconciler helps reconcile stage update run objects.
type Reconciler struct {
	hubClient         client.Client
	hubUncachedClient client.Reader

	placementResourceSnapshotManager *placementresourcesnapshot.Manager

	parallelizer parallelizer.Parallelizer

	bindingManagerClaimRateLimiter      workqueue.TypedRateLimiter[ctrl.Request]
	bindingAvailabilityCheckRateLimiter workqueue.TypedRateLimiter[types.NamespacedName]

	refreshPeriod time.Duration
}

// New creates a Reconciler.
func New(
	hubClient client.Client,
	hubUncachedClient client.Reader,
	placementResourceSnapshotManager *placementresourcesnapshot.Manager,
	parallelizer parallelizer.Parallelizer,
	bindingManagerClaimRateLimiter workqueue.TypedRateLimiter[ctrl.Request],
	bindingAvailabilityCheckRateLimiter workqueue.TypedRateLimiter[types.NamespacedName],
	refreshPeriod time.Duration,
) (*Reconciler, error) {
	if bindingManagerClaimRateLimiter == nil {
		bindingManagerClaimRateLimiter = workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](bindingManagerClaimRateLimiterBaseDelay, bindingManagerClaimRateLimiterMaxDelay)
	}
	if bindingAvailabilityCheckRateLimiter == nil {
		bindingAvailabilityCheckRateLimiter = workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](bindingAvailabilityCheckRateLimiterBaseDelay, bindingAvailabilityCheckRateLimiterMaxDelay)
	}

	if refreshPeriod < minRefreshPeriod {
		return nil, errors.NewUnexpectedError(nil, "refresh period is too short", "minRefreshPeriod", minRefreshPeriod, "observedRefreshPeriod", refreshPeriod)
	}

	return &Reconciler{
		hubClient:                           hubClient,
		hubUncachedClient:                   hubUncachedClient,
		placementResourceSnapshotManager:    placementResourceSnapshotManager,
		parallelizer:                        parallelizer,
		bindingManagerClaimRateLimiter:      bindingManagerClaimRateLimiter,
		bindingAvailabilityCheckRateLimiter: bindingAvailabilityCheckRateLimiter,
		refreshPeriod:                       refreshPeriod,
	}, nil
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "stagedUpdateRun", req.NamespacedName, "controller", controllerName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "stagedUpdateRun", req.NamespacedName, "latency", latency, "controller", controllerName)
	}()

	// Retrieve the StagedUpdateRun or ClusterStagedUpdateRun resource and return it as a StagedUpdateRunAccessor interface.
	stagedUpdateRun, err := r.retrieveStagedUpdateRun(ctx, req.NamespacedName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The staged update run cannot be found.
			return ctrl.Result{}, nil
		}

		wrappedErr := errors.Wraps(err, "", "stagedUpdateRun", req.NamespacedName, "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to retrieve staged update run object", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Clean things up if the staged update run object has been marked for deletion.
	if !stagedUpdateRun.GetDeletionTimestamp().IsZero() {
		if err := r.cleanup(ctx, stagedUpdateRun); err != nil {
			wrappedErr := errors.Wraps(err, "", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
			klog.ErrorS(wrappedErr, "Failed to cleanup staged update run", errors.Args(wrappedErr)...)
			return ctrl.Result{}, wrappedErr
		}
		return ctrl.Result{}, nil
	}

	shouldSkip, err := r.shouldSkipProcessing(ctx, stagedUpdateRun)
	if err != nil {
		wrappedErr := errors.Wraps(err, "", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to determine if staged update run should be skipped", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}
	if shouldSkip {
		// The staged update run is already in a terminal state (failed to initialize, complete), or has been
		// suspended. Give up the binding manager role and drop the cleanup finalizer.
		if err := r.cleanup(ctx, stagedUpdateRun); err != nil {
			wrappedErr := errors.Wraps(err, "", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
			klog.ErrorS(wrappedErr, "Failed to cleanup staged update run", errors.Args(wrappedErr)...)
			return ctrl.Result{}, wrappedErr
		}
		return ctrl.Result{}, nil
	}

	// Add a cleanup finalizer to the staged update run object if it doesn't have one.
	if err := r.ensureCleanupFinalizer(ctx, stagedUpdateRun); err != nil {
		wrappedErr := errors.Wraps(err, "", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to ensure cleanup finalizer on staged update run object", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Retrieve the corresponding (cluster) placement policy resource and return it as a placement policy accessor.
	placementPolicy, err := r.retrieveLinkedPlacementPolicy(ctx, stagedUpdateRun)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The linked placement policy cannot be found. No need to process the staged update run any more.
			msg := "No linked placement policy"
			if err := r.markStagedUpdateRunAsInTerminalState(ctx, stagedUpdateRun, msg); err != nil {
				wrappedErr := errors.Wraps(err, "", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
				klog.ErrorS(wrappedErr, "Failed to mark staged update run as in terminal state", errors.Args(wrappedErr)...)
				return ctrl.Result{}, wrappedErr
			}
			// Requeue to handle the terminal state update.
			return ctrl.Result{RequeueAfter: requeueImmediatelyAfterPeriod}, nil
		}
		wrappedErr := errors.Wraps(err, "failed to retrieve the linked placement policy", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to retrieve the linked placement policy", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Claim the staged update run object as the binding manager for the placement policy.
	// This ensures that no other controllers (e.g., the scheduling process, the migrations, etc.) can
	// interfere with the rollout process.
	claimed, err := r.claimAsBindingManagerFor(ctx, placementPolicy, stagedUpdateRun)
	if err != nil {
		wrappedErr := errors.Wraps(err, "", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to claim binding manager role for staged update run", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}
	if !claimed {
		backoffDuration := r.bindingManagerClaimRateLimiter.When(req)
		return ctrl.Result{RequeueAfter: backoffDuration}, nil
	}
	// Reset the binding manager claim rate limiter for the staged update run object.
	r.bindingManagerClaimRateLimiter.Forget(req)

	// Initialize the staged update run (if applicable).
	if err := r.initialize(ctx, placementPolicy, stagedUpdateRun); err != nil {
		if errors.Category(err) == errors.ErrCategoryUser {
			// A user error has occurred during initialization; consider the initialization step failed.
			msg := fmt.Sprintf("failed to initialize staged update run: %v", err)
			if err := r.markStagedUpdateRunAsFailedToInitialize(ctx, stagedUpdateRun, msg); err != nil {
				wrappedErr := errors.Wraps(err, "", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
				klog.ErrorS(wrappedErr, "Failed to mark staged update run as failed to initialize", errors.Args(wrappedErr)...)
				return ctrl.Result{}, wrappedErr
			}
			return ctrl.Result{RequeueAfter: requeueImmediatelyAfterPeriod}, nil
		}
		wrappedErr := errors.Wraps(err, "", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to initialize staged update run", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Before starting processing the staged update run, verify if the run has been suspended and needs to be skipped.
	shouldSkip, err = r.shouldSkipProcessing(ctx, stagedUpdateRun)
	if err != nil {
		wrappedErr := errors.Wraps(err, "", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to determine if staged update run should be skipped", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}
	if shouldSkip {
		// The staged update run is already in a terminal state (failed to initialize, complete), or has been
		// suspended. Give up the binding manager role and drop the cleanup finalizer.
		if err := r.cleanup(ctx, stagedUpdateRun); err != nil {
			wrappedErr := errors.Wraps(err, "", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
			klog.ErrorS(wrappedErr, "Failed to cleanup staged update run", errors.Args(wrappedErr)...)
			return ctrl.Result{}, wrappedErr
		}
		return ctrl.Result{}, nil
	}

	// Process the staged update run.
	allStagesProcessingRes, requeueAfter, processingErr := r.process(ctx, stagedUpdateRun)
	klog.V(2).InfoS("Processed staged update run",
		"stagedUpdateRun", klog.KObj(stagedUpdateRun),
		"placementPolicyName", stagedUpdateRun.GetSpec().PlacementPolicyName,
		"controller", controllerName,
		"allStagesProcessingRes", allStagesProcessingRes,
		"err", processingErr)

	// Refresh the staged update run status.
	//
	// Note that this is done regardless of the processing results, so as to keep the users informed of the progress
	// so far.
	if updateErr := r.hubClient.Status().Update(ctx, stagedUpdateRun); updateErr != nil {
		wrappedErr := errors.NewAPIServerError(updateErr, "", false,
			"stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to update staged update run status", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Handle processing errors and requeues accordingly.
	if processingErr != nil {
		wrappedErr := errors.Wraps(processingErr, "", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to process staged update run", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}
	switch allStagesProcessingRes {
	case allStagesProcessingResultSucceeded, allStagesProcessingResultFailed:
		// The staged update run has been completed. Requeue it to perform the cleanup steps.
		return ctrl.Result{RequeueAfter: requeueImmediatelyAfterPeriod}, nil
	case allStagesProcessingResultInProgress:
		// Requeue after the specified duration.
		if requeueAfter != nil {
			return ctrl.Result{RequeueAfter: *requeueAfter}, nil
		}
		return ctrl.Result{RequeueAfter: requeueImmediatelyAfterPeriod}, nil
	default:
		// An unexpected processing result is yielded; consider this as an unexpected error.
		wrappedErr := errors.NewUnexpectedError(nil, "encountered an unexpected processing result",
			"observedProcessingResult", allStagesProcessingRes,
			"stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		klog.ErrorS(wrappedErr, "", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}
}

// SetupWithManager sets up the controller with the manager.
//
// The controller watches both ClusterStagedUpdateRun (cluster-scoped) and StagedUpdateRun
// (namespace-scoped) objects, funneling events for either kind into the same reconcile queue;
// Reconcile (via retrieveStagedUpdateRun) tells the two apart by whether the incoming request
// carries a namespace.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrentReconciles int) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		Watches(&rolloutv1alpha1.ClusterStagedUpdateRun{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&rolloutv1alpha1.StagedUpdateRun{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&rolloutv1alpha1.ApprovalRequest{}, approvalReqEventHandlerFuncs).
		Watches(&rolloutv1alpha1.ClusterApprovalRequest{}, approvalReqEventHandlerFuncs).
		Complete(r)
}
