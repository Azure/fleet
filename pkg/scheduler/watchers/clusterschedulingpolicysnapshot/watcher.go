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

// Package clusterschedulingpolicysnapshot features a controller that enqueues CRPs for the
// scheduler to process where there is a change in their scheduling policy snapshots.
package clusterschedulingpolicysnapshot

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/queue"
	"go.goms.io/fleet/pkg/utils/controller"
)

// Reconciler reconciles the change in scheduling policy snapshots.
type Reconciler struct {
	// Client is the client the controller uses to access the hub cluster.
	client.Client
	// SchedulerWorkQueue is the workqueue in use by the scheduler.
	SchedulerWorkQueue queue.ClusterResourcePlacementSchedulingQueueWriter
}

// Reconcile reconciles the cluster scheduling policy snapshot.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	policySnapshotRef := klog.KRef("", req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Scheduler source reconciliation starts", "clusterSchedulingPolicySnapshot", policySnapshotRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Scheduler source reconciliation ends", "clusterSchedulingPolicySnapshot", policySnapshotRef, "latency", latency)
	}()

	// Retrieve the policy snapshot.
	policySnapshot := &fleetv1beta1.ClusterSchedulingPolicySnapshot{}
	if err := r.Client.Get(ctx, req.NamespacedName, policySnapshot); err != nil {
		klog.ErrorS(err, "Failed to get cluster scheduling policy snapshot", "clusterSchedulingPolicySnapshot", policySnapshotRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
	}

	// Check if the policy snapshot has been deleted.
	//
	// Normally this would not happen as the event filter is set to filter out all deletion events.
	if policySnapshot.DeletionTimestamp != nil {
		// The policy snapshot has been deleted; ignore it.
		return ctrl.Result{}, nil
	}

	// Verify if the policy snapshot is currently active.
	isLatestVal, ok := policySnapshot.Labels[fleetv1beta1.IsLatestSnapshotLabel]
	if !ok {
		// The IsLatestSnapshot label is not present; normally this should never occur.
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("IsLatestSnapshotLabel is missing")),
			"IsLatestSnapshot label is not present",
			"clusterSchedulingPolicySnapshot", policySnapshotRef)
		// This is not a situation that the controller can recover by itself. Should the label
		// value be corrected, the controller will be triggered again.
		return ctrl.Result{}, nil
	}
	isLatest, err := strconv.ParseBool(isLatestVal)
	if err != nil {
		// The label value is not a correctly formatted boolean value; normally this should never
		// occur.
		klog.ErrorS(controller.NewUnexpectedBehaviorError(err),
			"Failed to parse IsLatestSnapshot value",
			"clusterSchedulingPolicySnapshot", policySnapshotRef)
		// This is not an error that the controller can recover by itself; ignore the error.
		// Should the label value be corrected, the controller will be triggered again.
		return ctrl.Result{}, nil
	}
	if !isLatest {
		// The policy snapshot is not currently active, i.e., it is not the latest policy snapshot;
		// ignore it.
		return ctrl.Result{}, nil
	}

	// Retrieve the owner CRP.
	crpName, ok := policySnapshot.Labels[fleetv1beta1.CRPTrackingLabel]
	if !ok {
		// The CRPTracking label is not present; normally this should never occur.
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("CRPTrackingLabel is missing")),
			"CRPTracking label is not present",
			"clusterSchedulingPolicySnapshot", policySnapshotRef)
		// This is not a situation that the controller can recover by itself. Should the label
		// value be corrected, the controller will be triggered again.
		return ctrl.Result{}, nil
	}

	// Enqueue the CRP name for scheduler processing.
	r.SchedulerWorkQueue.Add(queue.ClusterResourcePlacementKey(crpName))

	// The reconciliation loop ends.
	return ctrl.Result{}, nil
}

// SetupWithManager SetupWithManger sets up the controller with the manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	customPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Always process newly created policy snapshots.
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Ignore deletion events.
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Check if the update event is valid.
			if e.ObjectOld == nil || e.ObjectNew == nil {
				err := controller.NewUnexpectedBehaviorError(fmt.Errorf("update event is invalid"))
				klog.ErrorS(err, "Failed to process update event")
				return false
			}

			// Policy snapshot spec is immutable; however, the scheduler will have to respond
			// to changes in the numberOfCluster & CRPGeneration annotations.
			oldAnnotations := e.ObjectOld.GetAnnotations()
			newAnnotations := e.ObjectNew.GetAnnotations()

			oldNumOfClusters := oldAnnotations[fleetv1beta1.NumberOfClustersAnnotation]
			newNumOfClusters := newAnnotations[fleetv1beta1.NumberOfClustersAnnotation]
			if oldNumOfClusters != newNumOfClusters {
				return true
			}

			// The scheduler needs to update the policy snapshot based on the latest CRP generation, when resource selector
			// has changed and there are no policy changes.
			oldObservedCRPGeneration := oldAnnotations[fleetv1beta1.CRPGenerationAnnotation]
			newObservedCRPGeneration := newAnnotations[fleetv1beta1.CRPGenerationAnnotation]
			if oldObservedCRPGeneration != newObservedCRPGeneration {
				return true
			}

			// Normally all the labels are added to the policy snapshot when the object is created;
			// and the value for the IsLatestSnapshot label can only change from true to false, but
			// not the other way around. However, to avoid corner cases where the label value
			// is changed back (which may happen when spec is updated back and forth successively),
			// or tampering from the user side, here the controller performs one additional check.
			oldLabels := e.ObjectOld.GetLabels()
			newLabels := e.ObjectNew.GetLabels()

			oldIsLatestVal := oldLabels[fleetv1beta1.IsLatestSnapshotLabel]
			newIsLatestVal := newLabels[fleetv1beta1.IsLatestSnapshotLabel]
			oldIsLatest, oldErr := strconv.ParseBool(oldIsLatestVal)
			newIsLatest, newErr := strconv.ParseBool(newIsLatestVal)
			switch {
			case oldErr != nil:
				// The label value changes from an invalid one to true; process the object
				// if the new value is true; ignore otherwise. Note that if the new value
				// is also invalid, newIsLatest will have the false value.
				return newIsLatest
			case newErr != nil:
				// The label value changes to an invalid one; ignore the object.
				return false
			case !oldIsLatest && newIsLatest:
				// The label value changes from false to true.
				return true
			}

			// All the other changes are ignored.
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).Named("clusterschedulingpolicysnapshot-scheduler-watcher").
		For(&fleetv1beta1.ClusterSchedulingPolicySnapshot{}).
		WithEventFilter(customPredicate).
		Complete(r)
}
