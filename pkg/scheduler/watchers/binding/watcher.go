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

package binding

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// Reconciler reconciles the deletion of a binding.
type Reconciler struct {
	// Client is the client the controller uses to access the hub cluster.
	client.Client
	// SchedulerWorkQueue is the workqueue in use by the scheduler.
	SchedulerWorkQueue queue.PlacementSchedulingQueueWriter
}

// Reconcile reconciles the binding.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	bindingRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Scheduler source reconciliation starts", "binding", bindingRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Scheduler source reconciliation ends", "binding", bindingRef, "latency", latency)
	}()

	var binding fleetv1beta1.BindingObj
	var err error
	if req.Namespace == "" {
		// ClusterResourceBinding (cluster-scoped)
		var clusterResourceBinding fleetv1beta1.ClusterResourceBinding
		if err = r.Client.Get(ctx, req.NamespacedName, &clusterResourceBinding); err != nil {
			klog.ErrorS(err, "Failed to get cluster resource binding", "binding", bindingRef)
			return ctrl.Result{}, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
		}
		binding = &clusterResourceBinding
	} else {
		// ResourceBinding (namespaced)
		var resourceBinding fleetv1beta1.ResourceBinding
		if err = r.Client.Get(ctx, req.NamespacedName, &resourceBinding); err != nil {
			klog.ErrorS(err, "Failed to get resource binding", "binding", bindingRef)
			return ctrl.Result{}, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
		}
		binding = &resourceBinding
	}

	// Check if the binding has been deleted and has the scheduler binding cleanup finalizer.
	if binding.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(binding, fleetv1beta1.SchedulerBindingCleanupFinalizer) {
		// The binding has been deleted and still has the scheduler binding cleanup finalizer; enqueue it's corresponding placement
		// for the scheduler to process.
		placementName, exist := binding.GetLabels()[fleetv1beta1.PlacementTrackingLabel]
		if !exist {
			err := controller.NewUnexpectedBehaviorError(fmt.Errorf("binding %s doesn't have placement tracking label", binding.GetName()))
			klog.ErrorS(err, "Failed to enqueue placement name for binding")
			// error cannot be retried.
			return ctrl.Result{}, nil
		}
		r.SchedulerWorkQueue.AddRateLimited(queue.PlacementKey(controller.GetObjectKeyFromNamespaceName(binding.GetNamespace(), placementName)))
	}

	// No action is needed for the scheduler to take in other cases.
	return ctrl.Result{}, nil
}

// buildCustomPredicate creates a predicate that only triggers on deletion timestamp changes.
func buildCustomPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Ignore creation events.
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Ignore deletion events (events emitted when the object is actually removed
			// from storage).
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Check if the update event is valid.
			if e.ObjectOld == nil || e.ObjectNew == nil {
				err := controller.NewUnexpectedBehaviorError(fmt.Errorf("update event is invalid"))
				klog.ErrorS(err, "Failed to process update event")
				return false
			}

			// Check if the deletion timestamp has been set.
			oldDeletionTimestamp := e.ObjectOld.GetDeletionTimestamp()
			newDeletionTimestamp := e.ObjectNew.GetDeletionTimestamp()
			if oldDeletionTimestamp == nil && newDeletionTimestamp != nil {
				return true
			}

			return false
		},
	}
}

// SetupWithManagerForClusterResourceBinding sets up the controller with the manager.
func (r *Reconciler) SetupWithManagerForClusterResourceBinding(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("clusterresourcebinding-scheduler-watcher").
		For(&fleetv1beta1.ClusterResourceBinding{}).
		WithEventFilter(buildCustomPredicate()).
		Complete(r)
}

// SetupWithManagerForResourceBinding sets up the controller with the manager.
func (r *Reconciler) SetupWithManagerForResourceBinding(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("resourcebinding-scheduler-watcher").
		For(&fleetv1beta1.ResourceBinding{}).
		WithEventFilter(buildCustomPredicate()).
		Complete(r)
}
