/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/prometheus/client_golang/prometheus"
	prometheusclientmodel "github.com/prometheus/client_model/go"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller/metrics"
	metricsutils "go.goms.io/fleet/pkg/utils/metrics"
)

const (
	// timeout is the maximum wait time for Eventually
	timeout = time.Second * 10
	// interval is the time to wait between retries for Eventually and Consistently
	interval = time.Millisecond * 250
	// duration is the time to duration to check for Consistently
	duration = time.Second * 20

	// numTargetClusters is the number of scheduled clusters
	numTargetClusters = 10
	// numUnscheduledClusters is the number of unscheduled clusters
	numUnscheduledClusters = 3
	// numberOfClustersAnnotation is the number of clusters in the test latest policy snapshot
	numberOfClustersAnnotation = numTargetClusters

	// testResourceSnapshotIndex is the index of the test resource snapshot
	testResourceSnapshotIndex = "0"
)

var (
	testUpdateRunName        string
	testCRPName              string
	testResourceSnapshotName string
	testUpdateStrategyName   string
	testCROName              string
	updateRunNamespacedName  types.NamespacedName
	testNamespace            []byte
	customRegistry           *prometheus.Registry
)

var _ = Describe("Test the clusterStagedUpdateRun controller", func() {

	BeforeEach(func() {
		testUpdateRunName = "updaterun-" + utils.RandStr()
		testCRPName = "crp-" + utils.RandStr()
		testResourceSnapshotName = testCRPName + "-" + testResourceSnapshotIndex + "-snapshot"
		testUpdateStrategyName = "updatestrategy-" + utils.RandStr()
		testCROName = "cro-" + utils.RandStr()
		updateRunNamespacedName = types.NamespacedName{Name: testUpdateRunName}

		customRegistry = initializeUpdateRunMetricsRegistry()
	})

	AfterEach(func() {
		By("Checking the update run status metrics are removed")
		// No metrics are emitted as all are removed after updateRun is deleted.
		validateUpdateRunMetricsEmitted(customRegistry)
		unregisterUpdateRunMetrics(customRegistry)
	})

	Context("Test reconciling a clusterStagedUpdateRun", func() {
		It("Should add the finalizer to the clusterStagedUpdateRun", func() {
			By("Creating a new clusterStagedUpdateRun")
			updateRun := generateTestClusterStagedUpdateRun()
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())

			By("Checking the finalizer is added")
			validateUpdateRunHasFinalizer(ctx, updateRun)

			By("Deleting the clusterStagedUpdateRun")
			Expect(k8sClient.Delete(ctx, updateRun)).Should(Succeed())

			By("Checking the clusterStagedUpdateRun is deleted")
			validateUpdateRunIsDeleted(ctx, updateRunNamespacedName)
		})
	})

	Context("Test deleting a clusterStagedUpdateRun", func() {
		It("Should delete the clusterStagedUpdateRun without any clusterApprovalRequests", func() {
			By("Creating a new clusterStagedUpdateRun")
			updateRun := generateTestClusterStagedUpdateRun()
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())

			By("Checking the finalizer is added")
			validateUpdateRunHasFinalizer(ctx, updateRun)

			By("Deleting the clusterStagedUpdateRun")
			Expect(k8sClient.Delete(ctx, updateRun)).Should(Succeed())

			By("Checking the clusterStagedUpdateRun is deleted")
			validateUpdateRunIsDeleted(ctx, updateRunNamespacedName)
		})

		It("Should delete the clusterStagedUpdateRun if it failed", func() {
			By("Creating a new clusterStagedUpdateRun")
			updateRun := generateTestClusterStagedUpdateRun()
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())

			By("Checking the finalizer is added")
			validateUpdateRunHasFinalizer(ctx, updateRun)

			By("Updating the clusterStagedUpdateRun to failed")
			startedcond := generateTrueCondition(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing)
			finishedcond := generateFalseCondition(updateRun, placementv1beta1.StagedUpdateRunConditionSucceeded)
			meta.SetStatusCondition(&updateRun.Status.Conditions, startedcond)
			meta.SetStatusCondition(&updateRun.Status.Conditions, finishedcond)
			Expect(k8sClient.Status().Update(ctx, updateRun)).Should(Succeed(), "failed to update the clusterStagedUpdateRun")

			By("Creating a clusterApprovalRequest")
			approvalRequest := generateTestApprovalRequest("req1")
			Expect(k8sClient.Create(ctx, approvalRequest)).Should(Succeed())

			By("Deleting the clusterStagedUpdateRun")
			Expect(k8sClient.Delete(ctx, updateRun)).Should(Succeed())

			By("Checking the clusterStagedUpdateRun is deleted")
			validateUpdateRunIsDeleted(ctx, updateRunNamespacedName)

			By("Checking the clusterApprovalRequest is deleted")
			validateApprovalRequestCount(ctx, 0)
		})

		It("Should not block deletion though the clusterStagedUpdateRun is still processing", func() {
			By("Creating a new clusterStagedUpdateRun")
			updateRun := generateTestClusterStagedUpdateRun()
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())

			By("Checking the finalizer is added")
			validateUpdateRunHasFinalizer(ctx, updateRun)

			By("Updating the clusterStagedUpdateRun status to processing")
			startedcond := generateTrueCondition(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing)
			meta.SetStatusCondition(&updateRun.Status.Conditions, startedcond)
			Expect(k8sClient.Status().Update(ctx, updateRun)).Should(Succeed(), "failed to add condition to the clusterStagedUpdateRun")

			By("Creating a clusterApprovalRequest")
			approvalRequest := generateTestApprovalRequest("req1")
			Expect(k8sClient.Create(ctx, approvalRequest)).Should(Succeed())

			By("Deleting the clusterStagedUpdateRun")
			Expect(k8sClient.Delete(ctx, updateRun)).Should(Succeed())

			By("Checking the clusterStagedUpdateRun is deleted")
			validateUpdateRunIsDeleted(ctx, updateRunNamespacedName)

			By("Checking the clusterApprovalRequest is deleted")
			validateApprovalRequestCount(ctx, 0)
		})

		It("Should delete all ClusterApprovalRequest objects associated with the clusterStagedUpdateRun", func() {
			By("Creating a new clusterStagedUpdateRun")
			updateRun := generateTestClusterStagedUpdateRun()
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())

			By("Creating ClusterApprovalRequests")
			approvalRequests := []*placementv1beta1.ClusterApprovalRequest{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "req1",
						Labels: map[string]string{
							placementv1beta1.TargetUpdateRunLabel: testUpdateRunName,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "req2",
						Labels: map[string]string{
							placementv1beta1.TargetUpdateRunLabel: testUpdateRunName,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "req3",
						Labels: map[string]string{
							placementv1beta1.TargetUpdateRunLabel: testUpdateRunName + "1", // different update run
						},
					},
				},
			}
			for _, req := range approvalRequests {
				Expect(k8sClient.Create(ctx, req)).Should(Succeed())
			}

			By("Checking the finalizer is added")
			validateUpdateRunHasFinalizer(ctx, updateRun)

			By("Deleting the clusterStagedUpdateRun")
			Expect(k8sClient.Delete(ctx, updateRun)).Should(Succeed())

			By("Checking the clusterStagedUpdateRun is deleted")
			validateUpdateRunIsDeleted(ctx, updateRunNamespacedName)

			By("Checking the clusterApprovalRequests are deleted")
			validateApprovalRequestCount(ctx, 1)
		})

	})
})

