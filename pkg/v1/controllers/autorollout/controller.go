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

// Package autorollout features the KubeFleet auto-rollout controller for automatically rolling out resource
// changes to placement policies.
package autorollout

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	parallelizerutil "github.com/kubefleet-dev/kubefleet/pkg/utils/parallelizer"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/managers/placementresourcesnapshot"
)

const (
	controllerName = "auto-rollout"

	requeueImmediatelyAfterPeriod = time.Millisecond * 200
)

const (
	autoRolloutMinimumWaitPeriodLowerBound = time.Minute
	autoRolloutMinimumWaitPeriodUpperBound = 1 * time.Hour
	autoRolloutDefaultMinimumWaitPeriod    = 3 * time.Minute
)

const (
	bindingManagerClaimRateLimiterBaseDelay = time.Second * 5
	bindingManagerClaimRateLimiterMaxDelay  = time.Second * 180
)

// Reconciler helps reconcile placement policies with auto-rollout enabled.
type Reconciler struct {
	hubClient client.Client

	placementResourceSnapshotManager *placementresourcesnapshot.Manager

	parallelizer parallelizerutil.Parallelizer

	bindingManagerClaimRateLimiter workqueue.TypedRateLimiter[ctrl.Request]

	eventRecorder events.EventRecorder
}

