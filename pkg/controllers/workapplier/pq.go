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

package workapplier

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/priorityqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// Note (chenyu1): the work applier is set to periodically requeue Work objects for processing;
// when the KubeFleet agent restarts, the work applier will also have to re-process all the existing
// Work objects for correctness reasons. In environments where a large number of Work objects
// are present, with the default FIFO queue implementation, the work applier might have to spend
// a significant amount of time processing old Work objects that have not been changed for a while
// and are already in a consistent state, before it can get to process recently created/updated Work
// objects, which results in increased latency for new placements to complete. To address this issue,
// we enabled the usage of priority queues in the work applier, so that events
// from newly created/updated Work objects will be processed first.

const (
	// A list of priority levels for the work applier priority queue.
	//
	// The work applier, when a priority queue is in use, will prioritize requests in the following
	// order:
	// * with high priority (>0): all Create/Delete events, and all Update events
	//   that concern recently created Work objects or Work objects that are in a failed/undeterminted
	//   state (apply op/availability check failure, or diff reporting failure). For specifics,
	//   see the implementation details below.
	//   Note that the top priority level is capped at 100 for consistency/cleanness reasons.
	// * with default priority (0): all other Update events.
	// * with low priority (-1): all requeues, and all Generic events.
	// * errors will be requeued in the request's original priority level.
	//
	// Note that requests with the same priority level will be processed in the FIFO order.
	highestPriorityLevel = 100
	defaultPriorityLevel = 0
	lowPriorityLevel     = -1
)

type CustomPriorityQueueManager interface {
	PriorityQueue() priorityqueue.PriorityQueue[reconcile.Request]
}

var _ handler.TypedEventHandler[client.Object, reconcile.Request] = &priorityBasedWorkObjEventHandler{}

// priorityBasedWorkObjEventHandler implements the TypedEventHandler interface.
//
// It is used to process work object events in a priority-based manner with a priority queue.
type priorityBasedWorkObjEventHandler struct {
	qm CustomPriorityQueueManager

	priLinearEqCoeffA int
	priLinearEqCoeffB int
}

// calcArgBasedPriWithLinearEquation calculates the priority for a work object
// based on its age using a linear equation: Pri(Work) = A * AgeSinceCreationInMinutes(Work) + B,
// where A and B are user-configurable coefficients.
//
// The calculated priority is capped between defaultPriorityLevel and highestPriorityLevel.
func (h *priorityBasedWorkObjEventHandler) calcArgBasedPriWithLinearEquation(workObjAgeMinutes int) int {
	priority := h.priLinearEqCoeffA*workObjAgeMinutes + h.priLinearEqCoeffB
	if priority < defaultPriorityLevel {
		return defaultPriorityLevel
	}
	if priority > highestPriorityLevel {
		return highestPriorityLevel
	}
	return priority
}

// Create implements the TypedEventHandler interface.
func (h *priorityBasedWorkObjEventHandler) Create(_ context.Context, createEvent event.TypedCreateEvent[client.Object], _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Do a sanity check.
	//
	// Normally when this method is called, the priority queue has been initialized. The check is added
	// as the implementation has no control over when this method is called.
	if h.qm.PriorityQueue() == nil {
		wrappedErr := fmt.Errorf("received a Create event, but the priority queue is not initialized")
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		klog.ErrorS(wrappedErr, "Failed to process Create event")
		return
	}

	// Enqueue the request with the highest priority.
	opts := priorityqueue.AddOpts{
		Priority: ptr.To(highestPriorityLevel),
	}
	workObjName := createEvent.Object.GetName()
	workObjNS := createEvent.Object.GetNamespace()
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: workObjNS,
			Name:      workObjName,
		},
	}
	h.qm.PriorityQueue().AddWithOpts(opts, req)
}

// Delete implements the TypedEventHandler interface.
func (h *priorityBasedWorkObjEventHandler) Delete(_ context.Context, deleteEvent event.TypedDeleteEvent[client.Object], _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Do a sanity check.
	if h.qm.PriorityQueue() == nil {
		wrappedErr := fmt.Errorf("received a Delete event, but the priority queue is not initialized")
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		klog.ErrorS(wrappedErr, "Failed to process Delete event")
		return
	}

	// Enqueue the request with the highest priority.
	opts := priorityqueue.AddOpts{
		Priority: ptr.To(highestPriorityLevel),
	}
	workObjName := deleteEvent.Object.GetName()
	workObjNS := deleteEvent.Object.GetNamespace()
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: workObjNS,
			Name:      workObjName,
		},
	}
	h.qm.PriorityQueue().AddWithOpts(opts, req)
}