func initializeUpdateRunMetricsRegistry() *prometheus.Registry {
	// Create a test registry
	customRegistry := prometheus.NewRegistry()
	Expect(customRegistry.Register(metrics.FleetUpdateRunStatusLastTimestampSeconds)).Should(Succeed())
	// Reset metrics before each test
	metrics.FleetUpdateRunStatusLastTimestampSeconds.Reset()
	return customRegistry
}

func unregisterUpdateRunMetrics(registry *prometheus.Registry) {
	Expect(registry.Unregister(metrics.FleetUpdateRunStatusLastTimestampSeconds)).Should(BeTrue())
}

// validateUpdateRunMetricsEmitted validates the update run status metrics are emitted and are emitted in the correct order.
func validateUpdateRunMetricsEmitted(registry *prometheus.Registry, wantMetrics ...*prometheusclientmodel.Metric) {
	Eventually(func() error {
		metricFamilies, err := registry.Gather()
		if err != nil {
			return fmt.Errorf("failed to gather metrics: %w", err)
		}
		var gotMetrics []*prometheusclientmodel.Metric
		for _, mf := range metricFamilies {
			if mf.GetName() == "fleet_workload_update_run_status_last_timestamp_seconds" {
				gotMetrics = mf.GetMetric()
			}
		}

		if diff := cmp.Diff(gotMetrics, wantMetrics, metricsutils.MetricsCmpOptions...); diff != "" {
			return fmt.Errorf("update run status metrics mismatch (-got, +want):\n%s", diff)
		}

		return nil
	}, timeout, interval).Should(Succeed(), "failed to validate the update run status metrics")
}

