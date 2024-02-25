/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package rollout

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
)

var (
	now = time.Now()

	cluster1 = "cluster-1"
	cluster2 = "cluster-2"
	cluster3 = "cluster-3"
	cluster4 = "cluster-4"
	cluster5 = "cluster-5"
)

func TestReconcilerHandleResourceSnapshot(t *testing.T) {
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
			queue := &controllertest.Queue{Interface: workqueue.New()}
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

func TestReconcilerHandleResourceBinding(t *testing.T) {
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
			queue := &controllertest.Queue{Interface: workqueue.New()}
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

func TestWaitForResourcesToCleanUp(t *testing.T) {
	tests := map[string]struct {
		allBindings []*fleetv1beta1.ClusterResourceBinding
		wantWait    bool
		wantErr     bool
	}{
		"test deleting binding block schedule binding on the same cluster": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
				generateDeletingClusterResourceBinding(cluster1),
			},
			wantWait: true,
			wantErr:  false,
		},
		"test deleting binding not block binding on different cluster": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
				generateDeletingClusterResourceBinding(cluster2),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster3),
			},
			wantWait: false,
			wantErr:  false,
		},
		"test deleting binding cannot co-exsit with a bound binding on same cluster": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateDeletingClusterResourceBinding(cluster1),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1),
			},
			wantWait: false,
			wantErr:  true,
		},
		"test no two non-deleting binding can co-exsit on same cluster, case one": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-2", cluster1),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1),
			},
			wantWait: false,
			wantErr:  true,
		},
		"test no two non-deleting binding can co-exsit on same cluster, case two": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-2", cluster1),
				generateClusterResourceBinding(fleetv1beta1.BindingStateUnscheduled, "snapshot-1", cluster1),
			},
			wantWait: false,
			wantErr:  true,
		},
	}
	for name, tt := range tests {
		crp := &fleetv1beta1.ClusterResourcePlacement{}
		t.Run(name, func(t *testing.T) {
			gotWait, err := waitForResourcesToCleanUp(tt.allBindings, crp)
			if (err != nil) != tt.wantErr {
				t.Errorf("waitForResourcesToCleanUp test `%s` error = %v, wantErr %v", name, err, tt.wantErr)
				return
			}
			if err != nil && !errors.Is(err, controller.ErrUnexpectedBehavior) {
				t.Errorf("waitForResourcesToCleanUp test `%s` get an unexpected error = %v", name, err)
			}
			if gotWait != tt.wantWait {
				t.Errorf("waitForResourcesToCleanUp test `%s` gotWait = %v, wantWait %v", name, gotWait, tt.wantWait)
			}
		})
	}
}

func TestReconcilerUpdateBindings(t *testing.T) {
	tests := map[string]struct {
		name                       string
		Client                     client.Client
		latestResourceSnapshotName string
		toBeUpgradedBinding        []*fleetv1beta1.ClusterResourceBinding
		wantErr                    bool
	}{
		"test update binding with nil toBeUpgradedBinding": {
			name:                       "Nil toBeUpgradedBinding",
			Client:                     &test.MockClient{},
			latestResourceSnapshotName: "snapshot-2",
			toBeUpgradedBinding:        nil,
			wantErr:                    false,
		},
		"test update binding with empty toBeUpgradedBinding": {
			name:                       "Empty toBeUpgradedBinding",
			Client:                     &test.MockClient{},
			latestResourceSnapshotName: "snapshot-2",
			toBeUpgradedBinding:        []*fleetv1beta1.ClusterResourceBinding{},
			wantErr:                    false,
		},
		"test update binding with failed binding": {
			name: "Binding State with update error",
			Client: &test.MockClient{
				MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					return errors.New("Failed to update binding")
				},
			},
			latestResourceSnapshotName: "snapshot-2",
			toBeUpgradedBinding: []*fleetv1beta1.ClusterResourceBinding{
				generateFailedToApplyClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1),
			},
			wantErr: true,
		},
		"test update binding with error for scheduled state": {
			name: "Scheduled state with update error",
			Client: &test.MockClient{
				MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					// Return an error for the scheduled state
					if obj.(*fleetv1beta1.ClusterResourceBinding).Spec.State == fleetv1beta1.BindingStateBound {
						return errors.New("Failed to mark binding")
					}
					return nil
				},
			},
			latestResourceSnapshotName: "snapshot-1",
			toBeUpgradedBinding: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
			},
			wantErr: true,
		},
		"test update binding with unscheduled state": {
			name: "Delete unscheduled state",
			Client: &test.MockClient{
				MockDelete: func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
					return nil
				},
			},
			latestResourceSnapshotName: "snapshot-2",
			toBeUpgradedBinding: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateUnscheduled, "snapshot-1", cluster1),
			},
			wantErr: false,
		},
		"test update binding with error for unscheduled state": {
			name: "Delete unscheduled state with error",
			Client: &test.MockClient{
				MockDelete: func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
					return errors.New("Failed to delete unselected binding")
				},
			},
			latestResourceSnapshotName: "snapshot-2",
			toBeUpgradedBinding: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateUnscheduled, "snapshot-1", cluster1),
			},
			wantErr: true,
		},
		"test update binding with IsNotFound error for unscheduled state": {
			name: "Delete unscheduled state with IsNotFound error",
			Client: &test.MockClient{
				MockDelete: func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
					return apierrors.NewNotFound(schema.GroupResource{}, "invalid")
				},
			},
			latestResourceSnapshotName: "snapshot-2",
			toBeUpgradedBinding: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateUnscheduled, "snapshot-1", cluster1),
			},
			wantErr: false,
		},
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

