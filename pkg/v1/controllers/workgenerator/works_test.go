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

package workgenerator

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "go.goms.io/fleet/pkg/utils/errors"
)

// TestWorkOwnerLabelValue tests the workOwnerLabelValue function.
//
// The hashes below are the first 12 hex characters of the SHA-256 digests of the original (dotted) owner names,
// computed independently of the implementation.
func TestWorkOwnerLabelValue(t *testing.T) {
	dottedOwnerName := dottedName(100, 3)
	// The dot-free form of the name, truncated to 48 characters (61 - 12 - 1).
	wantDottedOwnerNamePrefix := strings.ReplaceAll(dottedOwnerName, ".", "")[:48]

	testCases := []struct {
		name      string
		ownerName string
		want      string
	}{
		{
			name:      "short name (unchanged)",
			ownerName: "binding-1",
			want:      "binding-1",
		},
		{
			name:      "name exactly at the length limit (unchanged)",
			ownerName: strings.Repeat("a", 61),
			want:      strings.Repeat("a", 61),
		},
		{
			name:      "name one character over the length limit (truncated, with hash)",
			ownerName: strings.Repeat("a", 62),
			want:      strings.Repeat("a", 48) + "-f506898cc7c2",
		},
		{
			name:      "short name with dots (dots removed, with hash)",
			ownerName: "my.binding",
			want:      "mybinding-d602fbfb398e",
		},
		{
			name:      "long name with dots (dots removed, truncated, with hash)",
			ownerName: dottedOwnerName,
			want:      wantDottedOwnerNamePrefix + "-f996857ab27a",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := workOwnerLabelValue(tc.ownerName)
			if got != tc.want {
				t.Errorf("workOwnerLabelValue(%q) = %q, want %q", tc.ownerName, got, tc.want)
			}
			if errs := validation.IsValidLabelValue(got); len(errs) > 0 {
				t.Errorf("workOwnerLabelValue(%q) = %q, which is not a valid label value: %v", tc.ownerName, got, errs)
			}
		})
	}
}

// TestWorkOwnerLabelValueDistinct verifies that owner names that reduce to the same prefix still map to different
// label values, as the hash is computed over the original name.
func TestWorkOwnerLabelValueDistinct(t *testing.T) {
	ownerNames := []string{
		"a.bc",
		"ab.c",
		"abc.",
		strings.Repeat("a", 62),
		strings.Repeat("a", 63),
		strings.Repeat("a", 48) + "." + strings.Repeat("a", 20),
	}

	seen := make(map[string]string, len(ownerNames))
	for _, ownerName := range ownerNames {
		got := workOwnerLabelValue(ownerName)
		if got != workOwnerLabelValue(ownerName) {
			t.Errorf("workOwnerLabelValue(%q) is not deterministic", ownerName)
		}
		if other, found := seen[got]; found {
			t.Errorf("workOwnerLabelValue(%q) = %q, which collides with the value for %q", ownerName, got, other)
		}
		seen[got] = ownerName
	}
}

