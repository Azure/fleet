/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package rollout

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/work"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

var (
	now = time.Now()

	cluster1 = "cluster-1"
	cluster2 = "cluster-2"
	cluster3 = "cluster-3"
	cluster4 = "cluster-4"
	cluster5 = "cluster-5"

	cmpOptions = []cmp.Option{
		cmp.AllowUnexported(toBeUpdatedBinding{}),
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(fleetv1beta1.ClusterResourceBinding{}, "TypeMeta"),
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
		cmpopts.SortSlices(func(b1, b2 fleetv1beta1.ClusterResourceBinding) bool {
			return b1.Name < b2.Name
		}),
		cmpopts.IgnoreFields(metav1.Condition{}, "Message"),
		cmpopts.SortSlices(func(c1, c2 metav1.Condition) bool {
			return c1.Type < c2.Type
		}),
		cmp.Comparer(func(t1, t2 metav1.Time) bool {
			if t1.Time.IsZero() || t2.Time.IsZero() {
				return true // treat them as equal
			}
			if t1.Time.After(t2.Time) {
				t1, t2 = t2, t1 // ensure t1 is always before t2
			}
			// we're within the margin (10s) if x + margin >= y
			return !t1.Time.Add(10 * time.Second).Before(t2.Time)
		}),
	}
)

func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := fleetv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs (placement v1beta1) to the runtime scheme: %v", err)
	}
	if err := fleetv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs (placement v1alpha1) to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

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

