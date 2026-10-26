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

package v1alpha1

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
)

const (
	updateStrategyNameTemplate = "test-update-strategy-%d"
	updateRunNameTemplate      = "test-update-run-%d"
	stageName                  = "stage1"
	placementPolicyName        = "test-placement-policy"
	updateStrategyName         = "test-update-strategy"

	maxConcurrencyTooLowMsg         = "maxConcurrency must be at least 1"
	maxConcurrencyTooHighMsg        = "maxConcurrency must be at most 1000"
	waitTimeTooLongMsg              = "waitTime must be at most 720h (30 days)"
	waitTimeTooManyCharsMsg         = "spec.stages[0].afterStageTasks[0].waitTime: Too long"
	maxClusterFailuresTooHighMsg    = "spec.failurePolicy.maxAllowedClusterFailures in body should be less than or equal to 1000"
	maxClusterFailuresTooLowMsg     = "spec.failurePolicy.maxAllowedClusterFailures in body should be greater than or equal to 0"
	maxWaitTimePerClusterTooHighMsg = "spec.failurePolicy.maxWaitTimePerClusterMinutes in body should be less than or equal to 10080"
)

// createAndCheckValidation creates the given object and verifies that the API server accepts it
// (when wantMessage is empty) or rejects it with an error that contains wantMessage. Accepted
// objects are deleted when the current spec completes.
func createAndCheckValidation(obj client.Object, wantMessage string) {
	GinkgoHelper()

	err := hubClient.Create(ctx, obj)
	if wantMessage == "" {
		Expect(err).To(Succeed(), "Expected the object to be accepted")
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed())
		})
		return
	}
	Expect(err).To(HaveOccurred(), "Expected the object to be rejected")
	var statusErr *k8sErrors.StatusError
	Expect(errors.As(err, &statusErr)).To(BeTrue(), "The returned error is not a StatusError")
	Expect(statusErr.Status().Message).Should(ContainSubstring(wantMessage))
}

// timedWaitAfterStageTasks returns an after-stage task list with a single TimedWait task.
func timedWaitAfterStageTasks(waitTime time.Duration) []rolloutv1alpha1.StageTask {
	return []rolloutv1alpha1.StageTask{
		{
			Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
			WaitTime: &metav1.Duration{Duration: waitTime},
		},
	}
}

