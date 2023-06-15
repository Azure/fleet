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
	"go.goms.io/fleet/pkg/utils"
)

const (
	CRPName     = "test-placement"
	policyName  = "test-policy"
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

// TestExtractNumOfClustersFromPolicySnapshot tests the extractNumOfClustersFromPolicySnapshot function.
func TestExtractNumOfClustersFromPolicySnapshot(t *testing.T) {
	testCases := []struct {
		name              string
		policy            *fleetv1beta1.ClusterPolicySnapshot
		wantNumOfClusters int
		expectedToFail    bool
	}{
		{
			name: "valid annotation",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					Annotations: map[string]string{
						fleetv1beta1.NumOfClustersAnnotation: "1",
					},
				},
			},
			wantNumOfClusters: 1,
		},
		{
			name: "no annotation",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			},
			expectedToFail: true,
		},
		{
			name: "invalid annotation: not an integer",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					Annotations: map[string]string{
						fleetv1beta1.NumOfClustersAnnotation: "abc",
					},
				},
			},
			expectedToFail: true,
		},
		{
			name: "invalid annotation: negative integer",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					Annotations: map[string]string{
						fleetv1beta1.NumOfClustersAnnotation: "-1",
					},
				},
			},
			expectedToFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			numOfClusters, err := extractNumOfClustersFromPolicySnapshot(tc.policy)
			if tc.expectedToFail {
				if err == nil {
					t.Fatalf("extractNumOfClustersFromPolicySnapshot() = %v, %v, want error", numOfClusters, err)
				}
				return
			}

			if numOfClusters != tc.wantNumOfClusters {
				t.Fatalf("extractNumOfClustersFromPolicySnapshot() = %v, %v, want %v, nil", numOfClusters, err, tc.wantNumOfClusters)
			}
		})
	}
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

// TestExtractOwnerCRPNameFromPolicySnapshot tests the extractOwnerCRPNameFromPolicySnapshot method.
func TestExtractOwnerCRPNameFromPolicySnapshot(t *testing.T) {
	testCases := []struct {
		name         string
		policy       *fleetv1beta1.ClusterPolicySnapshot
		expectToFail bool
	}{
		{
			name: "policy with CRP owner reference",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: utils.CRPV1Beta1GVK.Kind,
							Name: CRPName,
						},
					},
				},
			},
		},
		{
			name: "policy without CRP owner reference",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:            policyName,
					OwnerReferences: []metav1.OwnerReference{},
				},
			},
			expectToFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			owner, err := extractOwnerCRPNameFromPolicySnapshot(tc.policy)
			if tc.expectToFail {
				if err == nil {
					t.Fatalf("extractOwnerCRPNameFromPolicySnapshot() = %v, %v, want cannot find owner ref error", owner, err)
				}
				return
			}

			if err != nil || owner != CRPName {
				t.Fatalf("extractOwnerCRPNameFromPolicySnapshot() = %v, %v, want %v, no error", owner, err, CRPName)
			}
		})
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
		policy                 *fleetv1beta1.ClusterPolicySnapshot
		expectToFail           bool
		expectToFindNoBindings bool
	}{
		{
			name:    "found matching bindings",
			binding: binding,
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: utils.CRPV1Beta1GVK.Kind,
							Name: CRPName,
						},
					},
				},
			},
		},
		{
			name: "no owner reference in policy",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			},
			expectToFail: true,
		},
		{
			name:    "no matching bindings",
			binding: binding,
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: utils.CRPV1Beta1GVK.Kind,
							Name: altCRPName,
						},
					},
				},
			},
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
			bindings, err := f.collectBindings(ctx, tc.policy)
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
	activeBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "active-binding",
		},
	}
	deletedBindingWithDispatcherFinalizer := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleted-binding-with-dispatcher-finalizer",
			DeletionTimestamp: &timestamp,
			Finalizers: []string{
				utils.DispatcherFinalizer,
			},
		},
	}
	deletedBindingWithoutDispatcherFinalizer := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleted-binding-without-dispatcher-finalizer",
			DeletionTimestamp: &timestamp,
		},
	}

	active, deletedWith, deletedWithout := classifyBindings([]fleetv1beta1.ClusterResourceBinding{*activeBinding, *deletedBindingWithDispatcherFinalizer, *deletedBindingWithoutDispatcherFinalizer})
	if diff := cmp.Diff(active, []*fleetv1beta1.ClusterResourceBinding{activeBinding}); diff != "" {
		t.Errorf("classifyBindings() active = %v, want %v", active, []*fleetv1beta1.ClusterResourceBinding{activeBinding})
	}
	if !cmp.Equal(deletedWith, []*fleetv1beta1.ClusterResourceBinding{deletedBindingWithDispatcherFinalizer}) {
		t.Errorf("classifyBindings() deletedWithDispatcherFinalizer = %v, want %v", deletedWith, []*fleetv1beta1.ClusterResourceBinding{deletedBindingWithDispatcherFinalizer})
	}
	if !cmp.Equal(deletedWithout, []*fleetv1beta1.ClusterResourceBinding{deletedBindingWithoutDispatcherFinalizer}) {
		t.Errorf("classifyBindings() deletedWithoutDispatcherFinalizer = %v, want %v", deletedWithout, []*fleetv1beta1.ClusterResourceBinding{deletedBindingWithoutDispatcherFinalizer})
	}
}

// TestRemoveSchedulerFinalizerFromBindings tests the removeSchedulerFinalizerFromBindings method.
func TestRemoveSchedulerFinalizerFromBindings(t *testing.T) {
	binding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:       bindingName,
			Finalizers: []string{utils.SchedulerFinalizer},
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

	if controllerutil.ContainsFinalizer(updatedBinding, utils.SchedulerFinalizer) {
		t.Fatalf("Binding %s finalizers = %v, want no scheduler finalizer", bindingName, updatedBinding.Finalizers)
	}
}
