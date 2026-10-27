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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "go.goms.io/fleet/pkg/utils/errors"
)

const (
	unitTestBindingName   = "binding"
	unitTestPolicyName    = "policy"
	unitTestResourceNS    = "resource-ns"
	unitTestSnapshotName0 = "policy-snapshot-0"
	unitTestSnapshotName1 = "policy-snapshot-1"
)

var (
	// A fixed last transition time for the conditions that the tests build, so that conditions copied as they are by
	// the work generator (e.g., into failed resources) compare as equal.
	unitTestLTT = metav1.NewTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))

	// The work generator sets the last transition times of placement binding conditions to the current time.
	ignoreConditionLTT = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")

	// Common work-level and manifest-level conditions.
	manifestAppliedCond      = newCondition(placementv1alpha1.ManifestCondTypeApplied, metav1.ConditionTrue, 1, "Applied", "applied")
	manifestNotAppliedCond   = newCondition(placementv1alpha1.ManifestCondTypeApplied, metav1.ConditionFalse, 1, "FailedToApply", "failed to apply")
	manifestAvailableCond    = newCondition(placementv1alpha1.ManifestCondTypeAvailable, metav1.ConditionTrue, 1, "Available", "available")
	manifestNotAvailableCond = newCondition(placementv1alpha1.ManifestCondTypeAvailable, metav1.ConditionFalse, 1, "NotAvailable", "not available")
)

// newCondition returns a condition with the fixed last transition time.
func newCondition(condType string, status metav1.ConditionStatus, observedGeneration int64, reason, message string) metav1.Condition {
	return metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: observedGeneration,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: unitTestLTT,
	}
}

// newManifestStatus returns the status of a manifest (a ConfigMap of the given name) with the given conditions.
func newManifestStatus(ordinal int, configMapName string, conds ...metav1.Condition) placementv1alpha1.PerManifestStatus {
	return placementv1alpha1.PerManifestStatus{
		Identifier: configMapManifestIdentifier(ordinal, unitTestResourceNS, configMapName),
		Conditions: conds,
	}
}

// newWorkWithStatus returns a work object of the given generation, linked to the given primary snapshot, with one
// manifest per manifest status and the given work-level conditions.
func newWorkWithStatus(
	name, linkedSnapshotName string,
	generation int64,
	workConds []metav1.Condition,
	manifestStatuses ...placementv1alpha1.PerManifestStatus,
) placementv1alpha1.Work {
	return placementv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  fmt.Sprintf("fleet-member-%s", memberCluster1Name),
			Name:       name,
			Generation: generation,
			Annotations: map[string]string{
				placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey: linkedSnapshotName,
			},
		},
		Spec: placementv1alpha1.WorkSpec{
			Manifests: make([]placementv1alpha1.Manifest, len(manifestStatuses)),
		},
		Status: placementv1alpha1.WorkStatus{
			Conditions: workConds,
			Manifests:  manifestStatuses,
		},
	}
}

// workConds returns the work-level Applied and Available conditions (each omitted if its status is empty) observing
// the given generation.
func workConds(generation int64, applied, available metav1.ConditionStatus) []metav1.Condition {
	var conds []metav1.Condition
	if applied != "" {
		conds = append(conds, newCondition(placementv1alpha1.WorkCondTypeApplied, applied, generation, "WorkApplied", "work applied"))
	}
	if available != "" {
		conds = append(conds, newCondition(placementv1alpha1.WorkCondTypeAvailable, available, generation, "WorkAvailable", "work available"))
	}
	return conds
}

// newFailedResource returns the failed resource that the work generator is expected to report for a manifest status
// and its false condition.
func newFailedResource(manifestStatus placementv1alpha1.PerManifestStatus, falseCond metav1.Condition) placementv1alpha1.FailedResource {
	return placementv1alpha1.FailedResource{
		ObjectRef:   objectRefFromManifestIdentifier(manifestStatus.Identifier),
		Conditions:  []metav1.Condition{falseCond},
		DiffDetails: manifestStatus.DiffDetails,
	}
}

// newBindingWithStatus returns a namespace-scoped placement binding of the given generation, bound to the given
// primary snapshot, with the given status.
func newBindingWithStatus(generation int64, snapshotName string, status placementv1alpha1.PlacementBindingStatus) *placementv1alpha1.PlacementBinding {
	return &placementv1alpha1.PlacementBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  appNamespaceName,
			Name:       unitTestBindingName,
			Generation: generation,
		},
		Spec: placementv1alpha1.PlacementBindingSpec{
			PlacementPolicyName:  unitTestPolicyName,
			ClusterName:          memberCluster1Name,
			ResourceSnapshotName: snapshotName,
		},
		Status: status,
	}
}

