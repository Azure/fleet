/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package rollout

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestReconciler_handleResourceSnapshot(t *testing.T) {
	tests := map[string]struct {
		snapshot      client.Object
		shouldEnqueue bool
	}{
		"test enqueue a new master active resourceSnapshot": {
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel:      "placement",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation: "hash",
					},
				},
			},
			shouldEnqueue: true,
		},
		"test skip a none master active resourceSnapshot": {
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel:      "placement",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
				},
			},
			shouldEnqueue: false,
		},
		"test skip a none active master resourceSnapshot": {
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation: "1",
					},
				},
			},
			shouldEnqueue: false,
		},
		"test skip a none active none master resourceSnapshot": {
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
				},
			},
			shouldEnqueue: false,
		},
		"test skip a malformatted active  master resourceSnapshot": {
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation: "1",
					},
				},
			},
			shouldEnqueue: false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			queue := controllertest.Queue{Interface: workqueue.New()}
			handleResourceSnapshot(tt.snapshot, queue)
			if tt.shouldEnqueue && queue.Len() == 0 {
				t.Errorf("handleResourceSnapshot test `%s` didn't queue the object when it should enqueue", name)
			}
			if !tt.shouldEnqueue && queue.Len() != 0 {
				t.Errorf("handleResourceSnapshot test `%s` queue the object when it should not enqueue", name)
			}
		})
	}
}

func TestReconciler_handleResourceBinding(t *testing.T) {
	tests := map[string]struct {
		resourceBinding client.Object
		shouldEnqueue   bool
	}{
		"test enqueue a good resourceBinding": {
			resourceBinding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
				},
			},
			shouldEnqueue: true,
		},
		"test skip a malformatted resourceBinding": {
			resourceBinding: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{},
			},
			shouldEnqueue: false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			queue := controllertest.Queue{Interface: workqueue.New()}
			handleResourceBinding(tt.resourceBinding, queue)
			if tt.shouldEnqueue && queue.Len() == 0 {
				t.Errorf("handleResourceBinding test `%s` didn't queue the object when it should enqueue", name)
			}
			if !tt.shouldEnqueue && queue.Len() != 0 {
				t.Errorf("handleResourceSnapshot test `%s` queue the object when it should not enqueue", name)
			}
		})
	}
}

func Test_pickBindingsToRoll(t *testing.T) {
	tests := map[string]struct {
		allBindings                []*fleetv1beta1.ClusterResourceBinding
		latestResourceSnapshotName string
		crp                        *fleetv1beta1.ClusterResourcePlacement
		tobeUpdatedBindings        []int
		needRoll                   bool
	}{
		// TODO: add more tests
		"test bound with out dated bindings": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", "cluster-1"),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickAllPlacementType, 0)),
			tobeUpdatedBindings: []int{0},
			needRoll:            true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			gotUpdatedBindings, gotNeedRoll := pickBindingsToRoll(tt.allBindings, tt.latestResourceSnapshotName, tt.crp)
			tobeUpdatedBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)
			for _, index := range tt.tobeUpdatedBindings {
				tobeUpdatedBindings = append(tobeUpdatedBindings, tt.allBindings[index])
			}
			if !reflect.DeepEqual(gotUpdatedBindings, tobeUpdatedBindings) {
				t.Errorf("pickBindingsToRoll() gotUpdatedBindings = %v, want %v", gotUpdatedBindings, tt.tobeUpdatedBindings)
			}
			if gotNeedRoll != tt.needRoll {
				t.Errorf("pickBindingsToRoll() gotNeedRoll = %v, want %v", gotNeedRoll, tt.needRoll)
			}
		})
	}
}

func TestReconciler_updateBindings(t *testing.T) {
	tests := map[string]struct {
		name                       string
		Client                     client.Client
		latestResourceSnapshotName string
		toBeUpgradedBinding        []*fleetv1beta1.ClusterResourceBinding
		wantErr                    bool
	}{
		// TODO: Add negative test cases with fake client
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := &Reconciler{
				Client: tt.Client,
			}
			if err := r.updateBindings(context.TODO(), tt.latestResourceSnapshotName, tt.toBeUpgradedBinding); (err != nil) != tt.wantErr {
				t.Errorf("updateBindings() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func createPlacementPolicyForTest(placementType fleetv1beta1.PlacementType, numberOfClusters int32) *fleetv1beta1.PlacementPolicy {
	return &fleetv1beta1.PlacementPolicy{
		PlacementType:    placementType,
		NumberOfClusters: pointer.Int32(numberOfClusters),
		Affinity: &fleetv1beta1.Affinity{
			ClusterAffinity: &fleetv1beta1.ClusterAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &fleetv1beta1.ClusterSelector{
					ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value1",
								},
							},
						},
					},
				},
			},
		},
	}
}

func clusterResourcePlacementForTest(crpName string, policy *fleetv1beta1.PlacementPolicy) *fleetv1beta1.ClusterResourcePlacement {
	return &fleetv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpName,
		},
		Spec: fleetv1beta1.ClusterResourcePlacementSpec{
			ResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
				{
					Group:   corev1.GroupName,
					Version: "v1",
					Kind:    "Service",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"region": "east"},
					},
				},
			},
			Policy: policy,
			Strategy: fleetv1beta1.RolloutStrategy{
				Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "20%",
					},
					MaxSurge: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 3,
					},
					UnavailablePeriodSeconds: pointer.Int(1),
				},
			},
		},
	}
}
