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

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterinventory "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

func TestFillInClusterStatus(t *testing.T) {
	reconciler := &Reconciler{}

	tests := []struct {
		name                 string
		memberCluster        *clusterv1beta1.MemberCluster
		clusterProfile       *clusterinventory.ClusterProfile
		expectVersion        bool
		expectedK8sVersion   string
		expectAccessProvider bool
		expectedServer       string
		expectedCAData       string
	}{
		{
			name: "Cluster property collection has not succeeded",
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cluster",
					Generation: 1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			clusterProfile:       &clusterinventory.ClusterProfile{},
			expectVersion:        false,
			expectAccessProvider: false,
		},
		{
			name: "Cluster property collection succeeded but no properties",
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cluster",
					Generation: 1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{},
				},
			},
			clusterProfile:       &clusterinventory.ClusterProfile{},
			expectVersion:        false,
			expectAccessProvider: true,
		},
		{
			name: "Cluster property collection succeeded with k8s version only",
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cluster",
					Generation: 1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.K8sVersionProperty: {
							Value: "v1.28.0",
						},
					},
				},
			},
			clusterProfile:       &clusterinventory.ClusterProfile{},
			expectVersion:        true,
			expectedK8sVersion:   "v1.28.0",
			expectAccessProvider: true,
		},
		{
			name: "Cluster property collection succeeded with all properties",
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cluster",
					Generation: 1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.K8sVersionProperty: {
							Value: "v1.29.1",
						},
						propertyprovider.ClusterEntryPointProperty: {
							Value: "https://api.test-cluster.example.com:6443",
						},
						propertyprovider.ClusterCertificateAuthorityProperty: {
							Value: "dGVzdC1jYS1kYXRh",
						},
					},
				},
			},
			clusterProfile:       &clusterinventory.ClusterProfile{},
			expectVersion:        true,
			expectedK8sVersion:   "v1.29.1",
			expectAccessProvider: true,
			expectedServer:       "https://api.test-cluster.example.com:6443",
			expectedCAData:       "dGVzdC1jYS1kYXRh",
		},
		{
			name: "Cluster property collection succeeded with partial properties",
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cluster",
					Generation: 1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.K8sVersionProperty: {
							Value: "v1.27.5",
						},
						propertyprovider.ClusterEntryPointProperty: {
							Value: "https://api.partial-cluster.example.com:6443",
						},
					},
				},
			},
			clusterProfile:       &clusterinventory.ClusterProfile{},
			expectVersion:        true,
			expectedK8sVersion:   "v1.27.5",
			expectAccessProvider: true,
			expectedServer:       "https://api.partial-cluster.example.com:6443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler.fillInClusterStatus(tt.memberCluster, tt.clusterProfile)

			expected := clusterinventory.ClusterProfileStatus{}
			if tt.expectVersion {
				expected.Version.Kubernetes = tt.expectedK8sVersion
			}
			if tt.expectAccessProvider {
				expected.AccessProviders = []clusterinventory.AccessProvider{{
					Name: controller.ClusterManagerName,
				}}
				if tt.expectedServer != "" {
					expected.AccessProviders[0].Cluster.Server = tt.expectedServer
				}
				if tt.expectedCAData != "" {
					expected.AccessProviders[0].Cluster.CertificateAuthorityData = []byte(tt.expectedCAData)
				}
			}

			if diff := cmp.Diff(expected, tt.clusterProfile.Status); diff != "" {
				t.Fatalf("test case `%s` failed diff (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}

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
			if condition == nil { //nolint: staticcheck // false positive SA5011: possible nil pointer dereference
				t.Fatalf("expected condition to be set, but it was not")
			}
			if condition.Status != tt.expectedConditionStatus { //nolint: staticcheck // false positive SA5011: possible nil pointer dereference
				t.Errorf("test case `%s` failed, expected condition status %v, got %v", tt.name, tt.expectedConditionStatus, condition.Status)
			}
			if condition.Reason != tt.expectedConditionReason { //nolint: staticcheck // false positive SA5011: possible nil pointer dereference
				t.Errorf("test case `%s` failed, expected condition reason %v, got %v", tt.name, tt.expectedConditionReason, condition.Reason)
			}
		})
	}
}
