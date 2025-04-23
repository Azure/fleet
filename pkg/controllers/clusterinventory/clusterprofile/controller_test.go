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

package clusterprofile

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterinventory "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
)

func TestSyncClusterProfileCondition(t *testing.T) {
	clusterUnhealthyThreshold := 5 * time.Minute
	reconciler := &Reconciler{
		ClusterUnhealthyThreshold: clusterUnhealthyThreshold,
	}

	tests := []struct {
		name                    string
		memberCluster           *clusterv1beta1.MemberCluster
		clusterProfile          *clusterinventory.ClusterProfile
		expectedConditionStatus metav1.ConditionStatus
		expectedConditionReason string
	}{
		{
			name: "Member agent has not reported its status yet",
			memberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{},
			},
			clusterProfile:          &clusterinventory.ClusterProfile{},
			expectedConditionStatus: metav1.ConditionUnknown,
			expectedConditionReason: clusterNoStatusReason,
		},
		{
			name: "Member agent has reported its status, but the health condition is missing",
			memberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type:       clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{},
						},
					},
				},
			},
			clusterProfile:          &clusterinventory.ClusterProfile{},
			expectedConditionStatus: metav1.ConditionUnknown,
			expectedConditionReason: clusterHealthUnknownReason,
		},
		{
			name: "Member agent has lost its heartbeat connection to the Fleet hub cluster",
			memberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentHealthy),
									Status: metav1.ConditionTrue,
								},
							},
							LastReceivedHeartbeat: metav1.Time{Time: time.Now().Add(-10 * clusterUnhealthyThreshold)},
						},
					},
				},
			},
			clusterProfile:          &clusterinventory.ClusterProfile{},
			expectedConditionStatus: metav1.ConditionFalse,
			expectedConditionReason: clusterHeartbeatLostReason,
		},
		{
			name: "Member agent health check result is out of date or unknown",
			memberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentHealthy),
									Status: metav1.ConditionUnknown,
								},
							},
							LastReceivedHeartbeat: metav1.Time{Time: time.Now().Add(-1 * time.Second)},
						},
					},
				},
			},
			clusterProfile:          &clusterinventory.ClusterProfile{},
			expectedConditionStatus: metav1.ConditionUnknown,
			expectedConditionReason: clusterHealthUnknownReason,
		},
		{
			name: "Member agent reports that the API server is unhealthy",
			memberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentHealthy),
									Status: metav1.ConditionFalse,
								},
							},
							LastReceivedHeartbeat: metav1.Time{Time: time.Now().Add(-1 * time.Second)},
						},
					},
				},
			},
			clusterProfile:          &clusterinventory.ClusterProfile{},
			expectedConditionStatus: metav1.ConditionFalse,
			expectedConditionReason: clusterUnHealthyReason,
		},
		{
			name: "Member agent reports that the API server is healthy",
			memberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type:                  clusterv1beta1.MemberAgent,
							LastReceivedHeartbeat: metav1.Time{Time: time.Now().Add(-1 * time.Second)},
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentHealthy),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			clusterProfile:          &clusterinventory.ClusterProfile{},
			expectedConditionStatus: metav1.ConditionTrue,
			expectedConditionReason: clusterHealthyReason,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler.syncClusterProfileCondition(tt.memberCluster, tt.clusterProfile)
			condition := meta.FindStatusCondition(tt.clusterProfile.Status.Conditions, clusterinventory.ClusterConditionControlPlaneHealthy)
			if condition == nil {
				t.Fatalf("expected condition to be set, but it was not")
			}
			if condition.Status != tt.expectedConditionStatus {
				t.Errorf("test case `%s` failed, expected condition status %v, got %v", tt.name, tt.expectedConditionStatus, condition.Status)
			}
			if condition.Reason != tt.expectedConditionReason {
				t.Errorf("test case `%s` failed, expected condition reason %v, got %v", tt.name, tt.expectedConditionReason, condition.Reason)
			}
		})
	}
}
