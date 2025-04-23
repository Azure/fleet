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

package clustereligibilitychecker

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
)

const (
	clusterName = "bravelion"
)

// TestIsClusterEligible tests the IsClusterEligible function.
func TestIsClusterEligible(t *testing.T) {
	clusterHeartbeatCheckTimeout := time.Minute * 15
	clusterHealthCheckTimeout := time.Minute * 15
	checker := New(
		WithClusterHeartbeatCheckTimeout(clusterHeartbeatCheckTimeout),
		WithClusterHealthCheckTimeout(clusterHealthCheckTimeout),
	)
	deleteTime := metav1.Now()
	testCases := []struct {
		name             string
		cluster          *clusterv1beta1.MemberCluster
		wantEligible     bool
		wantReasonPrefix string
	}{
		{
			name: "cluster left",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:              clusterName,
					DeletionTimestamp: &deleteTime,
				},
			},
			wantReasonPrefix: "cluster has left the fleet",
		},
		{
			name: "no member agent status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Status: clusterv1beta1.MemberClusterStatus{},
			},
			wantReasonPrefix: "cluster is not connected to the fleet: member agent not online yet",
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
			wantReasonPrefix: "cluster is not connected to the fleet: member agent not joined yet",
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
			wantReasonPrefix: "cluster is not connected to the fleet: member agent not joined yet",
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
			wantReasonPrefix: "cluster is not connected to the fleet: no recent heartbeat signals",
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
			wantReasonPrefix: "cluster is not connected to the fleet: health condition from member agent is not available",
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
			wantReasonPrefix: "cluster is not connected to the fleet: unhealthy for a prolonged period of time",
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
			wantEligible: true,
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
			wantEligible: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eligible, reason := checker.IsEligible(tc.cluster)
			if eligible != tc.wantEligible {
				t.Errorf("IsClusterEligible() eligible = %t, want %t", eligible, tc.wantEligible)
			}
			if !eligible && !strings.HasPrefix(reason, tc.wantReasonPrefix) {
				t.Errorf("IsClusterEligible() reason = %s, want %s", reason, tc.wantReasonPrefix)
			}
		})
	}
}
