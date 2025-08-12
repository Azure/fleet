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

// Package scheduler features the scheduler for Fleet workloads.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/metrics"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// Scheduler is the scheduler for Fleet workloads.
type Scheduler struct {
	// name is the name of the scheduler.
	name string

	// framework is the scheduling framework in use by the scheduler.
	//
	// At this stage, a scheduler is always associated with one scheduling framework; in the long
	// run, multiple frameworks may be supported, to allow the usage of varying scheduling configurations
	// for different types of workloads.
	framework framework.Framework

	// queue is the work queue in use by the scheduler; the scheduler pulls items from the queue and
	// performs scheduling in accordance with them.
	queue queue.PlacementSchedulingQueue

	// client is the (cached) client in use by the scheduler for accessing Kubernetes API server.
	client client.Client

	// uncachedReader is the uncached read-only client in use by the scheduler for accessing
	// Kubernetes API server; in most cases client should be used instead, unless consistency becomes
	// a serious concern.
	//
	// TO-DO (chenyu1): explore the possibilities of using a mutation cache for better performance.
	uncachedReader client.Reader

	// manager is the controller manager in use by the scheduler.
	manager ctrl.Manager

	// workerNumber is number of scheduling loop we will run concurrently
	workerNumber int

	// eventRecorder is the event recorder in use by the scheduler.
	eventRecorder record.EventRecorder
}

// NewScheduler creates a scheduler.
func NewScheduler(
	name string,
	framework framework.Framework,
	queue queue.PlacementSchedulingQueue,
	manager ctrl.Manager,
	workerNumber int,
) *Scheduler {
	return &Scheduler{
		name:           name,
		framework:      framework,
		queue:          queue,
		client:         manager.GetClient(),
		uncachedReader: manager.GetAPIReader(),
		manager:        manager,
		workerNumber:   workerNumber,
		eventRecorder:  manager.GetEventRecorderFor(name),
	}
}

