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
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
)

// Note (chenyu1): all test cases in this file use a separate test environment
// (same hub cluster, different fleet member reserved namespace, different
// work applier instance) from the other integration tests. This is needed
// to (relatively speaking) reliably verify the exponential backoff behavior
// in the work applier.

var (
	diffObservationTimeChangedActual = func(
		workName string,
		wantWorkStatus *fleetv1beta1.WorkStatus,
		curDiffObservedTime, lastDiffObservedTime *metav1.Time,
	) func() error {
		return func() error {
			// Retrieve the Work object.
			work := &fleetv1beta1.Work{}
			if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName2}, work); err != nil {
				return fmt.Errorf("failed to retrieve the Work object: %w", err)
			}

			// Verify that the status never changed (except for timestamps).
			if diff := cmp.Diff(
				&work.Status, wantWorkStatus,
				ignoreFieldConditionLTTMsg,
				ignoreDiffDetailsObsTime, ignoreDriftDetailsObsTime,
				cmpopts.SortSlices(lessFuncPatchDetail),
			); diff != "" {
				StopTrying("the work object status has changed unexpectedly").Wrap(fmt.Errorf("work status diff (-got, +want):\n%s", diff)).Now()
			}

			curDiffObservedTime.Time = work.Status.ManifestConditions[0].DiffDetails.ObservationTime.Time
			if !curDiffObservedTime.Equal(lastDiffObservedTime) {
				return nil
			}
			return fmt.Errorf("the diff observation time remains unchanged")
		}
	}

	driftObservationTimeChangedActual = func(
		workName string,
		wantWorkStatus *fleetv1beta1.WorkStatus,
		curDriftObservedTime, lastDriftObservedTime *metav1.Time,
	) func() error {
		return func() error {
			// Retrieve the Work object.
			work := &fleetv1beta1.Work{}
			if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName2}, work); err != nil {
				return fmt.Errorf("failed to retrieve the Work object: %w", err)
			}

			// Verify that the status never changed (except for timestamps).
			if diff := cmp.Diff(
				&work.Status, wantWorkStatus,
				ignoreFieldConditionLTTMsg,
				ignoreDiffDetailsObsTime, ignoreDriftDetailsObsTime,
				cmpopts.SortSlices(lessFuncPatchDetail),
			); diff != "" {
				StopTrying("the work object status has changed unexpectedly").Wrap(fmt.Errorf("work status diff (-got, +want):\n%s", diff)).Now()
			}

			curDriftObservedTime.Time = work.Status.ManifestConditions[0].DriftDetails.ObservationTime.Time
			if !curDriftObservedTime.Equal(lastDriftObservedTime) {
				return nil
			}
			return fmt.Errorf("the drift observation time remains unchanged")
		}
	}
)

