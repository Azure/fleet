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

package sameplacementaffinity

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

const (
	clusterName = "member-1"
)

var (
	cmpStatusOptions = cmp.Options{
		cmpopts.IgnoreFields(framework.Status{}, "reasons", "err"),
		cmp.AllowUnexported(framework.Status{}),
	}
	defaultPluginName = defaultPluginOptions.name
)

func TestFilter(t *testing.T) {
	tests := []struct {
		name                     string
		scheduledOrBoundBindings []*placementv1beta1.ClusterResourceBinding
		want                     *framework.Status
	}{
		{
			name: "placement has already been scheduled",
			scheduledOrBoundBindings: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
						State:         placementv1beta1.BindingStateScheduled,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding2",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "another-cluster",
						State:         placementv1beta1.BindingStateScheduled,
					},
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterAlreadySelected, defaultPluginName),
		},
		{
			name: "placement has already been bounded",
			scheduledOrBoundBindings: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
						State:         placementv1beta1.BindingStateBound,
					},
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterAlreadySelected, defaultPluginName),
		},
		{
			name:                     "no bindings",
			scheduledOrBoundBindings: []*placementv1beta1.ClusterResourceBinding{},
			want:                     nil,
		},
		{
			name: "placement has been bounded/scheduled on other cluster",
			scheduledOrBoundBindings: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "another-cluster",
						State:         placementv1beta1.BindingStateBound,
					},
				},
			},
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := New()
			state := framework.NewCycleState(nil, nil, tc.scheduledOrBoundBindings)
			cluster := clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			}
			got := p.Filter(context.Background(), state, nil, &cluster)
			if diff := cmp.Diff(tc.want, got, cmpStatusOptions); diff != "" {
				t.Errorf("Filter() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