// ScheduleOnce performs scheduling for one single item pulled from the work queue.
// it returns true if the context is not canceled, false otherwise.
func (s *Scheduler) scheduleOnce(ctx context.Context, worker int) {
	// Retrieve the next item (name of a placement) from the work queue.
	//
	// Note that this will block if no item is available.
	placementKey, closed := s.queue.NextPlacementKey()
	if closed {
		// End the run immediately if the work queue has been closed.
		klog.InfoS("Work queue has been closed")
		return
	}

	defer func() {
		// Mark the key as done.
		//
		// Note that this will happen even if an error occurs. Should the key get requeued by Add()
		// during the call, it will be added to the queue after this call returns.
		s.queue.Done(placementKey)
	}()

	// keep track of the number of active scheduling loop
	metrics.SchedulerActiveWorkers.WithLabelValues().Add(1)
	defer metrics.SchedulerActiveWorkers.WithLabelValues().Add(-1)

	startTime := time.Now()
	klog.V(2).InfoS("Schedule once started", "placement", placementKey, "worker", worker)
	defer func() {
		// Note that the time spent on pulling keys from the work queue (and the time spent on waiting
		// for a key to arrive) is not counted here, as we cannot reliably distinguish between
		// system processing latencies and actual duration of placement absence.
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Schedule once completed", "placement", placementKey, "latency", latency, "worker", worker)
	}()

	// Retrieve the placement object (either ClusterResourcePlacement or ResourcePlacement).
	placement, err := controller.FetchPlacementFromKey(ctx, s.client, placementKey)
	if err != nil {
		if apiErrors.IsNotFound(err) {
			// The placement has been gone before the scheduler gets a chance to
			// process it; normally this would not happen as sources would not enqueue any placement that
			// has been marked for deletion but does not have the scheduler cleanup finalizer to
			// the work queue. Such placements needs no further processing any way though, as the absence
			// of the cleanup finalizer implies that bindings derived from the placement are no longer present.
			klog.ErrorS(err, "placement is already deleted", "placement", placementKey)
			return
		}
		if errors.Is(err, controller.ErrUnexpectedBehavior) {
			// The placement is in an unexpected state; this is a scheduler-side error, and
			// Note that this is a scheduler-side error, so it does not return an error to the caller.
			// Raise an alert for it.
			klog.ErrorS(err, "Placement is in an unexpected state", "placement", placementKey)
			return
		}
		// Wrap the error for metrics; this method does not return an error.
		klog.ErrorS(controller.NewAPIServerError(true, err), "Failed to get placement", "placement", placementKey)

		// Requeue for later processing.
		s.queue.AddRateLimited(placementKey)
		return
	}

	// Check if the placement has been marked for deletion, and if it has the scheduler cleanup finalizer.
	if placement.GetDeletionTimestamp() != nil {
		// Use SchedulerCleanupFinalizer consistently for all placement types
		if controllerutil.ContainsFinalizer(placement, fleetv1beta1.SchedulerCleanupFinalizer) {
			if err := s.cleanUpAllBindingsFor(ctx, placement); err != nil {
				klog.ErrorS(err, "Failed to clean up all bindings for placement", "placement", placementKey)
				if errors.Is(err, controller.ErrUnexpectedBehavior) {
					// The placement is in an unexpected state, don't requeue it.
					return
				}
				// Requeue for later processing.
				s.queue.AddRateLimited(placementKey)
				return
			}
		}
		// The placement has been marked for deletion but no longer has the scheduler cleanup finalizer; no
		// additional handling is needed.

		// Untrack the key from the rate limiter.
		s.queue.Forget(placementKey)
		return
	}

	// The placement has not been marked for deletion; run the scheduling cycle for it.

	// Verify that it has an active policy snapshot.
	latestPolicySnapshot, err := s.lookupLatestPolicySnapshot(ctx, placement)
	if err != nil {
		klog.ErrorS(err, "Failed to lookup latest policy snapshot", "placement", placementKey)
		// No requeue is needed; the scheduler will be triggered again when an active policy
		// snapshot is created.

		// Untrack the key for quicker reprocessing.
		s.queue.Forget(placementKey)
		return
	}

	// Add the scheduler cleanup finalizer to the placement (if it does not have one yet).
	if err := s.addSchedulerCleanUpFinalizer(ctx, placement); err != nil {
		klog.ErrorS(err, "Failed to add scheduler cleanup finalizer", "placement", placementKey)
		// Requeue for later processing.
		s.queue.AddRateLimited(placementKey)
		return
	}

	// Run the scheduling cycle.
	//
	// Note that the scheduler will enter this cycle as long as the placement is active and an active
	// policy snapshot has been produced.
	cycleStartTime := time.Now()
	res, err := s.framework.RunSchedulingCycleFor(ctx, placementKey, latestPolicySnapshot)
	if err != nil {
		if errors.Is(err, controller.ErrUnexpectedBehavior) {
			// The placement is in an unexpected state; this is a scheduler-side error, and
			// Note that this is a scheduler-side error, so it does not return an error to the caller.
			// Raise an alert for it.
			klog.ErrorS(err, "Placement is in an unexpected state", "placement", placementKey)
			observeSchedulingCycleMetrics(cycleStartTime, true, false)
			return
		}
		// Requeue for later processing.
		klog.ErrorS(err, "Failed to run scheduling cycle", "placement", placementKey)
		s.queue.AddRateLimited(placementKey)
		observeSchedulingCycleMetrics(cycleStartTime, true, true)
		return
	}

	// Requeue if the scheduling cycle suggests so.
	if res.Requeue {
		if res.RequeueAfter > 0 {
			s.queue.AddAfter(placementKey, res.RequeueAfter)
			observeSchedulingCycleMetrics(cycleStartTime, false, true)
			return
		}
		// Untrack the key from the rate limiter.
		s.queue.Forget(placementKey)
		// Requeue for later processing.
		//
		// Note that the key is added directly to the queue without having to wait for any rate limiter's
		// approval. This is necessary as requeues, requested by the scheduler, occur when the scheduler
		// is certain that more scheduling work needs to be done but it cannot be completed in
		// one cycle (e.g., a plugin sets up a per-cycle batch limit, and consequently the scheduler must
		// finish the scheduling in multiple cycles); in such cases, rate limiter should not add
		// any delay to the requeues.
		s.queue.Add(placementKey)
		observeSchedulingCycleMetrics(cycleStartTime, false, true)
	} else {
		// no more failure, the following queue don't need to be rate limited
		s.queue.Forget(placementKey)
		observeSchedulingCycleMetrics(cycleStartTime, false, false)
	}
}

