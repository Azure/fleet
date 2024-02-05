/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package scheduler features the scheduler for Fleet workloads.
package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/scheduler/framework"
	"go.goms.io/fleet/pkg/scheduler/queue"
	"go.goms.io/fleet/pkg/utils/controller"
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
	queue queue.ClusterResourcePlacementSchedulingQueue

	// client is the (cached) client in use by the scheduler for accessing Kubernetes API server.
	client client.Client

	// uncachedReader is the uncached read-only client in use by the scheduler for accessing
	// Kubernetes API server; in most cases client should be used instead, unless consistency becomes
	// a serious concern.
	//
	// TO-DO (chenyu1): explore the possbilities of using a mutation cache for better performance.
	uncachedReader client.Reader

	// manager is the controller manager in use by the scheduler.
	manager ctrl.Manager

	// eventRecorder is the event recorder in use by the scheduler.
	eventRecorder record.EventRecorder
}

// NewScheduler creates a scheduler.
func NewScheduler(
	name string,
	framework framework.Framework,
	queue queue.ClusterResourcePlacementSchedulingQueue,
	manager ctrl.Manager,
) *Scheduler {
	return &Scheduler{
		name:           name,
		framework:      framework,
		queue:          queue,
		client:         manager.GetClient(),
		uncachedReader: manager.GetAPIReader(),
		manager:        manager,
		eventRecorder:  manager.GetEventRecorderFor(name),
	}
}

// ScheduleOnce performs scheduling for one single item pulled from the work queue.
func (s *Scheduler) scheduleOnce(ctx context.Context) {
	// Retrieve the next item (name of a CRP) from the work queue.
	//
	// Note that this will block if no item is available.
	crpName, closed := s.queue.NextClusterResourcePlacementKey()
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
		s.queue.Done(crpName)
	}()

	startTime := time.Now()
	crpRef := klog.KRef("", string(crpName))
	klog.V(2).InfoS("Schedule once", "clusterResourcePlacement", crpRef)
	defer func() {
		// Note that the time spent on pulling keys from the work queue (and the time spent on waiting
		// for a key to arrive) is not counted here, as we cannot reliably distinguish between
		// system processing latencies and actual duration of cluster resource placement absence.
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Schedule once completed", "clusterResourcePlacement", crpRef, "latency", latency)
	}()

	// Retrieve the CRP.
	crp := &fleetv1beta1.ClusterResourcePlacement{}
	crpKey := types.NamespacedName{Name: string(crpName)}
	if err := s.client.Get(ctx, crpKey, crp); err != nil {
		// Wrap the error for metrics; this method does not return an error.
		klog.ErrorS(controller.NewAPIServerError(true, err), "Failed to get cluster resource placement", "clusterResourcePlacement", crpRef)

		if errors.IsNotFound(err) {
			// The CRP has been gone before the scheduler gets a chance to
			// process it; normally this would not happen as sources would not enqueue any CRP that
			// has been marked for deletion but does not have the scheduler cleanup finalizer to
			// the work queue. Such CRPs needs no further processing any way though, as the absence
			// of the cleanup finalizer implies that bindings derived from the CRP are no longer present.
			return
		}

		// Requeue for later processing.
		s.queue.AddRateLimited(crpName)
		return
	}

	// Check if the CRP has been marked for deletion, and if it has the scheduler cleanup finalizer.
	if crp.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(crp, fleetv1beta1.SchedulerCRPCleanupFinalizer) {
			if err := s.cleanUpAllBindingsFor(ctx, crp); err != nil {
				klog.ErrorS(err, "Failed to clean up all bindings for cluster resource placement", "clusterResourcePlacement", crpRef)
				// Requeue for later processing.
				s.queue.AddRateLimited(crpName)
				return
			}
		}
		// The CRP has been marked for deletion but no longer has the scheduler cleanup finalizer; no
		// additional handling is needed.

		// Untrack the key from the rate limiter.
		s.queue.Forget(crpName)
		return
	}

	// The CRP has not been marked for deletion; run the scheduling cycle for it.

	// Verify that it has an active policy snapshot.
	latestPolicySnapshot, err := s.lookupLatestPolicySnapshot(ctx, crp)
	if err != nil {
		klog.ErrorS(err, "Failed to lookup latest policy snapshot", "clusterResourcePlacement", crpRef)
		// No requeue is needed; the scheduler will be triggered again when an active policy
		// snapshot is created.

		// Untrack the key for quicker reprocessing.
		s.queue.Forget(crpName)
		return
	}

	// Add the scheduler cleanup finalizer to the CRP (if it does not have one yet).
	if err := s.addSchedulerCleanUpFinalizer(ctx, crp); err != nil {
		klog.ErrorS(err, "Failed to add scheduler cleanup finalizer", "clusterResourcePlacement", crpRef)
		// Requeue for later processing.
		s.queue.AddRateLimited(crpName)
		return
	}

	// Run the scheduling cycle.
	//
	// Note that the scheduler will enter this cycle as long as the CRP is active and an active
	// policy snapshot has been produced.
	cycleStartTime := time.Now()
	res, err := s.framework.RunSchedulingCycleFor(ctx, crp.Name, latestPolicySnapshot)
	if err != nil {
		klog.ErrorS(err, "Failed to run scheduling cycle", "clusterResourcePlacement", crpRef)
		// Requeue for later processing.
		s.queue.AddRateLimited(crpName)
		observeSchedulingCycleMetrics(cycleStartTime, true, false)
		return
	}

	// Requeue if the scheduling cycle suggests so.
	if res.Requeue {
		if res.RequeueAfter > 0 {
			s.queue.AddAfter(crpName, res.RequeueAfter)
			observeSchedulingCycleMetrics(cycleStartTime, false, true)
			return
		}
		// Untrack the key from the rate limiter.
		s.queue.Forget(crpName)
		// Requeue for later processing.
		//
		// Note that the key is added directly to the queue without having to wait for any rate limiter's
		// approval. This is necessary as requeues, requested by the scheduler, occur when the scheduler
		// is certain that more scheduling work needs to be done but it cannot be completed in
		// one cycle (e.g., a plugin sets up a per-cycle batch limit, and consequently the scheduler must
		// finish the scheduling in multiple cycles); in such cases, rate limiter should not add
		// any delay to the requeues.
		s.queue.Add(crpName)
		observeSchedulingCycleMetrics(cycleStartTime, false, true)
	}
	observeSchedulingCycleMetrics(cycleStartTime, false, false)
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

	// Run scheduleOnce forever.
	//
	// The loop starts in a dedicated goroutine; it exits when the context is canceled.
	go wait.UntilWithContext(ctx, s.scheduleOnce, 0)

	// Wait for the context to be canceled.
	<-ctx.Done()

	// Stopping the scheduling queue; drain if necessary.
	//
	// Note that if a scheduling cycle is in progress; this will only return when the
	// cycle completes.
	s.queue.CloseWithDrain()
}