func generateMetricsLabels(
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
	condition, status, reason string,
) []*prometheusclientmodel.LabelPair {
	return []*prometheusclientmodel.LabelPair{
		{Name: ptr.To("name"), Value: &updateRun.Name},
		{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(updateRun.Generation, 10))},
		{Name: ptr.To("condition"), Value: ptr.To(condition)},
		{Name: ptr.To("status"), Value: ptr.To(status)},
		{Name: ptr.To("reason"), Value: ptr.To(reason)},
	}
}

func generateInitializationFailedMetric(updateRun *placementv1beta1.ClusterStagedUpdateRun) *prometheusclientmodel.Metric {
	return &prometheusclientmodel.Metric{
		Label: generateMetricsLabels(updateRun, string(placementv1beta1.StagedUpdateRunConditionInitialized),
			string(metav1.ConditionFalse), condition.UpdateRunInitializeFailedReason),
		Gauge: &prometheusclientmodel.Gauge{
			Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
		},
	}
}

func generateProgressingMetric(updateRun *placementv1beta1.ClusterStagedUpdateRun) *prometheusclientmodel.Metric {
	return &prometheusclientmodel.Metric{
		Label: generateMetricsLabels(updateRun, string(placementv1beta1.StagedUpdateRunConditionProgressing),
			string(metav1.ConditionTrue), condition.UpdateRunStartedReason),
		Gauge: &prometheusclientmodel.Gauge{
			Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
		},
	}
}

func generateWaitingMetric(updateRun *placementv1beta1.ClusterStagedUpdateRun) *prometheusclientmodel.Metric {
	return &prometheusclientmodel.Metric{
		Label: generateMetricsLabels(updateRun, string(placementv1beta1.StagedUpdateRunConditionProgressing),
			string(metav1.ConditionFalse), condition.UpdateRunWaitingReason),
		Gauge: &prometheusclientmodel.Gauge{
			Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
		},
	}
}

func generateStuckMetric(updateRun *placementv1beta1.ClusterStagedUpdateRun) *prometheusclientmodel.Metric {
	return &prometheusclientmodel.Metric{
		Label: generateMetricsLabels(updateRun, string(placementv1beta1.StagedUpdateRunConditionProgressing),
			string(metav1.ConditionFalse), condition.UpdateRunStuckReason),
		Gauge: &prometheusclientmodel.Gauge{
			Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
		},
	}
}

func generateFailedMetric(updateRun *placementv1beta1.ClusterStagedUpdateRun) *prometheusclientmodel.Metric {
	return &prometheusclientmodel.Metric{
		Label: generateMetricsLabels(updateRun, string(placementv1beta1.StagedUpdateRunConditionSucceeded),
			string(metav1.ConditionFalse), condition.UpdateRunFailedReason),
		Gauge: &prometheusclientmodel.Gauge{
			Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
		},
	}
}

func generateSucceededMetric(updateRun *placementv1beta1.ClusterStagedUpdateRun) *prometheusclientmodel.Metric {
	return &prometheusclientmodel.Metric{
		Label: generateMetricsLabels(updateRun, string(placementv1beta1.StagedUpdateRunConditionSucceeded),
			string(metav1.ConditionTrue), condition.UpdateRunSucceededReason),
		Gauge: &prometheusclientmodel.Gauge{
			Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
		},
	}
}

