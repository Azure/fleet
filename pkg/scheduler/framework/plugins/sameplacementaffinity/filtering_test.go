/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package sameplacementaffinity

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
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
		scheduledOrBoundBindings []*fleetv1beta1.ClusterResourceBinding
		want                     *framework.Status
	}{
		{
			name: "placement has already been scheduled",
			scheduledOrBoundBindings: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding1",
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
						State:         fleetv1beta1.BindingStateUnscheduled,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding2",
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: "another-cluster",
						State:         fleetv1beta1.BindingStateScheduled,
					},
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName),
		},
		{
			name: "placement has already been bounded",
			scheduledOrBoundBindings: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding1",
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
						State:         fleetv1beta1.BindingStateBound,
					},
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName),
		},
		{
			name:                     "no bindings",
			scheduledOrBoundBindings: []*fleetv1beta1.ClusterResourceBinding{},
			want:                     nil,
		},
		{
			name: "placement has been bounded/scheduled on other cluster",
			scheduledOrBoundBindings: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding1",
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: "another-cluster",
						State:         fleetv1beta1.BindingStateBound,
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
			cluster := fleetv1beta1.MemberCluster{
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
