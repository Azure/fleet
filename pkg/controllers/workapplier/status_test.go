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

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
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
						Reason:             WorkAllManifestsAppliedReason,
						ObservedGeneration: 1,
					},
					{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionTrue,
						Reason:             WorkAllManifestsAvailableReason,
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
						Reason:             WorkNotAllManifestsAppliedReason,
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
						Reason: WorkAllManifestsAppliedReason,
					},
					{
						Type:   fleetv1beta1.WorkConditionTypeAvailable,
						Status: metav1.ConditionFalse,
						Reason: WorkNotAllManifestsAvailableReason,
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
							Reason:             WorkNotAllManifestsAppliedReason,
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
						Reason:             WorkNotAllManifestsAppliedReason,
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
			name: "report diff mode, all diff found",
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
							Reason:             WorkNotAllManifestsAppliedReason,
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
						Type:               fleetv1beta1.WorkConditionTypeDiffReported,
						Status:             metav1.ConditionTrue,
						Reason:             WorkAllManifestsDiffReportedReason,
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
		{
			name: "report diff mode, no diff found",
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
							Status:             metav1.ConditionTrue,
							Reason:             WorkAllManifestsAppliedReason,
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
									Status: metav1.ConditionTrue,
									Reason: string(ManifestProcessingApplyResultTypeApplied),
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionTrue,
									Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
								},
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
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeNoDiffFound,
					diffs:              []fleetv1beta1.PatchDetail{},
				},
			},
			wantWorkStatus: &fleetv1beta1.WorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeDiffReported,
						Status:             metav1.ConditionTrue,
						Reason:             WorkAllManifestsDiffReportedReason,
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
								Type:   fleetv1beta1.WorkConditionTypeDiffReported,
								Status: metav1.ConditionTrue,
								Reason: string(ManifestProcessingReportDiffResultTypeNoDiffFound),
							},
						},
					},
				},
			},
			ignoreFirstDriftedDiffedTimestamps: true,
		},
		{
			name: "report diff mode, mixed",
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
							Reason:             WorkNotAllManifestsAppliedReason,
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
								Ordinal:  1,
								Group:    "",
								Version:  "v1",
								Kind:     "Namespace",
								Name:     nsName,
								Resource: "namespaces",
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
									Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
								},
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
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  1,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Name:     nsName,
						Resource: "namespaces",
					},
					inMemberClusterObj: toUnstructured(t, ns.DeepCopy()),
					applyResTyp:        ManifestProcessingApplyResultTypeNoApplyPerformed,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeSkipped,
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeNoDiffFound,
				},
			},
			wantWorkStatus: &fleetv1beta1.WorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeDiffReported,
						Status:             metav1.ConditionTrue,
						Reason:             WorkAllManifestsDiffReportedReason,
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
					{
						Identifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:  1,
							Group:    "",
							Version:  "v1",
							Kind:     "Namespace",
							Name:     nsName,
							Resource: "namespaces",
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeDiffReported,
								Status: metav1.ConditionTrue,
								Reason: string(ManifestProcessingReportDiffResultTypeNoDiffFound),
							},
						},
					},
				},
			},
			ignoreFirstDriftedDiffedTimestamps: true,
		},
		{
			name: "report diff mode, partial failure",
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
							Reason:             WorkNotAllManifestsAppliedReason,
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
								Ordinal:  1,
								Group:    "",
								Version:  "v1",
								Kind:     "Namespace",
								Name:     nsName,
								Resource: "namespaces",
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
									Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
								},
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
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeFailed,
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
					inMemberClusterObj: toUnstructured(t, ns.DeepCopy()),
					applyResTyp:        ManifestProcessingApplyResultTypeNoApplyPerformed,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeSkipped,
					reportDiffResTyp:   ManifestProcessingReportDiffResultTypeNoDiffFound,
				},
			},
			wantWorkStatus: &fleetv1beta1.WorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeDiffReported,
						Status:             metav1.ConditionFalse,
						Reason:             WorkNotAllManifestsDiffReportedReason,
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
								Type:   fleetv1beta1.WorkConditionTypeDiffReported,
								Status: metav1.ConditionFalse,
								Reason: string(ManifestProcessingReportDiffResultTypeFailed),
							},
						},
					},
					{
						Identifier: fleetv1beta1.WorkResourceIdentifier{
							Ordinal:  1,
							Group:    "",
							Version:  "v1",
							Kind:     "Namespace",
							Name:     nsName,
							Resource: "namespaces",
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeDiffReported,
								Status: metav1.ConditionTrue,
								Reason: string(ManifestProcessingReportDiffResultTypeNoDiffFound),
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

// TestSetManifestAppliedCondition tests the setManifestAppliedCondition function.
func TestSetManifestAppliedCondition(t *testing.T) {
	testCases := []struct {
		name                              string
		manifestCond                      *fleetv1beta1.ManifestCondition
		applyResTyp                       manifestProcessingAppliedResultType
		applyErr                          error
		observedInMemberClusterGeneration int64
		wantManifestCond                  *fleetv1beta1.ManifestCondition
	}{
		{
			name:                              "applied",
			manifestCond:                      &fleetv1beta1.ManifestCondition{},
			applyResTyp:                       ManifestProcessingApplyResultTypeApplied,
			observedInMemberClusterGeneration: 1,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingApplyResultTypeApplied),
						ObservedGeneration: 1,
					},
				},
			},
		},
		{
			name: "applied with failed drift detection",
			manifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingApplyResultTypeApplied),
						ObservedGeneration: 1,
					},
				},
			},
			applyResTyp:                       ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection,
			observedInMemberClusterGeneration: 1,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection),
						ObservedGeneration: 1,
					},
				},
			},
		},
		{
			name: "failed to apply",
			manifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingApplyResultTypeApplied),
						ObservedGeneration: 1,
					},
				},
			},
			applyResTyp:                       ManifestProcessingApplyResultTypeFailedToApply,
			observedInMemberClusterGeneration: 2,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						Reason:             string(ManifestProcessingApplyResultTypeFailedToApply),
						ObservedGeneration: 2,
					},
				},
			},
		},
		{
			name: "no apply performed",
			manifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingApplyResultTypeApplied),
						ObservedGeneration: 1,
					},
				},
			},
			applyResTyp:                       ManifestProcessingApplyResultTypeNoApplyPerformed,
			observedInMemberClusterGeneration: 1,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setManifestAppliedCondition(tc.manifestCond, tc.applyResTyp, tc.applyErr, tc.observedInMemberClusterGeneration)
			if diff := cmp.Diff(tc.manifestCond, tc.wantManifestCond, ignoreFieldConditionLTTMsg); diff != "" {
				t.Errorf("set manifest cond mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestSetManifestAvailableCondition tests the setManifestAvailableCondition function.
func TestSetManifestAvailableCondition(t *testing.T) {
	testCases := []struct {
		name                         string
		manifestCond                 *fleetv1beta1.ManifestCondition
		availabilityResTyp           ManifestProcessingAvailabilityResultType
		availabilityError            error
		inMemberClusterObjGeneration int64
		wantManifestCond             *fleetv1beta1.ManifestCondition
	}{
		{
			name:                         "available",
			manifestCond:                 &fleetv1beta1.ManifestCondition{},
			availabilityResTyp:           ManifestProcessingAvailabilityResultTypeAvailable,
			inMemberClusterObjGeneration: 1,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingAvailabilityResultTypeAvailable),
						ObservedGeneration: 1,
					},
				},
			},
		},
		{
			name: "unavailable",
			manifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingAvailabilityResultTypeAvailable),
						ObservedGeneration: 1,
					},
				},
			},
			availabilityResTyp:           ManifestProcessingAvailabilityResultTypeFailed,
			inMemberClusterObjGeneration: 2,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						Reason:             string(ManifestProcessingAvailabilityResultTypeFailed),
						ObservedGeneration: 2,
					},
				},
			},
		},
		{
			name: "not yet available",
			manifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingAvailabilityResultTypeAvailable),
						ObservedGeneration: 1,
					},
				},
			},
			availabilityResTyp:           ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			inMemberClusterObjGeneration: 1,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						Reason:             string(ManifestProcessingAvailabilityResultTypeNotYetAvailable),
						ObservedGeneration: 1,
					},
				},
			},
		},
		{
			name:                         "untrackable",
			manifestCond:                 &fleetv1beta1.ManifestCondition{},
			availabilityResTyp:           ManifestProcessingAvailabilityResultTypeNotTrackable,
			inMemberClusterObjGeneration: 1,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingAvailabilityResultTypeNotTrackable),
						ObservedGeneration: 1,
					},
				},
			},
		},
		{
			name: "skipped",
			manifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						Reason:             string(ManifestProcessingAvailabilityResultTypeFailed),
						ObservedGeneration: 1,
					},
				},
			},
			availabilityResTyp:           ManifestProcessingAvailabilityResultTypeSkipped,
			inMemberClusterObjGeneration: 2,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setManifestAvailableCondition(tc.manifestCond, tc.availabilityResTyp, tc.availabilityError, tc.inMemberClusterObjGeneration)
			if diff := cmp.Diff(tc.manifestCond, tc.wantManifestCond, ignoreFieldConditionLTTMsg); diff != "" {
				t.Errorf("set manifest cond mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestSetManifestDiffReportedCondition tests the setManifestDiffReportedCondition function.
func TestSetManifestDiffReportedCondition(t *testing.T) {
	testCases := []struct {
		name                         string
		manifestCond                 *fleetv1beta1.ManifestCondition
		reportDiffResTyp             ManifestProcessingReportDiffResultType
		reportDiffError              error
		inMemberClusterObjGeneration int64
		wantManifestCond             *fleetv1beta1.ManifestCondition
	}{
		{
			name:                         "failed",
			manifestCond:                 &fleetv1beta1.ManifestCondition{},
			reportDiffResTyp:             ManifestProcessingReportDiffResultTypeFailed,
			inMemberClusterObjGeneration: 1,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeDiffReported,
						Status:             metav1.ConditionFalse,
						Reason:             string(ManifestProcessingReportDiffResultTypeFailed),
						ObservedGeneration: 1,
					},
				},
			},
		},
		{
			name: "found diff",
			manifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeDiffReported,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingReportDiffResultTypeNoDiffFound),
						ObservedGeneration: 1,
					},
				},
			},
			reportDiffResTyp:             ManifestProcessingReportDiffResultTypeFoundDiff,
			inMemberClusterObjGeneration: 2,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeDiffReported,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingReportDiffResultTypeFoundDiff),
						ObservedGeneration: 2,
					},
				},
			},
		},
		{
			name: "no diff found",
			manifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeDiffReported,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingReportDiffResultTypeFoundDiff),
						ObservedGeneration: 1,
					},
				},
			},
			reportDiffResTyp:             ManifestProcessingReportDiffResultTypeNoDiffFound,
			inMemberClusterObjGeneration: 2,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeDiffReported,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingReportDiffResultTypeNoDiffFound),
						ObservedGeneration: 2,
					},
				},
			},
		},
		{
			name: "skipped",
			manifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{
					{
						Type:               fleetv1beta1.WorkConditionTypeDiffReported,
						Status:             metav1.ConditionTrue,
						Reason:             string(ManifestProcessingReportDiffResultTypeFoundDiff),
						ObservedGeneration: 1,
					},
				},
			},
			reportDiffResTyp:             ManifestProcessingReportDiffResultTypeNotEnabled,
			inMemberClusterObjGeneration: 1,
			wantManifestCond: &fleetv1beta1.ManifestCondition{
				Conditions: []metav1.Condition{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setManifestDiffReportedCondition(tc.manifestCond, tc.reportDiffResTyp, tc.reportDiffError, tc.inMemberClusterObjGeneration)
			if diff := cmp.Diff(tc.manifestCond, tc.wantManifestCond, ignoreFieldConditionLTTMsg); diff != "" {
				t.Errorf("set manifest cond mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestSetWorkAppliedCondition tests the setWorkAppliedCondition function.
func TestSetWorkAppliedCondition(t *testing.T) {
	testCases := []struct {
		name                     string
		work                     *fleetv1beta1.Work
		manifestCount            int
		appliedManifestCount     int
		wantWorkStatusConditions []metav1.Condition
	}{
		{
			name: "all applied",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Generation: 1,
				},
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
					},
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{},
				},
			},
			manifestCount:        2,
			appliedManifestCount: 2,
			wantWorkStatusConditions: []metav1.Condition{
				{
					Type:               fleetv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             WorkAllManifestsAppliedReason,
					ObservedGeneration: 1,
				},
			},
		},
		{
			name: "not all applied",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Generation: 2,
				},
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
					},
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             WorkAllManifestsAppliedReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			manifestCount:        2,
			appliedManifestCount: 1,
			wantWorkStatusConditions: []metav1.Condition{
				{
					Type:               fleetv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionFalse,
					Reason:             WorkNotAllManifestsAppliedReason,
					ObservedGeneration: 2,
				},
			},
		},
		{
			name: "no apply op performed",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Generation: 1,
				},
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
					},
				},
			},
			manifestCount:            2,
			appliedManifestCount:     0,
			wantWorkStatusConditions: []metav1.Condition{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setWorkAppliedCondition(tc.work, tc.manifestCount, tc.appliedManifestCount)
			if diff := cmp.Diff(
				tc.work.Status.Conditions, tc.wantWorkStatusConditions,
				ignoreFieldConditionLTTMsg, cmpopts.EquateEmpty(),
			); diff != "" {
				t.Errorf("set work status conditions mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestSetWorkAvailableCondition tests the setWorkAvailableCondition function.
func TestSetWorkAvailableCondition(t *testing.T) {
	testCases := []struct {
		name                     string
		work                     *fleetv1beta1.Work
		manifestCount            int
		availableManifestCount   int
		untrackableManifestCount int
		wantWorkStatusConditions []metav1.Condition
	}{
		{
			name: "all available and trackable",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Generation: 1,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             WorkAllManifestsAppliedReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			manifestCount:            2,
			availableManifestCount:   2,
			untrackableManifestCount: 0,
			wantWorkStatusConditions: []metav1.Condition{
				{
					Type:               fleetv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             WorkAllManifestsAppliedReason,
					ObservedGeneration: 1,
				},
				{
					Type:               fleetv1beta1.WorkConditionTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             WorkAllManifestsAvailableReason,
					ObservedGeneration: 1,
				},
			},
		},
		{
			name: "all available, partially untrackable",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Generation: 1,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             WorkAllManifestsAppliedReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			manifestCount:            2,
			availableManifestCount:   2,
			untrackableManifestCount: 1,
			wantWorkStatusConditions: []metav1.Condition{
				{
					Type:               fleetv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             WorkAllManifestsAppliedReason,
					ObservedGeneration: 1,
				},
				{
					Type:               fleetv1beta1.WorkConditionTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             WorkNotAllManifestsTrackableReason,
					ObservedGeneration: 1,
				},
			},
		},
		{
			name: "partially unavailable",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Generation: 1,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             WorkAllManifestsAppliedReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			manifestCount:            2,
			availableManifestCount:   1,
			untrackableManifestCount: 1,
			wantWorkStatusConditions: []metav1.Condition{
				{
					Type:               fleetv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             WorkAllManifestsAppliedReason,
					ObservedGeneration: 1,
				},
				{
					Type:               fleetv1beta1.WorkConditionTypeAvailable,
					Status:             metav1.ConditionFalse,
					Reason:             WorkNotAllManifestsAvailableReason,
					ObservedGeneration: 1,
				},
			},
		},
		{
			name: "not fully applied yet",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Generation: 2,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             WorkNotAllManifestsAppliedReason,
							ObservedGeneration: 2,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             WorkAllManifestsAvailableReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			manifestCount:            2,
			availableManifestCount:   1,
			untrackableManifestCount: 1,
			wantWorkStatusConditions: []metav1.Condition{
				{
					Type:               fleetv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionFalse,
					Reason:             WorkNotAllManifestsAppliedReason,
					ObservedGeneration: 2,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setWorkAvailableCondition(tc.work, tc.manifestCount, tc.availableManifestCount, tc.untrackableManifestCount)
			if diff := cmp.Diff(
				tc.work.Status.Conditions, tc.wantWorkStatusConditions,
				ignoreFieldConditionLTTMsg, cmpopts.EquateEmpty(),
			); diff != "" {
				t.Errorf("set work status conditions mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestSetWorkDiffReportedCondition tests the setWorkDiffReportedCondition function.
func TestSetWorkDiffReportedCondition(t *testing.T) {
	testCases := []struct {
		name                     string
		work                     *fleetv1beta1.Work
		manifestCount            int
		diffReportedObjectsCount int
		wantWorkStatusConditions []metav1.Condition
	}{
		{
			name: "not in report diff mode (no apply strategy)",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Generation: 1,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							Reason:             WorkAllManifestsDiffReportedReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			manifestCount:            2,
			diffReportedObjectsCount: 0,
			wantWorkStatusConditions: []metav1.Condition{},
		},
		{
			name: "not in report diff mode (apply strategy is not report diff)",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Generation: 1,
				},
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
					},
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							Reason:             WorkAllManifestsDiffReportedReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			manifestCount:            2,
			diffReportedObjectsCount: 0,
			wantWorkStatusConditions: []metav1.Condition{},
		},
		{
			name: "all diff reported",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Generation: 1,
				},
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
					},
				},
			},
			manifestCount:            2,
			diffReportedObjectsCount: 2,
			wantWorkStatusConditions: []metav1.Condition{
				{
					Type:               fleetv1beta1.WorkConditionTypeDiffReported,
					Status:             metav1.ConditionTrue,
					Reason:             WorkAllManifestsDiffReportedReason,
					ObservedGeneration: 1,
				},
			},
		},
		{
			name: "not all diff reported",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:       workName,
					Generation: 1,
				},
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
					},
				},
			},
			manifestCount:            2,
			diffReportedObjectsCount: 1,
			wantWorkStatusConditions: []metav1.Condition{
				{
					Type:               fleetv1beta1.WorkConditionTypeDiffReported,
					Status:             metav1.ConditionFalse,
					Reason:             WorkNotAllManifestsDiffReportedReason,
					ObservedGeneration: 1,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setWorkDiffReportedCondition(tc.work, tc.manifestCount, tc.diffReportedObjectsCount)
			if diff := cmp.Diff(
				tc.work.Status.Conditions, tc.wantWorkStatusConditions,
				ignoreFieldConditionLTTMsg, cmpopts.EquateEmpty(),
			); diff != "" {
				t.Errorf("set work status conditions mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}
