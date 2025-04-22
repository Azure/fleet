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

// Package clusterschedulingpolicysnapshot features a controller to reconcile the clusterSchedulingPolicySnapshot object.
package clusterschedulingpolicysnapshot

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// Reconciler reconciles a clusterSchedulingPolicySnapshot object.
type Reconciler struct {
	client.Client

	// PlacementController exposes the placement queue for the reconciler to push to.
	PlacementController controller.Controller
}

// Reconcile triggers a single CRP reconcile round when scheduling policy has changed.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	snapshot := fleetv1beta1.ClusterSchedulingPolicySnapshot{}
	snapshotKRef := klog.KRef(name.Namespace, name.Name)

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "clusterSchedulingPolicySnapshot", snapshotKRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "clusterSchedulingPolicySnapshot", snapshotKRef, "latency", latency)
	}()

	if err := r.Client.Get(ctx, name, &snapshot); err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring NotFound clusterSchedulingPolicySnapshot", "clusterSchedulingPolicySnapshot", snapshotKRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get clusterSchedulingPolicySnapshot", "clusterSchedulingPolicySnapshot", snapshotKRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}
	crp := snapshot.Labels[fleetv1beta1.CRPTrackingLabel]
	if len(crp) == 0 {
		err := fmt.Errorf("invalid label value %s", fleetv1beta1.CRPTrackingLabel)
		klog.ErrorS(err, "Invalid clusterSchedulingPolicySnapshot", "clusterSchedulingPolicySnapshot", snapshotKRef)
		return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
	}

	r.PlacementController.Enqueue(crp)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
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
