/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// TestCycleStateBasicOps tests the basic ops of a CycleState.
func TestCycleStateBasicOps(t *testing.T) {
	clusters := []fleetv1beta1.MemberCluster{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		},
	}
	scheduledOrBoundBindings := []*fleetv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: bindingName,
			},
			Spec: fleetv1beta1.ResourceBindingSpec{
				TargetCluster: clusterName,
				State:         fleetv1beta1.BindingStateBound,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: altBindingName,
			},
			Spec: fleetv1beta1.ResourceBindingSpec{
				TargetCluster: altClusterName,
				State:         fleetv1beta1.BindingStateScheduled,
			},
		},
	}

	cs := NewCycleState(clusters, scheduledOrBoundBindings)

	k, v := "key", "value"
	cs.Write(StateKey(k), StateValue(v))
	if out, err := cs.Read("key"); out != "value" || err != nil {
		t.Fatalf("Read(%v) = %v, %v, want %v, nil", k, out, err, v)
	}
	cs.Delete(StateKey(k))
	if out, err := cs.Read("key"); out != nil || err == nil {
		t.Fatalf("Read(%v) = %v, %v, want nil, not found error", k, out, err)
	}

	clustersInState := cs.ListClusters()
	if diff := cmp.Diff(clustersInState, clusters); diff != "" {
		t.Fatalf("ListClusters() diff (-got, +want): %s", diff)
	}

	for _, binding := range scheduledOrBoundBindings {
		if !cs.IsClusterScheduledOrBound(binding.Spec.TargetCluster) {
			t.Fatalf("IsClusterScheduledOrBound(%v) = false, want true", binding.Spec.TargetCluster)
		}
	}
}

// TestPrepareScheduledOrBoundMap tests the prepareScheduledOrBoundMap function.
func TestPrepareScheduledOrBoundMap(t *testing.T) {
	scheduled := []*fleetv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: bindingName,
			},
			Spec: fleetv1beta1.ResourceBindingSpec{
				TargetCluster: clusterName,
			},
		},
	}
	bound := []*fleetv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: altBindingName,
			},
			Spec: fleetv1beta1.ResourceBindingSpec{
				TargetCluster: altClusterName,
			},
		},
	}

	want := map[string]bool{
		clusterName:    true,
		altClusterName: true,
	}

	scheduleOrBoundMap := prepareScheduledOrBoundMap(scheduled, bound)
	if diff := cmp.Diff(scheduleOrBoundMap, want); diff != "" {
		t.Errorf("preparedScheduledOrBoundMap() scheduledOrBoundMap diff (-got, +want): %s", diff)
	}
}
