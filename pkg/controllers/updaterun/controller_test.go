/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"testing"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestHandleClusterApprovalRequest(t *testing.T) {
	tests := map[string]struct {
		obj           client.Object
		shouldEnqueue bool
		queuedName    string
	}{
		"it should not enqueue anything if the obj is not a ClusterApprovalRequest": {
			obj:           &placementv1alpha1.ClusterStagedUpdateRun{},
			shouldEnqueue: false,
		},
		"it should not enqueue anything if targetUpdateRun in spec is empty": {
			obj: &placementv1alpha1.ClusterApprovalRequest{
				Spec: placementv1alpha1.ApprovalRequestSpec{
					TargetUpdateRun: "",
				},
			},
			shouldEnqueue: false,
		},
		"it should enqueue the targetUpdateRun if it is not empty": {
			obj: &placementv1alpha1.ClusterApprovalRequest{
				Spec: placementv1alpha1.ApprovalRequestSpec{
					TargetUpdateRun: "test",
				},
			},
			shouldEnqueue: true,
			queuedName:    "test",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			queue := &controllertest.Queue{Interface: workqueue.New()}
			handleClusterApprovalRequest(tt.obj, queue)
			if got := queue.Len() != 0; got != tt.shouldEnqueue {
				t.Errorf("handleClusterApprovalRequest() shouldEnqueue test `%s` got %t, want %t", name, got, tt.shouldEnqueue)
			}
			if tt.shouldEnqueue {
				item, _ := queue.Get()
				req, ok := item.(reconcile.Request)
				if !ok {
					t.Errorf("handleClusterApprovalRequest() queuedItem test `%s` got %T, want reconcile.Request", name, item)
				}
				if req.Name != tt.queuedName {
					t.Errorf("handleClusterApprovalRequest() queuedName test `%s` got %s, want %s", name, req.Name, tt.queuedName)
				}
			}
		})
	}
}
