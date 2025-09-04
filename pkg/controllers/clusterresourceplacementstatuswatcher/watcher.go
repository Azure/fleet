/*
Copyright 2024 The Kubernetes Fleet Authors.
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

package clusterresourceplacementstatuswatcher

import (
	"context"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// Reconciler reconciles ClusterResourcePlacementStatus delete events.
type Reconciler struct {
	client.Client

	// PlacementController maintains a rate limited queue which used to store
	// the name of the placement objects and a reconcile function to consume the items in queue.
	PlacementController controller.Controller
}

// Reconcile handles ClusterResourcePlacementStatus delete events by checking if the
// corresponding ClusterResourcePlacement still exists and triggering reconciliation if needed.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	crpsRef := klog.KObj(&placementv1beta1.ClusterResourcePlacementStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
		},
	})
	klog.V(2).InfoS("ClusterResourcePlacementStatus watcher reconciliation starts", "crps", crpsRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("ClusterResourcePlacementStatus watcher reconciliation ends", "crps", crpsRef, "latency", latency)
	}()

	// The CRPS was deleted, so we need to check if the corresponding CRP still exists
	// and doesn't have a deletion timestamp. If so, we should trigger CRP reconciliation
	// to recreate the CRPS.
	crpName := req.Name // CRPS has the same name as its parent CRP
	crp := &placementv1beta1.ClusterResourcePlacement{}

	if err := r.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
		if k8serrors.IsNotFound(err) {
			// CRP doesn't exist, this is expected cleanup - no action needed
			klog.V(2).InfoS("CRP not found, expected cleanup", "crp", crpName, "crps", crpsRef)
		} else {
			klog.ErrorS(err, "Failed to get CRP", "crp", crpName, "crps", crpsRef)
		}
		return ctrl.Result{}, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
	}

	// Check if CRP has NamespaceAccessible status reporting scope, only CRPs with NamespaceAccessible scope should have CRPS objects
	if crp.Spec.StatusReportingScope != placementv1beta1.NamespaceAccessible {
		// CRP doesn't have NamespaceAccessible scope, but CRPS existed - this is unexpected behavior.
		// The CRPS deletion is actually cleaning up an inconsistent state, so no reconciliation needed.
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("CRPS existed for CRP without NamespaceAccessible scope")),
			"Unexpected CRPS found for CRP without NamespaceAccessible scope, deletion cleans up inconsistent state",
			"crp", klog.KObj(crp), "crps", crpsRef, "statusReportingScope", crp.Spec.StatusReportingScope)
		return ctrl.Result{}, nil
	}

	// Check if CRP is being deleted
	if crp.GetDeletionTimestamp() != nil {
		// CRP is being deleted, CRPS deletion is expected - no action needed
		klog.V(2).InfoS("CRP is being deleted, CRPS deletion is expected", "crp", klog.KObj(crp), "crps", crpsRef)
		return ctrl.Result{}, nil
	}

	// CRP exists, is not being deleted, and has NamespaceAccessible scope, but its CRPS was deleted.
	// This might be an accidental deletion or external cleanup, trigger CRP reconciliation to recreate the CRPS.
	klog.V(2).InfoS("CRP exists without deletion timestamp and has NamespaceAccessible scope, enqueueing for reconciliation to recreate CRPS",
		"crp", klog.KObj(crp), "crps", crpsRef)
	r.PlacementController.Enqueue(controller.GetObjectKeyFromNamespaceName(crp.GetNamespace(), crp.GetName()))

	return ctrl.Result{}, nil
}

// buildDeleteEventPredicate creates a predicate that only triggers on actual delete events
// (when objects are removed from storage).
func buildDeleteEventPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Ignore creation events
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Ignore update events
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Only trigger on actual deletion events
			if e.Object == nil {
				klog.ErrorS(fmt.Errorf("delete event object is nil"), "Failed to process delete event")
				return false
			}

			klog.V(2).InfoS("CRPS delete event received", "crps", klog.KObj(e.Object))
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			// Ignore generic events
			return false
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("clusterresourceplacementstatus-watcher").
		For(&placementv1beta1.ClusterResourcePlacementStatus{}).
		WithEventFilter(buildDeleteEventPredicate()).
		Complete(r)
}