var _ = Describe("Test rollout v1alpha1 API validation", func() {
	Context("Test StagedUpdateStrategy API validation", func() {
		// StagedUpdateStrategy and ClusterStagedUpdateStrategy share the same spec type, so each
		// case runs against both kinds.
		DescribeTable("the bounds of a stage",
			func(stage rolloutv1alpha1.Stage, wantMessage string) {
				name := fmt.Sprintf(updateStrategyNameTemplate, GinkgoParallelProcess())
				stage.Name = stageName
				spec := rolloutv1alpha1.StagedUpdateStrategySpec{
					Stages: []rolloutv1alpha1.Stage{stage},
				}

				createAndCheckValidation(&rolloutv1alpha1.StagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: metav1.NamespaceDefault,
					},
					Spec: *spec.DeepCopy(),
				}, wantMessage)
				createAndCheckValidation(&rolloutv1alpha1.ClusterStagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name: name,
					},
					Spec: *spec.DeepCopy(),
				}, wantMessage)
			},
			Entry("an integer maxConcurrency of 1000 is accepted", rolloutv1alpha1.Stage{
				MaxConcurrency: ptr.To(intstr.FromInt32(1000)),
			}, ""),
			Entry("a percentage maxConcurrency of 100% is accepted", rolloutv1alpha1.Stage{
				MaxConcurrency: ptr.To(intstr.FromString("100%")),
			}, ""),
			Entry("an integer maxConcurrency of 0 is rejected", rolloutv1alpha1.Stage{
				MaxConcurrency: ptr.To(intstr.FromInt32(0)),
			}, maxConcurrencyTooLowMsg),
			Entry("an integer maxConcurrency of 1001 is rejected", rolloutv1alpha1.Stage{
				MaxConcurrency: ptr.To(intstr.FromInt32(1001)),
			}, maxConcurrencyTooHighMsg),
			Entry("a wait time of 720h is accepted", rolloutv1alpha1.Stage{
				AfterStageTasks: timedWaitAfterStageTasks(720 * time.Hour),
			}, ""),
			Entry("a wait time of 720h1s is rejected", rolloutv1alpha1.Stage{
				AfterStageTasks: timedWaitAfterStageTasks(720*time.Hour + time.Second),
			}, waitTimeTooLongMsg),
			Entry("the maximum wait time a client can encode is rejected", rolloutv1alpha1.Stage{
				AfterStageTasks: timedWaitAfterStageTasks(time.Duration(math.MaxInt64)),
			}, waitTimeTooLongMsg),
		)

		// The typed client always encodes a wait time in its canonical form (e.g., 1h30m0s), so
		// these cases use unstructured objects to send forms that only a raw manifest can carry.
		DescribeTable("the raw wait time of a TimedWait task",
			func(waitTime string, wantMessage string) {
				name := fmt.Sprintf(updateStrategyNameTemplate, GinkgoParallelProcess())
				for _, kind := range []string{rolloutv1alpha1.StagedUpdateStrategyKind, rolloutv1alpha1.ClusterStagedUpdateStrategyKind} {
					obj := &unstructured.Unstructured{
						Object: map[string]interface{}{
							"spec": map[string]interface{}{
								"stages": []interface{}{
									map[string]interface{}{
										"name": stageName,
										"afterStageTasks": []interface{}{
											map[string]interface{}{
												"type":     string(rolloutv1alpha1.StageTaskTypeTimedWait),
												"waitTime": waitTime,
											},
										},
									},
								},
							},
						},
					}
					obj.SetGroupVersionKind(rolloutv1alpha1.GroupVersion.WithKind(kind))
					obj.SetName(name)
					if kind == rolloutv1alpha1.StagedUpdateStrategyKind {
						obj.SetNamespace(metav1.NamespaceDefault)
					}
					createAndCheckValidation(obj, wantMessage)
				}
			},
			Entry("a fractional wait time is accepted", "1.5h", ""),
			Entry("720h expressed in minutes is accepted", "43200m", ""),
			Entry("a wait time of 32 characters is accepted", strings.Repeat("1s", 16), ""),
			Entry("a wait time over 720h expressed in minutes is rejected", "43201m", waitTimeTooLongMsg),
			// The value overflows a duration; the API server cannot evaluate the rule on it, and
			// reports the failure under the rule's message.
			Entry("a wait time that overflows a duration is rejected", "9999999999999999999h", waitTimeTooLongMsg),
			Entry("a wait time of 33 characters is rejected", "1"+strings.Repeat("0", 31)+"s", waitTimeTooManyCharsMsg),
		)
	})

	Context("Test StagedUpdateRun API validation", func() {
		// StagedUpdateRun and ClusterStagedUpdateRun share the same spec type, so each case runs
		// against both kinds.
		DescribeTable("the bounds of a failure policy",
			func(failurePolicy rolloutv1alpha1.StagedUpdateRunFailurePolicy, wantMessage string) {
				name := fmt.Sprintf(updateRunNameTemplate, GinkgoParallelProcess())
				spec := rolloutv1alpha1.StagedUpdateRunSpec{
					PlacementPolicyName:      placementPolicyName,
					StagedUpdateStrategyName: updateStrategyName,
					FailurePolicy:            &failurePolicy,
				}

				createAndCheckValidation(&rolloutv1alpha1.StagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: metav1.NamespaceDefault,
					},
					Spec: *spec.DeepCopy(),
				}, wantMessage)
				createAndCheckValidation(&rolloutv1alpha1.ClusterStagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name: name,
					},
					Spec: *spec.DeepCopy(),
				}, wantMessage)
			},
			Entry("the default failure policy is accepted", rolloutv1alpha1.StagedUpdateRunFailurePolicy{}, ""),
			Entry("maxAllowedClusterFailures of 0 is accepted", rolloutv1alpha1.StagedUpdateRunFailurePolicy{
				MaxAllowedClusterFailures: ptr.To(int32(0)),
			}, ""),
			Entry("maxAllowedClusterFailures of 1000 is accepted", rolloutv1alpha1.StagedUpdateRunFailurePolicy{
				MaxAllowedClusterFailures: ptr.To(int32(1000)),
			}, ""),
			Entry("maxAllowedClusterFailures of -1 is rejected", rolloutv1alpha1.StagedUpdateRunFailurePolicy{
				MaxAllowedClusterFailures: ptr.To(int32(-1)),
			}, maxClusterFailuresTooLowMsg),
			Entry("maxAllowedClusterFailures of 1001 is rejected", rolloutv1alpha1.StagedUpdateRunFailurePolicy{
				MaxAllowedClusterFailures: ptr.To(int32(1001)),
			}, maxClusterFailuresTooHighMsg),
			Entry("maxWaitTimePerClusterMinutes of 10080 is accepted", rolloutv1alpha1.StagedUpdateRunFailurePolicy{
				MaxWaitTimePerClusterMinutes: 10080,
			}, ""),
			Entry("maxWaitTimePerClusterMinutes of 10081 is rejected", rolloutv1alpha1.StagedUpdateRunFailurePolicy{
				MaxWaitTimePerClusterMinutes: 10081,
			}, maxWaitTimePerClusterTooHighMsg),
		)
	})
})
