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

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
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

	policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}
	deleteTime := metav1.Now()
	testCases := []struct {
		name    string
		cluster *clusterv1beta1.MemberCluster
		want    *framework.Status
	}{
		{
			name: "cluster left",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:              clusterName,
					DeletionTimestamp: &deleteTime,
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName, ""),
		},
		{
			name: "no member agent status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Status: clusterv1beta1.MemberClusterStatus{},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName, ""),
		},
		{
			name: "no member agent joined condition",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type:                  clusterv1beta1.MemberAgent,
							LastReceivedHeartbeat: metav1.Now(),
						},
					},
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName, ""),
		},
		{
			name: "joined condition is false",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
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
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
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
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
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
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
								},
								{
									Type:               string(clusterv1beta1.AgentHealthy),
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
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
								},
								{
									Type:               string(clusterv1beta1.AgentHealthy),
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
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
								},
								{
									Type:               string(clusterv1beta1.AgentHealthy),
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