func generateTestClusterStagedUpdateRun() *placementv1beta1.ClusterStagedUpdateRun {
	return &placementv1beta1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: testUpdateRunName,
		},
		Spec: placementv1beta1.StagedUpdateRunSpec{
			PlacementName:            testCRPName,
			ResourceSnapshotIndex:    testResourceSnapshotIndex,
			StagedUpdateStrategyName: testUpdateStrategyName,
		},
	}
}

func generateTestClusterResourcePlacement() *placementv1beta1.ClusterResourcePlacement {
	return &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: testCRPName,
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.ExternalRolloutStrategyType,
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type:           placementv1beta1.ApplyStrategyTypeReportDiff,
					WhenToTakeOver: placementv1beta1.WhenToTakeOverTypeIfNoDiff,
				},
			},
		},
	}
}

func generateTestClusterSchedulingPolicySnapshot(idx int) *placementv1beta1.ClusterSchedulingPolicySnapshot {
	return &placementv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(placementv1beta1.PolicySnapshotNameFmt, testCRPName, idx),
			Labels: map[string]string{
				"kubernetes-fleet.io/parent-CRP":         testCRPName,
				"kubernetes-fleet.io/is-latest-snapshot": "true",
				"kubernetes-fleet.io/policy-index":       strconv.Itoa(idx),
			},
			Annotations: map[string]string{
				"kubernetes-fleet.io/number-of-clusters": strconv.Itoa(numberOfClustersAnnotation),
			},
		},
		Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickNPlacementType,
			},
			PolicyHash: []byte("hash"),
		},
	}
}

func generateTestClusterResourceBinding(policySnapshotName, targetCluster string, state placementv1beta1.BindingState) *placementv1beta1.ClusterResourceBinding {
	binding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-" + testResourceSnapshotName + "-" + targetCluster,
			Labels: map[string]string{
				placementv1beta1.CRPTrackingLabel: testCRPName,
			},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:                        state,
			TargetCluster:                targetCluster,
			SchedulingPolicySnapshotName: policySnapshotName,
		},
	}
	return binding
}

func generateTestMemberCluster(idx int, clusterName string, labels map[string]string) *clusterv1beta1.MemberCluster {
	clusterLabels := make(map[string]string)
	for k, v := range labels {
		clusterLabels[k] = v
	}
	clusterLabels["index"] = strconv.Itoa(idx)
	return &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   clusterName,
			Labels: clusterLabels,
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Name:      "testUser",
				Kind:      "ServiceAccount",
				Namespace: utils.FleetSystemNamespace,
			},
			HeartbeatPeriodSeconds: 60,
		},
	}
}

func generateTestClusterStagedUpdateStrategy() *placementv1beta1.ClusterStagedUpdateStrategy {
	sortingKey := "index"
	return &placementv1beta1.ClusterStagedUpdateStrategy{
		ObjectMeta: metav1.ObjectMeta{
			Name: testUpdateStrategyName,
		},
		Spec: placementv1beta1.StagedUpdateStrategySpec{
			Stages: []placementv1beta1.StageConfig{
				{
					Name: "stage1",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"group":  "prod",
							"region": "eastus",
						},
					},
					SortingLabelKey: &sortingKey,
					AfterStageTasks: []placementv1beta1.AfterStageTask{
						{
							Type: placementv1beta1.AfterStageTaskTypeTimedWait,
							WaitTime: metav1.Duration{
								Duration: time.Second * 4,
							},
						},
						{
							Type: placementv1beta1.AfterStageTaskTypeApproval,
						},
					},
				},
				{
					Name: "stage2",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"group":  "prod",
							"region": "westus",
						},
					},
					// no sortingLabelKey, should sort by cluster name
					AfterStageTasks: []placementv1beta1.AfterStageTask{
						{
							Type: placementv1beta1.AfterStageTaskTypeApproval,
						},
						{
							Type: placementv1beta1.AfterStageTaskTypeTimedWait,
							WaitTime: metav1.Duration{
								Duration: time.Second * 4,
							},
						},
					},
				},
			},
		},
	}
}