// TestAreWorksInSync tests the areWorksInSync function.
func TestAreWorksInSync(t *testing.T) {
	const (
		primarySnapshotName = "policy-0-snapshot"
		staleSnapshotName   = "policy-0-old-snapshot"
	)
	serverSideApplyStrategy := &placementv1alpha1.SyncStrategy{
		ApplyMethod: placementv1alpha1.ApplyMethodServerSideApply,
	}

	// A work object linked to the given snapshot; the linked work count annotation is set only if the count is
	// not empty (i.e., for the primary work object).
	newWork := func(name, linkedSnapshotName, linkedWorkCount string, syncStrategy *placementv1alpha1.SyncStrategy) placementv1alpha1.Work {
		annotations := map[string]string{
			placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey: linkedSnapshotName,
		}
		if len(linkedWorkCount) > 0 {
			annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = linkedWorkCount
		}
		return placementv1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Annotations: annotations,
			},
			Spec: placementv1alpha1.WorkSpec{
				SyncStrategy: syncStrategy,
			},
		}
	}
	newBinding := func(lastProcessedSnapshotName *string, syncStrategy *placementv1alpha1.SyncStrategy) placementv1alpha1.PlacementBindingAccessor {
		binding := &placementv1alpha1.PlacementBinding{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "app",
				Name:      "binding",
			},
			Spec: placementv1alpha1.PlacementBindingSpec{
				ClusterName:          "member-1",
				ResourceSnapshotName: primarySnapshotName,
				SyncStrategy:         syncStrategy,
			},
		}
		binding.Status.LastProcessedResourceSnapshotName = lastProcessedSnapshotName
		return binding
	}

	testCases := []struct {
		name       string
		binding    placementv1alpha1.PlacementBindingAccessor
		works      []placementv1alpha1.Work
		wantStatus workSyncStatus
		wantErr    bool
	}{
		{
			name:    "last processed snapshot is not set",
			binding: newBinding(nil, nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "1", nil),
			},
			wantStatus: workSyncStatusResourcesOutOfSync,
		},
		{
			name:    "last processed snapshot differs from the primary snapshot",
			binding: newBinding(ptr.To(staleSnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "1", nil),
			},
			wantStatus: workSyncStatusResourcesOutOfSync,
		},
		{
			name:       "no work objects",
			binding:    newBinding(ptr.To(primarySnapshotName), nil),
			wantStatus: workSyncStatusResourcesOutOfSync,
		},
		{
			name:    "a work object is linked to another snapshot",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "2", nil),
				newWork("work-1", staleSnapshotName, "", nil),
			},
			wantStatus: workSyncStatusResourcesOutOfSync,
		},
		{
			name:    "a work object has no link annotation",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "2", nil),
				{ObjectMeta: metav1.ObjectMeta{Name: "work-1"}},
			},
			wantStatus: workSyncStatusResourcesOutOfSync,
		},
		{
			name:    "the primary work object is missing (no linked work count)",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-1", primarySnapshotName, "", nil),
			},
			wantStatus: workSyncStatusResourcesOutOfSync,
		},
		{
			name:    "fewer work objects than the linked work count",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "2", nil),
			},
			wantStatus: workSyncStatusResourcesOutOfSync,
		},
		{
			name:    "more work objects than the linked work count",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "1", nil),
				newWork("work-1", primarySnapshotName, "", nil),
			},
			wantStatus: workSyncStatusResourcesOutOfSync,
		},
		{
			name:    "sync strategy is set on the binding but not on the work objects",
			binding: newBinding(ptr.To(primarySnapshotName), serverSideApplyStrategy),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "1", nil),
			},
			wantStatus: workSyncStatusStrategyOutOfSync,
		},
		{
			name:    "sync strategy is unset on the binding but set on a secondary work object",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "2", nil),
				newWork("work-1", primarySnapshotName, "", serverSideApplyStrategy),
			},
			wantStatus: workSyncStatusStrategyOutOfSync,
		},
		{
			name:    "multiple work objects have the linked work count annotation",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "2", nil),
				newWork("work-1", primarySnapshotName, "2", nil),
			},
			wantStatus: workSyncStatusErred,
			wantErr:    true,
		},
		{
			name:    "linked work count annotation is not a number",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "two", nil),
			},
			wantStatus: workSyncStatusErred,
			wantErr:    true,
		},
		{
			name:    "linked work count annotation is not positive",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "0", nil),
			},
			wantStatus: workSyncStatusErred,
			wantErr:    true,
		},
		{
			name:    "in sync (single work object)",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "1", nil),
			},
			wantStatus: workSyncStatusInSync,
		},
		{
			name:    "in sync (multiple work objects, with sync strategy)",
			binding: newBinding(ptr.To(primarySnapshotName), serverSideApplyStrategy),
			works: []placementv1alpha1.Work{
				newWork("work-1", primarySnapshotName, "", serverSideApplyStrategy.DeepCopy()),
				newWork("work-0", primarySnapshotName, "2", serverSideApplyStrategy.DeepCopy()),
			},
			wantStatus: workSyncStatusInSync,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, err := areWorksInSync(tc.binding, tc.works)
			if (err != nil) != tc.wantErr {
				t.Fatalf("areWorksInSync() = (%q, %v), want error: %t", gotStatus, err, tc.wantErr)
			}
			if gotStatus != tc.wantStatus {
				t.Errorf("areWorksInSync() = %q, want %q", gotStatus, tc.wantStatus)
			}
		})
	}
}

