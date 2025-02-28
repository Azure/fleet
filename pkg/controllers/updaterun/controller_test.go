/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestHandleClusterApprovalRequest(t *testing.T) {
	tests := map[string]struct {
		oldObj        client.Object
		newObj        client.Object
		shouldEnqueue bool
		queuedName    string
	}{
		"it should not enqueue anything if the obj is not a ClusterApprovalRequest": {
			oldObj:        &placementv1beta1.ClusterStagedUpdateRun{},
			shouldEnqueue: false,
		},
		"it should not enqueue anything if targetUpdateRun in spec is empty": {
			oldObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "",
				},
			},
			newObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
							ObservedGeneration: 1,
						},
					},
				},
			},
			shouldEnqueue: false,
		},
		"it should enqueue the targetUpdateRun if oldObj is not approved while newobj is approved": {
			oldObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
			},
			newObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
							ObservedGeneration: 1,
						},
					},
				},
			},
			shouldEnqueue: true,
			queuedName:    "test",
		},
		"it should enqueue the targetUpdateRun if oldObj is not declined while newobj is approved": {
			oldObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
							ObservedGeneration: 1,
						},
					},
				},
			},
			newObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
							ObservedGeneration: 1,
						},
					},
				},
			},
			shouldEnqueue: true,
			queuedName:    "test",
		},
		"it should enqueue the targetUpdateRun if oldObj is approved while newobj is not approved": {
			oldObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
							ObservedGeneration: 1,
						},
					},
				},
			},
			newObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
			},
			shouldEnqueue: true,
			queuedName:    "test",
		},
		"it should enqueue the targetUpdateRun if oldObj is approved while newobj is declined": {
			oldObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
							ObservedGeneration: 1,
						},
					},
				},
			},
			newObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
							ObservedGeneration: 1,
						},
					},
				},
			},
			shouldEnqueue: true,
			queuedName:    "test",
		},
		"it should not enqueue the targetUpdateRun if neither oldObj nor newobj is approved": {
			oldObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
			},
			newObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
			},
			shouldEnqueue: false,
		},
		"it should not enqueue the targetUpdateRun if both oldObj and newobj are approved": {
			oldObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
							ObservedGeneration: 1,
						},
					},
				},
			},
			newObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
							ObservedGeneration: 1,
						},
					},
				},
			},
			shouldEnqueue: false,
		},
		"it should not enqueue the targetUpdateRun if both oldObj and newobj are declined": {
			oldObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
							ObservedGeneration: 1,
						},
					},
				},
			},
			newObj: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
							ObservedGeneration: 1,
						},
					},
				},
			},
			shouldEnqueue: false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			queue := &controllertest.Queue{TypedInterface: workqueue.NewTypedRateLimitingQueue[reconcile.Request](
				workqueue.DefaultTypedItemBasedRateLimiter[reconcile.Request]())}
			handleClusterApprovalRequest(tt.oldObj, tt.newObj, queue)
			if got := queue.Len() != 0; got != tt.shouldEnqueue {
				t.Fatalf("handleClusterApprovalRequest() shouldEnqueue test `%s` got %t, want %t", name, got, tt.shouldEnqueue)
			}
			if tt.shouldEnqueue {
				req, _ := queue.TypedInterface.Get()
				if req.Name != tt.queuedName {
					t.Fatalf("handleClusterApprovalRequest() queuedName test `%s` got %s, want %s", name, req.Name, tt.queuedName)
				}
			}
		})
	}
}