func generateTestClusterResourceSnapshot() *placementv1beta1.ClusterResourceSnapshot {
	clusterResourceSnapshot := &placementv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: testResourceSnapshotName,
			Labels: map[string]string{
				placementv1beta1.CRPTrackingLabel:      testCRPName,
				placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
				placementv1beta1.ResourceIndexLabel:    testResourceSnapshotIndex,
			},
			Annotations: map[string]string{
				placementv1beta1.ResourceGroupHashAnnotation:         "hash",
				placementv1beta1.NumberOfResourceSnapshotsAnnotation: strconv.Itoa(1),
			},
		},
	}
	rawContents := [][]byte{testNamespace}
	for _, rawContent := range rawContents {
		clusterResourceSnapshot.Spec.SelectedResources = append(clusterResourceSnapshot.Spec.SelectedResources,
			placementv1beta1.ResourceContent{
				RawExtension: runtime.RawExtension{Raw: rawContent},
			},
		)
	}
	return clusterResourceSnapshot
}

func generateTestClusterResourceOverride() *placementv1alpha1.ClusterResourceOverrideSnapshot {
	return &placementv1alpha1.ClusterResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: testCROName,
			Labels: map[string]string{
				placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
			},
		},
		Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
			OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "test-namespace",
					},
				},
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"region": "eastus",
											},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/labels/test",
									Value:    apiextensionsv1.JSON{Raw: []byte(`"test"`)},
								},
							},
						},
					},
				},
			},
			OverrideHash: []byte("hash"),
		},
	}
}

func generateTestApprovalRequest(name string) *placementv1beta1.ClusterApprovalRequest {
	return &placementv1beta1.ClusterApprovalRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				placementv1beta1.TargetUpdateRunLabel: testUpdateRunName,
			},
		},
	}
}