// New creates a Reconciler.
//
// If no parallelizer or binding manager claim rate limiter is provided, the defaults are used. The hub client,
// the placement resource snapshot manager, and the event recorder are required.
func New(
	hubClient client.Client,
	placementResourceSnapshotManager *placementresourcesnapshot.Manager,
	parallelizer parallelizerutil.Parallelizer,
	bindingManagerClaimRateLimiter workqueue.TypedRateLimiter[ctrl.Request],
	eventRecorder events.EventRecorder,
) (*Reconciler, error) {
	if hubClient == nil {
		return nil, errors.NewUnexpectedError(nil, "hub client is nil")
	}
	if placementResourceSnapshotManager == nil {
		return nil, errors.NewUnexpectedError(nil, "placement resource snapshot manager is nil")
	}
	if eventRecorder == nil {
		return nil, errors.NewUnexpectedError(nil, "event recorder is nil")
	}
	if parallelizer == nil {
		parallelizer = parallelizerutil.NewParallelizer(parallelizerutil.DefaultNumOfWorkers)
	}
	if bindingManagerClaimRateLimiter == nil {
		bindingManagerClaimRateLimiter = workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](bindingManagerClaimRateLimiterBaseDelay, bindingManagerClaimRateLimiterMaxDelay)
	}

	return &Reconciler{
		hubClient:                        hubClient,
		placementResourceSnapshotManager: placementResourceSnapshotManager,
		parallelizer:                     parallelizer,
		bindingManagerClaimRateLimiter:   bindingManagerClaimRateLimiter,
		eventRecorder:                    eventRecorder,
	}, nil
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "placementPolicy", req.NamespacedName, "controller", controllerName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "placementPolicy", req.NamespacedName, "latency", latency, "controller", controllerName)
	}()

	// Retrieve the placement policy (or cluster placement policy) resource and return its accessor interface.
	placementPolicy, err := r.retrievePlacementPolicy(ctx, req.NamespacedName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The placement policy cannot be found; there is nothing to roll out.
			r.bindingManagerClaimRateLimiter.Forget(req)
			return ctrl.Result{}, nil
		}

		wrappedErr := errors.Wraps(err, "", "placementPolicy", req.NamespacedName, "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to retrieve placement policy object", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}
	klog.V(2).InfoS("Retrieved placement policy", "placementPolicy", klog.KObj(placementPolicy), "controller", controllerName)

	// Give up the binding manager role (if currently held) and skip reconciliation if the placement policy is
	// marked for deletion.
	if !placementPolicy.GetDeletionTimestamp().IsZero() {
		r.bindingManagerClaimRateLimiter.Forget(req)
		klog.V(2).InfoS("Placement policy is marked for deletion, relinquishing the binding manager role and skipping reconciliation", "placementPolicy", klog.KObj(placementPolicy), "controller", controllerName)
		// Note that the event handler of this controller guarantees that the controller will always see
		// any binding manager role it has claimed.
		if holdsBindingManagerRole(placementPolicy) {
			if err := r.relinquishBindingManagerRole(ctx, placementPolicy); err != nil {
				// The error has been wrapped and logged in the relinquish method.
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Skip processing if auto-rollout is not enabled, no rollout is needed, or the minimum wait period since the
	// last rollout has not passed. Give up the binding manager role (if currently held) in this case.
	minWaitPeriod := retrieveMinimumWaitPeriod(placementPolicy)
	shouldSkip, requeueAfter := shouldSkipProcessing(placementPolicy, minWaitPeriod)
	if shouldSkip {
		r.bindingManagerClaimRateLimiter.Forget(req)
		if holdsBindingManagerRole(placementPolicy) {
			// Note that the event handler of this controller guarantees that the controller will always see
			// any binding manager role it has claimed.
			if err := r.relinquishBindingManagerRole(ctx, placementPolicy); err != nil {
				// The error has been wrapped and logged in the relinquish method.
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	// Claim the binding manager role for the placement policy.
	// This ensures that no other controllers (e.g., the scheduling process, the migrations, etc.) can
	// interfere with the rollout process.
	claimed, err := r.claimAsBindingManagerFor(ctx, placementPolicy)
	if err != nil {
		// The error has been wrapped and logged in the claim method.
		return ctrl.Result{}, err
	}
	if !claimed {
		backoffDuration := r.bindingManagerClaimRateLimiter.When(req)
		return ctrl.Result{RequeueAfter: backoffDuration}, nil
	}
	// Reset the binding manager claim rate limiter for the placement policy.
	r.bindingManagerClaimRateLimiter.Forget(req)

	if err := r.rollout(ctx, placementPolicy); err != nil {
		wrappedErr := errors.Wraps(err, "", "placementPolicy", klog.KObj(placementPolicy))
		klog.ErrorS(wrappedErr, "Failed to rollout for the placement policy", errors.Args(wrappedErr)...)

		if errors.Category(wrappedErr) == errors.ErrCategoryUser {
			// The snapshotting failed due to a user error, which retries cannot fix; try again at the next rollout
			// attempt, i.e., once the minimum wait period passes.
			r.eventRecorder.Eventf(placementPolicy, nil, corev1.EventTypeWarning, "FailedToSnapshotResourcesDuringAutoRollout", "SnapshotResources",
				"Failed to complete auto-rollout for the placement policy due to a snapshotting error: %v", wrappedErr)

			// Bookkeep the last rollout attempt timestamp for the failed attempt as well. The claim of the binding
			// manager role above is a status update, which makes the event handler enqueue the placement policy again;
			// without the timestamp, the minimum wait period would not hold back that reconciliation, and the controller
			// would claim the role, fail, and relinquish it over and over.
			//
			// The timestamp is written before the role is relinquished, so that the wait period holds back retries
			// even if relinquishing fails.
			if err := r.bookkeepLastRolloutAttemptTimestamp(ctx, placementPolicy, time.Now()); err != nil {
				wrappedErr := errors.Wraps(err, "", "placementPolicy", klog.KObj(placementPolicy), "controller", controllerName)
				klog.ErrorS(wrappedErr, "Failed to bookkeep the last rollout attempt timestamp on the placement policy for a failed rollout", errors.Args(wrappedErr)...)
				return ctrl.Result{}, wrappedErr
			}
			if err := r.relinquishBindingManagerRole(ctx, placementPolicy); err != nil {
				// The error has been wrapped and logged in the relinquish method.
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: minWaitPeriod}, nil
		}
		return ctrl.Result{}, wrappedErr
	}

	// Bookkeep the last rollout attempt timestamp on the placement policy.
	if err := r.bookkeepLastRolloutAttemptTimestamp(ctx, placementPolicy, time.Now()); err != nil {
		wrappedErr := errors.Wraps(err, "", "placementPolicy", klog.KObj(placementPolicy), "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to bookkeep the last rollout attempt timestamp on the placement policy", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	klog.V(2).InfoS("Successfully rolled out changes to all bindings of the placement policy",
		"placementPolicy", klog.KObj(placementPolicy), "controller", controllerName)
	// The auto rollout has been completed. Requeue it right away to perform the cleanup steps.
	return ctrl.Result{RequeueAfter: requeueImmediatelyAfterPeriod}, nil
}

// SetupWithManager sets up the controller with the manager.
//
// The controller watches both ClusterPlacementPolicy (cluster-scoped) and PlacementPolicy (namespace-scoped)
// objects, funneling events for either kind into the same reconcile queue; Reconcile (via retrievePlacementPolicy)
// tells the two apart by whether the incoming request carries a namespace.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrentReconciles int) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		Watches(&placementv1alpha1.ClusterPlacementPolicy{}, placementPolicyEventHandlerFuncs).
		Watches(&placementv1alpha1.PlacementPolicy{}, placementPolicyEventHandlerFuncs).
		Complete(r)
}
