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
	"fmt"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

// TestIsSnapshotInUseByAnyPlacementBinding tests the isSnapshotInUseByAnyPlacementBinding method.
func TestIsSnapshotInUseByAnyPlacementBinding(t *testing.T) {
	const (
		policyName        = "policy"
		snapshotName      = "policy-resource-snapshot-0"
		otherSnapshotName = "policy-resource-snapshot-1"
		bindingName       = "binding"
		namespaceName     = "app"
		otherNamespace    = "other-app"
		clusterName       = "member-1"
	)
	policyUID := types.UID("policy-uid")
	recreatedPolicyUID := types.UID("recreated-policy-uid")

	policyOwnerRef := func(kind string, uid types.UID) metav1.OwnerReference {
		return metav1.OwnerReference{
			APIVersion: placementv1alpha1.GroupVersion.String(),
			Kind:       kind,
			Name:       policyName,
			UID:        uid,
		}
	}
	ownedByAnnotations := map[string]string{
		placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey: policyName,
	}
	clusterSnapshot := func(annotations map[string]string, ownerRefs ...metav1.OwnerReference) *placementv1alpha1.ClusterPlacementResourceSnapshot {
		return &placementv1alpha1.ClusterPlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:            snapshotName,
				Annotations:     annotations,
				OwnerReferences: ownerRefs,
			},
		}
	}
	clusterBinding := func(resourceSnapshotName string, ownerRefs ...metav1.OwnerReference) *placementv1alpha1.ClusterPlacementBinding {
		return &placementv1alpha1.ClusterPlacementBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:            bindingName,
				OwnerReferences: ownerRefs,
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
			Annotations:     ownedByAnnotations,
			OwnerReferences: []metav1.OwnerReference{policyOwnerRef(placementv1alpha1.PlacementPolicyKind, policyUID)},
		},
	}
	namespacedBinding := func(namespace string) *placementv1alpha1.PlacementBinding {
		return &placementv1alpha1.PlacementBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:            bindingName,
				Namespace:       namespace,
				OwnerReferences: []metav1.OwnerReference{policyOwnerRef(placementv1alpha1.PlacementPolicyKind, policyUID)},
			},
			Spec: placementv1alpha1.PlacementBindingSpec{
				PlacementPolicyName:  policyName,
				ClusterName:          clusterName,
				ResourceSnapshotName: snapshotName,
			},
		}
	}

	clusterPolicyOwnerRef := policyOwnerRef(placementv1alpha1.ClusterPlacementPolicyKind, policyUID)

	testCases := []struct {
		name     string
		snapshot placementv1alpha1.PlacementResourceSnapshotAccessor
		bindings []client.Object
		want     bool
		// The category of the error that is expected to be returned; leave empty if no error is expected.
		wantErrCategory kferrors.ErrCategory
	}{
		{
			name:     "no placement bindings",
			snapshot: clusterSnapshot(ownedByAnnotations, clusterPolicyOwnerRef),
			want:     false,
		},
		{
			name:     "placement binding using a different snapshot",
			snapshot: clusterSnapshot(ownedByAnnotations, clusterPolicyOwnerRef),
			bindings: []client.Object{clusterBinding(otherSnapshotName, clusterPolicyOwnerRef)},
			want:     false,
		},
		{
			name:     "placement binding using the snapshot, owned by the same placement policy",
			snapshot: clusterSnapshot(ownedByAnnotations, clusterPolicyOwnerRef),
			bindings: []client.Object{clusterBinding(snapshotName, clusterPolicyOwnerRef)},
			want:     true,
		},
		{
			name:     "placement binding using the snapshot, owned by a re-created placement policy with the same name",
			snapshot: clusterSnapshot(ownedByAnnotations, clusterPolicyOwnerRef),
			bindings: []client.Object{
				clusterBinding(snapshotName, policyOwnerRef(placementv1alpha1.ClusterPlacementPolicyKind, recreatedPolicyUID)),
			},
			wantErrCategory: kferrors.ErrCategoryTransient,
		},
		{
			name:            "placement binding without an owner reference to the placement policy",
			snapshot:        clusterSnapshot(ownedByAnnotations, clusterPolicyOwnerRef),
			bindings:        []client.Object{clusterBinding(snapshotName)},
			wantErrCategory: kferrors.ErrCategoryUnexpected,
		},
		{
			name:     "placement binding with an owner reference of a mismatched kind",
			snapshot: clusterSnapshot(ownedByAnnotations, clusterPolicyOwnerRef),
			bindings: []client.Object{
				clusterBinding(snapshotName, policyOwnerRef(placementv1alpha1.PlacementPolicyKind, policyUID)),
			},
			wantErrCategory: kferrors.ErrCategoryUnexpected,
		},
		{
			name:            "snapshot without an owner reference to the placement policy",
			snapshot:        clusterSnapshot(ownedByAnnotations),
			bindings:        []client.Object{clusterBinding(snapshotName, clusterPolicyOwnerRef)},
			wantErrCategory: kferrors.ErrCategoryUnexpected,
		},
		{
			name:            "snapshot without the owned-by annotation",
			snapshot:        clusterSnapshot(nil, clusterPolicyOwnerRef),
			bindings:        []client.Object{clusterBinding(snapshotName, clusterPolicyOwnerRef)},
			wantErrCategory: kferrors.ErrCategoryUnexpected,
		},
		{
			name:     "namespaced placement binding using the snapshot, owned by the same placement policy",
			snapshot: namespacedSnapshot,
			bindings: []client.Object{namespacedBinding(namespaceName)},
			want:     true,
		},
		{
			name:     "namespaced placement binding in a different namespace",
			snapshot: namespacedSnapshot,
			bindings: []client.Object{namespacedBinding(otherNamespace)},
			want:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manager{hubClient: newFakeHubClientWithIndexes(t, tc.bindings...)}

			got, err := m.isSnapshotInUseByAnyPlacementBinding(context.Background(), tc.snapshot)
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
		policyName    = "policy"
		primaryName   = "policy-resource-snapshot-0"
		secondaryName = "policy-resource-snapshot-0-1"
		tertiaryName  = "policy-resource-snapshot-0-2"
	)
	primaryUID := types.UID("primary-uid")
	recreatedPrimaryUID := types.UID("recreated-primary-uid")
	policyOwnerRef := metav1.OwnerReference{
		APIVersion: placementv1alpha1.GroupVersion.String(),
		Kind:       placementv1alpha1.ClusterPlacementPolicyKind,
		Name:       policyName,
		UID:        types.UID("policy-uid"),
	}
	recreatedPolicyOwnerRef := policyOwnerRef
	recreatedPolicyOwnerRef.UID = types.UID("recreated-policy-uid")

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
	gcRequest := snapshotGarbageCollectionRequest{
		snapshotNamespacedName:        types.NamespacedName{Name: primaryName},
		snapshotUID:                   primaryUID,
		placementPolicyNamespacedName: types.NamespacedName{Name: policyName},
	}

	testCases := []struct {
		name      string
		snapshots []client.Object
		// The category of the error that is expected to be returned; leave empty if no error is expected.
		wantErrCategory        kferrors.ErrCategory
		wantRemainingSnapshots []string
	}{
		{
			name: "primary and secondary snapshots owned by the same placement policy",
			snapshots: []client.Object{
				snapshot(primaryName, primaryUID, 0, policyOwnerRef),
				snapshot(secondaryName, "secondary-uid", 1, policyOwnerRef),
				snapshot(tertiaryName, "tertiary-uid", 2, policyOwnerRef),
			},
		},
		{
			name: "secondary snapshot owned by a re-created placement policy with the same name",
			snapshots: []client.Object{
				snapshot(primaryName, primaryUID, 0, policyOwnerRef),
				snapshot(secondaryName, "secondary-uid", 1, recreatedPolicyOwnerRef),
			},
			wantErrCategory:        kferrors.ErrCategoryTransient,
			wantRemainingSnapshots: []string{primaryName, secondaryName},
		},
		{
			name: "secondary snapshot without an owner reference to the placement policy",
			snapshots: []client.Object{
				snapshot(primaryName, primaryUID, 0, policyOwnerRef),
				snapshot(secondaryName, "secondary-uid", 1),
			},
			wantErrCategory:        kferrors.ErrCategoryUnexpected,
			wantRemainingSnapshots: []string{primaryName, secondaryName},
		},
		{
			name: "primary snapshot without an owner reference to the placement policy",
			snapshots: []client.Object{
				snapshot(primaryName, primaryUID, 0),
				snapshot(secondaryName, "secondary-uid", 1, policyOwnerRef),
			},
			wantErrCategory:        kferrors.ErrCategoryUnexpected,
			wantRemainingSnapshots: []string{primaryName, secondaryName},
		},
		{
			name: "primary snapshot has been re-created",
			snapshots: []client.Object{
				snapshot(primaryName, recreatedPrimaryUID, 0, recreatedPolicyOwnerRef),
				snapshot(secondaryName, "secondary-uid", 1, recreatedPolicyOwnerRef),
			},
			wantRemainingSnapshots: []string{primaryName, secondaryName},
		},
		{
			name: "primary snapshot not found",
			snapshots: []client.Object{
				snapshot(secondaryName, "secondary-uid", 1, policyOwnerRef),
			},
			wantRemainingSnapshots: []string{secondaryName},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := newFakeHubClientWithIndexes(t, tc.snapshots...)
			m := &Manager{hubClient: fakeClient}

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

// newFakeHubClientWithIndexes returns a fake client with the custom field indexes that the garbage collection
// process relies on.
func newFakeHubClientWithIndexes(t *testing.T, objs ...client.Object) client.Client {
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
			obj.GetLabels()[placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey],
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