func validateUpdateRunHasFinalizer(ctx context.Context, updateRun *placementv1beta1.ClusterStagedUpdateRun) {
	namespacedName := types.NamespacedName{Name: updateRun.Name}
	Eventually(func() error {
		if err := k8sClient.Get(ctx, namespacedName, updateRun); err != nil {
			return fmt.Errorf("failed to get clusterStagedUpdateRun %s: %w", namespacedName, err)
		}
		if !controllerutil.ContainsFinalizer(updateRun, placementv1beta1.ClusterStagedUpdateRunFinalizer) {
			return fmt.Errorf("finalizer not added to clusterStagedUpdateRun %s", namespacedName)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "failed to add finalizer to clusterStagedUpdateRun %s", namespacedName)
}

func validateUpdateRunIsDeleted(ctx context.Context, name types.NamespacedName) {
	Eventually(func() error {
		updateRun := &placementv1beta1.ClusterStagedUpdateRun{}
		if err := k8sClient.Get(ctx, name, updateRun); !errors.IsNotFound(err) {
			return fmt.Errorf("clusterStagedUpdateRun %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "failed to remove clusterStagedUpdateRun %s ", name)
}

func validateApprovalRequestCount(ctx context.Context, count int) {
	Eventually(func() (int, error) {
		appReqList := &placementv1beta1.ClusterApprovalRequestList{}
		if err := k8sClient.List(ctx, appReqList); err != nil {
			return -1, err
		}
		return len(appReqList.Items), nil
	}, timeout, interval).Should(Equal(count), "approval requests count mismatch")
}

func generateTrueCondition(obj client.Object, condType any) metav1.Condition {
	reason, typeStr := "", ""
	switch cond := condType.(type) {
	case placementv1beta1.StagedUpdateRunConditionType:
		switch cond {
		case placementv1beta1.StagedUpdateRunConditionInitialized:
			reason = condition.UpdateRunInitializeSucceededReason
		case placementv1beta1.StagedUpdateRunConditionProgressing:
			reason = condition.UpdateRunStartedReason
		case placementv1beta1.StagedUpdateRunConditionSucceeded:
			reason = condition.UpdateRunSucceededReason
		}
		typeStr = string(cond)
	case placementv1beta1.StageUpdatingConditionType:
		switch cond {
		case placementv1beta1.StageUpdatingConditionProgressing:
			reason = condition.StageUpdatingStartedReason
		case placementv1beta1.StageUpdatingConditionSucceeded:
			reason = condition.StageUpdatingSucceededReason
		}
		typeStr = string(cond)
	case placementv1beta1.ClusterUpdatingStatusConditionType:
		switch cond {
		case placementv1beta1.ClusterUpdatingConditionStarted:
			reason = condition.ClusterUpdatingStartedReason
		case placementv1beta1.ClusterUpdatingConditionSucceeded:
			reason = condition.ClusterUpdatingSucceededReason
		}
		typeStr = string(cond)
	case placementv1beta1.AfterStageTaskConditionType:
		switch cond {
		case placementv1beta1.AfterStageTaskConditionWaitTimeElapsed:
			reason = condition.AfterStageTaskWaitTimeElapsedReason
		case placementv1beta1.AfterStageTaskConditionApprovalRequestCreated:
			reason = condition.AfterStageTaskApprovalRequestCreatedReason
		case placementv1beta1.AfterStageTaskConditionApprovalRequestApproved:
			reason = condition.AfterStageTaskApprovalRequestApprovedReason
		}
		typeStr = string(cond)
	case placementv1beta1.ApprovalRequestConditionType:
		switch cond {
		case placementv1beta1.ApprovalRequestConditionApproved:
			reason = "LGTM"
		}
		typeStr = string(cond)
	case placementv1beta1.ResourceBindingConditionType:
		switch cond {
		case placementv1beta1.ResourceBindingAvailable:
			reason = condition.AvailableReason
		}
		typeStr = string(cond)
	}
	return metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               typeStr,
		ObservedGeneration: obj.GetGeneration(),
		Reason:             reason,
	}
}

func generateFalseCondition(obj client.Object, condType any) metav1.Condition {
	reason, typeStr := "", ""
	switch cond := condType.(type) {
	case placementv1beta1.StagedUpdateRunConditionType:
		switch cond {
		case placementv1beta1.StagedUpdateRunConditionInitialized:
			reason = condition.UpdateRunInitializeFailedReason
		case placementv1beta1.StagedUpdateRunConditionSucceeded:
			reason = condition.UpdateRunFailedReason
		case placementv1beta1.StagedUpdateRunConditionProgressing:
			reason = condition.UpdateRunWaitingReason
		}
		typeStr = string(cond)
	case placementv1beta1.StageUpdatingConditionType:
		switch cond {
		case placementv1beta1.StageUpdatingConditionSucceeded:
			reason = condition.StageUpdatingFailedReason
		case placementv1beta1.StageUpdatingConditionProgressing:
			reason = condition.StageUpdatingWaitingReason
		}
		typeStr = string(cond)
	case placementv1beta1.ClusterUpdatingStatusConditionType:
		switch cond {
		case placementv1beta1.ClusterUpdatingConditionSucceeded:
			reason = condition.ClusterUpdatingFailedReason
		}
		typeStr = string(cond)
	case placementv1beta1.ResourceBindingConditionType:
		switch cond {
		case placementv1beta1.ResourceBindingApplied:
			reason = condition.ApplyFailedReason
		}
		typeStr = string(cond)
	}
	return metav1.Condition{
		Status:             metav1.ConditionFalse,
		Type:               typeStr,
		ObservedGeneration: obj.GetGeneration(),
		Reason:             reason,
	}
}

func generateFalseProgressingCondition(obj client.Object, condType any, succeeded bool) metav1.Condition {
	falseCond := generateFalseCondition(obj, condType)
	reason := ""
	switch condType {
	case placementv1beta1.StagedUpdateRunConditionProgressing:
		if succeeded {
			reason = condition.UpdateRunSucceededReason
		} else {
			reason = condition.UpdateRunFailedReason
		}
	case placementv1beta1.StageUpdatingConditionProgressing:
		if succeeded {
			reason = condition.StageUpdatingSucceededReason
		} else {
			reason = condition.StageUpdatingFailedReason
		}
	}
	falseCond.Reason = reason
	return falseCond
}
