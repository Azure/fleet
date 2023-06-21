/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	CRPName     = "test-placement"
	bindingName = "test-binding"
)

// TO-DO (chenyu1): expand the test cases as development stablizes.

// TestMain sets up the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := fleetv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestCollectClusters tests the collectClusters method.
func TestCollectClusters(t *testing.T) {
	cluster := fleetv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-1",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(&cluster).
		Build()
	// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
	f := &framework{
		client: fakeClient,
	}

	ctx := context.Background()
	clusters, err := f.collectClusters(ctx)
	if err != nil {
		t.Fatalf("collectClusters() = %v, %v, want no error", clusters, err)
	}

	wantClusters := []fleetv1beta1.MemberCluster{cluster}
	if !cmp.Equal(clusters, wantClusters) {
		t.Fatalf("collectClusters() = %v, %v, want %v, nil", clusters, err, wantClusters)
	}
}

// TestCollectBindings tests the collectBindings method.
func TestCollectBindings(t *testing.T) {
	binding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
			Labels: map[string]string{
				fleetv1beta1.CRPTrackingLabel: CRPName,
			},
		},
	}
	altCRPName := "another-test-placement"

	testCases := []struct {
		name                   string
		binding                *fleetv1beta1.ClusterResourceBinding
		crpName                string
		expectToFail           bool
		expectToFindNoBindings bool
	}{
		{
			name:    "found matching bindings",
			binding: binding,
			crpName: CRPName,
		},
		{
			name:                   "no matching bindings",
			binding:                binding,
			crpName:                altCRPName,
			expectToFindNoBindings: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tc.binding != nil {
				fakeClientBuilder.WithObjects(tc.binding)
			}
			fakeClient := fakeClientBuilder.Build()
			// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
			f := &framework{
				uncachedReader: fakeClient,
			}

			ctx := context.Background()
			bindings, err := f.collectBindings(ctx, tc.crpName)
			if tc.expectToFail {
				if err == nil {
					t.Fatalf("collectBindings() = %v, %v, want failed to collect bindings error", bindings, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("collectBindings() = %v, %v, want %v, no error", bindings, err, binding)
			}
			wantBindings := []fleetv1beta1.ClusterResourceBinding{}
			if !tc.expectToFindNoBindings {
				wantBindings = append(wantBindings, *binding)
			}
			if !cmp.Equal(bindings, wantBindings) {
				t.Fatalf("collectBindings() = %v, %v, want %v, no error", bindings, err, wantBindings)
			}
		})
	}
}

// TestClassifyBindings tests the classifyBindings function.
func TestClassifyBindings(t *testing.T) {
	timestamp := metav1.Now()
	isPresent := "true"
	activeBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "active-binding",
			Labels: map[string]string{
				fleetv1beta1.ActiveBindingLabel: isPresent,
			},
		},
	}
	creatingBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "creating-binding",
			Labels: map[string]string{
				fleetv1beta1.CreatingBindingLabel: isPresent,
			},
		},
	}
	creatingWithoutTargetClusterBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "creating-binding",
			Labels: map[string]string{
				fleetv1beta1.CreatingBindingLabel:        isPresent,
				fleetv1beta1.NoTargetClusterBindingLabel: isPresent,
			},
		},
	}
	deletedBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleted-binding",
			DeletionTimestamp: &timestamp,
			Finalizers: []string{
				fleetv1beta1.DispatcherFinalizer,
				fleetv1beta1.SchedulerFinalizer,
			},
			Labels: map[string]string{
				fleetv1beta1.ObsoleteBindingLabel: isPresent,
			},
		},
	}

	active, creating, creatingWithoutTargetCluster, deleted, err := classifyBindings([]fleetv1beta1.ClusterResourceBinding{
		*activeBinding, *deletedBinding, *creatingBinding, *creatingWithoutTargetClusterBinding,
	})
	if err != nil {
		t.Fatalf("classifyBindings(), got %v, want no error", err)
	}
	if !cmp.Equal(active, []*fleetv1beta1.ClusterResourceBinding{activeBinding}) {
		t.Errorf("classifyBindings() active = %v, want %v", active, []*fleetv1beta1.ClusterResourceBinding{activeBinding})
	}
	if !cmp.Equal(deleted, []*fleetv1beta1.ClusterResourceBinding{deletedBinding}) {
		t.Errorf("classifyBindings() deleted = %v, want %v", deleted, []*fleetv1beta1.ClusterResourceBinding{deletedBinding})
	}
	if !cmp.Equal(creating, []*fleetv1beta1.ClusterResourceBinding{creatingBinding}) {
		t.Errorf("classifyBindings() creating = %v, want %v", creating, []*fleetv1beta1.ClusterResourceBinding{creatingBinding})
	}
	if !cmp.Equal(creatingWithoutTargetCluster, []*fleetv1beta1.ClusterResourceBinding{creatingWithoutTargetClusterBinding}) {
		t.Errorf("classifyBindings() creatingWithoutTargetCluster = %v, want %v", creatingWithoutTargetCluster, []*fleetv1beta1.ClusterResourceBinding{creatingWithoutTargetClusterBinding})
	}
}

// TestRemoveSchedulerFinalizerFromBindings tests the removeSchedulerFinalizerFromBindings method.
func TestRemoveSchedulerFinalizerFromBindings(t *testing.T) {
	binding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:       bindingName,
			Finalizers: []string{fleetv1beta1.SchedulerFinalizer},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(binding).
		Build()
	// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
	f := &framework{
		client: fakeClient,
	}

	ctx := context.Background()
	if err := f.removeSchedulerFinalizerFromBindings(ctx, []*fleetv1beta1.ClusterResourceBinding{binding}); err != nil {
		t.Fatalf("removeSchedulerFinalizerFromBindings() = %v, want no error", err)
	}

	// Verify that the finalizer has been removed.
	updatedBinding := &fleetv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: bindingName}, updatedBinding); err != nil {
		t.Fatalf("Binding Get(%v) = %v, want no error", bindingName, err)
	}

	if controllerutil.ContainsFinalizer(updatedBinding, fleetv1beta1.SchedulerFinalizer) {
		t.Fatalf("Binding %s finalizers = %v, want no scheduler finalizer", bindingName, updatedBinding.Finalizers)
	}
}
