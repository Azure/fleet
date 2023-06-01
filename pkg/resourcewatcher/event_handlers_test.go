/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package resourcewatcher

import (
	"context"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils/controller"
)

func TestHandleTombStoneObj(t *testing.T) {
	var (
		secretObj = &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
			},
		}
		clusterRoleObj = &rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Role",
				APIVersion: "rbac.authorization.k8s.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
			},
		}

		deletedRole = &rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Role",
				APIVersion: "rbac.authorization.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
			},
		}
	)
	tests := []struct {
		name    string
		object  interface{}
		wantErr bool
		want    client.Object
	}{
		{
			name:    "namespace scoped resource in core group",
			object:  secretObj,
			wantErr: false,
			want:    secretObj,
		},
		{
			name:    "cluster scoped resource",
			object:  clusterRoleObj,
			wantErr: false,
			want:    clusterRoleObj,
		},
		{
			name: "tomestone object",
			object: cache.DeletedFinalStateUnknown{
				Key: "foo",
				Obj: deletedRole,
			},
			wantErr: false,
			want:    deletedRole,
		},
		{
			name: "none runtime object should be error",
			object: fleetv1alpha1.ResourceIdentifier{
				Name:      "foo",
				Namespace: "bar",
			},
			wantErr: true,
		},
		{
			name:    "nil object should be error",
			object:  nil,
			wantErr: true,
		},
	}

	for _, test := range tests {
		tt := test
		t.Run(tt.name, func(t *testing.T) {
			got, err := handleTombStoneObj(tt.object)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleTombStoneObj() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("handleTombStoneObj() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChangeDetector_onMemberClusterUpdated(t *testing.T) {
	var tests = map[string]struct {
		oldMC       fleetv1alpha1.MemberCluster
		newMC       fleetv1alpha1.MemberCluster
		wantEnqueue bool
	}{
		"should enqueue member cluster if their work agent status has changed": {
			oldMC: fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
				},
			},
			newMC: fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			wantEnqueue: true,
		},
		"should enqueue member cluster if their work agent spec has changed": {
			oldMC: fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			},
			newMC: fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
			},
			wantEnqueue: true,
		},
		"should enqueue member cluster if their work agent label has changed": {
			oldMC: fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"foo": "bar"},
				},
			},
			newMC: fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"foo": "bar2"},
				},
			},
			wantEnqueue: true,
		},
		"should enqueue member cluster if their work agent status is added": {
			oldMC: fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
						},
					},
				},
			},
			newMC: fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			wantEnqueue: true,
		},
		"should not enqueue member cluster if their work agent status is the same regardless of heartbeat time": {
			oldMC: fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionFalse,
								},
							},
							LastReceivedHeartbeat: metav1.Now(),
						},
					},
				},
			},
			newMC: fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionFalse,
								},
							},
							LastReceivedHeartbeat: metav1.Time{
								Time: metav1.Now().Add(time.Second),
							},
						},
					},
				},
			},
			wantEnqueue: false,
		},
		"should not enqueue member cluster if other agent status changes": {
			oldMC: fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionFalse,
								},
							},
							LastReceivedHeartbeat: metav1.Now(),
						},
					},
				},
			},
			newMC: fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionFalse,
								},
							},
						},
						{
							Type: fleetv1alpha1.MultiClusterServiceAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionFalse,
								},
							},
							LastReceivedHeartbeat: metav1.Now(),
						},
					},
				},
			},
			wantEnqueue: false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mcpc := &fakeController{}
			d := &ChangeDetector{
				MemberClusterPlacementController: mcpc,
			}
			oldObj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(&tt.oldMC)
			newObj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(&tt.newMC)
			d.onMemberClusterUpdated(&unstructured.Unstructured{Object: oldObj}, &unstructured.Unstructured{Object: newObj})
			if tt.wantEnqueue != mcpc.Enqueued {
				t.Errorf("test `%s` is unexpected, want enqueued %t, get enqueued %t", name, tt.wantEnqueue, mcpc.Enqueued)
			}
		})
	}
}

var _ controller.Controller = &fakeController{}

// fakeController just record if there is an enqueue request or not
type fakeController struct {
	Enqueued bool
}

func (t *fakeController) Enqueue(_ interface{}) {
	t.Enqueued = true
}

func (t *fakeController) Run(_ context.Context, _ int) error {
	//TODO implement me
	panic("implement me")
}
