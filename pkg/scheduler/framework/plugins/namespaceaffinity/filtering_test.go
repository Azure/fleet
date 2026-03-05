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

package namespaceaffinity

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

const (
	clusterName1 = "cluster-1"
	pluginName   = "NamespaceAffinity"
)

var (
	ignoreStatusErrorField = cmpopts.IgnoreFields(framework.Status{}, "err")
)

// TestPreFilter tests the PreFilter extension point of the plugin.
func TestPreFilter(t *testing.T) {
	testCases := []struct {
		name       string
		ps         placementv1beta1.PolicySnapshotObj
		wantStatus *framework.Status
	}{
		{
			name: "cluster-scoped placement",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, pluginName, "cluster-scoped placement does not require namespace affinity filtering"),
		},
		{
			name: "namespace-scoped placement",
			ps: &placementv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "test-namespace",
				},
			},
			wantStatus: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := New()
			ctx := context.Background()
			state := framework.NewCycleState(nil, nil, nil)
			status := p.PreFilter(ctx, state, tc.ps)

			if diff := cmp.Diff(
				status, tc.wantStatus,
				cmp.AllowUnexported(framework.Status{}),
				ignoreStatusErrorField,
			); diff != "" {
				t.Errorf("PreFilter() unexpected status (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestFilter tests the Filter extension point of the plugin.
func TestFilter(t *testing.T) {
	testCases := []struct {
		name       string
		ps         *placementv1beta1.SchedulingPolicySnapshot
		cluster    *clusterv1beta1.MemberCluster
		wantStatus *framework.Status
	}{
		{
			name: "namespace collection not enabled - should pass",
			ps: &placementv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Namespaces: nil,
					Conditions: []metav1.Condition{},
				},
			},
			wantStatus: nil,
		},
		{
			name: "namespace collection condition false (degraded) but no namespace info - should filter",
			ps: &placementv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Namespaces: nil,
					Conditions: []metav1.Condition{
						{
							Type:   propertyprovider.NamespaceCollectionSucceededCondType,
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, pluginName, "cluster has no namespace information available"),
		},
		{
			name: "namespace collection condition false (degraded) with namespace exists - should pass",
			ps: &placementv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Namespaces: map[string]string{
						"test-namespace": "work-1",
					},
					Conditions: []metav1.Condition{
						{
							Type:   propertyprovider.NamespaceCollectionSucceededCondType,
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			wantStatus: nil,
		},
		{
			name: "namespace collection enabled but no namespace info - should filter",
			ps: &placementv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Namespaces: nil,
					Conditions: []metav1.Condition{
						{
							Type:   propertyprovider.NamespaceCollectionSucceededCondType,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, pluginName, "cluster has no namespace information available"),
		},
		{
			name: "namespace collection enabled, namespace missing - should filter",
			ps: &placementv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Namespaces: map[string]string{
						"other-namespace": "work-1",
					},
					Conditions: []metav1.Condition{
						{
							Type:   propertyprovider.NamespaceCollectionSucceededCondType,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, pluginName, "target namespace does not exist on cluster"),
		},
		{
			name: "namespace collection enabled, namespace exists - should pass",
			ps: &placementv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Namespaces: map[string]string{
						"test-namespace":  "work-1",
						"other-namespace": "work-2",
					},
					Conditions: []metav1.Condition{
						{
							Type:   propertyprovider.NamespaceCollectionSucceededCondType,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			wantStatus: nil,
		},
		{
			name: "namespace collection enabled, namespace exists with empty work name - should pass",
			ps: &placementv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Status: clusterv1beta1.MemberClusterStatus{
					Namespaces: map[string]string{
						"test-namespace": "",
					},
					Conditions: []metav1.Condition{
						{
							Type:   propertyprovider.NamespaceCollectionSucceededCondType,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			wantStatus: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := New()
			ctx := context.Background()
			state := framework.NewCycleState(nil, nil, nil)
			status := p.Filter(ctx, state, tc.ps, tc.cluster)

			if diff := cmp.Diff(
				status, tc.wantStatus,
				cmp.AllowUnexported(framework.Status{}),
				ignoreStatusErrorField,
			); diff != "" {
				t.Errorf("Filter() unexpected status (-got, +want):\n%s", diff)
			}
		})
	}
}
