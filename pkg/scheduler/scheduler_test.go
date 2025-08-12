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

package scheduler

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/metrics"
)

const (
	crpName = "test-crp"

	bindingName    = "test-binding"
	altBindingName = "another-test-binding"

	policySnapshotName        = "test-policy-snapshot"
	altPolicySnapshotName     = "another-test-policy-snapshot"
	anotherPolicySnapshotName = "yet-another-test-policy-snapshot"
)

var (
	ignoreObjectMetaResourceVersionField = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")
)

// TestMain sets up the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := fleetv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestAddSchedulerCleanUpFinalizer tests the addSchedulerCleanUpFinalizer method with PlacementObj interface.
func TestAddSchedulerCleanUpFinalizer(t *testing.T) {
	testCases := []struct {
		name           string
		placement      func() fleetv1beta1.PlacementObj
		wantFinalizers []string
	}{
		{
			name: "cluster-scoped placement should add scheduler cleanup finalizer",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
				}
			},
			wantFinalizers: []string{fleetv1beta1.SchedulerCleanupFinalizer},
		},
		{
			name: "namespaced placement should also add scheduler cleanup finalizer",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rp",
						Namespace: "test-namespace",
					},
				}
			},
			wantFinalizers: []string{fleetv1beta1.SchedulerCleanupFinalizer},
		},
		{
			name: "scheduler should only add finalizer",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       crpName,
						Finalizers: []string{fleetv1beta1.PlacementCleanupFinalizer},
					},
				}
			},
			wantFinalizers: []string{fleetv1beta1.PlacementCleanupFinalizer, fleetv1beta1.SchedulerCleanupFinalizer},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			placement := tc.placement()
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(placement).
				Build()
			s := &Scheduler{
				client:         fakeClient,
				uncachedReader: fakeClient,
			}

			ctx := context.Background()
			if err := s.addSchedulerCleanUpFinalizer(ctx, placement); err != nil {
				t.Fatalf("addSchedulerCleanUpFinalizer() = %v, want no error", err)
			}

			// Verify the finalizer was added
			gotFinalizers := placement.GetFinalizers()
			if !cmp.Equal(gotFinalizers, tc.wantFinalizers) {
				t.Errorf("Expected finalizer %v, got %v", tc.wantFinalizers, gotFinalizers)
			}
		})
	}
}