var _ = Describe("exponential backoff", func() {
	Context("slow backoff and fast backoff", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var regularNS *corev1.Namespace
		var lastDiffObservedTime *metav1.Time

		wantWorkStatus := &fleetv1beta1.WorkStatus{
			Conditions: []metav1.Condition{
				{
					Type:               fleetv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionFalse,
					Reason:             condition.WorkNotAllManifestsAppliedReason,
					ObservedGeneration: 1,
				},
			},
			ManifestConditions: []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyOrReportDiffResTypeFailedToTakeOver),
							ObservedGeneration: 0,
						},
					},
					DiffDetails: &fleetv1beta1.DiffDetails{
						ObservedInMemberClusterGeneration: ptr.To(int64(0)),
						ObservedDiffs: []fleetv1beta1.PatchDetail{
							{
								Path:          fmt.Sprintf("/metadata/labels/%s", dummyLabelKey),
								ValueInMember: dummyLabelValue2,
								ValueInHub:    dummyLabelValue1,
							},
						},
					},
				},
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNS.Labels = map[string]string{
				dummyLabelKey: dummyLabelValue1,
			}
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Create the NS object on the member cluster, with a different label value.
			preExistingNS := ns.DeepCopy()
			preExistingNS.Name = nsName
			preExistingNS.Labels = map[string]string{
				dummyLabelKey: dummyLabelValue2,
			}
			Expect(memberClient2.Create(ctx, preExistingNS)).To(Succeed(), "Failed to create pre-existing NS")

			// Create a new Work object with all the manifest JSONs.
			//
			// This Work object uses an apply strategy that allows takeover but will check for diffs (partial
			// comparison). Due to the presence of diffs, the Work object will be considered to be of a state
			// of apply op failure.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				WhenToTakeOver: fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
			}
			createWorkObject(workName, memberReservedNSName2, applyStrategy, regularNSJSON)
		})

		// For simplicity reasons, this test case will skip some of the regular apply op result verification
		// (finalizer check, AppliedWork object check, etc.).

		It("should update the Work object status", func() {
			// Prepare the status information.

			// Use custom check logic so that the test case can track timestamps across steps.
			Eventually(func() error {
				// Retrieve the Work object.
				work := &fleetv1beta1.Work{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName2}, work); err != nil {
					return fmt.Errorf("failed to retrieve the Work object: %w", err)
				}

				if diff := cmp.Diff(
					&work.Status, wantWorkStatus,
					ignoreFieldConditionLTTMsg,
					ignoreDiffDetailsObsTime, ignoreDriftDetailsObsTime,
					cmpopts.SortSlices(lessFuncPatchDetail),
				); diff != "" {
					return fmt.Errorf("work status diff (-got, +want):\n%s", diff)
				}

				// Track the observation timestamp of the diff details.
				lastDiffObservedTime = &work.Status.ManifestConditions[0].DiffDetails.ObservationTime
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update Work object status")
		})

		It("should wait for the fixed delay period of time before next reconciliation", func() {
			curDiffObservedTime := &metav1.Time{}

			// Need to poll a bit longer to avoid flakiness.
			Eventually(diffObservationTimeChangedActual(workName, wantWorkStatus, curDiffObservedTime, lastDiffObservedTime), eventuallyDuration*2, eventuallyInterval).Should(Or(Succeed()))

			// Fixed delay is set to 10 seconds. Give a one-sec leeway to avoid flakiness.
			Expect(curDiffObservedTime.Sub(lastDiffObservedTime.Time)).To(And(
				BeNumerically(">=", time.Second*9),
				BeNumerically("<=", time.Second*11),
			), "the interval between two observations is not as expected")

			// Update the tracked observation time.
			lastDiffObservedTime = curDiffObservedTime
		})

		It("should start to back off slowly (attempt 1)", func() {
			curDiffObservedTime := &metav1.Time{}

			// Need to poll a bit longer to avoid flakiness.
			Eventually(diffObservationTimeChangedActual(workName, wantWorkStatus, curDiffObservedTime, lastDiffObservedTime), eventuallyDuration*3, eventuallyInterval).Should(Or(Succeed()))

			// The first slow backoff delay is 20 seconds. Give a one-sec leeway to avoid flakiness.
			Expect(curDiffObservedTime.Sub(lastDiffObservedTime.Time)).To(And(
				BeNumerically(">=", time.Second*19),
				BeNumerically("<=", time.Second*21),
			), "the interval between two observations is not as expected")

			// Update the tracked observation time.
			lastDiffObservedTime = curDiffObservedTime
		})

		It("should start to back off slowly (attempt 2)", func() {
			curDiffObservedTime := &metav1.Time{}

			// Need to poll a bit longer to avoid flakiness.
			Eventually(diffObservationTimeChangedActual(workName, wantWorkStatus, curDiffObservedTime, lastDiffObservedTime), eventuallyDuration*4, eventuallyInterval).Should(Or(Succeed()))

			// The second slow backoff delay is 30 seconds. Give a one-sec leeway to avoid flakiness.
			Expect(curDiffObservedTime.Sub(lastDiffObservedTime.Time)).To(And(
				BeNumerically(">=", time.Second*29),
				BeNumerically("<=", time.Second*31),
			), "the interval between two observations is not as expected")

			// Update the tracked observation time.
			lastDiffObservedTime = curDiffObservedTime
		})

		It("should start to back off fastly (attempt #1)", func() {
			curDiffObservedTime := &metav1.Time{}

			// Need to poll a bit longer to avoid flakiness.
			Eventually(diffObservationTimeChangedActual(workName, wantWorkStatus, curDiffObservedTime, lastDiffObservedTime), eventuallyDuration*7, eventuallyInterval).Should(Or(Succeed()))

			// The first fast backoff delay is 60 seconds. Give a one-sec leeway to avoid flakiness.
			Expect(curDiffObservedTime.Sub(lastDiffObservedTime.Time)).To(And(
				BeNumerically(">=", time.Second*59),
				BeNumerically("<=", time.Second*61),
			), "the interval between two observations is not as expected")

			// Update the tracked observation time.
			lastDiffObservedTime = curDiffObservedTime
		})

		It("should reach maximum backoff", func() {
			curDiffObservedTime := &metav1.Time{}

			// Need to poll a bit longer to avoid flakiness.
			Eventually(diffObservationTimeChangedActual(workName, wantWorkStatus, curDiffObservedTime, lastDiffObservedTime), eventuallyDuration*10, eventuallyInterval).Should(Or(Succeed()))

			// The maximum backoff delay is 60 seconds. Give a one-sec leeway to avoid flakiness.
			Expect(curDiffObservedTime.Sub(lastDiffObservedTime.Time)).To(And(
				BeNumerically(">=", time.Second*89),
				BeNumerically("<=", time.Second*91),
			), "the interval between two observations is not as expected")

			// Update the tracked observation time.
			lastDiffObservedTime = curDiffObservedTime
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			deleteWorkObject(workName, memberReservedNSName2)

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName, nsName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the AppliedWork object")

			workRemovedActual := workRemovedActual(workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the Work object")
			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("skip to fast backoff (available)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var regularNS *corev1.Namespace
		var lastDriftObservedTime *metav1.Time

		wantWorkStatus := &fleetv1beta1.WorkStatus{
			Conditions: []metav1.Condition{
				{
					Type:               fleetv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             condition.WorkAllManifestsAppliedReason,
					ObservedGeneration: 1,
				},
				{
					Type:               fleetv1beta1.WorkConditionTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             condition.WorkAllManifestsAvailableReason,
					ObservedGeneration: 1,
				},
			},
			ManifestConditions: []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyOrReportDiffResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
					DriftDetails: &fleetv1beta1.DriftDetails{
						ObservedInMemberClusterGeneration: 0,
						ObservedDrifts: []fleetv1beta1.PatchDetail{
							{
								Path:          fmt.Sprintf("/metadata/labels/%s", dummyLabelKey),
								ValueInMember: dummyLabelValue2,
							},
							{
								Path:          "/spec/finalizers",
								ValueInMember: "[kubernetes]",
							},
						},
					},
				},
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Create the NS object on the member cluster, with a different label value.
			preExistingNS := ns.DeepCopy()
			preExistingNS.Name = nsName
			preExistingNS.Labels = map[string]string{
				dummyLabelKey: dummyLabelValue2,
			}
			Expect(memberClient2.Create(ctx, preExistingNS)).To(Succeed(), "Failed to create pre-existing NS")

			// Create a new Work object with all the manifest JSONs.
			//
			// This Work object uses an apply strategy that always take over and use full
			// comparison for drift detection. Apply op will be successful, and namespaces are
			// considered to be immediately available after creation; Fleet will report the label
			// differences as drifts without blocking the apply ops.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
				WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeAlways,
				WhenToApply:      fleetv1beta1.WhenToApplyTypeAlways,
			}
			createWorkObject(workName, memberReservedNSName2, applyStrategy, regularNSJSON)
		})

		// For simplicity reasons, this test case will skip some of the regular apply op result verification
		// (finalizer check, AppliedWork object check, etc.).

		It("should update the Work object status", func() {
			// Prepare the status information.

			// Use custom check logic so that the test case can track timestamps across steps.
			Eventually(func() error {
				// Retrieve the Work object.
				work := &fleetv1beta1.Work{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName2}, work); err != nil {
					return fmt.Errorf("failed to retrieve the Work object: %w", err)
				}

				if diff := cmp.Diff(
					&work.Status, wantWorkStatus,
					ignoreFieldConditionLTTMsg,
					ignoreDiffDetailsObsTime, ignoreDriftDetailsObsTime,
					cmpopts.SortSlices(lessFuncPatchDetail),
				); diff != "" {
					return fmt.Errorf("work status diff (-got, +want):\n%s", diff)
				}

				// Track the observation timestamp of the diff details.
				lastDriftObservedTime = &work.Status.ManifestConditions[0].DriftDetails.ObservationTime
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update Work object status")
		})

		It("should wait for the fixed delay period of time before next reconciliation", func() {
			curDriftObservedTime := &metav1.Time{}

			// Need to poll a bit longer to avoid flakiness.
			Eventually(driftObservationTimeChangedActual(workName, wantWorkStatus, curDriftObservedTime, lastDriftObservedTime), eventuallyDuration*2, eventuallyInterval).Should(Or(Succeed()))

			// Fixed delay is set to 10 seconds. Give a one-sec leeway to avoid flakiness.
			Expect(curDriftObservedTime.Sub(lastDriftObservedTime.Time)).To(And(
				BeNumerically(">=", time.Second*9),
				BeNumerically("<=", time.Second*11),
			), "the interval between two observations is not as expected")

			// Update the tracked observation time.
			lastDriftObservedTime = curDriftObservedTime
		})

		It("should start to back off slowly (attempt #1)", func() {
			curDriftObservedTime := &metav1.Time{}

			// Need to poll a bit longer to avoid flakiness.
			Eventually(driftObservationTimeChangedActual(workName, wantWorkStatus, curDriftObservedTime, lastDriftObservedTime), eventuallyDuration*3, eventuallyInterval).Should(Or(Succeed()))

			// The first fast backoff delay is 60 seconds. Give a one-sec leeway to avoid flakiness.
			Expect(curDriftObservedTime.Sub(lastDriftObservedTime.Time)).To(And(
				BeNumerically(">=", time.Second*19),
				BeNumerically("<=", time.Second*21),
			), "the interval between two observations is not as expected")

			// Update the tracked observation time.
			lastDriftObservedTime = curDriftObservedTime
		})

		It("should skip to backing off fastly (attempt #1)", func() {
			curDriftObservedTime := &metav1.Time{}

			// Need to poll a bit longer to avoid flakiness.
			Eventually(driftObservationTimeChangedActual(workName, wantWorkStatus, curDriftObservedTime, lastDriftObservedTime), eventuallyDuration*5, eventuallyInterval).Should(Or(Succeed()))

			// The first fast backoff delay is 40 seconds. Give a one-sec leeway to avoid flakiness.
			Expect(curDriftObservedTime.Sub(lastDriftObservedTime.Time)).To(And(
				BeNumerically(">=", time.Second*39),
				BeNumerically("<=", time.Second*41),
			), "the interval between two observations is not as expected")

			// Update the tracked observation time.
			lastDriftObservedTime = curDriftObservedTime
		})

		It("should skip to backing off fastly (attempt #2)", func() {
			curDriftObservedTime := &metav1.Time{}

			// Need to poll a bit longer to avoid flakiness.
			Eventually(driftObservationTimeChangedActual(workName, wantWorkStatus, curDriftObservedTime, lastDriftObservedTime), eventuallyDuration*9, eventuallyInterval).Should(Or(Succeed()))

			// The second fast backoff delay is 80 seconds. Give a one-sec leeway to avoid flakiness.
			Expect(curDriftObservedTime.Sub(lastDriftObservedTime.Time)).To(And(
				BeNumerically(">=", time.Second*79),
				BeNumerically("<=", time.Second*81),
			), "the interval between two observations is not as expected")

			// Update the tracked observation time.
			lastDriftObservedTime = curDriftObservedTime
		})

		It("should reach maximum backoff", func() {
			curDriftObservedTime := &metav1.Time{}

			// Need to poll a bit longer to avoid flakiness.
			Eventually(driftObservationTimeChangedActual(workName, wantWorkStatus, curDriftObservedTime, lastDriftObservedTime), eventuallyDuration*10, eventuallyInterval).Should(Or(Succeed()))

			// The maximum backoff delay is 60 seconds. Give a one-sec leeway to avoid flakiness.
			Expect(curDriftObservedTime.Sub(lastDriftObservedTime.Time)).To(And(
				BeNumerically(">=", time.Second*89),
				BeNumerically("<=", time.Second*91),
			), "the interval between two observations is not as expected")

			// Update the tracked observation time.
			lastDriftObservedTime = curDriftObservedTime
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			deleteWorkObject(workName, memberReservedNSName2)

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName, nsName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the AppliedWork object")

			workRemovedActual := workRemovedActual(workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the Work object")
			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})
})
