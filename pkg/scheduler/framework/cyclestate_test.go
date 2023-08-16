/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// TestCycleStateBasicOps tests the basic ops of a CycleState.
func TestCycleStateBasicOps(t *testing.T) {
	clusters := []clusterv1beta1.MemberCluster{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		},
	}
	scheduledOrBoundBindings := []*placementv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: bindingName,
			},
			Spec: placementv1beta1.ResourceBindingSpec{
				TargetCluster: clusterName,
				State:         placementv1beta1.BindingStateBound,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: altBindingName,
			},
			Spec: placementv1beta1.ResourceBindingSpec{
				TargetCluster: altClusterName,
				State:         placementv1beta1.BindingStateScheduled,
			},
		},
	}

	obsoleteBindings := []*placementv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: anotherBindingName,
			},
			Spec: placementv1beta1.ResourceBindingSpec{
				TargetCluster: anotherClusterName,
			},
		},
	}

	cs := NewCycleState(clusters, obsoleteBindings, scheduledOrBoundBindings)

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
		if !cs.HasScheduledOrBoundBindingFor(binding.Spec.TargetCluster) {
			t.Fatalf("HasScheduledOrBoundBindingFor(%v) = false, want true", binding.Spec.TargetCluster)
		}
	}

	for _, binding := range obsoleteBindings {
		if !cs.HasObsoleteBindingFor(binding.Spec.TargetCluster) {
			t.Fatalf("HasObsoleteBindingFor(%v) = false, want true", binding.Spec.TargetCluster)
		}
	}
}

// TestPrepareScheduledOrBoundBindingsMap tests the prepareScheduledOrBoundBindingsMap function.
func TestPrepareScheduledOrBoundBindingsMap(t *testing.T) {
	scheduled := []*placementv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: bindingName,
			},
			Spec: placementv1beta1.ResourceBindingSpec{
				TargetCluster: clusterName,
			},
		},
	}
	bound := []*placementv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: altBindingName,
			},
			Spec: placementv1beta1.ResourceBindingSpec{
				TargetCluster: altClusterName,
			},
		},
	}

	want := map[string]bool{
		clusterName:    true,
		altClusterName: true,
	}

	scheduleOrBoundBindingsMap := prepareScheduledOrBoundBindingsMap(scheduled, bound)
	if diff := cmp.Diff(scheduleOrBoundBindingsMap, want); diff != "" {
		t.Errorf("preparedScheduledOrBoundBindingsMap() scheduledOrBoundBindingsMap diff (-got, +want): %s", diff)
	}
}

// TestPrepareObsoleteBindingsMap tests the prepareObsoleteBindingsMap function.
func TestPrepareObsoleteBindingsMap(t *testing.T) {
	obsolete := []*placementv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: bindingName,
			},
			Spec: placementv1beta1.ResourceBindingSpec{
				TargetCluster: clusterName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: altBindingName,
			},
			Spec: placementv1beta1.ResourceBindingSpec{
				TargetCluster: altClusterName,
			},
		},
	}

	want := map[string]bool{
		clusterName:    true,
		altClusterName: true,
	}

	obsoleteBindingsMap := prepareObsoleteBindingsMap(obsolete)
	if diff := cmp.Diff(obsoleteBindingsMap, want); diff != "" {
		t.Errorf("prepareObsoleteBindingsMap() obsoleteBindingsMap diff (-got, +want): %s", diff)
	}
}