func TestUpdateBindings(t *testing.T) {
	currentTime := time.Now()
	oldTransitionTime := metav1.NewTime(currentTime.Add(-1 * time.Hour))

	tests := map[string]struct {
		skipPuttingBindings bool // whether skip to put the bindings into the api server
		// build toBeUpdatedBinding with currentBinding and desiredBinding
		bindings            []fleetv1beta1.ClusterResourceBinding
		desiredBindingsSpec []fleetv1beta1.ResourceBindingSpec
		wantBindings        []fleetv1beta1.ClusterResourceBinding
		wantErr             error
	}{
		"update bindings with nil": {
			bindings:     nil,
			wantBindings: nil,
		},
		"update bindings with empty": {
			bindings:     []fleetv1beta1.ClusterResourceBinding{},
			wantBindings: nil,
		},
		"update a bounded binding with latest resources": {
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateBound,
						TargetCluster:        cluster1,
						ResourceSnapshotName: "snapshot-1",
					},
				},
			},
			desiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                            fleetv1beta1.BindingStateBound,
					TargetCluster:                    cluster1,
					ResourceSnapshotName:             "snapshot-2",
					ClusterResourceOverrideSnapshots: []string{"cro-1", "cro-2"},
					ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{{Name: "ro-1", Namespace: "ns-1"}},
				},
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                            fleetv1beta1.BindingStateBound,
						TargetCluster:                    cluster1,
						ResourceSnapshotName:             "snapshot-2",
						ClusterResourceOverrideSnapshots: []string{"cro-1", "cro-2"},
						ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{{Name: "ro-1", Namespace: "ns-1"}},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 15,
								LastTransitionTime: metav1.NewTime(currentTime),
								Reason:             condition.RolloutStartedReason,
							},
						},
					},
				},
			},
		},
		"update a bounded binding (having status) with latest resources": {
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateBound,
						TargetCluster:        cluster1,
						ResourceSnapshotName: "snapshot-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 15,
								Reason:             condition.RolloutStartedReason,
								LastTransitionTime: oldTransitionTime,
							},
						},
					},
				},
			},
			desiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                            fleetv1beta1.BindingStateBound,
					TargetCluster:                    cluster1,
					ResourceSnapshotName:             "snapshot-2",
					ClusterResourceOverrideSnapshots: []string{"cro-1", "cro-2"},
					ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{{Name: "ro-1", Namespace: "ns-1"}},
				},
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15, // note, the fake client doesn't update the generation
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                            fleetv1beta1.BindingStateBound,
						TargetCluster:                    cluster1,
						ResourceSnapshotName:             "snapshot-2",
						ClusterResourceOverrideSnapshots: []string{"cro-1", "cro-2"},
						ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{{Name: "ro-1", Namespace: "ns-1"}},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 15,
								LastTransitionTime: oldTransitionTime,
								Reason:             condition.RolloutStartedReason,
							},
						},
					},
				},
			},
		},
		"update a scheduled binding with latest resources": {
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:         fleetv1beta1.BindingStateScheduled,
						TargetCluster: cluster1,
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 15,
								Reason:             condition.RolloutNotStartedYetReason,
								LastTransitionTime: oldTransitionTime,
							},
						},
					},
				},
			},
			desiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                            fleetv1beta1.BindingStateBound,
					TargetCluster:                    cluster1,
					ResourceSnapshotName:             "snapshot-2",
					ClusterResourceOverrideSnapshots: []string{"cro-1", "cro-2"},
					ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{{Name: "ro-1", Namespace: "ns-1"}},
				},
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                            fleetv1beta1.BindingStateBound,
						TargetCluster:                    cluster1,
						ResourceSnapshotName:             "snapshot-2",
						ClusterResourceOverrideSnapshots: []string{"cro-1", "cro-2"},
						ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{{Name: "ro-1", Namespace: "ns-1"}},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 15,
								Reason:             condition.RolloutStartedReason,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		"delete an unscheduled binding": {
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateUnscheduled,
						TargetCluster:        cluster1,
						ResourceSnapshotName: "snapshot-1",
					},
				},
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{},
		},
		"delete an not found unscheduled binding": {
			skipPuttingBindings: true,
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateUnscheduled,
						TargetCluster:        cluster1,
						ResourceSnapshotName: "snapshot-1",
					},
				},
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{},
		},
		"update multiple bindings": {
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:         fleetv1beta1.BindingStateScheduled,
						TargetCluster: cluster1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-2",
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateBound,
						TargetCluster:        cluster2,
						ResourceSnapshotName: "snapshot-2",
					},
				},
			},
			desiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                            fleetv1beta1.BindingStateBound,
					TargetCluster:                    cluster1,
					ResourceSnapshotName:             "snapshot-1",
					ClusterResourceOverrideSnapshots: []string{"cro-1", "cro-2"},
					ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{{Name: "ro-1", Namespace: "ns-1"}},
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster2,
					ResourceSnapshotName: "snapshot-3",
				},
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                            fleetv1beta1.BindingStateBound,
						TargetCluster:                    cluster1,
						ResourceSnapshotName:             "snapshot-1",
						ClusterResourceOverrideSnapshots: []string{"cro-1", "cro-2"},
						ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{{Name: "ro-1", Namespace: "ns-1"}},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 15,
								LastTransitionTime: metav1.NewTime(currentTime),
								Reason:             condition.RolloutStartedReason,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-2",
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateBound,
						TargetCluster:        cluster2,
						ResourceSnapshotName: "snapshot-3",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
								LastTransitionTime: metav1.NewTime(currentTime),
								Reason:             condition.RolloutStartedReason,
							},
						},
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var objects []client.Object
			for i := range tt.bindings {
				objects = append(objects, &tt.bindings[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			ctx := context.Background()
			inputs := make([]toBeUpdatedBinding, len(tt.bindings))
			for i := range tt.bindings {
				// Get the data from the api server first so that the update won't fail because of the revision.
				if err := fakeClient.Get(ctx, client.ObjectKey{Name: tt.bindings[i].Name}, &tt.bindings[i]); err != nil {
					t.Fatalf("failed to get the binding: %v", err)
				}
				inputs[i] = toBeUpdatedBinding{
					currentBinding: &tt.bindings[i],
				}
				if tt.desiredBindingsSpec != nil {
					inputs[i].desiredBinding = tt.bindings[i].DeepCopy()
					inputs[i].desiredBinding.Spec = tt.desiredBindingsSpec[i]
				}
			}
			err := r.updateBindings(ctx, inputs)
			if (err != nil) != (tt.wantErr != nil) || err != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("updateBindings() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			bindingList := &fleetv1beta1.ClusterResourceBindingList{}
			if err := fakeClient.List(ctx, bindingList); err != nil {
				t.Fatalf("ClusterResourceBinding List() got error %v, want no err", err)
			}
			if diff := cmp.Diff(tt.wantBindings, bindingList.Items, cmpOptions...); diff != "" {
				t.Errorf("clusterResourceSnapshot List() mismatch (-want, +got):\n%s", diff)
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
		"binding available (trackable) is ready": {
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 10,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingAvailable),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 10,
							Reason:             "any",
						},
					},
				},
			},
			readyTimeCutOff: now,
			wantReady:       true,
			wantWaitTime:    0,
		},
		"binding available (not trackable) before the ready time cut off should return ready": {
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 10,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingAvailable),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 10,
							LastTransitionTime: metav1.Time{
								Time: now.Add(-time.Millisecond),
							},
							Reason: work.WorkNotTrackableReason,
						},
					},
				},
			},
			readyTimeCutOff: now,
			wantReady:       true,
			wantWaitTime:    0,
		},
		"binding available (not trackable) after the ready time cut off should return not ready with a wait time": {
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 10,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingAvailable),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 10,
							LastTransitionTime: metav1.Time{
								Time: now.Add(time.Millisecond),
							},
							Reason: work.WorkNotTrackableReason,
						},
					},
				},
			},
			readyTimeCutOff: now,
			wantReady:       false,
			wantWaitTime:    time.Millisecond,
		},
		"binding not available should return not ready with a negative wait time": {
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 10,
				},
			},
			readyTimeCutOff: now,
			wantReady:       false,
			wantWaitTime:    -1,
		},
		"binding available for a previous generation should return not ready with a negative wait time": {
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 10,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingAvailable),
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
	crpWithApplyStrategy := clusterResourcePlacementForTest("test",
		createPlacementPolicyForTest(fleetv1beta1.PickAllPlacementType, 0))
	crpWithApplyStrategy.Spec.Strategy.ApplyStrategy = &fleetv1beta1.ApplyStrategy{
		Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
	}
	crpWithUnavailablePeriod := clusterResourcePlacementForTest("test",
		createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, 3))
	crpWithUnavailablePeriod.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds = ptr.To(60)

	readyBinding := generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1)
	readyBinding.Generation = 15
	readyBinding.Status.Conditions = []metav1.Condition{
		{
			Type:               string(fleetv1beta1.ResourceBindingApplied),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 15,
			LastTransitionTime: metav1.Time{
				Time: now.Add(-time.Hour),
			},
		},
	}
	notReadyUnscheduledBinding := generateClusterResourceBinding(fleetv1beta1.BindingStateUnscheduled, "snapshot-1", cluster2)
	notReadyUnscheduledBinding.Generation = 15
	notReadyUnscheduledBinding.Status.Conditions = []metav1.Condition{
		{
			Type:               string(fleetv1beta1.ResourceBindingApplied),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 15,
		},
		{
			Type:               string(fleetv1beta1.ResourceBindingAvailable),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 15,
			LastTransitionTime: metav1.Time{
				Time: now.Add(-1 * time.Minute),
			},
			Reason: work.WorkNotTrackableReason,
		},
	}

	notReadyUnscheduledBinding2 := generateClusterResourceBinding(fleetv1beta1.BindingStateUnscheduled, "snapshot-1", cluster3)
	notReadyUnscheduledBinding2.Generation = 15
	notReadyUnscheduledBinding2.Status.Conditions = []metav1.Condition{
		{
			Type:               string(fleetv1beta1.ResourceBindingApplied),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 15,
		},
		{
			Type:               string(fleetv1beta1.ResourceBindingAvailable),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 15,
			LastTransitionTime: metav1.Time{
				Time: now.Add(-35 * time.Second),
			},
			Reason: work.WorkNotTrackableReason,
		},
	}

	notReadyBoundBinding := generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-2", cluster2)
	notReadyBoundBinding.Generation = 15
	notReadyBoundBinding.Status.Conditions = []metav1.Condition{
		{
			Type:               string(fleetv1beta1.ResourceBindingApplied),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 15,
		},
		{
			Type:               string(fleetv1beta1.ResourceBindingAvailable),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 15,
			Reason:             work.WorkAvailableReason,
		},
	}

	notReadyBoundBinding2 := generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster3)
	notReadyUnscheduledBinding2.Generation = 15
	notReadyUnscheduledBinding2.Status.Conditions = []metav1.Condition{
		{
			Type:               string(fleetv1beta1.ResourceBindingApplied),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 15,
		},
		{
			Type:               string(fleetv1beta1.ResourceBindingAvailable),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 15,
			LastTransitionTime: metav1.Time{
				Time: now.Add(-35 * time.Second),
			},
			Reason: work.WorkNotTrackableReason,
		},
	}
	tests := map[string]struct {
		allBindings                 []*fleetv1beta1.ClusterResourceBinding
		latestResourceSnapshotName  string
		crp                         *fleetv1beta1.ClusterResourcePlacement
		matchedCROs                 []*fleetv1alpha1.ClusterResourceOverrideSnapshot
		matchedROs                  []*fleetv1alpha1.ResourceOverrideSnapshot
		clusters                    []clusterv1beta1.MemberCluster
		wantTobeUpdatedBindings     []int
		wantDesiredBindingsSpec     []fleetv1beta1.ResourceBindingSpec // used to construct the want toBeUpdatedBindings
		wantStaleUnselectedBindings []int
		wantNeedRoll                bool
		wantWaitTime                time.Duration
		wantErr                     error
	}{
		// TODO: add more tests
		"test bound with out dated bindings and nil overrides": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickAllPlacementType, 0)),
			wantTobeUpdatedBindings: []int{0},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-2",
				},
			},
			wantNeedRoll: true,
			wantWaitTime: 0,
		},
		"test bound with out dated bindings and updated apply strategy": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp:                        crpWithApplyStrategy,
			wantTobeUpdatedBindings:    []int{0},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-2",
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
					},
				},
			},
			wantNeedRoll: true,
			wantWaitTime: 0,
		},
		"test bound with out dated bindings and empty overrides": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickAllPlacementType, 0)),
			matchedROs:              []*fleetv1alpha1.ResourceOverrideSnapshot{},
			matchedCROs:             []*fleetv1alpha1.ClusterResourceOverrideSnapshot{},
			wantTobeUpdatedBindings: []int{0},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-2",
				},
			},
			wantNeedRoll: true,
			wantWaitTime: 0,
		},
		"test bound with out dated bindings and matched overrides": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickAllPlacementType, 0)),
			matchedCROs: []*fleetv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: fleetv1alpha1.ClusterResourceOverrideSpec{
							Policy: &fleetv1alpha1.OverridePolicy{
								OverrideRules: []fleetv1alpha1.OverrideRule{
									{
										ClusterSelector: &fleetv1beta1.ClusterSelector{
											ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key1": "value1",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			matchedROs: []*fleetv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "test",
					},
					Spec: fleetv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: fleetv1alpha1.ResourceOverrideSpec{
							Policy: &fleetv1alpha1.OverridePolicy{
								OverrideRules: []fleetv1alpha1.OverrideRule{
									{
										ClusterSelector: &fleetv1beta1.ClusterSelector{
											ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key2": "value2",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			clusters: []clusterv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-1",
						Labels: map[string]string{
							"key1": "value1",
							"key2": "value2",
						},
					},
				},
			},
			wantTobeUpdatedBindings: []int{0},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                            fleetv1beta1.BindingStateBound,
					TargetCluster:                    cluster1,
					ResourceSnapshotName:             "snapshot-2",
					ClusterResourceOverrideSnapshots: []string{"cro-1"},
					ResourceOverrideSnapshots: []fleetv1beta1.NamespacedName{
						{
							Namespace: "test",
							Name:      "ro-2",
						},
					},
				},
			},
			wantNeedRoll: true,
			wantWaitTime: 0,
		},
		"test bound with out dated bindings and no matched overrides": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickAllPlacementType, 0)),
			matchedCROs: []*fleetv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: fleetv1alpha1.ClusterResourceOverrideSpec{
							Policy: &fleetv1alpha1.OverridePolicy{
								OverrideRules: []fleetv1alpha1.OverrideRule{
									{
										ClusterSelector: &fleetv1beta1.ClusterSelector{
											ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key1": "value1",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			matchedROs: []*fleetv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "test",
					},
					Spec: fleetv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: fleetv1alpha1.ResourceOverrideSpec{
							Policy: &fleetv1alpha1.OverridePolicy{
								OverrideRules: []fleetv1alpha1.OverrideRule{
									{
										ClusterSelector: &fleetv1beta1.ClusterSelector{
											ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key2": "value2",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			clusters: []clusterv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-1",
					},
				},
			},
			wantTobeUpdatedBindings: []int{0},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-2",
				},
			},
			wantNeedRoll: true,
			wantWaitTime: 0,
		},
		"test bound with out dated bindings and stale overrides": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
			},
			latestResourceSnapshotName: "snapshot-1",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickAllPlacementType, 0)),
			matchedCROs: []*fleetv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: fleetv1alpha1.ClusterResourceOverrideSpec{
							Policy: &fleetv1alpha1.OverridePolicy{
								OverrideRules: []fleetv1alpha1.OverrideRule{
									{
										ClusterSelector: &fleetv1beta1.ClusterSelector{
											ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key1": "value1",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			matchedROs: []*fleetv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "test",
					},
					Spec: fleetv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: fleetv1alpha1.ResourceOverrideSpec{
							Policy: &fleetv1alpha1.OverridePolicy{
								OverrideRules: []fleetv1alpha1.OverrideRule{
									{
										ClusterSelector: &fleetv1beta1.ClusterSelector{
											ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key2": "value2",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			clusters: []clusterv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-1",
						Labels: map[string]string{
							"key1": "value1",
							"key2": "value2",
						},
					},
				},
			},
			wantTobeUpdatedBindings: []int{0},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                            fleetv1beta1.BindingStateBound,
					TargetCluster:                    cluster1,
					ResourceSnapshotName:             "snapshot-1",
					ClusterResourceOverrideSnapshots: []string{"cro-1"},
					ResourceOverrideSnapshots: []fleetv1beta1.NamespacedName{
						{
							Namespace: "test",
							Name:      "ro-2",
						},
					},
				},
			},
			wantNeedRoll: true,
			wantWaitTime: 0,
		},
		"test bound with latest resources": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1),
			},
			latestResourceSnapshotName: "snapshot-1",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickAllPlacementType, 0)),
			wantTobeUpdatedBindings:     []int{},
			wantStaleUnselectedBindings: []int{},
			wantNeedRoll:                false,
			wantWaitTime:                time.Second,
		},
		"test bound with only failed to apply binding": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateFailedToApplyClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, 5)),
			wantTobeUpdatedBindings: []int{0},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-2",
				},
			},
			wantNeedRoll: true,
			wantWaitTime: time.Second,
		},
		"test bound with failed to apply binding, unselected bound bindings": {
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
			wantTobeUpdatedBindings:     []int{0},
			wantStaleUnselectedBindings: []int{1, 2, 3, 4},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster2,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster3,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster4,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster5,
					ResourceSnapshotName: "snapshot-2",
				},
			},
			wantNeedRoll: true,
			wantWaitTime: time.Second,
		},
		"test bound with failed to apply bindings when there is no max unavailable allowed": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateFailedToApplyClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1),
				generateFailedToApplyClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster2),
				generateFailedToApplyClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster3),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster4),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster5),
			},
			latestResourceSnapshotName:  "snapshot-2",
			crp:                         noMaxUnavailableCRP,
			wantTobeUpdatedBindings:     []int{0, 1, 2},
			wantStaleUnselectedBindings: []int{3, 4},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster2,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster3,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster4,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster5,
					ResourceSnapshotName: "snapshot-2",
				},
			},
			wantNeedRoll: true,
			wantWaitTime: time.Second,
		},
		"test no binding when there is no max unavailable allowed": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster2),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster3),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster4),
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster5),
			},
			latestResourceSnapshotName:  "snapshot-2",
			crp:                         noMaxUnavailableCRP,
			wantTobeUpdatedBindings:     []int{},
			wantStaleUnselectedBindings: []int{0, 1, 2, 3, 4},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster2,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster3,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster4,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster5,
					ResourceSnapshotName: "snapshot-2",
				},
			},
			wantNeedRoll: true,
			wantWaitTime: time.Second,
		},
		"test with no bindings": {
			allBindings:                []*fleetv1beta1.ClusterResourceBinding{},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, 5)),
			wantTobeUpdatedBindings: []int{},
			wantNeedRoll:            false,
			wantWaitTime:            0,
		},
		"test with scheduled bindings (one of them is failed)": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster1),
				generateFailedToApplyClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster2),
			},
			latestResourceSnapshotName: "snapshot-2",
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, 2)),
			wantTobeUpdatedBindings: []int{0, 1},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster2,
					ResourceSnapshotName: "snapshot-2",
				},
			},
			wantNeedRoll: true,
			wantWaitTime: 0,
		},
		"test with scheduled bindings (always update the scheduled binding first)": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				readyBinding,
				generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, "snapshot-1", cluster2),
			},
			latestResourceSnapshotName:  "snapshot-2",
			crp:                         crpWithUnavailablePeriod,
			wantTobeUpdatedBindings:     []int{1},
			wantStaleUnselectedBindings: []int{0},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster2,
					ResourceSnapshotName: "snapshot-2",
				},
			},
			wantNeedRoll: true,
			wantWaitTime: 60 * time.Second,
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
			wantTobeUpdatedBindings: []int{0, 2},
			// empty list as unscheduled bindings will be removed and are not tracked in the CRP today.
			wantStaleUnselectedBindings: []int{},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-1",
				},
				// unscheduled binding does not have desired spec so that putting the empty here.
				{},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster3,
					ResourceSnapshotName: "snapshot-1",
				},
				// unscheduled binding does not have desired spec so that putting the empty here.
				{},
			},
			wantNeedRoll: true,
			wantWaitTime: time.Second,
		},
		"test overrides and the cluster is not found": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				generateClusterResourceBinding(fleetv1beta1.BindingStateBound, "snapshot-1", cluster1),
			},
			latestResourceSnapshotName: "snapshot-1",
			matchedCROs: []*fleetv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: fleetv1alpha1.ClusterResourceOverrideSpec{
							Policy: &fleetv1alpha1.OverridePolicy{
								OverrideRules: []fleetv1alpha1.OverrideRule{
									{
										ClusterSelector: &fleetv1beta1.ClusterSelector{
											ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key1": "value1",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			crp: clusterResourcePlacementForTest("test",
				createPlacementPolicyForTest(fleetv1beta1.PickAllPlacementType, 0)),
			wantErr: controller.ErrExpectedBehavior,
		},
		"test bound bindings with different waitTimes": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				notReadyBoundBinding,
				readyBinding,
				notReadyBoundBinding2,
			},
			latestResourceSnapshotName:  "snapshot-2",
			crp:                         crpWithUnavailablePeriod,
			wantStaleUnselectedBindings: []int{1, 2},
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster2,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster1,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster3,
					ResourceSnapshotName: "snapshot-2",
				},
			},
			wantNeedRoll: true,
			wantWaitTime: 60 * time.Second,
		},
		"test unscheduled bindings with different waitTimes": {
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				notReadyUnscheduledBinding,
				notReadyUnscheduledBinding2,
			},
			latestResourceSnapshotName: "snapshot-2",
			crp:                        crpWithUnavailablePeriod,
			wantDesiredBindingsSpec: []fleetv1beta1.ResourceBindingSpec{
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster2,
					ResourceSnapshotName: "snapshot-2",
				},
				{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        cluster3,
					ResourceSnapshotName: "snapshot-2",
				},
			},
			wantNeedRoll: true,
			wantWaitTime: 25 * time.Second,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			scheme := serviceScheme(t)
			var objects []client.Object
			for i := range tt.clusters {
				objects = append(objects, &tt.clusters[i])
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			resourceSnapshot := &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.latestResourceSnapshotName,
				},
			}
			gotUpdatedBindings, gotStaleUnselectedBindings, gotNeedRoll, gotWaitTime, err := r.pickBindingsToRoll(context.Background(), tt.allBindings, resourceSnapshot, tt.crp, tt.matchedCROs, tt.matchedROs)
			if (err != nil) != (tt.wantErr != nil) || err != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("pickBindingsToRoll() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			wantTobeUpdatedBindings := make([]toBeUpdatedBinding, len(tt.wantTobeUpdatedBindings))
			for i, index := range tt.wantTobeUpdatedBindings {
				wantTobeUpdatedBindings[i].currentBinding = tt.allBindings[index]
				wantTobeUpdatedBindings[i].desiredBinding = tt.allBindings[index].DeepCopy()
				wantTobeUpdatedBindings[i].desiredBinding.Spec = tt.wantDesiredBindingsSpec[index]
			}
			wantStaleUnselectedBindings := make([]toBeUpdatedBinding, len(tt.wantStaleUnselectedBindings))
			for i, index := range tt.wantStaleUnselectedBindings {
				wantStaleUnselectedBindings[i].currentBinding = tt.allBindings[index]
				wantStaleUnselectedBindings[i].desiredBinding = tt.allBindings[index].DeepCopy()
				wantStaleUnselectedBindings[i].desiredBinding.Spec = tt.wantDesiredBindingsSpec[index]
			}

			if diff := cmp.Diff(wantTobeUpdatedBindings, gotUpdatedBindings, cmpOptions...); diff != "" {
				t.Errorf("pickBindingsToRoll() toBeUpdatedBindings mismatch (-want, +got):\n%s", diff)
			}
			if diff := cmp.Diff(wantStaleUnselectedBindings, gotStaleUnselectedBindings, cmpOptions...); diff != "" {
				t.Errorf("pickBindingsToRoll() staleUnselectedBindings mismatch (-want, +got):\n%s", diff)
			}
			if gotNeedRoll != tt.wantNeedRoll {
				t.Errorf("pickBindingsToRoll() = needRoll %v, want %v", gotNeedRoll, tt.wantNeedRoll)
			}

			if gotWaitTime.Round(time.Second) != tt.wantWaitTime {
				t.Errorf("pickBindingsToRoll() = waitTime %v, want %v", gotWaitTime, tt.wantWaitTime)
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
							LabelSelector: &metav1.LabelSelector{
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
					Kind:    "Namespace",
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

func TestUpdateStaleBindingsStatus(t *testing.T) {
	currentTime := time.Now()
	oldTransitionTime := metav1.NewTime(currentTime.Add(-1 * time.Hour))

	tests := map[string]struct {
		skipPuttingBindings bool // whether skip to put the bindings into the api server
		// build toBeUpdatedBinding with currentBinding and desiredBinding
		bindings     []fleetv1beta1.ClusterResourceBinding
		wantBindings []fleetv1beta1.ClusterResourceBinding
		wantErr      error
	}{
		"update bindings with nil": {
			bindings:     nil,
			wantBindings: nil,
		},
		"update bindings with empty": {
			bindings:     []fleetv1beta1.ClusterResourceBinding{},
			wantBindings: nil,
		},
		"update a bounded binding status": {
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateBound,
						TargetCluster:        cluster1,
						ResourceSnapshotName: "snapshot-1",
					},
				},
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateBound,
						TargetCluster:        cluster1,
						ResourceSnapshotName: "snapshot-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 15,
								LastTransitionTime: metav1.NewTime(currentTime),
								Reason:             condition.RolloutNotStartedYetReason,
							},
						},
					},
				},
			},
		},
		"skip updating unscheduled binding status": {
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateUnscheduled,
						TargetCluster:        cluster1,
						ResourceSnapshotName: "snapshot-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 15,
								LastTransitionTime: metav1.NewTime(currentTime),
								Reason:             condition.RolloutStartedReason,
							},
						},
					},
				},
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateUnscheduled,
						TargetCluster:        cluster1,
						ResourceSnapshotName: "snapshot-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 15,
								LastTransitionTime: metav1.NewTime(currentTime),
								Reason:             condition.RolloutStartedReason,
							},
						},
					},
				},
			},
		},
		"update multiple bindings": {
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:         fleetv1beta1.BindingStateScheduled,
						TargetCluster: cluster1,
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 15,
								LastTransitionTime: oldTransitionTime,
								Reason:             condition.RolloutNotStartedYetReason,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-2",
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateBound,
						TargetCluster:        cluster2,
						ResourceSnapshotName: "snapshot-2",
					},
				},
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 15,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:         fleetv1beta1.BindingStateScheduled,
						TargetCluster: cluster1,
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 15,
								LastTransitionTime: oldTransitionTime,
								Reason:             condition.RolloutNotStartedYetReason,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-2",
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                fleetv1beta1.BindingStateBound,
						TargetCluster:        cluster2,
						ResourceSnapshotName: "snapshot-2",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								LastTransitionTime: metav1.NewTime(currentTime),
								Reason:             condition.RolloutNotStartedYetReason,
							},
						},
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var objects []client.Object
			for i := range tt.bindings {
				objects = append(objects, &tt.bindings[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			ctx := context.Background()
			inputs := make([]toBeUpdatedBinding, len(tt.bindings))
			for i := range tt.bindings {
				// Get the data from the api server first so that the update won't fail because of the revision.
				if err := fakeClient.Get(ctx, client.ObjectKey{Name: tt.bindings[i].Name}, &tt.bindings[i]); err != nil {
					t.Fatalf("failed to get the binding: %v", err)
				}
				inputs[i] = toBeUpdatedBinding{
					currentBinding: &tt.bindings[i],
				}
			}
			if err := r.updateStaleBindingsStatus(ctx, inputs); err != nil {
				t.Fatalf("updateStaleBindingsStatus() got error %v, want no err", err)
			}
			bindingList := &fleetv1beta1.ClusterResourceBindingList{}
			if err := fakeClient.List(ctx, bindingList); err != nil {
				t.Fatalf("updateStaleBindingsStatus List() got error %v, want no err", err)
			}
			if diff := cmp.Diff(tt.wantBindings, bindingList.Items, cmpOptions...); diff != "" {
				t.Errorf("updateStaleBindingsStatus List() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestCheckAndUpdateStaleBindingsStatus(t *testing.T) {
	generation := int64(15)
	latestBindings := []*fleetv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "binding-1",
				Generation: generation,
			},
			Spec: fleetv1beta1.ResourceBindingSpec{
				State: fleetv1beta1.BindingStateBound,
			},
			Status: fleetv1beta1.ResourceBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
						Status:             metav1.ConditionTrue,
						ObservedGeneration: generation,
						Reason:             condition.RolloutStartedReason,
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "binding-2",
				Generation: generation,
			},
			Spec: fleetv1beta1.ResourceBindingSpec{
				State: fleetv1beta1.BindingStateScheduled,
			},
			Status: fleetv1beta1.ResourceBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
						Status:             metav1.ConditionTrue,
						ObservedGeneration: generation,
						Reason:             condition.RolloutStartedReason,
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "binding-3",
				Generation: generation,
			},
			Spec: fleetv1beta1.ResourceBindingSpec{
				State: fleetv1beta1.BindingStateUnscheduled,
			},
		},
	}

	tests := map[string]struct {
		bindings     []*fleetv1beta1.ClusterResourceBinding
		wantBindings []fleetv1beta1.ClusterResourceBinding
	}{
		"update bindings with nil": {
			bindings:     nil,
			wantBindings: nil,
		},
		"update bindings with empty": {
			bindings:     []*fleetv1beta1.ClusterResourceBinding{},
			wantBindings: nil,
		},
		"bindings with rollout started condition": {
			bindings: latestBindings,
			wantBindings: []fleetv1beta1.ClusterResourceBinding{
				*latestBindings[0],
				*latestBindings[1],
				*latestBindings[2],
			},
		},
		"update stale bounded binding": {
			bindings: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: generation,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State: fleetv1beta1.BindingStateBound,
					},
				},
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{
				*latestBindings[0],
			},
		},
		"update stale scheduled bindings": {
			bindings: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-2",
						Generation: generation,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State: fleetv1beta1.BindingStateScheduled,
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: generation,
							},
						},
					},
				},
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{
				*latestBindings[1],
			},
		},
		"update multiple stale bindings": {
			bindings: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: generation,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State: fleetv1beta1.BindingStateBound,
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: generation - 1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-2",
						Generation: generation,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State: fleetv1beta1.BindingStateScheduled,
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: generation,
							},
						},
					},
				},
				latestBindings[2],
			},
			wantBindings: []fleetv1beta1.ClusterResourceBinding{
				*latestBindings[0],
				*latestBindings[1],
				*latestBindings[2],
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var objects []client.Object
			for i := range tt.bindings {
				objects = append(objects, tt.bindings[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			ctx := context.Background()
			if err := r.checkAndUpdateStaleBindingsStatus(ctx, tt.bindings); err != nil {
				t.Fatalf("checkAndUpdateStaleBindingsStatus() got error %v, want no err", err)
			}
			bindingList := &fleetv1beta1.ClusterResourceBindingList{}
			if err := fakeClient.List(ctx, bindingList); err != nil {
				t.Fatalf("checkAndUpdateStaleBindingsStatus List() got error %v, want no err", err)
			}
			if diff := cmp.Diff(tt.wantBindings, bindingList.Items, cmpOptions...); diff != "" {
				t.Errorf("checkAndUpdateStaleBindingsStatus List() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

// TestProcessApplyStrategyUpdates tests the processApplyStrategyUpdates method.
func TestProcessApplyStrategyUpdates(t *testing.T) {
	// Use RFC 3339 copy to avoid time precision issues.
	now := metav1.Now().Rfc3339Copy()

	testCases := []struct {
		name            string
		crp             *fleetv1beta1.ClusterResourcePlacement
		allBindings     []*fleetv1beta1.ClusterResourceBinding
		wantAllBindings []*fleetv1beta1.ClusterResourceBinding
	}{
		{
			name: "nil apply strategy",
			crp: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Strategy: fleetv1beta1.RolloutStrategy{},
				},
			},
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
					},
					Spec: fleetv1beta1.ResourceBindingSpec{},
				},
			},
			wantAllBindings: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type:             fleetv1beta1.ApplyStrategyTypeClientSideApply,
							ComparisonOption: fleetv1beta1.ComparisonOptionTypePartialComparison,
							WhenToApply:      fleetv1beta1.WhenToApplyTypeAlways,
							WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeAlways,
						},
					},
				},
			},
		},
		{
			name: "push apply strategy to bindings of various states",
			crp: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Strategy: fleetv1beta1.RolloutStrategy{
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type:             fleetv1beta1.ApplyStrategyTypeServerSideApply,
							ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
							WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
							WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
						},
					},
				},
			},
			allBindings: []*fleetv1beta1.ClusterResourceBinding{
				// A binding that has been marked for deletion.
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "binding-1",
						DeletionTimestamp: &now,
						// The fake client requires that all objects that have been marked
						// for deletion should have at least one finalizer set.
						Finalizers: []string{
							"custom-deletion-blocker",
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{},
				},
				// A binding that already has the latest apply strategy.
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-2",
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: "snapshot-2",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type:             fleetv1beta1.ApplyStrategyTypeServerSideApply,
							ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
							WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
							WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
						},
					},
				},
				// A binding that has an out of date apply strategy.
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-3",
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: "snapshot-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type:             fleetv1beta1.ApplyStrategyTypeClientSideApply,
							ComparisonOption: fleetv1beta1.ComparisonOptionTypePartialComparison,
							WhenToApply:      fleetv1beta1.WhenToApplyTypeAlways,
							WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeAlways,
						},
					},
				},
			},
			wantAllBindings: []*fleetv1beta1.ClusterResourceBinding{
				// Binding that has been marked for deletion should not be updated.
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "binding-1",
						DeletionTimestamp: &now,
						Finalizers: []string{
							"custom-deletion-blocker",
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{},
				},
				// Binding that already has the latest apply strategy should not be updated.
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-2",
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: "snapshot-2",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type:             fleetv1beta1.ApplyStrategyTypeServerSideApply,
							ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
							WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
							WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
						},
					},
				},
				// Binding that has an out of date apply strategy should be updated.
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-3",
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: "snapshot-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type:             fleetv1beta1.ApplyStrategyTypeServerSideApply,
							ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
							WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
							WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			objs := make([]client.Object, 0, len(tc.allBindings))
			for idx := range tc.allBindings {
				objs = append(objs, tc.allBindings[idx])
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(objs...).
				Build()
			r := &Reconciler{
				Client: fakeClient,
			}

			if err := r.processApplyStrategyUpdates(ctx, tc.crp, tc.allBindings); err != nil {
				t.Errorf("processApplyStrategyUpdates() error = %v, want no error", err)
			}

			for idx := range tc.wantAllBindings {
				wantBinding := tc.wantAllBindings[idx]

				gotBinding := &fleetv1beta1.ClusterResourceBinding{}
				if err := fakeClient.Get(ctx, client.ObjectKey{Name: wantBinding.Name}, gotBinding); err != nil {
					t.Fatalf("failed to retrieve binding: %v", err)
				}

				if diff := cmp.Diff(
					gotBinding, wantBinding,
					cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
				); diff != "" {
					t.Errorf("updated binding mismatch (-got, +want):\n%s", diff)
				}
			}
		})
	}
}
