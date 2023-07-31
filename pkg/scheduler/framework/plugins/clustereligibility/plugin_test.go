/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clustereligibility

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/clustereligibilitychecker"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

const (
	clusterName = "bravelion"

	policyName = "test-policy"
)

var (
	ignoredStatusFields = cmpopts.IgnoreFields(framework.Status{}, "reasons", "err")
)

// Mock framework.Handle interface for set up the plugin.
type MockHandle struct {
	clusterEligibilityChecker *clustereligibilitychecker.ClusterEligibilityChecker
}

var (
	_ framework.Handle = &MockHandle{}
)

func (mh *MockHandle) Client() client.Client               { return nil }
func (mh *MockHandle) Manager() ctrl.Manager               { return nil }
func (mh *MockHandle) UncachedReader() client.Reader       { return nil }
func (mh *MockHandle) EventRecorder() record.EventRecorder { return nil }
func (mh *MockHandle) ClusterEligibilityChecker() *clustereligibilitychecker.ClusterEligibilityChecker {
	return mh.clusterEligibilityChecker
}

// TestFilter tests the Filter method.
func TestFilter(t *testing.T) {
	p := New()
	p.SetUpWithFramework(&MockHandle{
		clusterEligibilityChecker: clustereligibilitychecker.New(),
	})

	policy := &fleetv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}

	testCases := []struct {
		name    string
		cluster *fleetv1beta1.MemberCluster
		want    *framework.Status
	}{
		{
			name: "cluster left",
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: fleetv1beta1.MemberClusterSpec{
					State: fleetv1beta1.ClusterStateLeave,
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName, ""),
		},
		{
			name: "no member agent status",
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: fleetv1beta1.MemberClusterSpec{
					State: fleetv1beta1.ClusterStateJoin,
				},
				Status: fleetv1beta1.MemberClusterStatus{},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName, ""),
		},
		{
			name: "no member agent joined condition",
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: fleetv1beta1.MemberClusterSpec{
					State: fleetv1beta1.ClusterStateJoin,
				},
				Status: fleetv1beta1.MemberClusterStatus{
					AgentStatus: []fleetv1beta1.AgentStatus{
						{
							Type:                  fleetv1beta1.MemberAgent,
							LastReceivedHeartbeat: metav1.Now(),
						},
					},
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName, ""),
		},
		{
			name: "joined condition is false",
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: fleetv1beta1.MemberClusterSpec{
					State: fleetv1beta1.ClusterStateJoin,
				},
				Status: fleetv1beta1.MemberClusterStatus{
					AgentStatus: []fleetv1beta1.AgentStatus{
						{
							Type: fleetv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1beta1.AgentJoined),
									Status: metav1.ConditionFalse,
								},
							},
							LastReceivedHeartbeat: metav1.Now(),
						},
					},
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName, ""),
		},
		{
			name: "no recent heartbeat signals",
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: fleetv1beta1.MemberClusterSpec{
					State: fleetv1beta1.ClusterStateJoin,
				},
				Status: fleetv1beta1.MemberClusterStatus{
					AgentStatus: []fleetv1beta1.AgentStatus{
						{
							Type: fleetv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
								},
							},
							LastReceivedHeartbeat: metav1.NewTime(time.Now().Add(time.Minute * (-20))),
						},
					},
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName, ""),
		},
		{
			name: "no health check signals",
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: fleetv1beta1.MemberClusterSpec{
					State: fleetv1beta1.ClusterStateJoin,
				},
				Status: fleetv1beta1.MemberClusterStatus{
					AgentStatus: []fleetv1beta1.AgentStatus{
						{
							Type: fleetv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
								},
							},
							LastReceivedHeartbeat: metav1.NewTime(time.Now()),
						},
					},
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName, ""),
		},
		{
			name: "health check fails for a long period",
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: fleetv1beta1.MemberClusterSpec{
					State: fleetv1beta1.ClusterStateJoin,
				},
				Status: fleetv1beta1.MemberClusterStatus{
					AgentStatus: []fleetv1beta1.AgentStatus{
						{
							Type: fleetv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
								},
								{
									Type:               string(fleetv1beta1.AgentHealthy),
									Status:             metav1.ConditionFalse,
									LastTransitionTime: metav1.NewTime(time.Now().Add(time.Minute * (-20))),
								},
							},
							LastReceivedHeartbeat: metav1.NewTime(time.Now()),
						},
					},
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName, ""),
		},
		{
			name: "recent health check failure",
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: fleetv1beta1.MemberClusterSpec{
					State: fleetv1beta1.ClusterStateJoin,
				},
				Status: fleetv1beta1.MemberClusterStatus{
					AgentStatus: []fleetv1beta1.AgentStatus{
						{
							Type: fleetv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
								},
								{
									Type:               string(fleetv1beta1.AgentHealthy),
									Status:             metav1.ConditionFalse,
									LastTransitionTime: metav1.NewTime(time.Now().Add(time.Minute * (-1))),
								},
							},
							LastReceivedHeartbeat: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		},
		{
			name: "normal cluster",
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: fleetv1beta1.MemberClusterSpec{
					State: fleetv1beta1.ClusterStateJoin,
				},
				Status: fleetv1beta1.MemberClusterStatus{
					AgentStatus: []fleetv1beta1.AgentStatus{
						{
							Type: fleetv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
								},
								{
									Type:               string(fleetv1beta1.AgentHealthy),
									Status:             metav1.ConditionFalse,
									LastTransitionTime: metav1.NewTime(time.Now()),
								},
							},
							LastReceivedHeartbeat: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			state := framework.NewCycleState(nil, nil)

			status := p.Filter(ctx, state, policy, tc.cluster)
			if diff := cmp.Diff(status, tc.want, cmp.AllowUnexported(framework.Status{}), ignoredStatusFields); diff != "" {
				t.Errorf("p.Filter() status diff (-got, +want): %s", diff)
			}
		})
	}
}
