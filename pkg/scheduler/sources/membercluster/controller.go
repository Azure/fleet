/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package membercluster features a controller that enqueues CRPs on member cluster changes.
//
// Generally speaking, in a fleet there might be the following three types of cluster changes:
//
//  1. a cluster, originally ineligible for resource placement for some reason, becomes eligible;
//
//     It may happen for 2 reasons:
//
//     a) the cluster setting, specifically its labels, has changed; and/or
//     b) an unexpected development which originally leads the scheduler to disregard the cluster
//     (e.g., agents not joining, network partition, etc.) has been resolved.
//
//  2. a cluster, originally eligible for resource placement, becomes ineligible for some reason.
//
//     Similarly, it may happen for 2 reasons:
//
//     a) the cluster setting, specifically its labels, has changed; and/or
//     b) an unexpected development (e.g., agents failing, network partition, etc.) has occurred.
//
//  3. a cluster, which may or may not have resources placed on it, has left the fleet.
//
// Among the cases,
//
// 2b) requires no attention on the scheduler's end, as in this condition, the scheduler will
// automatically disregard the cluster, and existing resource placements should be kept on the
// cluster until further notice from the user, to avoid unexpected fluctuations.
//
// The other cases are handled by this controller. Note that it is only guaranteed that this
// controller will not emit false negatives, i.e., all the changes that require the scheduler's
// attention will be captured; false positives may still happen, i.e., this controller may
// trigger the scheduler to run a scheduling loop even though there is no change needed. In
// such situations, the scheduler will be the final judge and process the CRPs accordingly.
package onchange

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework/agentstatus"
	"go.goms.io/fleet/pkg/scheduler/queue"
	"go.goms.io/fleet/pkg/utils/controller"
)

// Reconciler is the member cluster controller reconciler.
type Reconciler struct {
	// Client is a (cached) client for accessing the Kubernetes API server.
	Client client.Client

	// SchedulerWorkQueue is the work queue for the scheduler.
	SchedulerWorkQueue queue.ClusterResourcePlacementSchedulingQueueWriter

	// clusterHeartbeatTimeout is the timeout value this controller uses for checking if a cluster
	// has been disconnected from the fleet for a prolonged period of time.
	clusterHeartbeatTimeout time.Duration

	// clusterHealthCheckTimeout is the timeout value this plugin uses for checking if a cluster
	// is still in a healthy state.
	clusterHealthCheckTimeout time.Duration
}

// Reconcile reconciles a member cluster.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	memberClusterRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "memberCluster", memberClusterRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "memberCluster", memberClusterRef, "latency", latency)
	}()

	// List all CRPs.
	//
	// Note that this controller reads CRPs from the same cache as the scheduler.
	crpList := &fleetv1beta1.ClusterResourcePlacementList{}
	if err := r.Client.List(ctx, crpList); err != nil {
		klog.ErrorS(err, "Failed to list CRPs", "memberCluster", memberClusterRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}

	// Enqueue the CRPs.
	//
	// Note that all the CRPs in the system are enqueued; technically speaking, for situation
	// 1a) and 1b), PickN CRPs that have been fully scheduled needs no further processing, however,
	// for simplicity reasons, this controller will not distinguish between the cases.
	for idx := range crpList.Items {
		crp := &crpList.Items[idx]
		klog.V(2).InfoS(
			"Enqueueing CRP for scheduler processing",
			"memberCluster", memberClusterRef,
			"clusterResourcePlacement", klog.KObj(crp))
		r.SchedulerWorkQueue.AddRateLimited(queue.ClusterResourcePlacementKey(crp.Name))
	}

	// The reconciliation loop completes.
	return ctrl.Result{}, nil
}

// SetupWithManager builds a controller with Reconciler and sets it up with a controller manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	customPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Ignore newly created cluster objects, as they are not yet ready for scheduling.
			// When the clusters do become ready, the controller will catch an update event.
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Ignore deletion events; the removal of a cluster is first signaled by the join
			// state change (join -> leave), which is already handled separately.
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Check if the update event is valid.
			if e.ObjectOld == nil || e.ObjectNew == nil {
				err := controller.NewUnexpectedBehaviorError(fmt.Errorf("update event is invalid"))
				klog.ErrorS(err, "Failed to process update event")
				return false
			}

			oldCluster, oldOk := e.ObjectOld.(*fleetv1beta1.MemberCluster)
			newCluster, newOk := e.ObjectNew.(*fleetv1beta1.MemberCluster)
			if !oldOk || !newOk {
				err := controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to cast runtime objects in update event to member cluster objects"))
				klog.ErrorS(err, "Failed to process update event")
				return false
			}

			// Check the resource placement eligibility for the old and new cluster object.
			oldEligible, _ := agentstatus.IsClusterEligible(oldCluster, r.clusterHeartbeatTimeout, r.clusterHealthCheckTimeout)
			newEligible, _ := agentstatus.IsClusterEligible(newCluster, r.clusterHeartbeatTimeout, r.clusterHealthCheckTimeout)

			if !oldEligible && newEligible {
				// The cluster becomes eligible for resource placement.
				return true
			}

			// Note that we disregard the case where the cluster becomes ineligible for resource
			// placement, as in this case, the scheduler will ignore the cluster, and any resource
			// that has been placed on the cluster should be kept until further notice from
			// the user.

			// All the other changes are ignored.
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1beta1.MemberCluster{}).
		// All label changes are captured, even if they should not trigger any scheduling changes.
		WithEventFilter(predicate.Or(predicate.LabelChangedPredicate{}, customPredicate)).
		Complete(r)
}