// TestCleanUpAllBindingsFor tests the cleanUpAllBindingsFor method with PlacementObj interface.
func TestCleanUpAllBindingsFor(t *testing.T) {
	now := metav1.Now()

	testCases := []struct {
		name                  string
		placement             func() fleetv1beta1.PlacementObj
		existingBindings      []fleetv1beta1.BindingObj
		wantFinalizers        []string
		wantRemainingBindings []fleetv1beta1.BindingObj
	}{
		{
			name: "cluster-scoped placement cleanup",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:              crpName,
						DeletionTimestamp: &now,
						Finalizers:        []string{fleetv1beta1.SchedulerCleanupFinalizer},
					},
				}
			},
			existingBindings: []fleetv1beta1.BindingObj{
				&fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bindingName1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: crpName,
						},
					},
				},
				&fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bindingName2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: crpName,
						},
					},
				},
			},
			wantFinalizers:        []string{},
			wantRemainingBindings: []fleetv1beta1.BindingObj{},
		},
		{
			name: "cluster-scoped placement cleanup without bindings but have placement cleanup finalizer",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:              crpName,
						DeletionTimestamp: &now,
						Finalizers:        []string{fleetv1beta1.SchedulerCleanupFinalizer, fleetv1beta1.PlacementCleanupFinalizer},
					},
				}
			},
			existingBindings:      []fleetv1beta1.BindingObj{},
			wantFinalizers:        []string{fleetv1beta1.PlacementCleanupFinalizer},
			wantRemainingBindings: []fleetv1beta1.BindingObj{},
		},
		{
			name: "namespaced placement cleanup",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-rp",
						Namespace:         "test-namespace",
						DeletionTimestamp: &now,
						Finalizers:        []string{fleetv1beta1.SchedulerCleanupFinalizer},
					},
				}
			},
			existingBindings: []fleetv1beta1.BindingObj{
				&fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bindingName,
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
					},
				},
			},
			wantFinalizers:        []string{},
			wantRemainingBindings: []fleetv1beta1.BindingObj{},
		},
		{
			name: "test placement cleanup only delete the bindings that match the placement",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-rp",
						Namespace:         "test-namespace",
						DeletionTimestamp: &now,
						Finalizers:        []string{fleetv1beta1.SchedulerCleanupFinalizer},
					},
				}
			},
			existingBindings: []fleetv1beta1.BindingObj{
				&fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tobeDeletedBinding",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
						Finalizers: []string{fleetv1beta1.SchedulerBindingCleanupFinalizer},
					},
				},
				&fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "remainingBinding",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "otherrp",
						},
						Finalizers: []string{fleetv1beta1.SchedulerBindingCleanupFinalizer},
					},
				},
				&fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "remainingBinding2",
						Namespace: "another-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
						Finalizers: []string{fleetv1beta1.SchedulerBindingCleanupFinalizer},
					},
				},
			},
			wantFinalizers: []string{},
			wantRemainingBindings: []fleetv1beta1.BindingObj{
				&fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "remainingBinding",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "otherrp",
						},
						Finalizers: []string{fleetv1beta1.SchedulerBindingCleanupFinalizer},
					},
				},
				&fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "remainingBinding2",
						Namespace: "another-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
						Finalizers: []string{fleetv1beta1.SchedulerBindingCleanupFinalizer},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			placement := tc.placement()
			// Create a fake client with the placement and existing bindings
			fakeClientBuilder := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(placement)
			for _, binding := range tc.existingBindings {
				fakeClientBuilder.WithObjects(binding)
			}
			fakeClient := fakeClientBuilder.Build()

			s := &Scheduler{
				client:         fakeClient,
				uncachedReader: fakeClient,
			}

			ctx := context.Background()
			if err := s.cleanUpAllBindingsFor(ctx, placement); err != nil {
				t.Fatalf("cleanUpAllBindingsFor() = %v, want no error", err)
			}

			// Verify the finalizer was removed from placement
			gotFinalizers := placement.GetFinalizers()
			if !cmp.Equal(gotFinalizers, tc.wantFinalizers) {
				t.Errorf("Expected finalizer %v, got %v", tc.wantFinalizers, gotFinalizers)
			}

			// Verify bindings were cleaned up
			var gotBindings fleetv1beta1.BindingObjList
			if placement.GetNamespace() == "" {
				gotBindings = &fleetv1beta1.ClusterResourceBindingList{}
			} else {
				gotBindings = &fleetv1beta1.ResourceBindingList{}
			}
			if err := fakeClient.List(ctx, gotBindings); err != nil {
				t.Fatalf("List() bindings = %v, want no error", err)
			}

			if diff := cmp.Diff(gotBindings.GetBindingObjs(), tc.wantRemainingBindings, ignoreObjectMetaResourceVersionField,
				cmpopts.SortSlices(func(b1, b2 fleetv1beta1.BindingObj) bool {
					return b1.GetName() < b2.GetName()
				})); diff != "" {
				t.Errorf("Remaining bindings diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestLookupLatestPolicySnapshot tests the lookupLatestPolicySnapshot method with PlacementObj interface.
func TestLookupLatestPolicySnapshot(t *testing.T) {
	testCases := []struct {
		name               string
		placement          func() fleetv1beta1.PlacementObj
		policySnapshots    []fleetv1beta1.PolicySnapshotObj
		wantPolicySnapshot fleetv1beta1.PolicySnapshotObj
		expectedToFail     bool
	}{
		{
			name: "cluster-scoped placement with policy snapshot",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       crpName,
						Finalizers: []string{fleetv1beta1.SchedulerCleanupFinalizer},
					},
				}
			},
			policySnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: policySnapshotName,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: crpName,
							fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-policy-snapshot",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-crp",
							fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
						},
					},
				},
			},
			wantPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policySnapshotName,
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: crpName,
						fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
					},
				},
			},
		},
		{
			name: "namespaced placement with policy snapshot",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rp",
						Namespace: "test-namespace",
					},
				}
			},
			policySnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      policySnapshotName,
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
							fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-policy-snapshot",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-rp",
							fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
						},
					},
				},
			},
			wantPolicySnapshot: &fleetv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policySnapshotName,
					Namespace: "test-namespace",
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: "test-rp",
						fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
					},
				},
			},
		},
		{
			name: "namespaced placement with policy snapshot only in its namespace",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rp",
						Namespace: "test-namespace",
					},
				}
			},
			policySnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      policySnapshotName,
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
							fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      policySnapshotName,
						Namespace: "other-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
							fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
						},
					},
				},
			},
			wantPolicySnapshot: &fleetv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policySnapshotName,
					Namespace: "test-namespace",
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: "test-rp",
						fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
					},
				},
			},
		},
		{
			name: "cluster-scoped placement should not select namespaced policy snapshot with same placement label",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       crpName,
						Finalizers: []string{fleetv1beta1.SchedulerCleanupFinalizer},
					},
				}
			},
			policySnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: policySnapshotName,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: crpName,
							fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "namespaced-policy-snapshot",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: crpName,
							fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
						},
					},
				},
			},
			wantPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policySnapshotName,
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: crpName,
						fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
					},
				},
			},
		},
		{
			name: "namespaced placement should not select cluster-scoped policy snapshot with same placement label",
			placement: func() fleetv1beta1.PlacementObj {
				return &fleetv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rp",
						Namespace: "test-namespace",
					},
				}
			},
			policySnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      policySnapshotName,
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
							fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-policy-snapshot",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
							fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
						},
					},
				},
			},
			wantPolicySnapshot: &fleetv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policySnapshotName,
					Namespace: "test-namespace",
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: "test-rp",
						fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			placement := tc.placement()

			fakeClientBuilder := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(placement)
			for _, policySnapshot := range tc.policySnapshots {
				fakeClientBuilder.WithObjects(policySnapshot)
			}
			fakeClient := fakeClientBuilder.Build()

			s := &Scheduler{
				client:         fakeClient,
				uncachedReader: fakeClient,
			}

			ctx := context.Background()
			activePolicySnapshot, err := s.lookupLatestPolicySnapshot(ctx, placement)
			if tc.expectedToFail {
				if err == nil {
					t.Errorf("lookupLatestPolicySnapshot() = %v, want error", activePolicySnapshot)
				}
				return
			}

			if err != nil {
				t.Fatalf("lookupLatestPolicySnapshot() = %v, want no error", err)
			}

			if diff := cmp.Diff(activePolicySnapshot, tc.wantPolicySnapshot, ignoreObjectMetaResourceVersionField); diff != "" {
				t.Errorf("active policy snapshot diff (-got, +want): %s", diff)
			}
		})
	}
}