func TestIsBindingReady(t *testing.T) {
	tests := map[string]struct {
		binding         *fleetv1beta1.ClusterResourceBinding
		readyTimeCutOff time.Time
		wantReady       bool
		wantWaitTime    time.Duration
	}{
		"binding applied before the ready time cut off should return ready": {
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 10,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 10,
							LastTransitionTime: metav1.Time{
								Time: now.Add(-time.Millisecond),
							},
						},
					},
				},
			},
			readyTimeCutOff: now,
			wantReady:       true,
			wantWaitTime:    0,
		},
		"binding applied after the ready time cut off should return not ready with a wait time": {
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 10,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 10,
							LastTransitionTime: metav1.Time{
								Time: now.Add(time.Millisecond),
							},
						},
					},
				},
			},
			readyTimeCutOff: now,
			wantReady:       false,
			wantWaitTime:    time.Millisecond,
		},
		"binding not applied should return not ready with a negative wait time": {
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 10,
				},
			},
			readyTimeCutOff: now,
			wantReady:       false,
			wantWaitTime:    -1,
		},
		"binding applied for a previous generation should return not ready with a negative wait time": {
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 10,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 9, //not the current generation
							LastTransitionTime: metav1.Time{
								Time: now.Add(-time.Second),
							},
						},
					},
				},
			},
			readyTimeCutOff: now,
			wantReady:       false,
			wantWaitTime:    -1,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			gotWaitTime, gotReady := isBindingReady(tt.binding, tt.readyTimeCutOff)
			if gotReady != tt.wantReady {
				t.Errorf("isBindingReady test `%s` gotReady = %v, wantReady %v", name, gotReady, tt.wantReady)
			}
			if gotWaitTime != tt.wantWaitTime {
				t.Errorf("isBindingReady test `%s` gotWaitTime = %v, wantWaitTime %v", name, gotWaitTime, tt.wantWaitTime)
			}
		})
	}
}

func TestPickBindingsToRoll(t *testing.T) {
	noMaxUnavailableCRP := clusterResourcePlacementForTest("test",
		createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, 5))
	noMaxUnavailableCRP.Spec.Strategy.RollingUpdate.MaxUnavailable = &intstr.IntOrString{
		Type:   intstr.Int,
		IntVal: 0,
	}
	noMaxSurgeCRP := clusterResourcePlacementForTest("test",
		createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, 5))
	noMaxSurgeCRP.Spec.Strategy.RollingUpdate.MaxSurge = &intstr.IntOrString{
		Type:   intstr.Int,
		IntVal: 0,
	}
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
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickAllPlacementType, 0)),
			tobeUpdatedBindings: []int{0},
			needRoll:            true,
		},
		"test bound with failed to apply bindings": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateFailedToApplyClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster2),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster3),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster4),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster5),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, 5)),
			tobeUpdatedBindings: []int{0},
			needRoll:            true,
		},
		"test bound with failed to apply bindings when there is no max unavailable allowed": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateFailedToApplyClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1),
				generateFailedToApplyClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster2),
				generateFailedToApplyClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster3),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster4),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster5),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp:                        noMaxUnavailableCRP,
			tobeUpdatedBindings:        []int{0, 1, 2},
			needRoll:                   true,
		},
		"test no binding when there is no max unavailable allowed": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster2),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster3),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster4),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster5),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp:                        noMaxUnavailableCRP,
			tobeUpdatedBindings:        []int{},
			needRoll:                   true,
		},
		"test with no bindings": {
			allBindings:                []*fleetv1beta1.ClusterResourceBinding{},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, 5)),
			tobeUpdatedBindings: []int{},
			needRoll:            false,
		},
		"test with scheduled bindings": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
				generateFailedToApplyClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster2),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, 2)),
			tobeUpdatedBindings: []int{0, 1},
			needRoll:            true,
		},
		"test remove unscheduled bindings": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
				generateClusterResourceBinding(fleetv1beta1.BindingStateUnscheduled, "snapshot-1", cluster2),
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster3),
				generateClusterResourceBinding(fleetv1beta1.BindingStateUnscheduled, "snapshot-1", cluster4),
			},
			latestResourceSnapshotName: "snapshot-1",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, 4)),
			tobeUpdatedBindings: []int{0, 2},
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
				t.Errorf("pickBindingsToRoll test `%s` gotUpdatedBindings = %v, wantReady %v", name, gotUpdatedBindings, tt.tobeUpdatedBindings)
			}
			if gotNeedRoll != tt.needRoll {
				t.Errorf("pickBindingsToRoll test `%s` gotNeedRoll = %v, wantReady %v", name, gotNeedRoll, tt.needRoll)
			}
		})
	}
}

func createPlacementPolicyForTest(placementType fleetv1beta1.PlacementType, numberOfClusters int32) *fleetv1beta1.PlacementPolicy {
	return &fleetv1beta1.PlacementPolicy{
		PlacementType:    placementType,
		NumberOfClusters: ptr.To(numberOfClusters),
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
					UnavailablePeriodSeconds: ptr.To(1),
				},
			},
		},
	}
}

func generateFailedToApplyClusterResourceBinding(state fleetv1beta1.BindingState, resourceSnapshotName, targetCluster string) *fleetv1beta1.ClusterResourceBinding {
	binding := generateClusterResourceBinding(state, resourceSnapshotName, targetCluster)
	binding.Status.Conditions = append(binding.Status.Conditions, metav1.Condition{
		Type:   string(fleetv1beta1.ResourceBindingApplied),
		Status: metav1.ConditionFalse,
	})
	return binding
}
