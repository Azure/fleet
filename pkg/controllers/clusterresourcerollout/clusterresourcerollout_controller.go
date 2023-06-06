/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourcerollout

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1 "go.goms.io/fleet/apis/v1"
)

// Reconciler reconciles the active ClusterResourceSnapshot object.
type Reconciler struct {
	client.Client
	recorder record.EventRecorder
}

// Reconcile rollouts the resources by updating/deleting the clusterResourceBindings.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	crs := fleetv1.ClusterResourceSnapshot{}
	crsKRef := klog.KRef(name.Namespace, name.Name)

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "clusterResourceSnapshot", crsKRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "clusterResourceSnapshot", crsKRef, "latency", latency)
	}()

	if err := r.Client.Get(ctx, name, &crs); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring NotFound clusterResourceSnapshot", "clusterResourceSnapshot", crsKRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get clusterResourceSnapshot", "clusterResourceSnapshot", crsKRef)
		return ctrl.Result{}, err
	}

	if crs.ObjectMeta.DeletionTimestamp != nil {
		return r.handleDelete(ctx, &crs)
	}

	// register finalizer
	if !controllerutil.ContainsFinalizer(&crs, fleetv1.ClusterResourceSnapshotFinalizer) {
		controllerutil.AddFinalizer(&crs, fleetv1.ClusterResourceSnapshotFinalizer)
		if err := r.Update(ctx, &crs); err != nil {
			klog.ErrorS(err, "Failed to add mcs finalizer", "clusterResourceSnapshot", crsKRef)
			return ctrl.Result{}, err
		}
	}
	return r.handleUpdate(ctx, &crs)
}

func (r *Reconciler) handleDelete(_ context.Context, _ *fleetv1.ClusterResourceSnapshot) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *Reconciler) handleUpdate(_ context.Context, _ *fleetv1.ClusterResourceSnapshot) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("clusterResourceRollout")
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1.ClusterResourceSnapshot{}).
		Complete(r)
}