// cleanUpAllBindingsFor cleans up all bindings derived from a CRP.
func (s *Scheduler) cleanUpAllBindingsFor(ctx context.Context, crp *fleetv1beta1.ClusterResourcePlacement) error {
	crpRef := klog.KObj(crp)

	// List all bindings derived from the CRP.
	//
	// Note that the listing is performed using the uncached client; this is to ensure that all related
	// bindings can be found, even if they have not been synced to the cache yet.
	bindingList := &fleetv1beta1.ClusterResourceBindingList{}
	listOptions := client.MatchingLabels{
		fleetv1beta1.CRPTrackingLabel: crp.Name,
	}
	// TO-DO (chenyu1): this is a very expensive op; explore options for optimization.
	if err := s.uncachedReader.List(ctx, bindingList, listOptions); err != nil {
		klog.ErrorS(err, "Failed to list all bindings", "ClusterResourcePlacement", crpRef)
		return controller.NewAPIServerError(false, err)
	}

	// Remove the scheduler cleanup finalizer from all the bindings, and delete them.
	//
	// Note that once a CRP has been marked for deletion, it will no longer enter the scheduling cycle,
	// so any cleanup finalizer has to be removed here.
	//
	// Also note that for deleted CRPs, derived bindings are deleted right away by the scheduler;
	// the scheduler no longer marks them as deleting and waits for another controller to actually
	// run the deletion.
	for idx := range bindingList.Items {
		binding := bindingList.Items[idx]
		// Delete the binding if it has not been marked for deletion yet.
		if binding.DeletionTimestamp == nil {
			if err := s.client.Delete(ctx, &binding); err != nil && !errors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to delete binding", "clusterResourceBinding", klog.KObj(&binding))
				return controller.NewAPIServerError(false, err)
			}
		}

		// Note that the scheduler will not add any cleanup finalizer to a binding.
	}

	// All bindings have been deleted; remove the scheduler cleanup finalizer from the CRP.
	controllerutil.RemoveFinalizer(crp, fleetv1beta1.SchedulerCRPCleanupFinalizer)
	if err := s.client.Update(ctx, crp); err != nil {
		klog.ErrorS(err, "Failed to remove scheduler cleanup finalizer from cluster resource placement", "clusterResourcePlacement", crpRef)
		return controller.NewUpdateIgnoreConflictError(err)
	}

	return nil
}

