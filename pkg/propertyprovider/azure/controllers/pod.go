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

// Package controllers feature a number of controllers that are in use
// by the Azure property provider.
package controllers

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/pkg/propertyprovider/azure/trackers"
)

// TO-DO (chenyu1): this is a relatively expensive watcher, due to how frequent pods can change
// in a Kubernetes cluster; unfortunately at this moment there does not seem to be a better way
// to observe the changes of requested resources in a cluster. The alternative, which is to use
// Lists, adds too much overhead to the API server.

// PodReconciler reconciles Pod objects.
type PodReconciler struct {
	PT     *trackers.PodTracker
	Client client.Client
}

// Reconcile reconciles a pod object.
func (p *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	podRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts for pod objects in the Azure property provider", "pod", podRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends for pod objects in the Azure property provider", "pod", podRef, "latency", latency)
	}()

	// Retrieve the pod object.
	pod := &corev1.Pod{}
	if err := p.Client.Get(ctx, req.NamespacedName, pod); err != nil {
		// Failed to get the pod object.
		if errors.IsNotFound(err) {
			// This branch essentially processes the pod deletion event (the actual deletion).
			// At this point the pod may have not been tracked by the tracker at all; if that's
			// the case, the removal (untracking) operation is a no-op.
			//
			// Note that this controller will not add any finalizer to pod objects, so as to
			// avoid blocking normal Kuberneters operations under unexpected circumstances.
			p.PT.Remove(req.NamespacedName.String())
			return ctrl.Result{}, nil
		}

		// For other errors, retry the reconciliation.
		klog.ErrorS(err, "Failed to get the pod object", "pod", podRef)
		return ctrl.Result{}, err
	}

	// Note that this controller will not untrack a pod when it is first marked for deletion;
	// instead, it performs the untracking when the pod object is actually gone from the
	// etcd store. This is intentional, as when a pod is marked for deletion, workloads might
	// not have been successfully terminated yet, and untracking the pod too early might lead to a
	// case of temporary inconsistency.

	// Track the pod if:
	//
	// * it is **NOT** of the Succeeded or Failed state; and
	// * it has been assigned to a node.
	//
	// This behavior is consistent with how the Kubernetes CLI tool reports requested capacity
	// on a specific node (`kubectl describe node` command).
	//
	// Note that the tracker will attempt to track the pod even if it has been marked for deletion.
	if len(pod.Spec.NodeName) > 0 && pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
		klog.V(2).InfoS("Attempt to track the pod", "pod", podRef)
		p.PT.AddOrUpdate(pod)
	} else {
		// Untrack the pod.
		//
		// It may have been descheduled, or transited into a terminal state.
		klog.V(2).InfoS("Untrack the pod", "pod", podRef)
		p.PT.Remove(req.NamespacedName.String())
	}

	return ctrl.Result{}, nil
}

func (p *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Reconcile any pod changes (create, update, delete).
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(p)
}
