/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// TestRefreshWorkStatus tests the refreshWorkStatus method.
func TestRefreshWorkStatus(t *testing.T) {
	ctx := context.Background()

	deploy1 := deploy.DeepCopy()
	deploy1.Generation = 2

	deployName2 := "deploy-2"
	deploy2 := deploy.DeepCopy()
	deploy2.Name = deployName2

	deployName3 := "deploy-3"
	deploy3 := deploy.DeepCopy()
	deploy3.Name = deployName3

	workNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberReservedNSName,
		},
	}

	// Round up the timestamps due to K8s API server's precision limits.
	firstDriftedTime := metav1.Time{
		Time: metav1.Now().Rfc3339Copy().Time.Add(-1 * time.Hour),
	}
	firstDiffedTime := metav1.Time{
		Time: metav1.Now().Rfc3339Copy().Time.Add(-1 * time.Hour),
	}
	driftObservedTimeMustBefore := metav1.Time{
		Time: metav1.Now().Rfc3339Copy().Time.Add(-1 * time.Minute),
	}

	testCases := []struct {
		name                               string
		work                               *fleetv1beta1.Work
		bundles                            []*manifestProcessingBundle
		wantWorkStatus                     *fleetv1beta1.WorkStatus
		ignoreFirstDriftedDiffedTimestamps bool
	}{
		{
			name: "all applied, all available",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Namespace:  memberReservedNSName,
					Generation: 1,
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   0,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      deployName,
						Namespace: nsName,
						Resource:  "deployments",
					},
					inMemberClusterObj: toUnstructured(t, deploy1.DeepCopy()),
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeAvailable,
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeNotEnabled,
				},
			},
			wantWorkStatus: &fleetv1beta1.WorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingApplyResultTypeApplied),
						ObservedGeneration: 1,
					},
					{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingAvailabilityResultTypeAvailable),
						ObservedGeneration: 1,
					},
				},
				ManifestConditions: []fleetv1beta1.ManifestCondition{
					{
						Identifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:   0,
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      deployName,
							Namespace: nsName,
							Resource:  "deployments",
						},
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								Reason:             string(ManifestProcessingApplyResultTypeApplied),
								ObservedGeneration: 2,
							},
							{
								Type:               fleetv1beta1.WorkConditionTypeAvailable,
								Status:             metav1.ConditionTrue,
								Reason:             string(ManifestProcessingAvailabilityResultTypeAvailable),
								ObservedGeneration: 2,
							},
						},
					},
				},
			},
		},
		{
			name: "mixed applied result",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Namespace:  memberReservedNSName,
					Generation: 2,
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   0,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      deployName2,
						Namespace: nsName,
						Resource:  "deployments",
					},
					inMemberClusterObj: toUnstructured(t, deploy2.DeepCopy()),
					applyResTyp:        ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeSkipped,
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeNotEnabled,
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      deployName3,
						Namespace: nsName,
						Resource:  "deployments",
					},
					inMemberClusterObj: toUnstructured(t, deploy3.DeepCopy()),
					applyResTyp:        ManifestProcessingApplyResultTypeFailedToTakeOver,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeSkipped,
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeNotEnabled,
				},
			},
			wantWorkStatus: &fleetv1beta1.WorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						Reason:             notAllManifestsAppliedReason,
						ObservedGeneration: 2,
					},
					{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						Reason:             notAllAppliedObjectsAvailableReason,
						ObservedGeneration: 2,
					},
				},
				ManifestConditions: []fleetv1beta1.ManifestCondition{
					{
						Identifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:   0,
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      deployName2,
							Namespace: nsName,
							Resource:  "deployments",
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
								Reason: string(ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection),
							},
						},
					},
					{
						Identifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:   1,
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      deployName3,
							Namespace: nsName,
							Resource:  "deployments",
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
								Reason: string(ManifestProcessingApplyResultTypeFailedToTakeOver),
							},
						},
					},
				},
			},
		},
		{
			name: "mixed availability check",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: memberReservedNSName,
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   0,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      deployName,
						Namespace: nsName,
						Resource:  "deployments",
					},
					inMemberClusterObj: toUnstructured(t, deploy.DeepCopy()),
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeFailed,
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeNotEnabled,
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      deployName2,
						Namespace: nsName,
						Resource:  "deployments",
					},
					inMemberClusterObj: toUnstructured(t, deploy.DeepCopy()),
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeNotEnabled,
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   2,
						Group:     "batch",
						Version:   "v1",
						Kind:      "Job",
						Name:      "job",
						Namespace: nsName,
						Resource:  "jobs",
					},
					inMemberClusterObj: toUnstructured(t, deploy.DeepCopy()),
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeNotTrackable,
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeNotEnabled,
				},
			},
			wantWorkStatus: &fleetv1beta1.WorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:   fleetv1beta1.WorkConditionTypeApplied,
						Status: metav1.ConditionTrue,
						Reason: string(ManifestProcessingApplyResultTypeApplied),
					},
					{
						Type:   fleetv1beta1.WorkConditionTypeAvailable,
						Status: metav1.ConditionFalse,
						Reason: notAllAppliedObjectsAvailableReason,
					},
				},
				ManifestConditions: []fleetv1beta1.ManifestCondition{
					{
						Identifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:   0,
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      deployName,
							Namespace: nsName,
							Resource:  "deployments",
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
								Reason: string(ManifestProcessingApplyResultTypeApplied),
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
								Reason: string(ManifestProcessingAvailabilityResultTypeFailed),
							},
						},
					},
					{
						Identifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:   1,
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      deployName2,
							Namespace: nsName,
							Resource:  "deployments",
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
								Reason: string(ManifestProcessingApplyResultTypeApplied),
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
								Reason: string(ManifestProcessingAvailabilityResultTypeNotYetAvailable),
							},
						},
					},
					{
						Identifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:   2,
							Group:     "batch",
							Version:   "v1",
							Kind:      "Job",
							Name:      "job",
							Namespace: nsName,
							Resource:  "jobs",
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
								Reason: string(ManifestProcessingApplyResultTypeApplied),
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionTrue,
								Reason: string(ManifestProcessingAvailabilityResultTypeNotTrackable),
							},
						},
					},
				},
			},
		},
		{
			name: "drift and diff details",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Namespace:  memberReservedNSName,
					Generation: 2,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             notAllManifestsAppliedReason,
							ObservedGeneration: 1,
						},
					},
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "apps",
								Version:   "v1",
								Kind:      "Deployment",
								Name:      deployName,
								Namespace: nsName,
								Resource:  "deployments",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionFalse,
									Reason: string(ManifestProcessingApplyResultTypeFoundDrifts),
								},
							},
							DriftDetails: &fleetv1beta1.DriftDetails{
								FirstDriftedObservedTime: firstDriftedTime,
							},
						},
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   1,
								Group:     "apps",
								Version:   "v1",
								Kind:      "Deployment",
								Name:      deployName2,
								Namespace: nsName,
								Resource:  "deployments",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionFalse,
									Reason: string(ManifestProcessingApplyResultTypeFailedToTakeOver),
								},
							},
							DiffDetails: &fleetv1beta1.DiffDetails{
								ObservedInMemberClusterGeneration: ptr.To(int64(0)),
								FirstDiffedObservedTime:           firstDiffedTime,
							},
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   0,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      deployName,
						Namespace: nsName,
						Resource:  "deployments",
					},
					inMemberClusterObj: toUnstructured(t, deploy.DeepCopy()),
					applyResTyp:        ManifestProcessingApplyResultTypeFoundDrifts,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeSkipped,
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeNotEnabled,
					drifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInMember: "1",
						},
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      deployName2,
						Namespace: nsName,
						Resource:  "deployments",
					},
					inMemberClusterObj: toUnstructured(t, deploy2.DeepCopy()),
					applyResTyp:        ManifestProcessingApplyResultTypeFailedToTakeOver,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeSkipped,
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeNotEnabled,
					diffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInMember: "2",
							ValueInHub:    "3",
						},
					},
				},
			},
			wantWorkStatus: &fleetv1beta1.WorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						Reason:             notAllManifestsAppliedReason,
						ObservedGeneration: 2,
					},
					{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						Reason:             notAllAppliedObjectsAvailableReason,
						ObservedGeneration: 2,
					},
				},
				ManifestConditions: []fleetv1beta1.ManifestCondition{
					{
						Identifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:   0,
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      deployName,
							Namespace: nsName,
							Resource:  "deployments",
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
								Reason: string(ManifestProcessingApplyResultTypeFoundDrifts),
							},
						},
						DriftDetails: &fleetv1beta1.DriftDetails{
							FirstDriftedObservedTime: firstDriftedTime,
							ObservedDrifts: []fleetv1beta1.PatchDetail{
								{
									Path:          "/spec/replicas",
									ValueInMember: "1",
								},
							},
						},
					},
					{
						Identifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:   1,
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      deployName2,
							Namespace: nsName,
							Resource:  "deployments",
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
								Reason: string(ManifestProcessingApplyResultTypeFailedToTakeOver),
							},
						},
						DiffDetails: &fleetv1beta1.DiffDetails{
							FirstDiffedObservedTime:           firstDiffedTime,
							ObservedInMemberClusterGeneration: ptr.To(int64(0)),
							ObservedDiffs: []fleetv1beta1.PatchDetail{
								{
									Path:          "/spec/replicas",
									ValueInMember: "2",
									ValueInHub:    "3",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "report diff mode",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Namespace:  memberReservedNSName,
					Generation: 2,
				},
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
					},
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             notAllManifestsAppliedReason,
							ObservedGeneration: 1,
						},
					},
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "apps",
								Version:   "v1",
								Kind:      "Deployment",
								Name:      deployName,
								Namespace: nsName,
								Resource:  "deployments",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionFalse,
									Reason: string(ManifestProcessingApplyResultTypeFoundDrifts),
								},
							},
							DriftDetails: &fleetv1beta1.DriftDetails{
								FirstDriftedObservedTime: firstDriftedTime,
							},
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   0,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      deployName,
						Namespace: nsName,
						Resource:  "deployments",
					},
					inMemberClusterObj: toUnstructured(t, deploy.DeepCopy()),
					applyResTyp:        ManifestProcessingApplyResultTypeNoApplyPerformed,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeSkipped,
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeFoundDiff,
					diffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/x",
							ValueInMember: "0",
						},
					},
				},
			},
			wantWorkStatus: &fleetv1beta1.WorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						Reason:             notAllManifestsAppliedReason,
						ObservedGeneration: 1,
					},
					{
						Type:               fleetv1beta1.WorkConditionTypeDiffReported,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingReportDiffResultTypeFoundDiff),
						ObservedGeneration: 2,
					},
				},
				ManifestConditions: []fleetv1beta1.ManifestCondition{
					{
						Identifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:   0,
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      deployName,
							Namespace: nsName,
							Resource:  "deployments",
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
								Reason: string(ManifestProcessingApplyResultTypeFoundDrifts),
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeDiffReported,
								Status: metav1.ConditionTrue,
								Reason: string(ManifestProcessingReportDiffResultTypeFoundDiff),
							},
						},
						DiffDetails: &fleetv1beta1.DiffDetails{
							ObservedInMemberClusterGeneration: ptr.To(int64(0)),
							ObservedDiffs: []fleetv1beta1.PatchDetail{
								{
									Path:          "/x",
									ValueInMember: "0",
								},
							},
						},
					},
				},
			},
			ignoreFirstDriftedDiffedTimestamps: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(workNS, tc.work).
				WithStatusSubresource(tc.work).
				Build()
			r := &Reconciler{
				hubClient:     fakeClient,
				workNameSpace: memberReservedNSName,
			}

			err := r.refreshWorkStatus(ctx, tc.work, tc.bundles)
			if err != nil {
				t.Fatalf("refreshWorkStatus() = %v, want no error", err)
			}

			updatedWork := &fleetv1beta1.Work{}
			if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: memberReservedNSName, Name: workName}, updatedWork); err != nil {
				t.Fatalf("Work Get() = %v, want no error", err)
			}
			opts := []cmp.Option{
				ignoreFieldConditionLTTMsg,
				cmpopts.IgnoreFields(fleetv1beta1.DriftDetails{}, "ObservationTime"),
				cmpopts.IgnoreFields(fleetv1beta1.DiffDetails{}, "ObservationTime"),
			}
			if tc.ignoreFirstDriftedDiffedTimestamps {
				opts = append(opts, cmpopts.IgnoreFields(fleetv1beta1.DriftDetails{}, "FirstDriftedObservedTime"))
				opts = append(opts, cmpopts.IgnoreFields(fleetv1beta1.DiffDetails{}, "FirstDiffedObservedTime"))
			}
			if diff := cmp.Diff(
				&updatedWork.Status, tc.wantWorkStatus,
				opts...,
			); diff != "" {
				t.Errorf("refreshed Work status mismatches (-got, +want):\n%s", diff)
			}

			for _, manifestCond := range updatedWork.Status.ManifestConditions {
				if manifestCond.DriftDetails != nil && manifestCond.DriftDetails.ObservationTime.Time.Before(driftObservedTimeMustBefore.Time) {
					t.Errorf("DriftDetails.ObservationTime = %v, want after %v", manifestCond.DriftDetails.ObservationTime, driftObservedTimeMustBefore)
				}

				if manifestCond.DiffDetails != nil && manifestCond.DiffDetails.ObservationTime.Time.Before(driftObservedTimeMustBefore.Time) {
					t.Errorf("DiffDetails.ObservationTime = %v, want after %v", manifestCond.DiffDetails.ObservationTime, driftObservedTimeMustBefore)
				}
			}
		})
	}
}