// Run starts the scheduler.
//
// Note that this is a blocking call. It will only return when the context is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	klog.V(2).InfoS("Starting the scheduler")
	defer func() {
		klog.V(2).InfoS("Stopping the scheduler")
	}()

	// Starting the scheduling queue.
	s.queue.Run()

	wg := &sync.WaitGroup{}
	wg.Add(s.workerNumber)
	for i := 0; i < s.workerNumber; i++ {
		go func(index int) {
			defer wg.Done()
			defer utilruntime.HandleCrash()
			// Run scheduleOnce forever until context is cancelled
			for {
				select {
				case <-ctx.Done():
					return
				default:
					s.scheduleOnce(ctx, index)
				}
			}
		}(i)
	}

	// Wait for the context to be canceled.
	<-ctx.Done()
	// The loop starts in a dedicated goroutine; it exits when the context is canceled.
	wg.Wait()
	// Stopping the scheduling queue; drain if necessary.
	//
	// Note that if a scheduling cycle is in progress; this will only return when the
	// cycle completes.
	s.queue.CloseWithDrain()
}

// cleanUpAllBindingsFor cleans up all bindings derived from a placement.
func (s *Scheduler) cleanUpAllBindingsFor(ctx context.Context, placement fleetv1beta1.PlacementObj) error {
	placementRef := klog.KObj(placement)

	// List all bindings derived from the placement.
	//
	// Note that the listing is performed using the uncached client; this is to ensure that all related
	// bindings can be found, even if they have not been synced to the cache yet.
	// TO-DO (chenyu1): this is a very expensive op; explore options for optimization.
	bindings, err := controller.ListBindingsFromKey(ctx, s.uncachedReader, types.NamespacedName{Namespace: placement.GetNamespace(), Name: placement.GetName()})
	if err != nil {
		klog.ErrorS(err, "Failed to list all bindings", "placement", placementRef)
		return err
	}

	// Remove scheduler binding cleanup finalizer from deleting bindings.
	//
	// Note that once a placement has been marked for deletion, it will no longer enter the scheduling cycle,
	// so any cleanup finalizer has to be removed here.
	//
	// Also note that for deleted placements, derived bindings are deleted right away by the scheduler;
	// the scheduler no longer marks them as deleting and waits for another controller to actually
	// run the deletion.
	for idx := range bindings {
		binding := bindings[idx]
		controllerutil.RemoveFinalizer(binding, fleetv1beta1.SchedulerBindingCleanupFinalizer)
		if err := s.client.Update(ctx, binding); err != nil {
			klog.ErrorS(err, "Failed to remove scheduler reconcile finalizer from binding", "binding", klog.KObj(binding))
			return controller.NewUpdateIgnoreConflictError(err)
		}
		// Delete the binding if it has not been marked for deletion yet.
		if binding.GetDeletionTimestamp() == nil {
			if err := s.client.Delete(ctx, binding); err != nil && !apiErrors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to delete binding", "binding", klog.KObj(binding))
				return controller.NewAPIServerError(false, err)
			}
		}
	}

	// All bindings have been deleted; remove the scheduler cleanup finalizer from the placement.
	// Use SchedulerCleanupFinalizer consistently for all placement types.
	controllerutil.RemoveFinalizer(placement, fleetv1beta1.SchedulerCleanupFinalizer)
	if err := s.client.Update(ctx, placement); err != nil {
		klog.ErrorS(err, "Failed to remove scheduler cleanup finalizer from placement", "placement", placementRef)
		return controller.NewUpdateIgnoreConflictError(err)
	}

	return nil
}

