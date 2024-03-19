/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package controllers

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/pkg/propertyprovider/aks/trackers"
)

// NodeReconciler reconciles Node objects.
type NodeReconciler struct {
	NT     *trackers.NodeTracker
	Client client.Client
}

// Reconcile reconciles a node object.
func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	nodeRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "node", nodeRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "node", nodeRef, "latency", latency)
	}()

	// Retrieve the node object.
	node := &corev1.Node{}
	if err := r.Client.Get(ctx, req.NamespacedName, node); err != nil {
		// Failed to get the node object; this signals that the node should untracked.
		if errors.IsNotFound(err) {
			// This branch essentially processes the node deletion event (the actual deletion).
			// At this point the node may have not been tracked by the tracker at all; if that's
			// the case, the removal (untracking) operation is a no-op.
			//
			// Note that this controller will not add any finalizer to node objects, so as to
			// avoid blocking normal Kuberneters operations under unexpected circumstances.
			klog.V(2).InfoS("Node is not found; untrack it from the property provider", "node", nodeRef)
			r.NT.Remove(req.Name)
			return ctrl.Result{}, nil
		}
		// For other errors, retry the reconciliation.
		klog.V(2).ErrorS(err, "Failed to get the node object", "node", nodeRef)
		return ctrl.Result{}, err
	}

	// Note that this controller will not untrack a node when it is first marked for deletion;
	// instead, it performs the untracking when the node object is actually gone from the
	// etcd store. This is intentional, as when a node is marked for deletion, workloads might
	// not have been drained from it, and untracking the node too early might lead to a
	// case of temporary inconsistency where the amount of requested resource exceed the
	// allocatable capacity.

	// Track the node. If it has been tracked, update its total and allocatable capacity
	// information with the tracker.
	//
	// Note that normally the capacity information remains immutable before object
	// creation; the tracker update only serves as a sanity check.
	//
	// Also note that the tracker will attempt to track the node even if it has been
	// marked for deletion, as cordoned, or as unschedulable. This behavior is consistent with
	// the original Fleet setup.
	klog.V(2).InfoS("Attempt to track the node", "node", nodeRef)
	r.NT.AddOrUpdate(node)

	return ctrl.Result{}, nil
}

func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Reconcile any node changes (create, update, delete).
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}
