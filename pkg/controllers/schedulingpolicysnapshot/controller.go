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

// Package schedulingpolicysnapshot features a controller to reconcile the clusterSchedulingPolicySnapshot or the schedulingPolicySnapshot objects.
package schedulingpolicysnapshot

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// Reconciler reconciles a clusterSchedulingPolicySnapshot or schedulingPolicySnapshot object.
type Reconciler struct {
	client.Client

	// PlacementController exposes the placement queue for the reconciler to push to.
	PlacementController controller.Controller
}

// Reconcile triggers a single placement reconcile round when scheduling policy has changed.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	snapshotKRef := klog.KRef(req.Namespace, req.Name)

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "policySnapshot", snapshotKRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "policySnapshot", snapshotKRef, "latency", latency)
	}()

	var snapshot fleetv1beta1.PolicySnapshotObj
	var err error
	if req.Namespace == "" {
		// ClusterSchedulingPolicySnapshot (cluster-scoped)
		var clusterSchedulingPolicySnapshot fleetv1beta1.ClusterSchedulingPolicySnapshot
		if err = r.Client.Get(ctx, req.NamespacedName, &clusterSchedulingPolicySnapshot); err != nil {
			klog.ErrorS(err, "Failed to get cluster scheduling policy snapshot", "policySnapshot", snapshotKRef)
			return ctrl.Result{}, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
		}
		snapshot = &clusterSchedulingPolicySnapshot
	} else {
		// schedulingPolicySnapshot (namespaced)
		var schedulingPolicySnapshot fleetv1beta1.SchedulingPolicySnapshot
		if err = r.Client.Get(ctx, req.NamespacedName, &schedulingPolicySnapshot); err != nil {
			klog.ErrorS(err, "Failed to get scheduling policy snapshot", "policySnapshot", snapshotKRef)
			return ctrl.Result{}, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
		}
		snapshot = &schedulingPolicySnapshot
	}

	placementName := snapshot.GetLabels()[fleetv1beta1.PlacementTrackingLabel]
	if len(placementName) == 0 {
		err := fmt.Errorf("label %s is missing or has empty value", fleetv1beta1.PlacementTrackingLabel)
		klog.ErrorS(err, "Failed to enqueue placement due to invalid schedulingPolicySnapshot", "policySnapshot", snapshotKRef)
		return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
	}

	r.PlacementController.Enqueue(controller.GetObjectKeyFromNamespaceName(req.Namespace, placementName))
	return ctrl.Result{}, nil
}

// SetupWithManagerForClusterSchedulingPolicySnapshot sets up the controller with the Manager for ClusterSchedulingPolicySnapshot.
func (r *Reconciler) SetupWithManagerForClusterSchedulingPolicySnapshot(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("clusterschedulingpolicysnapshot-watcher").
		For(&fleetv1beta1.ClusterSchedulingPolicySnapshot{}).
		WithEventFilter(predicate.Funcs{
			// skipping delete and create events so that CRP controller does not need to update the status.
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			CreateFunc: func(e event.CreateEvent) bool {
				return false
			},
		}).Complete(r)
}

// SetupWithManagerForSchedulingPolicySnapshot sets up the controller with the Manager for SchedulingPolicySnapshot.
func (r *Reconciler) SetupWithManagerForSchedulingPolicySnapshot(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("schedulingpolicysnapshot-watcher").
		For(&fleetv1beta1.SchedulingPolicySnapshot{}).
		WithEventFilter(predicate.Funcs{
			// skipping delete and create events so that RP controller does not need to update the status.
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			CreateFunc: func(e event.CreateEvent) bool {
				return false
			},
		}).Complete(r)
}
