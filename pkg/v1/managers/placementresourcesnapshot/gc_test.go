/*
Copyright 2026 The KubeFleet Authors.

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

package placementresourcesnapshot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/managers/placementresourcesnapshot/uniquename"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

// TestCheckForInconsistentResourceSnapshots tests the checkForInconsistentResourceSnapshots method.
func TestCheckForInconsistentResourceSnapshots(t *testing.T) {
	const (
		policyName      = "policy"
		otherPolicyName = "other-policy"
		namespaceName   = "app"
	)
	policyUID := types.UID("policy-uid")

	clusterPolicy := &placementv1alpha1.ClusterPlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
			UID:  policyUID,
		},
	}
	namespacedPolicy := &placementv1alpha1.PlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: namespaceName,
			UID:       policyUID,
		},
	}

	ownerRef := func(kind, name string, uid types.UID) metav1.OwnerReference {
		return metav1.OwnerReference{
			APIVersion: placementv1alpha1.GroupVersion.String(),
			Kind:       kind,
			Name:       name,
			UID:        uid,
		}
	}
	clusterPolicyOwnerRef := ownerRef(placementv1alpha1.ClusterPlacementPolicyKind, policyName, policyUID)
	namespacedPolicyOwnerRef := ownerRef(placementv1alpha1.PlacementPolicyKind, policyName, policyUID)
	// The owner reference to a placement policy that has been deleted and then re-created with the same name.
	staleClusterPolicyOwnerRef := ownerRef(placementv1alpha1.ClusterPlacementPolicyKind, policyName, "stale-policy-uid")
	otherClusterPolicyOwnerRef := ownerRef(placementv1alpha1.ClusterPlacementPolicyKind, otherPolicyName, "other-policy-uid")

	snapshotName := func(idx int) string {
		return uniquename.PrimaryPlacementResourceSnapshotName(policyName, policyUID, idx)
	}
	snapshotUID := func(idx int) types.UID {
		return types.UID(fmt.Sprintf("snapshot-%d-uid", idx))
	}
	clusterSnapshot := func(idx int, ownerRefs ...metav1.OwnerReference) *placementv1alpha1.ClusterPlacementResourceSnapshot {
		return &placementv1alpha1.ClusterPlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:            snapshotName(idx),
				UID:             snapshotUID(idx),
				OwnerReferences: ownerRefs,
			},
		}
	}
	namespacedSnapshot := func(idx int, ownerRefs ...metav1.OwnerReference) *placementv1alpha1.PlacementResourceSnapshot {
		return &placementv1alpha1.PlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:            snapshotName(idx),
				Namespace:       namespaceName,
				UID:             snapshotUID(idx),
				OwnerReferences: ownerRefs,
			},
		}
	}
	deletionTimestamp := metav1.NewTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	// markedForDeletion marks the given cluster placement resource snapshot for deletion.
	markedForDeletion := func(snapshot *placementv1alpha1.ClusterPlacementResourceSnapshot) *placementv1alpha1.ClusterPlacementResourceSnapshot {
		snapshot.DeletionTimestamp = &deletionTimestamp
		return snapshot
	}
	gcRequest := func(namespace string, idx int) snapshotGarbageCollectionRequest {
		return snapshotGarbageCollectionRequest{
			snapshotNamespacedName:        types.NamespacedName{Namespace: namespace, Name: snapshotName(idx)},
			snapshotUID:                   snapshotUID(idx),
			placementPolicyNamespacedName: types.NamespacedName{Namespace: namespace, Name: policyName},
		}
	}
	leftover := func(namespace string, idx int) klog.ObjectRef {
		return klog.ObjectRef{Namespace: namespace, Name: snapshotName(idx)}
	}

	testCases := []struct {
		name            string
		placementPolicy placementv1alpha1.PlacementPolicyAccessor
		sortedSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor
		wantLeftovers   []klog.ObjectRef
		// The category of the error that is expected to be returned; leave empty if no error is expected.
		wantErrCategory kferrors.ErrCategory
		wantGCRequests  []snapshotGarbageCollectionRequest
	}{
		{
			name:            "no snapshots",
			placementPolicy: clusterPolicy,
		},
		{
			name:            "all snapshots owned by the placement policy",
			placementPolicy: clusterPolicy,
			sortedSnapshots: []placementv1alpha1.PlacementResourceSnapshotAccessor{
				clusterSnapshot(0, clusterPolicyOwnerRef),
				clusterSnapshot(1, clusterPolicyOwnerRef),
			},
		},
		{
			name:            "orphaned snapshots",
			placementPolicy: clusterPolicy,
			sortedSnapshots: []placementv1alpha1.PlacementResourceSnapshotAccessor{
				clusterSnapshot(0),
				clusterSnapshot(1, clusterPolicyOwnerRef),
				clusterSnapshot(2),
			},
			wantLeftovers:  []klog.ObjectRef{leftover("", 0), leftover("", 2)},
			wantGCRequests: []snapshotGarbageCollectionRequest{gcRequest("", 0), gcRequest("", 2)},
		},
		{
			name:            "orphaned snapshot marked for deletion",
			placementPolicy: clusterPolicy,
			sortedSnapshots: []placementv1alpha1.PlacementResourceSnapshotAccessor{
				markedForDeletion(clusterSnapshot(0)),
			},
			// Snapshotting stays blocked, but no GC request is enqueued again.
			wantLeftovers: []klog.ObjectRef{leftover("", 0)},
		},
		{
			name:            "orphaned snapshots, one of which is marked for deletion",
			placementPolicy: clusterPolicy,
			sortedSnapshots: []placementv1alpha1.PlacementResourceSnapshotAccessor{
				markedForDeletion(clusterSnapshot(0)),
				clusterSnapshot(1),
				clusterSnapshot(2, clusterPolicyOwnerRef),
			},
			wantLeftovers:  []klog.ObjectRef{leftover("", 0), leftover("", 1)},
			wantGCRequests: []snapshotGarbageCollectionRequest{gcRequest("", 1)},
		},
		{
			name:            "snapshot owned by a deleted placement policy with the same name",
			placementPolicy: clusterPolicy,
			sortedSnapshots: []placementv1alpha1.PlacementResourceSnapshotAccessor{
				clusterSnapshot(0, staleClusterPolicyOwnerRef),
				clusterSnapshot(1, clusterPolicyOwnerRef),
			},
			wantErrCategory: kferrors.ErrCategoryTransient,
		},
		{
			name:            "orphaned snapshot followed by a snapshot owned by a deleted placement policy with the same name",
			placementPolicy: clusterPolicy,
			sortedSnapshots: []placementv1alpha1.PlacementResourceSnapshotAccessor{
				clusterSnapshot(0),
				clusterSnapshot(1, staleClusterPolicyOwnerRef),
			},
			wantLeftovers:   []klog.ObjectRef{leftover("", 0)},
			wantErrCategory: kferrors.ErrCategoryTransient,
			wantGCRequests:  []snapshotGarbageCollectionRequest{gcRequest("", 0)},
		},
		{
			name:            "snapshot owned by a differently named placement policy",
			placementPolicy: clusterPolicy,
			sortedSnapshots: []placementv1alpha1.PlacementResourceSnapshotAccessor{
				clusterSnapshot(0, otherClusterPolicyOwnerRef),
			},
			wantErrCategory: kferrors.ErrCategoryUser,
		},
		{
			name:            "snapshot owned by the placement policy and another owner",
			placementPolicy: clusterPolicy,
			sortedSnapshots: []placementv1alpha1.PlacementResourceSnapshotAccessor{
				clusterSnapshot(0, clusterPolicyOwnerRef, otherClusterPolicyOwnerRef),
			},
			wantErrCategory: kferrors.ErrCategoryUser,
		},
		{
			name:            "snapshot with an owner reference of a mismatched kind",
			placementPolicy: clusterPolicy,
			sortedSnapshots: []placementv1alpha1.PlacementResourceSnapshotAccessor{
				clusterSnapshot(0, namespacedPolicyOwnerRef),
			},
			wantErrCategory: kferrors.ErrCategoryUser,
		},
		{
			name:            "namespaced snapshots owned by the placement policy",
			placementPolicy: namespacedPolicy,
			sortedSnapshots: []placementv1alpha1.PlacementResourceSnapshotAccessor{
				namespacedSnapshot(0, namespacedPolicyOwnerRef),
				namespacedSnapshot(1, namespacedPolicyOwnerRef),
			},
		},
		{
			name:            "orphaned namespaced snapshot",
			placementPolicy: namespacedPolicy,
			sortedSnapshots: []placementv1alpha1.PlacementResourceSnapshotAccessor{
				namespacedSnapshot(0),
				namespacedSnapshot(1, namespacedPolicyOwnerRef),
			},
			wantLeftovers:  []klog.ObjectRef{leftover(namespaceName, 0)},
			wantGCRequests: []snapshotGarbageCollectionRequest{gcRequest(namespaceName, 0)},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gcwq := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[snapshotGarbageCollectionRequest]())
			defer gcwq.ShutDown()
			m := &Manager{gcwq: gcwq}

			gotLeftovers, err := m.checkForInconsistentResourceSnapshots(tc.sortedSnapshots, tc.placementPolicy)
			switch {
			case tc.wantErrCategory != "" && err == nil:
				t.Errorf("checkForInconsistentResourceSnapshots() = %v, nil, want error of category %v", gotLeftovers, tc.wantErrCategory)
			case tc.wantErrCategory != "":
				verifyErrCategory(t, "checkForInconsistentResourceSnapshots()", err, tc.wantErrCategory)
			case err != nil:
				t.Errorf("checkForInconsistentResourceSnapshots() = %v, want no error", err)
			}
			if diff := cmp.Diff(gotLeftovers, tc.wantLeftovers, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("checkForInconsistentResourceSnapshots() leftovers mismatch (-got +want):\n%s", diff)
			}

			gotGCRequests := make([]snapshotGarbageCollectionRequest, 0, gcwq.Len())
			for gcwq.Len() > 0 {
				req, _ := gcwq.Get()
				gotGCRequests = append(gotGCRequests, req)
				gcwq.Done(req)
			}
			if diff := cmp.Diff(gotGCRequests, tc.wantGCRequests,
				cmp.AllowUnexported(snapshotGarbageCollectionRequest{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("checkForInconsistentResourceSnapshots() enqueued GC requests mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// TestIsSnapshotInUseByAnyPlacementBinding tests the isSnapshotInUseByAnyPlacementBinding method.
func TestIsSnapshotInUseByAnyPlacementBinding(t *testing.T) {
	const (
		policyName      = "policy"
		otherPolicyName = "other-policy"
		bindingName     = "binding"
		namespaceName   = "app"
		otherNamespace  = "other-app"
		clusterName     = "member-1"
	)
	policyUID := types.UID("policy-uid")
	snapshotName := uniquename.PrimaryPlacementResourceSnapshotName(policyName, policyUID, 0)
	otherSnapshotName := uniquename.PrimaryPlacementResourceSnapshotName(policyName, policyUID, 1)

	policyOwnerRef := func(kind, name string) metav1.OwnerReference {
		return metav1.OwnerReference{
			APIVersion: placementv1alpha1.GroupVersion.String(),
			Kind:       kind,
			Name:       name,
			UID:        policyUID,
		}
	}
	clusterPolicyOwnerRef := policyOwnerRef(placementv1alpha1.ClusterPlacementPolicyKind, policyName)
	namespacedPolicyOwnerRef := policyOwnerRef(placementv1alpha1.PlacementPolicyKind, policyName)

	clusterSnapshot := func(ownerRefs ...metav1.OwnerReference) *placementv1alpha1.ClusterPlacementResourceSnapshot {
		return &placementv1alpha1.ClusterPlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:            snapshotName,
				OwnerReferences: ownerRefs,
			},
		}
	}
	clusterBinding := func(resourceSnapshotName string) *placementv1alpha1.ClusterPlacementBinding {
		return &placementv1alpha1.ClusterPlacementBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:            bindingName,
				OwnerReferences: []metav1.OwnerReference{clusterPolicyOwnerRef},
			},
			Spec: placementv1alpha1.PlacementBindingSpec{
				PlacementPolicyName:  policyName,
				ClusterName:          clusterName,
				ResourceSnapshotName: resourceSnapshotName,
			},
		}
	}
	namespacedSnapshot := &placementv1alpha1.PlacementResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:            snapshotName,
			Namespace:       namespaceName,
			OwnerReferences: []metav1.OwnerReference{namespacedPolicyOwnerRef},
		},
	}
	namespacedBinding := func(namespace string) *placementv1alpha1.PlacementBinding {
		return &placementv1alpha1.PlacementBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:            bindingName,
				Namespace:       namespace,
				OwnerReferences: []metav1.OwnerReference{namespacedPolicyOwnerRef},
			},
			Spec: placementv1alpha1.PlacementBindingSpec{
				PlacementPolicyName:  policyName,
				ClusterName:          clusterName,
				ResourceSnapshotName: snapshotName,
			},
		}
	}

	clusterPolicyNamespacedName := types.NamespacedName{Name: policyName}
	namespacedPolicyNamespacedName := types.NamespacedName{Namespace: namespaceName, Name: policyName}

	testCases := []struct {
		name                          string
		snapshot                      placementv1alpha1.PlacementResourceSnapshotAccessor
		placementPolicyNamespacedName types.NamespacedName
		bindings                      []client.Object
		interceptorFuncs              interceptor.Funcs
		want                          bool
		// The category of the error that is expected to be returned; leave empty if no error is expected.
		wantErrCategory kferrors.ErrCategory
	}{
		{
			name:                          "no placement bindings",
			snapshot:                      clusterSnapshot(clusterPolicyOwnerRef),
			placementPolicyNamespacedName: clusterPolicyNamespacedName,
			want:                          false,
		},
		{
			name:                          "placement binding using a different snapshot",
			snapshot:                      clusterSnapshot(clusterPolicyOwnerRef),
			placementPolicyNamespacedName: clusterPolicyNamespacedName,
			bindings:                      []client.Object{clusterBinding(otherSnapshotName)},
			want:                          false,
		},
		{
			name:                          "placement binding using the snapshot",
			snapshot:                      clusterSnapshot(clusterPolicyOwnerRef),
			placementPolicyNamespacedName: clusterPolicyNamespacedName,
			bindings:                      []client.Object{clusterBinding(snapshotName)},
			want:                          true,
		},
		{
			name:                          "orphaned snapshot (no owner references) referenced by a placement binding",
			snapshot:                      clusterSnapshot(),
			placementPolicyNamespacedName: clusterPolicyNamespacedName,
			bindings:                      []client.Object{clusterBinding(snapshotName)},
			want:                          false,
		},
		{
			name:                          "snapshot owned by a differently named placement policy",
			snapshot:                      clusterSnapshot(policyOwnerRef(placementv1alpha1.ClusterPlacementPolicyKind, otherPolicyName)),
			placementPolicyNamespacedName: clusterPolicyNamespacedName,
			bindings:                      []client.Object{clusterBinding(snapshotName)},
			want:                          false,
		},
		{
			name:                          "snapshot with an owner reference of a mismatched kind",
			snapshot:                      clusterSnapshot(namespacedPolicyOwnerRef),
			placementPolicyNamespacedName: clusterPolicyNamespacedName,
			bindings:                      []client.Object{clusterBinding(snapshotName)},
			want:                          false,
		},
		{
			name:                          "failed to list placement bindings",
			snapshot:                      clusterSnapshot(clusterPolicyOwnerRef),
			placementPolicyNamespacedName: clusterPolicyNamespacedName,
			bindings:                      []client.Object{clusterBinding(snapshotName)},
			interceptorFuncs: interceptor.Funcs{
				List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
					return errors.New("simulated list failure")
				},
			},
			wantErrCategory: kferrors.ErrCategoryAPIServer,
		},
		{
			name:                          "namespaced placement binding using the snapshot",
			snapshot:                      namespacedSnapshot,
			placementPolicyNamespacedName: namespacedPolicyNamespacedName,
			bindings:                      []client.Object{namespacedBinding(namespaceName)},
			want:                          true,
		},
		{
			name:                          "namespaced placement binding in a different namespace",
			snapshot:                      namespacedSnapshot,
			placementPolicyNamespacedName: namespacedPolicyNamespacedName,
			bindings:                      []client.Object{namespacedBinding(otherNamespace)},
			want:                          false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manager{hubClient: interceptor.NewClient(newFakeHubClientWithIndexes(t, tc.bindings...), tc.interceptorFuncs)}

			got, err := m.isSnapshotInUseByAnyPlacementBinding(context.Background(), tc.snapshot, tc.placementPolicyNamespacedName)
			if tc.wantErrCategory != "" {
				if err == nil {
					t.Fatalf("isSnapshotInUseByAnyPlacementBinding() = %v, nil, want error of category %v", got, tc.wantErrCategory)
				}
				verifyErrCategory(t, "isSnapshotInUseByAnyPlacementBinding()", err, tc.wantErrCategory)
				return
			}
			if err != nil {
				t.Fatalf("isSnapshotInUseByAnyPlacementBinding() = %v, want no error", err)
			}
			if got != tc.want {
				t.Errorf("isSnapshotInUseByAnyPlacementBinding() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestGarbageCollect tests the garbageCollect method.
func TestGarbageCollect(t *testing.T) {
	const (
		policyName  = "policy"
		bindingName = "binding"
		clusterName = "member-1"
	)
	policyUID := types.UID("policy-uid")
	recreatedPolicyUID := types.UID("recreated-policy-uid")
	primaryName := uniquename.PrimaryPlacementResourceSnapshotName(policyName, policyUID, 0)
	secondaryName := uniquename.SecondaryPlacementResourceSnapshotName(policyName, policyUID, 0, 1)
	tertiaryName := uniquename.SecondaryPlacementResourceSnapshotName(policyName, policyUID, 0, 2)
	// Unexpected primary and secondary snapshots at the same index as the primary snapshot specified in the GC
	// request, named as those of a re-created placement policy of the same name.
	otherPrimaryName := uniquename.PrimaryPlacementResourceSnapshotName(policyName, recreatedPolicyUID, 0)
	otherSecondaryName := uniquename.SecondaryPlacementResourceSnapshotName(policyName, recreatedPolicyUID, 0, 1)
	primaryUID := types.UID("primary-uid")
	recreatedPrimaryUID := types.UID("recreated-primary-uid")
	policyOwnerRef := metav1.OwnerReference{
		APIVersion: placementv1alpha1.GroupVersion.String(),
		Kind:       placementv1alpha1.ClusterPlacementPolicyKind,
		Name:       policyName,
		UID:        policyUID,
	}
	recreatedPolicyOwnerRef := policyOwnerRef
	recreatedPolicyOwnerRef.UID = recreatedPolicyUID

	snapshot := func(name string, uid types.UID, subIdx int, ownerRefs ...metav1.OwnerReference) *placementv1alpha1.ClusterPlacementResourceSnapshot {
		return &placementv1alpha1.ClusterPlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				UID:  uid,
				Labels: map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  policyName,
					placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    "0",
					placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: strconv.Itoa(subIdx),
				},
				Annotations: map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey: policyName,
				},
				OwnerReferences: ownerRefs,
			},
		}
	}
	bindingUsingPrimary := &placementv1alpha1.ClusterPlacementBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            bindingName,
			OwnerReferences: []metav1.OwnerReference{policyOwnerRef},
		},
		Spec: placementv1alpha1.PlacementBindingSpec{
			PlacementPolicyName:  policyName,
			ClusterName:          clusterName,
			ResourceSnapshotName: primaryName,
		},
	}
	gcRequest := snapshotGarbageCollectionRequest{
		snapshotNamespacedName:        types.NamespacedName{Name: primaryName},
		snapshotUID:                   primaryUID,
		placementPolicyNamespacedName: types.NamespacedName{Name: policyName},
	}

	testCases := []struct {
		name             string
		objs             []client.Object
		interceptorFuncs interceptor.Funcs
		// The category of the error that is expected to be returned; leave empty if no error is expected.
		wantErrCategory        kferrors.ErrCategory
		wantRemainingSnapshots []string
	}{
		{
			name: "primary and secondary snapshots owned by the placement policy",
			objs: []client.Object{
				snapshot(primaryName, primaryUID, 0, policyOwnerRef),
				snapshot(secondaryName, "secondary-uid", 1, policyOwnerRef),
				snapshot(tertiaryName, "tertiary-uid", 2, policyOwnerRef),
			},
		},
		{
			name: "orphaned primary and secondary snapshots",
			objs: []client.Object{
				snapshot(primaryName, primaryUID, 0),
				snapshot(secondaryName, "secondary-uid", 1),
			},
		},
		{
			name: "another primary snapshot at the same index",
			objs: []client.Object{
				snapshot(primaryName, primaryUID, 0, policyOwnerRef),
				snapshot(secondaryName, "secondary-uid", 1, policyOwnerRef),
				snapshot(otherPrimaryName, "other-primary-uid", 0, policyOwnerRef),
			},
			wantRemainingSnapshots: []string{otherPrimaryName},
		},
		{
			name: "orphaned primary snapshot, with snapshots of a re-created placement policy at the same index",
			objs: []client.Object{
				snapshot(primaryName, primaryUID, 0),
				snapshot(secondaryName, "secondary-uid", 1),
				snapshot(otherPrimaryName, "other-primary-uid", 0, recreatedPolicyOwnerRef),
				snapshot(otherSecondaryName, "other-secondary-uid", 1, recreatedPolicyOwnerRef),
			},
			wantRemainingSnapshots: []string{otherPrimaryName, otherSecondaryName},
		},
		{
			name: "secondary snapshot owned by a different placement policy of the same name",
			objs: []client.Object{
				snapshot(primaryName, primaryUID, 0, policyOwnerRef),
				snapshot(secondaryName, "secondary-uid", 1, recreatedPolicyOwnerRef),
				snapshot(tertiaryName, "tertiary-uid", 2, policyOwnerRef),
			},
			wantRemainingSnapshots: []string{secondaryName},
		},
		{
			name: "orphaned secondary snapshot with a primary snapshot owned by the placement policy",
			objs: []client.Object{
				snapshot(primaryName, primaryUID, 0, policyOwnerRef),
				snapshot(secondaryName, "secondary-uid", 1),
			},
			wantRemainingSnapshots: []string{secondaryName},
		},
		{
			name: "primary snapshot in use by a placement binding",
			objs: []client.Object{
				snapshot(primaryName, primaryUID, 0, policyOwnerRef),
				snapshot(secondaryName, "secondary-uid", 1, policyOwnerRef),
				bindingUsingPrimary,
			},
			wantRemainingSnapshots: []string{primaryName, secondaryName},
		},
		{
			name: "orphaned primary snapshot referenced by a placement binding",
			objs: []client.Object{
				snapshot(primaryName, primaryUID, 0),
				snapshot(secondaryName, "secondary-uid", 1),
				bindingUsingPrimary,
			},
		},
		{
			name: "primary snapshot has been re-created",
			// Snapshot names factor in the UID of the placement policy; only the same placement policy can re-create
			// a snapshot under the same name (e.g., after the snapshot has been deleted by hand).
			objs: []client.Object{
				snapshot(primaryName, recreatedPrimaryUID, 0, policyOwnerRef),
				snapshot(secondaryName, "secondary-uid", 1, policyOwnerRef),
			},
			wantRemainingSnapshots: []string{primaryName, secondaryName},
		},
		{
			name: "primary snapshot not found",
			objs: []client.Object{
				snapshot(secondaryName, "secondary-uid", 1, policyOwnerRef),
			},
			wantRemainingSnapshots: []string{secondaryName},
		},
		{
			name: "failed to retrieve the primary snapshot",
			objs: []client.Object{
				snapshot(primaryName, primaryUID, 0, policyOwnerRef),
				snapshot(secondaryName, "secondary-uid", 1, policyOwnerRef),
			},
			interceptorFuncs: interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return errors.New("simulated get failure")
				},
			},
			wantErrCategory:        kferrors.ErrCategoryAPIServer,
			wantRemainingSnapshots: []string{primaryName, secondaryName},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := newFakeHubClientWithIndexes(t, tc.objs...)
			m := &Manager{hubClient: interceptor.NewClient(fakeClient, tc.interceptorFuncs)}

			err := m.garbageCollect(context.Background(), gcRequest)
			switch {
			case tc.wantErrCategory != "" && err == nil:
				t.Errorf("garbageCollect() = nil, want error of category %v", tc.wantErrCategory)
			case tc.wantErrCategory != "":
				verifyErrCategory(t, "garbageCollect()", err, tc.wantErrCategory)
			case err != nil:
				t.Errorf("garbageCollect() = %v, want no error", err)
			}

			snapshotList := &placementv1alpha1.ClusterPlacementResourceSnapshotList{}
			if err := fakeClient.List(context.Background(), snapshotList); err != nil {
				t.Fatalf("List() = %v, want no error", err)
			}
			gotRemainingSnapshots := make([]string, 0, len(snapshotList.Items))
			for idx := range snapshotList.Items {
				gotRemainingSnapshots = append(gotRemainingSnapshots, snapshotList.Items[idx].Name)
			}
			if diff := cmp.Diff(gotRemainingSnapshots, tc.wantRemainingSnapshots,
				cmpopts.EquateEmpty(), cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("garbageCollect() remaining snapshots mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// TestStart tests the GC worker loop that the Start method runs.
func TestStart(t *testing.T) {
	const policyName = "policy"
	policyUID := types.UID("policy-uid")
	primaryName0 := uniquename.PrimaryPlacementResourceSnapshotName(policyName, policyUID, 0)
	secondaryName := uniquename.SecondaryPlacementResourceSnapshotName(policyName, policyUID, 0, 1)
	primaryName1 := uniquename.PrimaryPlacementResourceSnapshotName(policyName, policyUID, 1)
	// The primary snapshot at index 2; it is given a malformed index label below.
	malformedName := uniquename.PrimaryPlacementResourceSnapshotName(policyName, policyUID, 2)
	policyOwnerRef := metav1.OwnerReference{
		APIVersion: placementv1alpha1.GroupVersion.String(),
		Kind:       placementv1alpha1.ClusterPlacementPolicyKind,
		Name:       policyName,
		UID:        policyUID,
	}

	// snapshot returns a cluster placement resource snapshot owned by the placement policy; the index label is a
	// string so that a malformed value can be injected.
	snapshot := func(name, idxLabel string, subIdx int) *placementv1alpha1.ClusterPlacementResourceSnapshot {
		return &placementv1alpha1.ClusterPlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				UID:  types.UID(name + "-uid"),
				Labels: map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  policyName,
					placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    idxLabel,
					placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: strconv.Itoa(subIdx),
				},
				Annotations: map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey: policyName,
				},
				OwnerReferences: []metav1.OwnerReference{policyOwnerRef},
			},
		}
	}
	gcRequestOf := func(primaryName string) snapshotGarbageCollectionRequest {
		return snapshotGarbageCollectionRequest{
			snapshotNamespacedName:        types.NamespacedName{Name: primaryName},
			snapshotUID:                   types.UID(primaryName + "-uid"),
			placementPolicyNamespacedName: types.NamespacedName{Name: policyName},
		}
	}
	remainingSnapshotNames := func(t *testing.T, c client.Client) []string {
		t.Helper()

		snapshotList := &placementv1alpha1.ClusterPlacementResourceSnapshotList{}
		if err := c.List(context.Background(), snapshotList); err != nil {
			t.Fatalf("List() = %v, want no error", err)
		}
		names := make([]string, 0, len(snapshotList.Items))
		for idx := range snapshotList.Items {
			names = append(names, snapshotList.Items[idx].Name)
		}
		return names
	}

	t.Run("retryable failure is retried with backoff until it succeeds", func(t *testing.T) {
		fakeClient := newFakeHubClientWithIndexes(t, snapshot(primaryName0, "0", 0), snapshot(secondaryName, "0", 1))
		var failDeletes atomic.Bool
		failDeletes.Store(true)
		m := &Manager{
			hubClient: interceptor.NewClient(fakeClient, interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if failDeletes.Load() {
						return errors.New("simulated delete failure")
					}
					return c.Delete(ctx, obj, opts...)
				},
			}),
			gcwq: newTestGCQueue(),
		}
		stop := startGCWorker(t, m)

		gcRequest := gcRequestOf(primaryName0)
		m.gcwq.Add(gcRequest)
		pollUntil(t, "the failed GC request is retried at least twice", func() bool {
			return m.gcwq.NumRequeues(gcRequest) >= 2
		})
		if got := remainingSnapshotNames(t, fakeClient); len(got) != 2 {
			t.Errorf("remaining snapshots = %v, want both snapshots kept while deletions fail", got)
		}

		failDeletes.Store(false)
		pollUntil(t, "the snapshots are garbage collected once deletions succeed", func() bool {
			return len(remainingSnapshotNames(t, fakeClient)) == 0
		})
		// The backoff is reset once the GC request succeeds.
		pollUntil(t, "the GC request is forgotten once it succeeds", func() bool {
			return m.gcwq.NumRequeues(gcRequest) == 0
		})

		if err := stop(); err != nil {
			t.Errorf("Start() = %v, want no error", err)
		}
	})

	t.Run("unexpected failure is dropped, and later requests are still processed", func(t *testing.T) {
		fakeClient := newFakeHubClientWithIndexes(t, snapshot(malformedName, "malformed", 0), snapshot(primaryName1, "1", 0))
		var malformedGetCnt atomic.Int32
		m := &Manager{
			hubClient: interceptor.NewClient(fakeClient, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if key.Name == malformedName {
						malformedGetCnt.Add(1)
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}),
			gcwq: newTestGCQueue(),
		}
		stop := startGCWorker(t, m)

		malformedGCRequest := gcRequestOf(malformedName)
		m.gcwq.Add(malformedGCRequest)
		pollUntil(t, "the GC request with a malformed index label is processed", func() bool {
			return malformedGetCnt.Load() >= 1
		})

		m.gcwq.Add(gcRequestOf(primaryName1))
		pollUntil(t, "a later GC request is processed", func() bool {
			return !slices.Contains(remainingSnapshotNames(t, fakeClient), primaryName1)
		})

		// With the backoff of the test workqueue, a retry would have happened long before this.
		time.Sleep(100 * time.Millisecond)
		if got := malformedGetCnt.Load(); got != 1 {
			t.Errorf("GC attempts on the snapshot with a malformed index label = %d, want 1 (no retries)", got)
		}
		if got := m.gcwq.NumRequeues(malformedGCRequest); got != 0 {
			t.Errorf("NumRequeues() = %d, want 0", got)
		}
		if diff := cmp.Diff(remainingSnapshotNames(t, fakeClient), []string{malformedName}); diff != "" {
			t.Errorf("remaining snapshots mismatch (-got +want):\n%s", diff)
		}

		if err := stop(); err != nil {
			t.Errorf("Start() = %v, want no error", err)
		}
	})

	t.Run("canceling the context stops the loop", func(t *testing.T) {
		m := &Manager{hubClient: newFakeHubClientWithIndexes(t), gcwq: newTestGCQueue()}
		stop := startGCWorker(t, m)

		if err := stop(); err != nil {
			t.Errorf("Start() = %v, want no error", err)
		}
		if !m.gcwq.ShuttingDown() {
			t.Errorf("ShuttingDown() = false, want true")
		}
	})
}

// newTestGCQueue returns a GC workqueue with a short backoff, so that retries happen quickly in tests.
func newTestGCQueue() workqueue.TypedRateLimitingInterface[snapshotGarbageCollectionRequest] {
	return workqueue.NewTypedRateLimitingQueue(
		workqueue.NewTypedItemExponentialFailureRateLimiter[snapshotGarbageCollectionRequest](time.Millisecond, 10*time.Millisecond))
}

// startGCWorker runs the GC worker loop of the given manager in the background; the returned function cancels the
// context, waits for the loop to exit, and returns the error that Start returns.
func startGCWorker(t *testing.T, m *Manager) func() error {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- m.Start(ctx)
	}()
	return func() error {
		cancel()
		select {
		case err := <-errCh:
			return err
		case <-time.After(10 * time.Second):
			t.Fatalf("Start() did not return after the context was canceled")
			return nil
		}
	}
}

// pollUntil polls the given condition until it holds; it fails the test if the condition does not hold in time.
func pollUntil(t *testing.T, desc string, cond func() bool) {
	t.Helper()

	err := wait.PollUntilContextTimeout(context.Background(), 5*time.Millisecond, 10*time.Second, true,
		func(context.Context) (bool, error) {
			return cond(), nil
		})
	if err != nil {
		t.Fatalf("Timed out waiting until %s: %v", desc, err)
	}
}

// newFakeHubClientWithIndexes returns a fake client with the custom field indexes that the garbage collection
// process relies on.
func newFakeHubClientWithIndexes(t *testing.T, objs ...client.Object) client.WithWatch {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := placementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}

	// Mirror the field indexes that the hub controller manager sets up.
	bindingExtractor := func(obj client.Object) []string {
		spec := obj.(placementv1alpha1.PlacementBindingAccessor).GetSpec()
		return []string{fmt.Sprintf(fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt,
			spec.PlacementPolicyName, spec.ResourceSnapshotName)}
	}
	snapshotExtractor := func(obj client.Object) []string {
		return []string{fmt.Sprintf(fieldindexers.PlacementResourceSnapshotOwnedByAndIndexedCustomFieldValFmt,
			obj.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey],
			obj.GetLabels()[placementv1alpha1.PlacementResourceSnapshotIndexLabelKey])}
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithIndex(&placementv1alpha1.ClusterPlacementBinding{},
			fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName, bindingExtractor).
		WithIndex(&placementv1alpha1.PlacementBinding{},
			fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName, bindingExtractor).
		WithIndex(&placementv1alpha1.ClusterPlacementResourceSnapshot{},
			fieldindexers.PlacementResourceSnapshotOwnedByAndIndexedCustomFieldName, snapshotExtractor).
		WithIndex(&placementv1alpha1.PlacementResourceSnapshot{},
			fieldindexers.PlacementResourceSnapshotOwnedByAndIndexedCustomFieldName, snapshotExtractor).
		Build()
}

// verifyErrCategory verifies that the given error is a KubeFleet error of the wanted category.
func verifyErrCategory(t *testing.T, funcName string, err error, wantErrCategory kferrors.ErrCategory) {
	t.Helper()

	// The error category is the first key-value pair in the error attributes.
	gotErrAttrs := kferrors.Args(err)
	if len(gotErrAttrs) < 2 {
		t.Fatalf("%s error = %v, want a KubeFleet error", funcName, err)
	}
	wantErrCategoryAttrs := []interface{}{"errCategory", wantErrCategory}
	if diff := cmp.Diff(gotErrAttrs[:2], wantErrCategoryAttrs); diff != "" {
		t.Errorf("%s error category attributes mismatch (-got +want):\n%s\nerror: %v", funcName, diff, err)
	}
}