// newUnitTestHubClient returns a fake hub client holding the given objects, with the given interceptor functions.
func newUnitTestHubClient(t *testing.T, funcs interceptor.Funcs, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := placementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&placementv1alpha1.PlacementBinding{}, &placementv1alpha1.ClusterPlacementBinding{}, &placementv1alpha1.Work{}).
		WithInterceptorFuncs(funcs).
		Build()
}

// countStatusUpdates returns interceptor functions that count the status updates, and fail them with updateErr if it
// is set.
func countStatusUpdates(count *int, updateErr error) interceptor.Funcs {
	return interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			*count++
			if updateErr != nil {
				return updateErr
			}
			return c.SubResource(subResourceName).Update(ctx, obj, opts...)
		},
	}
}

// getUnitTestBinding retrieves the placement binding from the fake hub client.
func getUnitTestBinding(t *testing.T, c client.Client) *placementv1alpha1.PlacementBinding {
	t.Helper()
	binding := &placementv1alpha1.PlacementBinding{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: appNamespaceName, Name: unitTestBindingName}, binding); err != nil {
		t.Fatalf("Get() = %v, want no error", err)
	}
	return binding
}

// TestCountResourcesInWorksByProcessingResults tests the countResourcesInWorksByProcessingResults function.
func TestCountResourcesInWorksByProcessingResults(t *testing.T) {
	diffDetails := &placementv1alpha1.DiffDetails{
		ObservedInMemberClusterGeneration: ptr.To(int64(2)),
		FirstDiffedObservedTimestamp:      unitTestLTT,
		ObservedDiffs: []placementv1alpha1.PatchDetail{
			{Path: "/data/foo", ValueInMember: "baz", ValueInHub: "bar"},
		},
	}
	notAppliedWithDiffs := newManifestStatus(1, "configmap-b", manifestNotAppliedCond)
	notAppliedWithDiffs.DiffDetails = diffDetails
	notAvailable := newManifestStatus(0, "configmap-a", manifestAppliedCond, manifestNotAvailableCond)
	// A ConfigMap in another namespace that sorts before configmap-a by namespace.
	notAvailableInOtherNS := placementv1alpha1.PerManifestStatus{
		Identifier: configMapManifestIdentifier(0, "another-ns", "configmap-z"),
		Conditions: []metav1.Condition{manifestAppliedCond, manifestNotAvailableCond},
	}
	// A Deployment that sorts before the ConfigMaps by API group.
	notAvailableDeploy := placementv1alpha1.PerManifestStatus{
		Identifier: deployManifestIdentifier(2, unitTestResourceNS, "deploy-a"),
		Conditions: []metav1.Condition{manifestAppliedCond, manifestNotAvailableCond},
	}

	testCases := []struct {
		name          string
		works         []placementv1alpha1.Work
		wantTotal     int32
		wantSynced    int32
		wantAvailable int32
		wantFailed    []placementv1alpha1.FailedResource
	}{
		{
			name: "no work objects",
		},
		{
			name: "manifests not yet processed",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 1, nil,
					newManifestStatus(0, "configmap-a"),
					newManifestStatus(1, "configmap-b"),
				),
			},
			wantTotal: 2,
		},
		{
			name: "manifests applied, with and without completed availability checks",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 1, nil,
					newManifestStatus(0, "configmap-a", manifestAppliedCond, manifestAvailableCond),
					// The availability check has not completed yet; the manifest is not considered failed.
					newManifestStatus(1, "configmap-b", manifestAppliedCond),
				),
			},
			wantTotal:     2,
			wantSynced:    2,
			wantAvailable: 1,
		},
		{
			name: "manifests failed to be applied or to become available",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 1, nil,
					notAppliedWithDiffs,
					notAvailable,
					// An Available condition left on a manifest that has failed to be applied is ignored.
					newManifestStatus(2, "configmap-c", manifestNotAppliedCond, manifestAvailableCond),
				),
			},
			wantTotal: 3,
			// configmap-a has been applied.
			wantSynced: 1,
			wantFailed: []placementv1alpha1.FailedResource{
				newFailedResource(notAvailable, manifestNotAvailableCond),
				newFailedResource(notAppliedWithDiffs, manifestNotAppliedCond),
				newFailedResource(newManifestStatus(2, "configmap-c"), manifestNotAppliedCond),
			},
		},
		{
			name: "failed resources across work objects, sorted by group, version, kind, namespace, and name",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 1, nil,
					notAvailable,
					newManifestStatus(1, "configmap-ok", manifestAppliedCond, manifestAvailableCond),
				),
				newWorkWithStatus("work-1", unitTestSnapshotName0, 1, nil,
					notAvailableDeploy,
					notAvailableInOtherNS,
				),
			},
			wantTotal:     4,
			wantSynced:    4,
			wantAvailable: 1,
			wantFailed: []placementv1alpha1.FailedResource{
				newFailedResource(notAvailableInOtherNS, manifestNotAvailableCond),
				newFailedResource(notAvailable, manifestNotAvailableCond),
				newFailedResource(notAvailableDeploy, manifestNotAvailableCond),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotTotal, gotSynced, gotAvailable, gotFailed := countResourcesInWorksByProcessingResults(tc.works)
			if gotTotal != tc.wantTotal || gotSynced != tc.wantSynced || gotAvailable != tc.wantAvailable {
				t.Errorf("countResourcesInWorksByProcessingResults() counters = (%d, %d, %d), want (%d, %d, %d)",
					gotTotal, gotSynced, gotAvailable, tc.wantTotal, tc.wantSynced, tc.wantAvailable)
			}
			if diff := cmp.Diff(gotFailed, tc.wantFailed); diff != "" {
				t.Errorf("countResourcesInWorksByProcessingResults() failed resources mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// TestFailedResourceFromManifestStatus tests the failedResourceFromManifestStatus function.
func TestFailedResourceFromManifestStatus(t *testing.T) {
	diffDetails := &placementv1alpha1.DiffDetails{
		ObservedInMemberClusterGeneration: ptr.To(int64(1)),
		FirstDiffedObservedTimestamp:      unitTestLTT,
	}
	manifestStatus := newManifestStatus(3, "configmap-a", manifestNotAppliedCond)
	manifestStatus.DiffDetails = diffDetails

	testCases := []struct {
		name      string
		falseCond *metav1.Condition
		want      placementv1alpha1.FailedResource
	}{
		{
			name:      "with the false condition",
			falseCond: &manifestNotAppliedCond,
			want: placementv1alpha1.FailedResource{
				ObjectRef: placementv1alpha1.ObjectReference{
					Namespace:  unitTestResourceNS,
					Name:       "configmap-a",
					APIVersion: "v1",
					Kind:       configMapKind,
				},
				Conditions:  []metav1.Condition{manifestNotAppliedCond},
				DiffDetails: diffDetails,
			},
		},
		{
			name: "without the false condition",
			want: placementv1alpha1.FailedResource{
				ObjectRef: placementv1alpha1.ObjectReference{
					Namespace:  unitTestResourceNS,
					Name:       "configmap-a",
					APIVersion: "v1",
					Kind:       configMapKind,
				},
				DiffDetails: diffDetails,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := failedResourceFromManifestStatus(&manifestStatus, tc.falseCond)
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("failedResourceFromManifestStatus() mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// TestRefreshPlacementBindingSyncCond tests the refreshPlacementBindingSyncCond function.
func TestRefreshPlacementBindingSyncCond(t *testing.T) {
	existingConds := []metav1.Condition{
		newCondition(placementv1alpha1.PlacementBindingCondTypeSynchronized, metav1.ConditionFalse, 2,
			placementv1alpha1.PlacementBindingSynchronizedCondReasonWaitingForSynchronization, "waiting"),
	}

	testCases := []struct {
		name         string
		works        []placementv1alpha1.Work
		wantNotReady bool
		wantConds    []metav1.Condition
	}{
		{
			name: "a work object has not been applied yet",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, "")),
				newWorkWithStatus("work-1", unitTestSnapshotName0, 1, nil),
			},
			wantNotReady: true,
			wantConds:    existingConds,
		},
		{
			name: "a work object has a stale Applied condition",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 2, workConds(1, metav1.ConditionTrue, "")),
			},
			wantNotReady: true,
			wantConds:    existingConds,
		},
		{
			name: "all work objects have been applied",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, "")),
				newWorkWithStatus("work-1", unitTestSnapshotName0, 3, workConds(3, metav1.ConditionTrue, "")),
			},
			wantConds: []metav1.Condition{
				newCondition(placementv1alpha1.PlacementBindingCondTypeSynchronized, metav1.ConditionTrue, 2,
					placementv1alpha1.PlacementBindingSynchronizedCondReasonAllResourcesSynchronized,
					"All resources have been synchronized to the target cluster"),
			},
		},
		{
			name: "a work object has failed to be applied",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, "")),
				newWorkWithStatus("work-1", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionFalse, "")),
			},
			wantConds: []metav1.Condition{
				newCondition(placementv1alpha1.PlacementBindingCondTypeSynchronized, metav1.ConditionFalse, 2,
					placementv1alpha1.PlacementBindingSynchronizedCondReasonFailedToSynchronizeSomeResources,
					"Some resources might be out of sync in the target cluster"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			binding := newBindingWithStatus(2, unitTestSnapshotName0, placementv1alpha1.PlacementBindingStatus{
				Conditions: append([]metav1.Condition{}, existingConds...),
			})
			gotNotReady := refreshPlacementBindingSyncCond(binding, tc.works)
			if gotNotReady != tc.wantNotReady {
				t.Errorf("refreshPlacementBindingSyncCond() = %t, want %t", gotNotReady, tc.wantNotReady)
			}
			if diff := cmp.Diff(binding.Status.Conditions, tc.wantConds, ignoreConditionLTT); diff != "" {
				t.Errorf("refreshPlacementBindingSyncCond() conditions mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// TestRefreshPlacementBindingAvailableCond tests the refreshPlacementBindingAvailableCond function.
func TestRefreshPlacementBindingAvailableCond(t *testing.T) {
	existingConds := []metav1.Condition{
		newCondition(placementv1alpha1.PlacementBindingCondTypeAvailable, metav1.ConditionUnknown, 2,
			placementv1alpha1.PlacementBindingAvailableCondReasonWaitingForAvailabilityCheck, "waiting"),
	}

	testCases := []struct {
		name         string
		works        []placementv1alpha1.Work
		wantNotReady bool
		wantConds    []metav1.Condition
	}{
		{
			name: "a work object has not been checked for availability yet",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, metav1.ConditionTrue)),
				newWorkWithStatus("work-1", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, "")),
			},
			wantNotReady: true,
			wantConds:    existingConds,
		},
		{
			name: "a work object has a stale Available condition",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 2, workConds(1, metav1.ConditionTrue, metav1.ConditionTrue)),
			},
			wantNotReady: true,
			wantConds:    existingConds,
		},
		{
			name: "all work objects are available",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, metav1.ConditionTrue)),
				newWorkWithStatus("work-1", unitTestSnapshotName0, 3, workConds(3, metav1.ConditionTrue, metav1.ConditionTrue)),
			},
			wantConds: []metav1.Condition{
				newCondition(placementv1alpha1.PlacementBindingCondTypeAvailable, metav1.ConditionTrue, 2,
					placementv1alpha1.PlacementBindingAvailableCondReasonAllResourcesAvailable,
					"All resources are available in the target cluster"),
			},
		},
		{
			name: "a work object is not available",
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, metav1.ConditionTrue)),
				newWorkWithStatus("work-1", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, metav1.ConditionFalse)),
			},
			wantConds: []metav1.Condition{
				newCondition(placementv1alpha1.PlacementBindingCondTypeAvailable, metav1.ConditionFalse, 2,
					placementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable,
					"Some resources might be unavailable in the target cluster"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			binding := newBindingWithStatus(2, unitTestSnapshotName0, placementv1alpha1.PlacementBindingStatus{
				Conditions: append([]metav1.Condition{}, existingConds...),
			})
			gotNotReady := refreshPlacementBindingAvailableCond(binding, tc.works)
			if gotNotReady != tc.wantNotReady {
				t.Errorf("refreshPlacementBindingAvailableCond() = %t, want %t", gotNotReady, tc.wantNotReady)
			}
			if diff := cmp.Diff(binding.Status.Conditions, tc.wantConds, ignoreConditionLTT); diff != "" {
				t.Errorf("refreshPlacementBindingAvailableCond() conditions mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// failedToApplyWorkWithManyManifests returns a work object, linked to the given snapshot, with more manifests than the
// failed resource reporting limit, all of which have failed to be applied; the manifests are listed in descending
// order of their names. It also returns the failed resources that the work generator is expected to report (the
// first ones by name, up to the limit).
func failedToApplyWorkWithManyManifests(linkedSnapshotName string) (placementv1alpha1.Work, []placementv1alpha1.FailedResource) {
	manifestCount := maxFailedResourcesToReport + 1
	manifestStatuses := make([]placementv1alpha1.PerManifestStatus, manifestCount)
	for idx := 0; idx < manifestCount; idx++ {
		manifestStatuses[idx] = newManifestStatus(idx, fmt.Sprintf(paddedConfigMapNameTemplate, manifestCount-1-idx), manifestNotAppliedCond)
	}
	wantFailed := make([]placementv1alpha1.FailedResource, maxFailedResourcesToReport)
	for num := 0; num < maxFailedResourcesToReport; num++ {
		wantFailed[num] = newFailedResource(newManifestStatus(0, fmt.Sprintf(paddedConfigMapNameTemplate, num)), manifestNotAppliedCond)
	}
	work := newWorkWithStatus("work-0", linkedSnapshotName, 1, workConds(1, metav1.ConditionFalse, metav1.ConditionFalse), manifestStatuses...)
	return work, wantFailed
}

// TestRefreshPlacementBindingStatus tests the refreshPlacementBindingStatus method.
func TestRefreshPlacementBindingStatus(t *testing.T) {
	const bindingGeneration = 2

	waitingStatus := placementv1alpha1.PlacementBindingStatus{
		Conditions: []metav1.Condition{
			newCondition(placementv1alpha1.PlacementBindingCondTypeSynchronized, metav1.ConditionFalse, bindingGeneration,
				placementv1alpha1.PlacementBindingSynchronizedCondReasonWaitingForSynchronization, "waiting"),
			newCondition(placementv1alpha1.PlacementBindingCondTypeAvailable, metav1.ConditionUnknown, bindingGeneration,
				placementv1alpha1.PlacementBindingAvailableCondReasonWaitingForAvailabilityCheck, "waiting"),
		},
		SelectedResources:                 ptr.To(int32(3)),
		LastProcessedResourceSnapshotName: ptr.To(unitTestSnapshotName0),
	}
	syncedStatus := placementv1alpha1.PlacementBindingStatus{
		Conditions: []metav1.Condition{
			newCondition(placementv1alpha1.PlacementBindingCondTypeSynchronized, metav1.ConditionTrue, bindingGeneration,
				placementv1alpha1.PlacementBindingSynchronizedCondReasonAllResourcesSynchronized,
				"All resources have been synchronized to the target cluster"),
			newCondition(placementv1alpha1.PlacementBindingCondTypeAvailable, metav1.ConditionTrue, bindingGeneration,
				placementv1alpha1.PlacementBindingAvailableCondReasonAllResourcesAvailable,
				"All resources are available in the target cluster"),
		},
		SelectedResources:                 ptr.To(int32(3)),
		SynchronizedResources:             ptr.To(int32(3)),
		AvailableResources:                ptr.To(int32(3)),
		LastProcessedResourceSnapshotName: ptr.To(unitTestSnapshotName0),
	}
	failedConds := []metav1.Condition{
		newCondition(placementv1alpha1.PlacementBindingCondTypeSynchronized, metav1.ConditionFalse, bindingGeneration,
			placementv1alpha1.PlacementBindingSynchronizedCondReasonFailedToSynchronizeSomeResources,
			"Some resources might be out of sync in the target cluster"),
		newCondition(placementv1alpha1.PlacementBindingCondTypeAvailable, metav1.ConditionFalse, bindingGeneration,
			placementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable,
			"Some resources might be unavailable in the target cluster"),
	}

	syncedWorks := []placementv1alpha1.Work{
		newWorkWithStatus("work-0", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, metav1.ConditionTrue),
			newManifestStatus(0, "configmap-a", manifestAppliedCond, manifestAvailableCond),
			newManifestStatus(1, "configmap-b", manifestAppliedCond, manifestAvailableCond),
		),
		newWorkWithStatus("work-1", unitTestSnapshotName0, 3, workConds(3, metav1.ConditionTrue, metav1.ConditionTrue),
			newManifestStatus(0, "configmap-c", manifestAppliedCond, manifestAvailableCond),
		),
	}
	notApplied := newManifestStatus(0, "configmap-b", manifestNotAppliedCond)
	notAvailable := newManifestStatus(1, "configmap-a", manifestAppliedCond, manifestNotAvailableCond)
	someFailedWorks := []placementv1alpha1.Work{
		newWorkWithStatus("work-0", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionFalse, metav1.ConditionFalse),
			notApplied, notAvailable),
		newWorkWithStatus("work-1", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, metav1.ConditionTrue),
			newManifestStatus(0, "configmap-c", manifestAppliedCond, manifestAvailableCond),
		),
	}
	manyFailedWork, wantManyFailed := failedToApplyWorkWithManyManifests(unitTestSnapshotName0)

	testCases := []struct {
		name           string
		existingStatus placementv1alpha1.PlacementBindingStatus
		works          []placementv1alpha1.Work
		updateErr      error
		wantStatus     placementv1alpha1.PlacementBindingStatus
		wantWrites     int
		wantErr        bool
	}{
		{
			name:           "a work object has not been applied yet",
			existingStatus: waitingStatus,
			works: []placementv1alpha1.Work{
				syncedWorks[0],
				newWorkWithStatus("work-1", unitTestSnapshotName0, 1, nil, newManifestStatus(0, "configmap-c")),
			},
			wantStatus: waitingStatus,
		},
		{
			name:           "a work object has a stale Applied condition",
			existingStatus: waitingStatus,
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 2, workConds(1, metav1.ConditionTrue, metav1.ConditionTrue),
					newManifestStatus(0, "configmap-a", manifestAppliedCond, manifestAvailableCond),
				),
			},
			wantStatus: waitingStatus,
		},
		{
			name:           "a work object has been applied, but not yet checked for availability",
			existingStatus: waitingStatus,
			works: []placementv1alpha1.Work{
				newWorkWithStatus("work-0", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, ""),
					newManifestStatus(0, "configmap-a", manifestAppliedCond),
				),
			},
			wantStatus: waitingStatus,
		},
		{
			name:           "all resources synchronized and available",
			existingStatus: waitingStatus,
			works:          syncedWorks,
			wantStatus:     syncedStatus,
			wantWrites:     1,
		},
		{
			name:           "some resources failed to be applied or to become available",
			existingStatus: waitingStatus,
			works:          someFailedWorks,
			wantStatus: placementv1alpha1.PlacementBindingStatus{
				Conditions:            failedConds,
				SelectedResources:     ptr.To(int32(3)),
				SynchronizedResources: ptr.To(int32(2)),
				AvailableResources:    ptr.To(int32(1)),
				FailedResources: []placementv1alpha1.FailedResource{
					newFailedResource(notAvailable, manifestNotAvailableCond),
					newFailedResource(notApplied, manifestNotAppliedCond),
				},
				LastProcessedResourceSnapshotName: ptr.To(unitTestSnapshotName0),
			},
			wantWrites: 1,
		},
		{
			name:           "status unchanged",
			existingStatus: syncedStatus,
			works:          syncedWorks,
			wantStatus:     syncedStatus,
		},
		{
			name:           "more failed resources than the reporting limit",
			existingStatus: waitingStatus,
			works:          []placementv1alpha1.Work{manyFailedWork},
			wantStatus: placementv1alpha1.PlacementBindingStatus{
				Conditions:                        failedConds,
				SelectedResources:                 ptr.To(int32(maxFailedResourcesToReport + 1)),
				SynchronizedResources:             ptr.To(int32(0)),
				AvailableResources:                ptr.To(int32(0)),
				FailedResources:                   wantManyFailed,
				LastProcessedResourceSnapshotName: ptr.To(unitTestSnapshotName0),
			},
			wantWrites: 1,
		},
		{
			name:           "status update fails",
			existingStatus: waitingStatus,
			works:          syncedWorks,
			updateErr:      apierrors.NewServiceUnavailable("the API server is unavailable"),
			wantStatus:     waitingStatus,
			wantWrites:     1,
			wantErr:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			writes := 0
			c := newUnitTestHubClient(t, countStatusUpdates(&writes, tc.updateErr),
				newBindingWithStatus(bindingGeneration, unitTestSnapshotName0, *tc.existingStatus.DeepCopy()))
			r := New(c, 1, nil)

			err := r.refreshPlacementBindingStatus(ctx, getUnitTestBinding(t, c), tc.works)
			if (err != nil) != tc.wantErr {
				t.Fatalf("refreshPlacementBindingStatus() = %v, want error: %t", err, tc.wantErr)
			}
			if err != nil && kferrors.Category(err) != kferrors.ErrCategoryAPIServer {
				t.Errorf("refreshPlacementBindingStatus() error category = %q, want %q", kferrors.Category(err), kferrors.ErrCategoryAPIServer)
			}
			if writes != tc.wantWrites {
				t.Errorf("refreshPlacementBindingStatus() made %d status updates, want %d", writes, tc.wantWrites)
			}
			if diff := cmp.Diff(getUnitTestBinding(t, c).Status, tc.wantStatus, ignoreConditionLTT); diff != "" {
				t.Errorf("refreshPlacementBindingStatus() status mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// TestMarkPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots tests the
// markPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots method.
func TestMarkPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots(t *testing.T) {
	const (
		errDetails      = "placement resource snapshot \"policy-snapshot-1\" not found"
		otherErrDetails = "found placement resource snapshots with duplicate sub-indices"
	)
	invalidSnapshotsCond := func(generation int64, details string) metav1.Condition {
		return newCondition(placementv1alpha1.PlacementBindingCondTypeSynchronized, metav1.ConditionFalse, generation,
			placementv1alpha1.PlacementBindingSynchronizedCondReasonInvalidSnapshots,
			fmt.Sprintf("Failed to synchronize resources as the given placement resource snapshots are invalid: %s", details))
	}
	incompleteSetOfResourcesCond := func(generation int64) metav1.Condition {
		return newCondition(placementv1alpha1.PlacementBindingCondTypeAvailable, metav1.ConditionFalse, generation,
			placementv1alpha1.PlacementBindingAvailableCondReasonIncompleteSetOfResources,
			"Cannot check the availability of resources as the given placement resource snapshots are invalid; some resources might be missing from the target cluster")
	}
	// The status of a placement binding whose resources (from snapshot 0) have been synchronized and become available
	// at generation 2.
	syncedStatus := placementv1alpha1.PlacementBindingStatus{
		Conditions: []metav1.Condition{
			newCondition(placementv1alpha1.PlacementBindingCondTypeSynchronized, metav1.ConditionTrue, 2,
				placementv1alpha1.PlacementBindingSynchronizedCondReasonAllResourcesSynchronized, "synchronized"),
			newCondition(placementv1alpha1.PlacementBindingCondTypeAvailable, metav1.ConditionTrue, 2,
				placementv1alpha1.PlacementBindingAvailableCondReasonAllResourcesAvailable, "available"),
		},
		SelectedResources:                 ptr.To(int32(3)),
		SynchronizedResources:             ptr.To(int32(3)),
		AvailableResources:                ptr.To(int32(3)),
		LastProcessedResourceSnapshotName: ptr.To(unitTestSnapshotName0),
	}

	notAvailable := newManifestStatus(1, "configmap-b", manifestAppliedCond, manifestNotAvailableCond)
	// The work objects linked to snapshot 0 (the last processed one), plus one linked to snapshot 1, which should not
	// be counted.
	works := []placementv1alpha1.Work{
		newWorkWithStatus("work-0", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, metav1.ConditionFalse),
			newManifestStatus(0, "configmap-a", manifestAppliedCond, manifestAvailableCond),
			notAvailable,
		),
		newWorkWithStatus("work-1", unitTestSnapshotName0, 1, workConds(1, metav1.ConditionTrue, metav1.ConditionTrue),
			newManifestStatus(0, "configmap-c", manifestAppliedCond, manifestAvailableCond),
		),
		newWorkWithStatus("work-2", unitTestSnapshotName1, 1, nil,
			newManifestStatus(0, "configmap-d", manifestNotAppliedCond),
		),
	}
	// The status after rolling out to snapshot 1 (at generation 3) with the given error details.
	rolledOutStatus := func(details string) placementv1alpha1.PlacementBindingStatus {
		return placementv1alpha1.PlacementBindingStatus{
			Conditions:                        []metav1.Condition{invalidSnapshotsCond(3, details), syncedStatus.Conditions[1]},
			SelectedResources:                 ptr.To(int32(3)),
			SynchronizedResources:             ptr.To(int32(3)),
			AvailableResources:                ptr.To(int32(2)),
			FailedResources:                   []placementv1alpha1.FailedResource{newFailedResource(notAvailable, manifestNotAvailableCond)},
			LastProcessedResourceSnapshotName: ptr.To(unitTestSnapshotName0),
		}
	}
	statusWithoutAvailability := *syncedStatus.DeepCopy()
	statusWithoutAvailability.Conditions = statusWithoutAvailability.Conditions[:1]
	rolledOutStatusWithoutAvailability := rolledOutStatus(errDetails)
	rolledOutStatusWithoutAvailability.Conditions = rolledOutStatusWithoutAvailability.Conditions[:1]
	manyFailedWork, wantManyFailed := failedToApplyWorkWithManyManifests(unitTestSnapshotName0)

	testCases := []struct {
		name              string
		bindingGeneration int64
		snapshotName      string
		existingStatus    placementv1alpha1.PlacementBindingStatus
		works             []placementv1alpha1.Work
		errDetails        string
		updateErr         error
		wantStatus        placementv1alpha1.PlacementBindingStatus
		wantWrites        int
		wantErr           bool
	}{
		{
			name:              "rolled out to an invalid snapshot",
			bindingGeneration: 3,
			snapshotName:      unitTestSnapshotName1,
			existingStatus:    syncedStatus,
			works:             works,
			errDetails:        errDetails,
			wantStatus:        rolledOutStatus(errDetails),
			wantWrites:        1,
		},
		{
			name:              "rolled out with no Available condition",
			bindingGeneration: 3,
			snapshotName:      unitTestSnapshotName1,
			existingStatus:    statusWithoutAvailability,
			works:             works,
			errDetails:        errDetails,
			wantStatus:        rolledOutStatusWithoutAvailability,
			wantWrites:        1,
		},
		{
			name:              "no snapshot processed yet",
			bindingGeneration: 1,
			snapshotName:      unitTestSnapshotName0,
			// The work objects (e.g., left over from a previous binding of the same name) are not counted.
			works:      works,
			errDetails: errDetails,
			wantStatus: placementv1alpha1.PlacementBindingStatus{
				Conditions:            []metav1.Condition{invalidSnapshotsCond(1, errDetails), incompleteSetOfResourcesCond(1)},
				SelectedResources:     ptr.To(int32(0)),
				SynchronizedResources: ptr.To(int32(0)),
				AvailableResources:    ptr.To(int32(0)),
			},
			wantWrites: 1,
		},
		{
			name:              "the processed snapshot has become invalid",
			bindingGeneration: 2,
			snapshotName:      unitTestSnapshotName0,
			existingStatus:    syncedStatus,
			works:             works,
			errDetails:        errDetails,
			wantStatus: placementv1alpha1.PlacementBindingStatus{
				Conditions:                        []metav1.Condition{invalidSnapshotsCond(2, errDetails), incompleteSetOfResourcesCond(2)},
				SelectedResources:                 ptr.To(int32(3)),
				SynchronizedResources:             ptr.To(int32(3)),
				AvailableResources:                ptr.To(int32(2)),
				FailedResources:                   []placementv1alpha1.FailedResource{newFailedResource(notAvailable, manifestNotAvailableCond)},
				LastProcessedResourceSnapshotName: ptr.To(unitTestSnapshotName0),
			},
			wantWrites: 1,
		},
		{
			name:              "more failed resources than the reporting limit",
			bindingGeneration: 3,
			snapshotName:      unitTestSnapshotName1,
			existingStatus:    syncedStatus,
			works:             []placementv1alpha1.Work{manyFailedWork},
			errDetails:        errDetails,
			wantStatus: placementv1alpha1.PlacementBindingStatus{
				Conditions:                        []metav1.Condition{invalidSnapshotsCond(3, errDetails), syncedStatus.Conditions[1]},
				SelectedResources:                 ptr.To(int32(maxFailedResourcesToReport + 1)),
				SynchronizedResources:             ptr.To(int32(0)),
				AvailableResources:                ptr.To(int32(0)),
				FailedResources:                   wantManyFailed,
				LastProcessedResourceSnapshotName: ptr.To(unitTestSnapshotName0),
			},
			wantWrites: 1,
		},
		{
			name:              "already marked with the same error details",
			bindingGeneration: 3,
			snapshotName:      unitTestSnapshotName1,
			existingStatus:    rolledOutStatus(errDetails),
			works:             works,
			errDetails:        errDetails,
			wantStatus:        rolledOutStatus(errDetails),
		},
		{
			name:              "already marked with different error details",
			bindingGeneration: 3,
			snapshotName:      unitTestSnapshotName1,
			existingStatus:    rolledOutStatus(otherErrDetails),
			works:             works,
			errDetails:        errDetails,
			wantStatus:        rolledOutStatus(errDetails),
			wantWrites:        1,
		},
		{
			name:              "status update fails",
			bindingGeneration: 3,
			snapshotName:      unitTestSnapshotName1,
			existingStatus:    syncedStatus,
			works:             works,
			errDetails:        errDetails,
			updateErr:         apierrors.NewConflict(schema.GroupResource{Group: placementv1alpha1.GroupVersion.Group, Resource: "placementbindings"}, unitTestBindingName, fmt.Errorf("conflict")),
			wantStatus:        syncedStatus,
			wantWrites:        1,
			wantErr:           true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			writes := 0
			c := newUnitTestHubClient(t, countStatusUpdates(&writes, tc.updateErr),
				newBindingWithStatus(tc.bindingGeneration, tc.snapshotName, *tc.existingStatus.DeepCopy()))
			r := New(c, 1, nil)

			err := r.markPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots(ctx, getUnitTestBinding(t, c), tc.works, tc.errDetails)
			if (err != nil) != tc.wantErr {
				t.Fatalf("markPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots() = %v, want error: %t", err, tc.wantErr)
			}
			if err != nil && kferrors.Category(err) != kferrors.ErrCategoryAPIServer {
				t.Errorf("markPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots() error category = %q, want %q",
					kferrors.Category(err), kferrors.ErrCategoryAPIServer)
			}
			if writes != tc.wantWrites {
				t.Errorf("markPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots() made %d status updates, want %d", writes, tc.wantWrites)
			}
			if diff := cmp.Diff(getUnitTestBinding(t, c).Status, tc.wantStatus, ignoreConditionLTT); diff != "" {
				t.Errorf("markPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots() status mismatch (-got +want):\n%s", diff)
			}
		})
	}
}
