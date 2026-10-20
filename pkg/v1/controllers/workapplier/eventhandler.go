/*
Copyright 2026 The KubeFleet Authors.

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

package workapplier

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

var (
	// workEventHandler maps every work object event to a reconcile request for the corresponding primary work
	// object, as the work applier always processes all the linked work objects via their primary work object.
	workEventHandler = handler.TypedFuncs[client.Object, reconcile.Request]{
		CreateFunc: func(_ context.Context, evt event.TypedCreateEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueuePrimaryWork(evt.Object, "create", queue)
		},
		UpdateFunc: func(_ context.Context, evt event.TypedUpdateEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueuePrimaryWork(evt.ObjectNew, "update", queue)
		},
		DeleteFunc: func(_ context.Context, evt event.TypedDeleteEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueuePrimaryWork(evt.Object, "delete", queue)
		},
		GenericFunc: func(_ context.Context, evt event.TypedGenericEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueuePrimaryWork(evt.Object, "generic", queue)
		},
	}
)

// enqueuePrimaryWork enqueues a reconcile request for the primary work object associated with the given work object.
//
// A primary work object (one with the linked work count annotation) is enqueued directly; a secondary work object
// is resolved to its primary work object via its controller owner reference.
func enqueuePrimaryWork(work client.Object, eventType string, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if work == nil {
		wrappedErr := errors.NewUnexpectedError(nil, "received a work object event with no object", "eventType", eventType)
		klog.ErrorS(wrappedErr, "Failed to process a work object event", errors.Args(wrappedErr)...)
		return
	}

	if _, isPrimary := work.GetAnnotations()[placementv1alpha1.LinkedWorkCountAnnotationKey]; isPrimary {
		klog.V(2).InfoS("Enqueueing a primary work object", "work", klog.KObj(work), "eventType", eventType)
		queue.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: work.GetNamespace(),
			Name:      work.GetName(),
		}})
		return
	}

	ownerRef := metav1.GetControllerOfNoCopy(work)
	if ownerRef == nil ||
		ownerRef.APIVersion != placementv1alpha1.GroupVersion.String() ||
		ownerRef.Kind != placementv1alpha1.WorkKind ||
		ownerRef.Name == "" {
		wrappedErr := errors.NewUnexpectedError(nil,
			"the work object is neither a primary work object nor a secondary work object with a valid owner work reference",
			"work", klog.KObj(work), "eventType", eventType, "ownerRefs", work.GetOwnerReferences())
		klog.ErrorS(wrappedErr, "Failed to process a work object event", errors.Args(wrappedErr)...)
		return
	}

	// Owner references can only point to objects in the same namespace.
	klog.V(2).InfoS("Enqueueing the owner primary work object of a secondary work object",
		"work", klog.KObj(work), "primaryWork", klog.KRef(work.GetNamespace(), ownerRef.Name), "eventType", eventType)
	queue.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Namespace: work.GetNamespace(),
		Name:      ownerRef.Name,
	}})
}