// TestRefreshAppliedWorkStatus tests the refreshAppliedWorkStatus method.
func TestRefreshAppliedWorkStatus(t *testing.T) {
	ctx := context.Background()

	deploy1 := deploy.DeepCopy()
	deploy1.UID = "123-xyz"

	deploy2 := deploy.DeepCopy()
	deployName2 := "deploy-2"
	deploy2.Name = deployName2
	deploy2.UID = "789-lmn"

	ns1 := ns.DeepCopy()
	ns1.UID = "456-abc"

	testCases := []struct {
		name                  string
		appliedWork           *fleetv1beta1.AppliedWork
		bundles               []*manifestProcessingBundle
		wantAppliedWorkStatus *fleetv1beta1.AppliedWorkStatus
	}{
		{
			name: "mixed",
			appliedWork: &fleetv1beta1.AppliedWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: workName,
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   0,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      deployName,
						Namespace: nsName,
						Resource:  "deployments",
					},
					inMemberClusterObj: toUnstructured(t, deploy1),
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  1,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Name:     nsName,
						Resource: "namespaces",
					},
					inMemberClusterObj: toUnstructured(t, ns1),
					applyResTyp:        ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection,
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   0,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      deployName2,
						Namespace: nsName,
						Resource:  "deployments",
					},
					inMemberClusterObj: toUnstructured(t, deploy2),
					applyResTyp:        ManifestProcessingApplyResultTypeFailedToFindObjInMemberCluster,
				},
			},
			wantAppliedWorkStatus: &fleetv1beta1.AppliedWorkStatus{
				AppliedResources: []fleetv1beta1.AppliedResourceMeta{
					{
						WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:   0,
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      deployName,
							Namespace: nsName,
							Resource:  "deployments",
						},
						UID: "123-xyz",
					},
					{
						WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:  1,
							Group:    "",
							Version:  "v1",
							Kind:     "Namespace",
							Name:     nsName,
							Resource: "namespaces",
						},
						UID: "456-abc",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.appliedWork).
				WithStatusSubresource(tc.appliedWork).
				Build()
			r := &Reconciler{
				spokeClient: fakeClient,
			}

			err := r.refreshAppliedWorkStatus(ctx, tc.appliedWork, tc.bundles)
			if err != nil {
				t.Fatalf("refreshAppliedWorkStatus() = %v, want no error", err)
			}

			updatedAppliedWork := &fleetv1beta1.AppliedWork{}
			if err := fakeClient.Get(ctx, types.NamespacedName{Name: workName}, updatedAppliedWork); err != nil {
				t.Fatalf("AppliedWork Get() = %v, want no error", err)
			}

			if diff := cmp.Diff(&updatedAppliedWork.Status, tc.wantAppliedWorkStatus); diff != "" {
				t.Errorf("refreshed AppliedWork status mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}