// lookupLatestPolicySnapshot returns the latest (i.e., active) policy snapshot associated with a placement.
// TODO(ryan): move this to a common lib
func (s *Scheduler) lookupLatestPolicySnapshot(ctx context.Context, placement fleetv1beta1.PlacementObj) (fleetv1beta1.PolicySnapshotObj, error) {
	placementRef := klog.KObj(placement)
	// Prepare the list options to filter policy snapshots by the placement name and the latest snapshot label.
	var listOptions []client.ListOption
	labelSelector := labels.SelectorFromSet(labels.Set{
		fleetv1beta1.PlacementTrackingLabel: placement.GetName(),
		fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
	})
	listOptions = append(listOptions, &client.ListOptions{LabelSelector: labelSelector})
	// Find out the latest policy snapshot associated with the placement.
	var policySnapshotList fleetv1beta1.PolicySnapshotList
	if placement.GetNamespace() == "" {
		policySnapshotList = &fleetv1beta1.ClusterSchedulingPolicySnapshotList{}
	} else {
		policySnapshotList = &fleetv1beta1.SchedulingPolicySnapshotList{}
		listOptions = append(listOptions, client.InNamespace(placement.GetNamespace()))
	}

	// The scheduler lists with a cached client; this does not have any consistency concern as sources
	// will always trigger the scheduler when a new policy snapshot is created.
	if err := s.client.List(ctx, policySnapshotList, listOptions...); err != nil {
		klog.ErrorS(err, "Failed to list policy snapshots of a placement", "placement", placementRef)
		return nil, controller.NewAPIServerError(true, err)
	}
	policySnapshots := policySnapshotList.GetPolicySnapshotObjs()
	switch {
	case len(policySnapshots) == 0:
		// There is no latest policy snapshot associated with the placement; it could happen when
		// * the placement is newly created; or
		// * the new policy snapshot is in the middle of being replaced.
		//
		// Either way, it is out of the scheduler's scope to handle such a case; the scheduler will
		// be triggered again if the situation is corrected.
		err := fmt.Errorf("no latest policy snapshot associated with placement")
		klog.ErrorS(err, "Failed to find the latest policy snapshot, will retry", "placement", placementRef)
		return nil, err
	case len(policySnapshots) > 1:
		// There are multiple active policy snapshots associated with the placement; normally this
		// will never happen, as only one policy snapshot can be active in the sequence.
		//
		// Similarly, it is out of the scheduler's scope to handle such a case; the scheduler will
		// report this unexpected occurrence but does not register it as a scheduler-side error.
		// If (and when) the situation is corrected, the scheduler will be triggered again.
		err := controller.NewUnexpectedBehaviorError(fmt.Errorf("too many active policy snapshots: %d, want 1", len(policySnapshots)))
		klog.ErrorS(err, "There are multiple latest policy snapshots associated with placement", "placement", placementRef)
		return nil, err
	default:
		// Found the one and only active policy snapshot.
		return policySnapshots[0], nil
	}
}

// addSchedulerCleanupFinalizer adds the scheduler cleanup finalizer to a placement (if it does not
// have it yet).
func (s *Scheduler) addSchedulerCleanUpFinalizer(ctx context.Context, placement fleetv1beta1.PlacementObj) error {
	// Add the finalizer only if the placement does not have one yet.
	if !controllerutil.ContainsFinalizer(placement, fleetv1beta1.SchedulerCleanupFinalizer) {
		controllerutil.AddFinalizer(placement, fleetv1beta1.SchedulerCleanupFinalizer)

		if err := s.client.Update(ctx, placement); err != nil {
			klog.ErrorS(err, "Failed to update placement", "placement", klog.KObj(placement))
			return controller.NewUpdateIgnoreConflictError(err)
		}
	}
	return nil
}

// observeSchedulingCycleMetrics adds a data point to the scheduling cycle duration metric.
func observeSchedulingCycleMetrics(startTime time.Time, isFailed, needsRequeue bool) {
	metrics.SchedulingCycleDurationMilliseconds.
		WithLabelValues(fmt.Sprintf("%t", isFailed), fmt.Sprintf("%t", needsRequeue)).
		Observe(float64(time.Since(startTime).Milliseconds()))
}