// Update implements the TypedEventHandler interface.
func (h *priorityBasedWorkObjEventHandler) Update(_ context.Context, updateEvent event.TypedUpdateEvent[client.Object], _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Do a sanity check.
	if h.qm.PriorityQueue() == nil {
		wrappedErr := fmt.Errorf("received an Update event, but the priority queue is not initialized")
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		klog.ErrorS(wrappedErr, "Failed to process Update event")
		return
	}

	// Ignore status only updates.
	if updateEvent.ObjectOld.GetGeneration() == updateEvent.ObjectNew.GetGeneration() {
		return
	}

	// No need to check the updated Work object, as both objects have the same creation timestamp,
	// and the prioritization logic concerns only the old Work object's status.
	oldWorkObj, oldOK := updateEvent.ObjectOld.(*fleetv1beta1.Work)
	if !oldOK {
		wrappedErr := fmt.Errorf("received an Update event, but the objects cannot be cast to Work objects")
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		klog.ErrorS(wrappedErr, "Failed to process Update event")
		return
	}

	pri := h.determineUpdateEventPriority(oldWorkObj)
	opts := priorityqueue.AddOpts{
		Priority: ptr.To(pri),
	}
	workObjName := oldWorkObj.GetName()
	workObjNS := oldWorkObj.GetNamespace()
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: workObjNS,
			Name:      workObjName,
		},
	}
	h.qm.PriorityQueue().AddWithOpts(opts, req)
}

// Generic implements the TypedEventHandler interface.
func (h *priorityBasedWorkObjEventHandler) Generic(_ context.Context, genericEvent event.TypedGenericEvent[client.Object], _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Do a sanity check.
	if h.qm.PriorityQueue() == nil {
		wrappedErr := fmt.Errorf("received a Generic event, but the priority queue is not initialized")
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		klog.ErrorS(wrappedErr, "Failed to process Generic event")
		return
	}

	// Enqueue the request with low priority.
	opts := priorityqueue.AddOpts{
		Priority: ptr.To(lowPriorityLevel),
	}
	workObjName := genericEvent.Object.GetName()
	workObjNS := genericEvent.Object.GetNamespace()
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: workObjNS,
			Name:      workObjName,
		},
	}
	h.qm.PriorityQueue().AddWithOpts(opts, req)
}

func (h *priorityBasedWorkObjEventHandler) determineUpdateEventPriority(oldWorkObj *fleetv1beta1.Work) int {
	// If the work object is recently created (its age is within the given threshold),
	// process its Update event with high priority.

	// The age is expected to be the same for both old and new work objects, as the field
	// is immutable and not user configurable.
	workObjAgeMinutes := int(time.Since(oldWorkObj.CreationTimestamp.Time).Minutes())

	// Check if the work object is in a failed/undetermined state.
	//
	// * If the work object is in such a state, process its Update event with highest priority (even if the work object
	//   was created long ago);
	// * Otherwise, prioritize the processing of the Update event if the work object is recently created;
	// * Use the default priority level for all other cases.
	oldApplyStrategy := oldWorkObj.Spec.ApplyStrategy
	isReportDiffModeEnabled := oldApplyStrategy != nil && oldApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff

	appliedCond := meta.FindStatusCondition(oldWorkObj.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
	availableCond := meta.FindStatusCondition(oldWorkObj.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
	diffReportedCond := meta.FindStatusCondition(oldWorkObj.Status.Conditions, fleetv1beta1.WorkConditionTypeDiffReported)

	// Note (chenyu1): it might be true that the Update event involves an apply strategy change; however, the prioritization
	// logic stays the same: if the old work object is in a failed/undetermined state, the apply strategy change
	// should receive the highest priority; otherwise, the Update event should be processed with medium priority.
	switch {
	case isReportDiffModeEnabled && condition.IsConditionStatusTrue(diffReportedCond, oldWorkObj.Generation):
		// The ReportDiff mode is enabled and the status suggests that the diff reporting has been completed successfully.
		// Determine the priority level based on the age of the Work object.
		return h.calcArgBasedPriWithLinearEquation(workObjAgeMinutes)
	case isReportDiffModeEnabled:
		// The ReportDiff mode is enabled, but the diff reporting has not been completed yet or has failed.
		// Use high priority for the Update event.
		return highestPriorityLevel
	case condition.IsConditionStatusTrue(appliedCond, oldWorkObj.Generation) && condition.IsConditionStatusTrue(availableCond, oldWorkObj.Generation):
		// The apply strategy is set to the CSA/SSA mode and the work object is applied and available.
		// Determine the priority level based on the age of the Work object.
		return h.calcArgBasedPriWithLinearEquation(workObjAgeMinutes)
	default:
		// The apply strategy is set to the CSA/SSA mode and the work object is in a failed/undetermined state.
		// Use high priority for the Update event.
		return highestPriorityLevel
	}
}

// Requeue requeues the given work object for processing after the given delay.
func (h *priorityBasedWorkObjEventHandler) Requeue(work *fleetv1beta1.Work, delay time.Duration) {
	// Do a sanity check.
	if h.qm.PriorityQueue() == nil {
		wrappedErr := fmt.Errorf("received a requeue request, but the priority queue is not initialized")
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		klog.ErrorS(wrappedErr, "Failed to process Requeue request")
		return
	}

	opts := priorityqueue.AddOpts{
		// All requeued requests have the low priority level.
		Priority: ptr.To(int(lowPriorityLevel)),
	}
	// Set the RateLimited flag in consistency with the controller runtime requeueing behavior.
	if delay == 0 {
		opts.RateLimited = true
	} else {
		opts.After = delay
	}

	workObjName := work.GetName()
	workObjNS := work.GetNamespace()
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: workObjNS,
			Name:      workObjName,
		},
	}
	h.qm.PriorityQueue().AddWithOpts(opts, req)
}
