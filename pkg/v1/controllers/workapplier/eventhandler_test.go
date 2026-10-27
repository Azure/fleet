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
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
)

const (
	eventHandlerTestNSName            = "fleet-member-experimental"
	eventHandlerTestPrimaryWorkName   = "work-1"
	eventHandlerTestSecondaryWorkName = "work-1-1"
)

func eventHandlerTestPrimaryWork() *placementv1alpha1.Work {
	return &placementv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: eventHandlerTestNSName,
			Name:      eventHandlerTestPrimaryWorkName,
			Annotations: map[string]string{
				placementv1alpha1.LinkedWorkCountAnnotationKey: "2",
			},
		},
	}
}

func eventHandlerTestSecondaryWork(ownerRef *metav1.OwnerReference) *placementv1alpha1.Work {
	work := &placementv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: eventHandlerTestNSName,
			Name:      eventHandlerTestSecondaryWorkName,
		},
	}
	if ownerRef != nil {
		work.OwnerReferences = []metav1.OwnerReference{*ownerRef}
	}
	return work
}

func eventHandlerTestOwnerRef(mutate func(*metav1.OwnerReference)) *metav1.OwnerReference {
	ownerRef := &metav1.OwnerReference{
		APIVersion: placementv1alpha1.GroupVersion.String(),
		Kind:       placementv1alpha1.WorkKind,
		Name:       eventHandlerTestPrimaryWorkName,
		UID:        "owner-uid",
		Controller: ptr.To(true),
	}
	if mutate != nil {
		mutate(ownerRef)
	}
	return ownerRef
}

// drainQueue returns all the requests currently in the queue.
func drainQueue(queue workqueue.TypedRateLimitingInterface[reconcile.Request]) []reconcile.Request {
	reqs := []reconcile.Request{}
	for queue.Len() > 0 {
		req, _ := queue.Get()
		reqs = append(reqs, req)
		queue.Done(req)
	}
	return reqs
}

func newTestQueue() workqueue.TypedRateLimitingInterface[reconcile.Request] {
	return workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
}

func primaryWorkRequest() reconcile.Request {
	return reconcile.Request{NamespacedName: types.NamespacedName{
		Namespace: eventHandlerTestNSName,
		Name:      eventHandlerTestPrimaryWorkName,
	}}
}

// TestEnqueuePrimaryWork tests the enqueuePrimaryWork function.
func TestEnqueuePrimaryWork(t *testing.T) {
	testCases := []struct {
		name     string
		work     client.Object
		wantReqs []reconcile.Request
	}{
		{
			name:     "primary work",
			work:     eventHandlerTestPrimaryWork(),
			wantReqs: []reconcile.Request{primaryWorkRequest()},
		},
		{
			name: "primary work with an owner reference",
			work: func() client.Object {
				work := eventHandlerTestPrimaryWork()
				work.OwnerReferences = []metav1.OwnerReference{*eventHandlerTestOwnerRef(func(o *metav1.OwnerReference) {
					o.Name = "some-other-work"
				})}
				return work
			}(),
			wantReqs: []reconcile.Request{primaryWorkRequest()},
		},
		{
			name:     "secondary work with a valid controller owner reference",
			work:     eventHandlerTestSecondaryWork(eventHandlerTestOwnerRef(nil)),
			wantReqs: []reconcile.Request{primaryWorkRequest()},
		},
		{
			name:     "nil work",
			work:     nil,
			wantReqs: []reconcile.Request{},
		},
		{
			name:     "work with no annotation and no owner reference",
			work:     eventHandlerTestSecondaryWork(nil),
			wantReqs: []reconcile.Request{},
		},
		{
			name: "work with a non-controller owner reference",
			work: eventHandlerTestSecondaryWork(eventHandlerTestOwnerRef(func(o *metav1.OwnerReference) {
				o.Controller = ptr.To(false)
			})),
			wantReqs: []reconcile.Request{},
		},
		{
			name: "work with an owner reference with a wrong API version",
			work: eventHandlerTestSecondaryWork(eventHandlerTestOwnerRef(func(o *metav1.OwnerReference) {
				o.APIVersion = "placement.kubefleet.dev/v1beta1"
			})),
			wantReqs: []reconcile.Request{},
		},
		{
			name: "work with an owner reference with a wrong kind",
			work: eventHandlerTestSecondaryWork(eventHandlerTestOwnerRef(func(o *metav1.OwnerReference) {
				o.Kind = placementv1alpha1.AppliedWorkKind
			})),
			wantReqs: []reconcile.Request{},
		},
		{
			name: "work with an owner reference with an empty name",
			work: eventHandlerTestSecondaryWork(eventHandlerTestOwnerRef(func(o *metav1.OwnerReference) {
				o.Name = ""
			})),
			wantReqs: []reconcile.Request{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			queue := newTestQueue()
			defer queue.ShutDown()

			enqueuePrimaryWork(tc.work, "test", queue)

			gotReqs := drainQueue(queue)
			if diff := cmp.Diff(gotReqs, tc.wantReqs); diff != "" {
				t.Errorf("enqueuePrimaryWork() requests diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestWorkEventHandler tests the workEventHandler event handler funcs.
func TestWorkEventHandler(t *testing.T) {
	ctx := context.Background()

	events := []struct {
		name string
		// handle invokes the handler func for the event type with the given object.
		handle func(obj client.Object, queue workqueue.TypedRateLimitingInterface[reconcile.Request])
	}{
		{
			name: "create",
			handle: func(obj client.Object, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				workEventHandler.Create(ctx, event.TypedCreateEvent[client.Object]{Object: obj}, queue)
			},
		},
		{
			name: "update",
			handle: func(obj client.Object, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				// The old object is deliberately a different work object; only the new one should be used.
				workEventHandler.Update(ctx, event.TypedUpdateEvent[client.Object]{
					ObjectOld: &placementv1alpha1.Work{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: eventHandlerTestNSName,
							Name:      "stale-work",
							Annotations: map[string]string{
								placementv1alpha1.LinkedWorkCountAnnotationKey: "1",
							},
						},
					},
					ObjectNew: obj,
				}, queue)
			},
		},
		{
			name: "delete",
			handle: func(obj client.Object, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				workEventHandler.Delete(ctx, event.TypedDeleteEvent[client.Object]{Object: obj}, queue)
			},
		},
		{
			name: "generic",
			handle: func(obj client.Object, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				workEventHandler.Generic(ctx, event.TypedGenericEvent[client.Object]{Object: obj}, queue)
			},
		},
	}

	objects := []struct {
		name     string
		obj      client.Object
		wantReqs []reconcile.Request
	}{
		{
			name:     "primary work",
			obj:      eventHandlerTestPrimaryWork(),
			wantReqs: []reconcile.Request{primaryWorkRequest()},
		},
		{
			name:     "secondary work",
			obj:      eventHandlerTestSecondaryWork(eventHandlerTestOwnerRef(nil)),
			wantReqs: []reconcile.Request{primaryWorkRequest()},
		},
		{
			name:     "nil work",
			obj:      nil,
			wantReqs: []reconcile.Request{},
		},
	}

	for _, evt := range events {
		for _, o := range objects {
			t.Run(evt.name+"/"+o.name, func(t *testing.T) {
				queue := newTestQueue()
				defer queue.ShutDown()

				evt.handle(o.obj, queue)

				gotReqs := drainQueue(queue)
				if diff := cmp.Diff(gotReqs, o.wantReqs); diff != "" {
					t.Errorf("workEventHandler %s requests diff (-got, +want):\n%s", evt.name, diff)
				}
			})
		}
	}
}