func TestObserveSchedulingCycleMetrics(t *testing.T) {
	metricMetadata := `
		# HELP scheduling_cycle_duration_milliseconds The duration of a scheduling cycle run in milliseconds
		# TYPE scheduling_cycle_duration_milliseconds histogram
	`

	testCases := []struct {
		name            string
		cycleStartTime  time.Time
		wantMetricCount int
		wantHistogram   string
	}{
		{
			name:            "should observe a data point",
			cycleStartTime:  time.Now(),
			wantMetricCount: 1,
			wantHistogram: `
			scheduling_cycle_duration_milliseconds_bucket{is_failed="false",needs_requeue="false",le="10"} 1
            scheduling_cycle_duration_milliseconds_bucket{is_failed="false",needs_requeue="false",le="50"} 1
            scheduling_cycle_duration_milliseconds_bucket{is_failed="false",needs_requeue="false",le="100"} 1
            scheduling_cycle_duration_milliseconds_bucket{is_failed="false",needs_requeue="false",le="500"} 1
            scheduling_cycle_duration_milliseconds_bucket{is_failed="false",needs_requeue="false",le="1000"} 1
            scheduling_cycle_duration_milliseconds_bucket{is_failed="false",needs_requeue="false",le="5000"} 1
            scheduling_cycle_duration_milliseconds_bucket{is_failed="false",needs_requeue="false",le="10000"} 1
            scheduling_cycle_duration_milliseconds_bucket{is_failed="false",needs_requeue="false",le="50000"} 1
            scheduling_cycle_duration_milliseconds_bucket{is_failed="false",needs_requeue="false",le="+Inf"} 1
            scheduling_cycle_duration_milliseconds_sum{is_failed="false",needs_requeue="false"} 0
            scheduling_cycle_duration_milliseconds_count{is_failed="false",needs_requeue="false"} 1
			`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			observeSchedulingCycleMetrics(tc.cycleStartTime, false, false)

			if c := testutil.CollectAndCount(metrics.SchedulingCycleDurationMilliseconds); c != tc.wantMetricCount {
				t.Fatalf("metric counts, got %d, want %d", c, tc.wantMetricCount)
			}

			if err := testutil.CollectAndCompare(metrics.SchedulingCycleDurationMilliseconds, strings.NewReader(metricMetadata+tc.wantHistogram)); err != nil {
				t.Errorf("%s", err)
			}
		})
	}
}
