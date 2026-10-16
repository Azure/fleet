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

package stagedupdaterun

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

var (
	approvalReqEventHandlerFuncs = &handler.TypedFuncs[client.Object, ctrl.Request]{
		// Normally the staged update run controller does not need to watch for approval request create events, as the
		// controller itself is the creator of the objects and needs only to wait for the created requests to be
		// updated. However, a corner case still exists, where the controller might be restarted and the approval
		// request gets updated during the downtime; in this situation, the update event will not be delivered and
		// the controller sees only a create event for the already updated approval request, hence the handler setup
		// here.
		CreateFunc: func(
			ctx context.Context,
			e event.TypedCreateEvent[client.Object],
			q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
			obj := e.Object
			req, err := castToApprovalRequest(obj)
			if err != nil {
				klog.ErrorS(err, "Failed to enqueue an approval request create event", errors.Args(err)...)
				return
			}

			// Enqueue the request if the approval condition exists (in whatever status).
			//
			// If the condition is absent, the handler will be triggered again when it is added.
			if meta.FindStatusCondition(req.GetStatus().Conditions, rolloutv1alpha1.ApprovalRequestCondTypeApproved) == nil {
				return
			}

			// Do a sanity check; verify that the approval request references a staged update run.
			if req.GetSpec().StagedUpdateRunName == "" {
				err := errors.NewUnexpectedError(nil, "no staged update run reference on approval request", "approvalRequest", klog.KObj(obj))
				klog.ErrorS(err, "Failed to enqueue an approval request create event", errors.Args(err)...)
				return
			}

			// The staged update run shares the namespace with the approval request (empty for cluster-scoped ones).
			q.Add(ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: req.GetNamespace(),
					Name:      req.GetSpec().StagedUpdateRunName,
				},
			})
		},
		// Handle update events for approval requests; enqueue the staged update run for processing if the approval
		// status changes.
		UpdateFunc: func(
			ctx context.Context,
			e event.TypedUpdateEvent[client.Object],
			q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
			oldObj, newObj := e.ObjectOld, e.ObjectNew
			oldReq, err := castToApprovalRequest(oldObj)
			if err != nil {
				klog.ErrorS(err, "Failed to cast old approval request in update event", errors.Args(err)...)
				return
			}
			newReq, err := castToApprovalRequest(newObj)
			if err != nil {
				klog.ErrorS(err, "Failed to cast new approval request in update event", errors.Args(err)...)
				return
			}

			// Enqueue the request if the approval condition's status changed between the old and new approval request.
			oldApprovalCond := meta.FindStatusCondition(oldReq.GetStatus().Conditions, rolloutv1alpha1.ApprovalRequestCondTypeApproved)
			newApprovalCond := meta.FindStatusCondition(newReq.GetStatus().Conditions, rolloutv1alpha1.ApprovalRequestCondTypeApproved)
			switch {
			case newApprovalCond == nil:
				// The approval condition is absent in the new object; no need to enqueue the request.
				return
			case oldApprovalCond == nil:
				// The approval condition was added; enqueue the request.
			case newApprovalCond.Status != oldApprovalCond.Status:
				// The approval condition's status changed; enqueue the request.
			default:
				// The approval condition did not change in a way that requires enqueuing the request.
				return
			}

			// Do a sanity check; verify that the approval request references a staged update run.
			if newReq.GetSpec().StagedUpdateRunName == "" {
				err := errors.NewUnexpectedError(nil, "no staged update run reference on approval request", "approvalRequest", klog.KObj(newObj))
				klog.ErrorS(err, "Failed to enqueue an approval request update event", errors.Args(err)...)
				return
			}

			q.Add(ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: newReq.GetNamespace(),
					Name:      newReq.GetSpec().StagedUpdateRunName,
				},
			})
		},
		// Handle a corner case where the approval request is deleted and needs re-creation.
		DeleteFunc: func(
			ctx context.Context,
			e event.TypedDeleteEvent[client.Object],
			q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
			obj := e.Object
			req, err := castToApprovalRequest(obj)
			if err != nil {
				klog.ErrorS(err, "Failed to cast deleted approval request", errors.Args(err)...)
				return
			}

			// Do a sanity check; verify that the approval request references a staged update run.
			if req.GetSpec().StagedUpdateRunName == "" {
				err := errors.NewUnexpectedError(nil, "no staged update run reference on deleted approval request", "approvalRequest", klog.KObj(obj))
				klog.ErrorS(err, "Failed to enqueue an approval request delete event", errors.Args(err)...)
				return
			}

			q.Add(ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: req.GetNamespace(),
					Name:      req.GetSpec().StagedUpdateRunName,
				},
			})
		},
	}
)

func castToApprovalRequest(obj client.Object) (rolloutv1alpha1.ApprovalRequestAccessor, error) {
	if obj.GetNamespace() == "" {
		req, ok := obj.(*rolloutv1alpha1.ClusterApprovalRequest)
		if !ok {
			return nil, errors.NewUnexpectedError(nil, "failed to cast object as a cluster approval request", "gvk", obj.GetObjectKind().GroupVersionKind(), "obj", klog.KObj(obj))
		}
		return req, nil
	} else {
		req, ok := obj.(*rolloutv1alpha1.ApprovalRequest)
		if !ok {
			return nil, errors.NewUnexpectedError(nil, "failed to cast object as an approval request", "gvk", obj.GetObjectKind().GroupVersionKind(), "obj", klog.KObj(obj))
		}
		return req, nil
	}
}