// configMapManifestForUnitTest returns the manifest of a ConfigMap of the given name.
func configMapManifestForUnitTest(name string) runtime.RawExtension {
	return runtime.RawExtension{Raw: fmt.Appendf(nil, `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q,"namespace":%q}}`, name, unitTestResourceNS)}
}

// configMapResourcesForUnitTest returns the snapshotted resources for ConfigMaps of the given names, and the
// manifests that the work generator is expected to build from them.
func configMapResourcesForUnitTest(names ...string) ([]placementv1alpha1.SnapshottedResource, []placementv1alpha1.Manifest) {
	resources := make([]placementv1alpha1.SnapshottedResource, len(names))
	manifests := make([]placementv1alpha1.Manifest, len(names))
	for idx, name := range names {
		resources[idx] = placementv1alpha1.SnapshottedResource{
			Identifier: placementv1alpha1.ObjectReference{Namespace: unitTestResourceNS, Name: name, APIVersion: "v1", Kind: configMapKind},
			Manifest:   configMapManifestForUnitTest(name),
		}
		manifests[idx] = placementv1alpha1.Manifest{RawExtension: configMapManifestForUnitTest(name)}
	}
	return resources, manifests
}

// TestBuildWorkObjectFor tests the buildWorkObjectFor function.
func TestBuildWorkObjectFor(t *testing.T) {
	serverSideApplyStrategy := &placementv1alpha1.SyncStrategy{
		ApplyMethod: placementv1alpha1.ApplyMethodServerSideApply,
	}
	newSnapshot := func(subIdx string, resources []placementv1alpha1.SnapshottedResource) placementv1alpha1.PlacementResourceSnapshotAccessor {
		labels := map[string]string{}
		if subIdx != "" {
			labels[placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey] = subIdx
		}
		return &placementv1alpha1.PlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{Namespace: appNamespaceName, Name: "snapshot", Labels: labels},
			Spec:       placementv1alpha1.PlacementResourceSnapshotSpec{Resources: resources},
		}
	}

	// A namespace-scoped binding with short names and a sync strategy.
	nsBinding := &placementv1alpha1.PlacementBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: appNamespaceName, Name: unitTestBindingName},
		Spec: placementv1alpha1.PlacementBindingSpec{
			PlacementPolicyName:  unitTestPolicyName,
			ClusterName:          memberCluster1Name,
			ResourceSnapshotName: unitTestSnapshotName1,
			SyncStrategy:         serverSideApplyStrategy,
		},
	}
	// A cluster-scoped binding with long, dotted names and no sync strategy.
	longBindingName := dottedName(100, 3)
	longPolicyName := dottedName(120, 5)
	clusterBinding := &placementv1alpha1.ClusterPlacementBinding{
		ObjectMeta: metav1.ObjectMeta{Name: longBindingName},
		Spec: placementv1alpha1.PlacementBindingSpec{
			PlacementPolicyName:  longPolicyName,
			ClusterName:          memberCluster2Name,
			ResourceSnapshotName: unitTestSnapshotName1,
		},
	}
	primaryResources, primaryManifests := configMapResourcesForUnitTest("configmap-a", "configmap-b")
	secondaryResources, secondaryManifests := configMapResourcesForUnitTest("configmap-c")

	testCases := []struct {
		name     string
		snapshot placementv1alpha1.PlacementResourceSnapshotAccessor
		binding  placementv1alpha1.PlacementBindingAccessor
		want     *placementv1alpha1.Work
		wantErr  bool
	}{
		{
			name:     "primary snapshot, namespace-scoped binding with a sync strategy",
			snapshot: newSnapshot("0", primaryResources),
			binding:  nsBinding,
			want: &placementv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: fmt.Sprintf("fleet-member-%s", memberCluster1Name),
					Name: uniqueNameForWorkDerivedFromPlacementResourceSnapshot(nsBinding, true,
						&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "0"}),
					Labels: map[string]string{
						placementv1alpha1.WorkOwnerNamespaceLabelKey:          appNamespaceName,
						placementv1alpha1.WorkOwnedByPlacementPolicyLabelKey:  unitTestPolicyName,
						placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: unitTestBindingName,
					},
					Annotations: map[string]string{
						placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey: unitTestSnapshotName1,
						placementv1alpha1.WorkDerivedFromSourceAnnotationKey:                        "placement-resource-snapshot/0",
						placementv1alpha1.WorkOwnedByPlacementPolicyAnnotationKey:                   unitTestPolicyName,
						placementv1alpha1.WorkOwnedByPlacementBindingAnnotationKey:                  unitTestBindingName,
					},
				},
				Spec: placementv1alpha1.WorkSpec{
					Manifests:    primaryManifests,
					SyncStrategy: serverSideApplyStrategy.DeepCopy(),
				},
			},
		},
		{
			name:     "secondary snapshot, cluster-scoped binding with long dotted names",
			snapshot: newSnapshot("2", secondaryResources),
			binding:  clusterBinding,
			want: &placementv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: fmt.Sprintf("fleet-member-%s", memberCluster2Name),
					Name: uniqueNameForWorkDerivedFromPlacementResourceSnapshot(clusterBinding, false,
						&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "2"}),
					Labels: map[string]string{
						placementv1alpha1.WorkOwnerNamespaceLabelKey:          "",
						placementv1alpha1.WorkOwnedByPlacementPolicyLabelKey:  workOwnerLabelValue(longPolicyName),
						placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: workOwnerLabelValue(longBindingName),
					},
					// The annotations carry the full names.
					Annotations: map[string]string{
						placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey: unitTestSnapshotName1,
						placementv1alpha1.WorkDerivedFromSourceAnnotationKey:                        "placement-resource-snapshot/2",
						placementv1alpha1.WorkOwnedByPlacementPolicyAnnotationKey:                   longPolicyName,
						placementv1alpha1.WorkOwnedByPlacementBindingAnnotationKey:                  longBindingName,
					},
				},
				Spec: placementv1alpha1.WorkSpec{
					Manifests: secondaryManifests,
				},
			},
		},
		{
			name:     "snapshot without a sub-index label",
			snapshot: newSnapshot("", primaryResources),
			binding:  nsBinding,
			wantErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildWorkObjectFor(tc.snapshot, tc.binding, unitTestSnapshotName1)
			if (err != nil) != tc.wantErr {
				t.Fatalf("buildWorkObjectFor() = (%v, %v), want error: %t", got, err, tc.wantErr)
			}
			if err != nil {
				if kferrors.Category(err) != kferrors.ErrCategoryUnexpected {
					t.Errorf("buildWorkObjectFor() error category = %q, want %q", kferrors.Category(err), kferrors.ErrCategoryUnexpected)
				}
				return
			}
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("buildWorkObjectFor() mismatch (-got +want):\n%s", diff)
			}
			if got.Spec.SyncStrategy != nil && got.Spec.SyncStrategy == tc.binding.GetSpec().SyncStrategy {
				t.Errorf("buildWorkObjectFor() shares the sync strategy with the placement binding, want a copy")
			}
		})
	}
}