// lookupLatestPolicySnapshot returns the latest (i.e., active) policy snapshot associated with
// a CRP.
func (s *Scheduler) lookupLatestPolicySnapshot(ctx context.Context, crp *fleetv1beta1.ClusterResourcePlacement) (*fleetv1beta1.ClusterSchedulingPolicySnapshot, error) {
	crpRef := klog.KObj(crp)

	// Find out the latest policy snapshot associated with the CRP.
	policySnapshotList := &fleetv1beta1.ClusterSchedulingPolicySnapshotList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		fleetv1beta1.CRPTrackingLabel:      crp.Name,
		fleetv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
	})
	listOptions := &client.ListOptions{LabelSelector: labelSelector}
	// The scheduler lists with a cached client; this does not have any consistency concern as sources
	// will always trigger the scheduler when a new policy snapshot is created.
	if err := s.client.List(ctx, policySnapshotList, listOptions); err != nil {
		klog.ErrorS(err, "Failed to list policy snapshots of a cluster resource placement", "clusterResourcePlacement", crpRef)
		return nil, controller.NewAPIServerError(true, err)
	}

	switch {
	case len(policySnapshotList.Items) == 0:
		// There is no latest policy snapshot associated with the CRP; it could happen when
		// * the CRP is newly created; or
		// * the sequence of policy snapshots is in an inconsistent state.
		//
		// Either way, it is out of the scheduler's scope to handle such a case; the scheduler will
		// be triggered again if the situation is corrected.
		err := controller.NewExpectedBehaviorError(fmt.Errorf("no latest policy snapshot associated with cluster resource placement"))
		klog.ErrorS(err, "Failed to find the latest policy snapshot", "clusterResourcePlacement", crpRef)
		return nil, err
	case len(policySnapshotList.Items) > 1:
		// There are multiple active policy snapshots associated with the CRP; normally this
		// will never happen, as only one policy snapshot can be active in the sequence.
		//
		// Similarly, it is out of the scheduler's scope to handle such a case; the scheduler will
		// report this unexpected occurrence but does not register it as a scheduler-side error.
		// If (and when) the situation is corrected, the scheduler will be triggered again.
		err := controller.NewUnexpectedBehaviorError(fmt.Errorf("too many active policy snapshots: %d, want 1", len(policySnapshotList.Items)))
		klog.ErrorS(err, "There are multiple latest policy snapshots associated with cluster resource placement", "clusterResourcePlacement", crpRef)
		return nil, err
	default:
		// Found the one and only active policy snapshot.
		return &policySnapshotList.Items[0], nil
	}
}

// addSchedulerCleanupFinalizer adds the scheduler cleanup finalizer to a CRP (if it does not
// have it yet).
func (s *Scheduler) addSchedulerCleanUpFinalizer(ctx context.Context, crp *fleetv1beta1.ClusterResourcePlacement) error {
	// Add the finalizer only if the CRP does not have one yet.
	if !controllerutil.ContainsFinalizer(crp, fleetv1beta1.SchedulerCRPCleanupFinalizer) {
		controllerutil.AddFinalizer(crp, fleetv1beta1.SchedulerCRPCleanupFinalizer)

		if err := s.client.Update(ctx, crp); err != nil {
			klog.ErrorS(err, "Failed to update cluster resource placement", "clusterResourcePlacement", klog.KObj(crp))
			return controller.NewUpdateIgnoreConflictError(err)
		}
	}

	return nil
}

func observeSchedulingCycleMetrics(startTime time.Time, isFailed, needsRequeue bool) {
	metrics.SchedulingCycleDurationMilliseconds.
		WithLabelValues(fmt.Sprintf("%t", isFailed), fmt.Sprintf("%t", needsRequeue)).
		Observe(float64(time.Since(startTime).Milliseconds()))
}
