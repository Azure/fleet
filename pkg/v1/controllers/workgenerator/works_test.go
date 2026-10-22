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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
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
			wantStatus: workSyncStatusOutOfSync,
		},
		{
			name:    "last processed snapshot differs from the primary snapshot",
			binding: newBinding(ptr.To(staleSnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "1", nil),
			},
			wantStatus: workSyncStatusOutOfSync,
		},
		{
			name:       "no work objects",
			binding:    newBinding(ptr.To(primarySnapshotName), nil),
			wantStatus: workSyncStatusOutOfSync,
		},
		{
			name:    "a work object is linked to another snapshot",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "2", nil),
				newWork("work-1", staleSnapshotName, "", nil),
			},
			wantStatus: workSyncStatusOutOfSync,
		},
		{
			name:    "a work object has no link annotation",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "2", nil),
				{ObjectMeta: metav1.ObjectMeta{Name: "work-1"}},
			},
			wantStatus: workSyncStatusOutOfSync,
		},
		{
			name:    "the primary work object is missing (no linked work count)",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-1", primarySnapshotName, "", nil),
			},
			wantStatus: workSyncStatusOutOfSync,
		},
		{
			name:    "fewer work objects than the linked work count",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "2", nil),
			},
			wantStatus: workSyncStatusOutOfSync,
		},
		{
			name:    "more work objects than the linked work count",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "1", nil),
				newWork("work-1", primarySnapshotName, "", nil),
			},
			wantStatus: workSyncStatusOutOfSync,
		},
		{
			name:    "sync strategy is set on the binding but not on the work objects",
			binding: newBinding(ptr.To(primarySnapshotName), serverSideApplyStrategy),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "1", nil),
			},
			wantStatus: workSyncStatusOutOfSync,
		},
		{
			name:    "sync strategy is unset on the binding but set on a secondary work object",
			binding: newBinding(ptr.To(primarySnapshotName), nil),
			works: []placementv1alpha1.Work{
				newWork("work-0", primarySnapshotName, "2", nil),
				newWork("work-1", primarySnapshotName, "", serverSideApplyStrategy),
			},
			wantStatus: workSyncStatusOutOfSync,
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
