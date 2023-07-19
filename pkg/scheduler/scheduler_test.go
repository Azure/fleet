/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package scheduler

import (
	"context"
	"log"
	"os"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
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
	ignoreTypeMetaAPIVersionKindFields   = cmpopts.IgnoreFields(metav1.TypeMeta{}, "APIVersion", "Kind")
)

// TestMain sets up the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := fleetv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestCleanUpAllBindingsFor tests the cleanUpAllBindingsFor method.
func TestCleanUpAllBindingsFor(t *testing.T) {
	now := metav1.Now()
	crp := &fleetv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:              crpName,
			DeletionTimestamp: &now,
			Finalizers:        []string{fleetv1beta1.SchedulerCRPCleanupFinalizer},
		},
	}

	bindings := []*fleetv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: bindingName,
				Labels: map[string]string{
					fleetv1beta1.CRPTrackingLabel: crpName,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: altBindingName,
				Labels: map[string]string{
					fleetv1beta1.CRPTrackingLabel: crpName,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(crp, bindings[0], bindings[1]).
		Build()
	// Construct scheduler manually instead of using NewScheduler() to avoid mocking the controller
	// manager.
	s := &Scheduler{
		client:         fakeClient,
		uncachedReader: fakeClient,
	}

	ctx := context.Background()
	if err := s.cleanUpAllBindingsFor(ctx, crp); err != nil {
		t.Fatalf("cleanUpAllBindingsFor() = %v, want no error", err)
	}

	if err := fakeClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err == nil {
		t.Fatalf("Get() CRP = %v, want no error", err)
	}
	wantCRP := &fleetv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:              crpName,
			DeletionTimestamp: &now,
			Finalizers:        []string{},
		},
	}
	if diff := cmp.Diff(crp, wantCRP, ignoreObjectMetaResourceVersionField, ignoreTypeMetaAPIVersionKindFields); diff != "" {
		t.Errorf("updated CRP diff (-got, +want): %s", diff)
	}

	bindingList := &fleetv1beta1.ClusterResourceBindingList{}
	if err := fakeClient.List(ctx, bindingList); err != nil {
		t.Fatalf("List() bindings = %v, want no error", err)
	}

	if len(bindingList.Items) != 0 {
		t.Errorf("binding list length = %d, want 0", len(bindingList.Items))
	}
}

// TestLookupLatestPolicySnapshot tests the lookupLatestPolicySnapshot method.
func TestLookupLatestPolicySnapshot(t *testing.T) {
	crp := &fleetv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       crpName,
			Finalizers: []string{fleetv1beta1.SchedulerCRPCleanupFinalizer},
		},
	}

	testCases := []struct {
		name               string
		policySnapshots    []*fleetv1beta1.ClusterSchedulingPolicySnapshot
		wantPolicySnapshot *fleetv1beta1.ClusterSchedulingPolicySnapshot
		expectedToFail     bool
	}{
		{
			name:           "no active policy snapshot",
			expectedToFail: true,
		},
		{
			name: "multiple active policy snapshots",
			policySnapshots: []*fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: policySnapshotName,
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:      crpName,
							fleetv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altPolicySnapshotName,
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:      crpName,
							fleetv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: anotherPolicySnapshotName,
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: crpName,
						},
					},
				},
			},
			expectedToFail: true,
		},
		{
			name: "found one active policy snapshot",
			policySnapshots: []*fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: policySnapshotName,
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: crpName,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altPolicySnapshotName,
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:      crpName,
							fleetv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
						},
					},
				},
			},
			wantPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: altPolicySnapshotName,
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel:      crpName,
						fleetv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClientBuilder := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(crp)
			for _, policySnapshot := range tc.policySnapshots {
				fakeClientBuilder.WithObjects(policySnapshot)
			}
			fakeClient := fakeClientBuilder.Build()
			// Construct scheduler manually instead of using NewScheduler() to avoid mocking the controller
			// manager.
			s := &Scheduler{
				client:         fakeClient,
				uncachedReader: fakeClient,
			}

			ctx := context.Background()
			activePolicySnapshot, err := s.lookupLatestPolicySnapshot(ctx, crp)
			if tc.expectedToFail {
				if err == nil {
					t.Errorf("lookUpLatestPolicySnapshot() = %v, want error", activePolicySnapshot)
				}

				return
			}

			if diff := cmp.Diff(activePolicySnapshot, tc.wantPolicySnapshot, ignoreObjectMetaResourceVersionField); diff != "" {
				t.Errorf("active policy snapshot diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestAddSchedulerCleanUpFinalizer tests the addSchedulerCleanUpFinalizer method.
func TestAddSchedulerCleanUpFinalizer(t *testing.T) {
	crp := &fleetv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpName,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(crp).
		Build()
	// Construct scheduler manually instead of using NewScheduler() to avoid mocking the controller
	// manager.
	s := &Scheduler{
		client:         fakeClient,
		uncachedReader: fakeClient,
	}

	ctx := context.Background()
	if err := s.addSchedulerCleanUpFinalizer(ctx, crp); err != nil {
		t.Fatalf("addSchedulerCleanUpFinalizer() = %v, want no error", err)
	}

	if err := fakeClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
		t.Fatalf("Get() CRP = %v, want no error", err)
	}
	wantCRP := &fleetv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       crpName,
			Finalizers: []string{fleetv1beta1.SchedulerCRPCleanupFinalizer},
		},
	}
	if diff := cmp.Diff(crp, wantCRP, ignoreObjectMetaResourceVersionField, ignoreTypeMetaAPIVersionKindFields); diff != "" {
		t.Errorf("updated CRP diff (-got, +want): %s", diff)
	}
}
