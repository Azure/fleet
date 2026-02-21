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
// by the default property provider.
package controllers

import (
	"context"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider/default/trackers"
)

// NamespaceReconciler reconciles Namespace objects.
type NamespaceReconciler struct {
	NamespaceTracker *trackers.NamespaceTracker
	Client           client.Client
}

// Reconcile reconciles a namespace object.
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	namespaceRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts for namespace objects in the property provider", "namespace", namespaceRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends for namespace objects in the property provider", "namespace", namespaceRef, "latency", latency)
	}()

	namespace := &corev1.Namespace{}
	if err := r.Client.Get(ctx, req.NamespacedName, namespace); err != nil {
		if errors.IsNotFound(err) {
			klog.V(2).InfoS("Namespace is not found; untrack it from the property provider", "namespace", namespaceRef)
			r.NamespaceTracker.Remove(req.Name)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get the namespace object", "namespace", namespaceRef)
		return ctrl.Result{}, err
	}

	// Note that the tracker will attempt to untrack the namespace if it has been marked
	// for deletion or is in a terminating state. So that the new workloads will not be scheduled
	// to this namespace.
	if namespace.DeletionTimestamp != nil {
		klog.V(2).InfoS("Namespace is marked for deletion; untrack it from the property provider", "namespace", namespaceRef)
		r.NamespaceTracker.Remove(req.Name)
		return ctrl.Result{}, nil
	}

	// Track the namespace. If it has been tracked, update its information with the tracker.
	klog.V(2).InfoS("Attempt to track the namespace", "namespace", namespaceRef)
	r.NamespaceTracker.AddOrUpdate(namespace)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager, controllerName string) error {
	// Watch Create, Update (for terminating state and owner reference changes), and Delete events.
	// Deletion is handled via the Delete event which triggers reconciliation,
	// and the NotFound check in Reconcile removes it from the tracker.
	namespaceEventFilter := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true // Watch all create events
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNs, ok := e.ObjectOld.(*corev1.Namespace)
			if !ok {
				return false
			}
			newNs, ok := e.ObjectNew.(*corev1.Namespace)
			if !ok {
				return false
			}

			// Watch for namespace transitioning to terminating state
			// because we want to stop tracking the namespace once it
			// is marked for deletion, to prevent new workloads from being
			// scheduled to the namespace.
			if oldNs.DeletionTimestamp == nil && newNs.DeletionTimestamp != nil {
				return true
			}

			// Watch for owner reference changes because the owner applied work name
			// is part of the namespace information we track for namespace affinity.
			// We want to keep the namespace information up to date in the tracker
			// if the owner reference changes.
			if !reflect.DeepEqual(oldNs.OwnerReferences, newNs.OwnerReferences) {
				return true
			}

			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true // Watch all delete events
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false // Ignore generic events
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		For(&corev1.Namespace{}).
		WithEventFilter(namespaceEventFilter).
		Complete(r)
}