// TestUpdateWorkObjectMetadataAndSpec tests the updateWorkObjectMetadataAndSpec function.
func TestUpdateWorkObjectMetadataAndSpec(t *testing.T) {
	serverSideApplyStrategy := &placementv1alpha1.SyncStrategy{
		ApplyMethod: placementv1alpha1.ApplyMethodServerSideApply,
	}
	_, oldManifests := configMapResourcesForUnitTest("configmap-old-a", "configmap-old-b")
	newResources, newManifests := configMapResourcesForUnitTest("configmap-new")
	workNS := fmt.Sprintf("fleet-member-%s", memberCluster1Name)
	const (
		workName          = "work"
		wantDerivedFromID = "placement-resource-snapshot/1"
	)

	testCases := []struct {
		name         string
		work         *placementv1alpha1.Work
		syncStrategy *placementv1alpha1.SyncStrategy
		want         *placementv1alpha1.Work
	}{
		{
			name:         "work object without metadata or spec",
			work:         &placementv1alpha1.Work{ObjectMeta: metav1.ObjectMeta{Namespace: workNS, Name: workName}},
			syncStrategy: serverSideApplyStrategy,
			want: &placementv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: workNS,
					Name:      workName,
					Labels: map[string]string{
						placementv1alpha1.WorkOwnerNamespaceLabelKey:          appNamespaceName,
						placementv1alpha1.WorkOwnedByPlacementPolicyLabelKey:  unitTestPolicyName,
						placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: unitTestBindingName,
					},
					Annotations: map[string]string{
						placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey: unitTestSnapshotName1,
						placementv1alpha1.WorkDerivedFromSourceAnnotationKey:                        wantDerivedFromID,
						placementv1alpha1.WorkOwnedByPlacementPolicyAnnotationKey:                   unitTestPolicyName,
						placementv1alpha1.WorkOwnedByPlacementBindingAnnotationKey:                  unitTestBindingName,
					},
				},
				Spec: placementv1alpha1.WorkSpec{
					Manifests:    newManifests,
					SyncStrategy: serverSideApplyStrategy,
				},
			},
		},
		{
			name: "work object with existing metadata and spec",
			work: &placementv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: workNS,
					Name:      workName,
					Labels: map[string]string{
						placementv1alpha1.WorkOwnerNamespaceLabelKey:          appNamespaceName,
						placementv1alpha1.WorkOwnedByPlacementPolicyLabelKey:  unitTestPolicyName,
						placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: unitTestBindingName,
						"unrelated-label": "foo",
					},
					Annotations: map[string]string{
						placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey: unitTestSnapshotName0,
						placementv1alpha1.WorkDerivedFromSourceAnnotationKey:                        wantDerivedFromID,
						placementv1alpha1.WorkOwnedByPlacementPolicyAnnotationKey:                   unitTestPolicyName,
						placementv1alpha1.WorkOwnedByPlacementBindingAnnotationKey:                  unitTestBindingName,
						placementv1alpha1.LinkedWorkCountAnnotationKey:                              "2",
					},
				},
				Spec: placementv1alpha1.WorkSpec{
					Manifests:    oldManifests,
					SyncStrategy: serverSideApplyStrategy,
				},
			},
			// The owned labels and annotations, the manifests, and the sync strategy are overwritten (the sync
			// strategy is unset); the other labels and annotations are kept.
			want: &placementv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: workNS,
					Name:      workName,
					Labels: map[string]string{
						placementv1alpha1.WorkOwnerNamespaceLabelKey:          appNamespaceName,
						placementv1alpha1.WorkOwnedByPlacementPolicyLabelKey:  unitTestPolicyName,
						placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: unitTestBindingName,
						"unrelated-label": "foo",
					},
					Annotations: map[string]string{
						placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey: unitTestSnapshotName1,
						placementv1alpha1.WorkDerivedFromSourceAnnotationKey:                        wantDerivedFromID,
						placementv1alpha1.WorkOwnedByPlacementPolicyAnnotationKey:                   unitTestPolicyName,
						placementv1alpha1.WorkOwnedByPlacementBindingAnnotationKey:                  unitTestBindingName,
						placementv1alpha1.LinkedWorkCountAnnotationKey:                              "2",
					},
				},
				Spec: placementv1alpha1.WorkSpec{
					Manifests: newManifests,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			work := tc.work.DeepCopy()
			updateWorkObjectMetadataAndSpec(work, appNamespaceName, unitTestPolicyName, unitTestBindingName, unitTestSnapshotName1,
				&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "1"}, newResources, tc.syncStrategy)
			if diff := cmp.Diff(work, tc.want); diff != "" {
				t.Errorf("updateWorkObjectMetadataAndSpec() mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// TestRefreshSyncStrategyOnlyForWorks tests the refreshSyncStrategyOnlyForWorks method.
func TestRefreshSyncStrategyOnlyForWorks(t *testing.T) {
	serverSideApplyStrategy := &placementv1alpha1.SyncStrategy{
		ApplyMethod: placementv1alpha1.ApplyMethodServerSideApply,
	}
	_, manifests := configMapResourcesForUnitTest("configmap-a")
	const (
		workName0 = "work-0"
		workName1 = "work-1"
		workName2 = "work-2"
	)
	newStoredWork := func(name string, syncStrategy *placementv1alpha1.SyncStrategy) *placementv1alpha1.Work {
		return &placementv1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: fmt.Sprintf("fleet-member-%s", memberCluster1Name),
				Name:      name,
				Annotations: map[string]string{
					placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey: unitTestSnapshotName0,
				},
			},
			Spec: placementv1alpha1.WorkSpec{
				Manifests:    manifests,
				SyncStrategy: syncStrategy.DeepCopy(),
			},
		}
	}

	testCases := []struct {
		name            string
		bindingStrategy *placementv1alpha1.SyncStrategy
		storedWorks     []*placementv1alpha1.Work
		// The name of a work object that is updated out of band after it has been read, i.e., the copy passed to
		// the method is stale.
		staleWorkName    string
		wantPatchedNames []string
		// The sync strategies of the work objects as stored afterwards, keyed by work name.
		wantStrategies map[string]*placementv1alpha1.SyncStrategy
		wantConflict   bool
	}{
		{
			name:            "set a sync strategy",
			bindingStrategy: serverSideApplyStrategy,
			storedWorks: []*placementv1alpha1.Work{
				newStoredWork(workName0, nil),
				newStoredWork(workName1, nil),
				newStoredWork(workName2, serverSideApplyStrategy),
			},
			wantPatchedNames: []string{workName0, workName1},
			wantStrategies: map[string]*placementv1alpha1.SyncStrategy{
				workName0: serverSideApplyStrategy,
				workName1: serverSideApplyStrategy,
				workName2: serverSideApplyStrategy,
			},
		},
		{
			name: "unset the sync strategy",
			storedWorks: []*placementv1alpha1.Work{
				newStoredWork(workName0, serverSideApplyStrategy),
				newStoredWork(workName1, nil),
			},
			wantPatchedNames: []string{workName0},
			wantStrategies: map[string]*placementv1alpha1.SyncStrategy{
				workName0: nil,
				workName1: nil,
			},
		},
		{
			name:            "all work objects in sync",
			bindingStrategy: serverSideApplyStrategy,
			storedWorks: []*placementv1alpha1.Work{
				newStoredWork(workName0, serverSideApplyStrategy),
			},
			wantStrategies: map[string]*placementv1alpha1.SyncStrategy{
				workName0: serverSideApplyStrategy,
			},
		},
		{
			name:            "a work object has been updated since it was read",
			bindingStrategy: serverSideApplyStrategy,
			storedWorks: []*placementv1alpha1.Work{
				newStoredWork(workName0, nil),
			},
			staleWorkName:    workName0,
			wantPatchedNames: []string{workName0},
			wantStrategies: map[string]*placementv1alpha1.SyncStrategy{
				workName0: nil,
			},
			wantConflict: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			var mu sync.Mutex
			var gotPatchedNames []string
			objs := make([]client.Object, len(tc.storedWorks))
			for idx := range tc.storedWorks {
				objs[idx] = tc.storedWorks[idx]
			}
			c := newUnitTestHubClient(t, interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					mu.Lock()
					gotPatchedNames = append(gotPatchedNames, obj.GetName())
					mu.Unlock()
					return c.Patch(ctx, obj, patch, opts...)
				},
			}, objs...)

			// Read the work objects, as the work generator would do.
			workList := &placementv1alpha1.WorkList{}
			if err := c.List(ctx, workList); err != nil {
				t.Fatalf("List() = %v, want no error", err)
			}
			if tc.staleWorkName != "" {
				work := &placementv1alpha1.Work{}
				if err := c.Get(ctx, client.ObjectKey{Namespace: fmt.Sprintf("fleet-member-%s", memberCluster1Name), Name: tc.staleWorkName}, work); err != nil {
					t.Fatalf("Get() = %v, want no error", err)
				}
				work.Labels = map[string]string{"updated-out-of-band": "true"}
				if err := c.Update(ctx, work); err != nil {
					t.Fatalf("Update() = %v, want no error", err)
				}
			}

			binding := newBindingWithStatus(1, unitTestSnapshotName0, placementv1alpha1.PlacementBindingStatus{})
			binding.Spec.SyncStrategy = tc.bindingStrategy.DeepCopy()
			r := New(c, 2, nil)
			err := r.refreshSyncStrategyOnlyForWorks(ctx, binding, workList.Items)
			if (err != nil) != tc.wantConflict {
				t.Fatalf("refreshSyncStrategyOnlyForWorks() = %v, want error: %t", err, tc.wantConflict)
			}
			if err != nil {
				if !apierrors.IsConflict(err) {
					t.Errorf("refreshSyncStrategyOnlyForWorks() = %v, want a conflict error", err)
				}
				if kferrors.Category(err) != kferrors.ErrCategoryAPIServer {
					t.Errorf("refreshSyncStrategyOnlyForWorks() error category = %q, want %q", kferrors.Category(err), kferrors.ErrCategoryAPIServer)
				}
			}

			slices.Sort(gotPatchedNames)
			if diff := cmp.Diff(gotPatchedNames, tc.wantPatchedNames, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("refreshSyncStrategyOnlyForWorks() patched work objects mismatch (-got +want):\n%s", diff)
			}

			// Only the sync strategies should have changed.
			gotWorkList := &placementv1alpha1.WorkList{}
			if err := c.List(ctx, gotWorkList); err != nil {
				t.Fatalf("List() = %v, want no error", err)
			}
			if len(gotWorkList.Items) != len(tc.storedWorks) {
				t.Fatalf("List() returned %d work objects, want %d", len(gotWorkList.Items), len(tc.storedWorks))
			}
			for idx := range gotWorkList.Items {
				gotWork := &gotWorkList.Items[idx]
				wantSpec := newStoredWork(gotWork.Name, tc.wantStrategies[gotWork.Name]).Spec
				if diff := cmp.Diff(gotWork.Spec, wantSpec); diff != "" {
					t.Errorf("work object %s spec mismatch (-got +want):\n%s", gotWork.Name, diff)
				}
				wantAnnotations := newStoredWork(gotWork.Name, nil).Annotations
				if diff := cmp.Diff(gotWork.Annotations, wantAnnotations); diff != "" {
					t.Errorf("work object %s annotations mismatch (-got +want):\n%s", gotWork.Name, diff)
				}
			}
		})
	}
}
