/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clustereligibilitychecker

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	clusterName = "bravelion"
)

// TestIsClusterEligible tests the IsClusterEligible function.
func TestIsClusterEligible(t *testing.T) {
	clusterHeartbeatTimeout := time.Minute * 15
	clusterHealthCheckTimeout := time.Minute * 15
	checker := New(
		WithClusterHeartbeatTimeout(clusterHeartbeatTimeout),
		WithClusterHeartbeatTimeout(clusterHealthCheckTimeout),
	)

	testCases := []struct {
		name             string
		cluster          *fleetv1beta1.MemberCluster
		wantEligible     bool
		wantReasonPrefix string
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
			wantReasonPrefix: "cluster has left the fleet",
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
			wantReasonPrefix: "cluster is not connected to the fleet: member agent not online yet",
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
			wantReasonPrefix: "cluster is not connected to the fleet: member agent not joined yet",
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
			wantReasonPrefix: "cluster is not connected to the fleet: member agent not joined yet",
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
			wantReasonPrefix: "cluster is not connected to the fleet: no recent heartbeat signals",
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
			wantReasonPrefix: "cluster is not connected to the fleet: health condition from member agent is not available",
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
			wantReasonPrefix: "cluster is not connected to the fleet: unhealthy for a prolonged period of time",
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
			wantEligible: true,
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
