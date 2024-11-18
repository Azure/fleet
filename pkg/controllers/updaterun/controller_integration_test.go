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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
)

const (
	// timeout is the maximum wait time for Eventually
	timeout = time.Second * 10
	// interval is the time to wait between retries for Eventually and Consistently
	interval = time.Millisecond * 250
	// duration is the time to check for Consistently
	duration = time.Second * 20

	// numTargetClusters is the number of scheduled clusters
	numTargetClusters = 10
	// numUnscheduledClusters is the number of unscheduled clusters
	numUnscheduledClusters = 3
	// numberOfClustersAnnotation is the number of clusters in the test latest policy snapshot
	numberOfClustersAnnotation = numTargetClusters
)

var (
	testUpdateRunName        string
	testCRPName              string
	testResourceSnapshotName string
	testUpdateStrategyName   string
	testCROName              string
	updateRunNamespacedName  types.NamespacedName
	testNamespace            []byte
)

var _ = Describe("Test the clusterStagedUpdateRun controller", func() {

	BeforeEach(func() {
		testUpdateRunName = "updaterun-" + utils.RandStr()
		testCRPName = "crp-" + utils.RandStr()
		testResourceSnapshotName = "snapshot-" + utils.RandStr()
		testUpdateStrategyName = "updatestrategy-" + utils.RandStr()
		testCROName = "cro-" + utils.RandStr()
		updateRunNamespacedName = types.NamespacedName{Name: testUpdateRunName}
	})

	Context("Test reconciling a clusterStagedUpdateRun", func() {
		It("Should add the finalizer to the clusterStagedUpdateRun", func() {
			By("Creating a new clusterStagedUpdateRun")
			updateRun := getTestClusterStagedUpdateRun()
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
			updateRun := getTestClusterStagedUpdateRun()
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
			updateRun := getTestClusterStagedUpdateRun()
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())

			By("Checking the finalizer is added")
			validateUpdateRunHasFinalizer(ctx, updateRun)

			By("Updating the clusterStagedUpdateRun to failed")
			startedcond := getTrueCondition(updateRun, string(placementv1alpha1.StagedUpdateRunConditionProgressing))
			finishedcond := getFalseCondition(updateRun, string(placementv1alpha1.StagedUpdateRunConditionSucceeded))
			meta.SetStatusCondition(&updateRun.Status.Conditions, startedcond)
			meta.SetStatusCondition(&updateRun.Status.Conditions, finishedcond)
			Expect(k8sClient.Status().Update(ctx, updateRun)).Should(Succeed(), "failed to update the clusterStagedUpdateRun")

			By("Creating a clusterApprovalRequest")
			approvalRequest := getTestApprovalRequest("req1")
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
			updateRun := getTestClusterStagedUpdateRun()
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())

			By("Checking the finalizer is added")
			validateUpdateRunHasFinalizer(ctx, updateRun)

			By("Updating the clusterStagedUpdateRun status to processing")
			startedcond := getTrueCondition(updateRun, string(placementv1alpha1.StagedUpdateRunConditionProgressing))
			meta.SetStatusCondition(&updateRun.Status.Conditions, startedcond)
			Expect(k8sClient.Status().Update(ctx, updateRun)).Should(Succeed(), "failed to add condition to the clusterStagedUpdateRun")

			By("Creating a clusterApprovalRequest")
			approvalRequest := getTestApprovalRequest("req1")
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
			updateRun := getTestClusterStagedUpdateRun()
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())

			By("Creating ClusterApprovalRequests")
			approvalRequests := []*placementv1alpha1.ClusterApprovalRequest{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "req1",
						Labels: map[string]string{
							placementv1alpha1.TargetUpdateRunLabel: testUpdateRunName,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "req2",
						Labels: map[string]string{
							placementv1alpha1.TargetUpdateRunLabel: testUpdateRunName,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "req3",
						Labels: map[string]string{
							placementv1alpha1.TargetUpdateRunLabel: testUpdateRunName + "1", // different update run
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

func getTestClusterStagedUpdateRun() *placementv1alpha1.ClusterStagedUpdateRun {
	return &placementv1alpha1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: testUpdateRunName,
		},
		Spec: placementv1alpha1.StagedUpdateRunSpec{
			PlacementName:            testCRPName,
			ResourceSnapshotIndex:    testResourceSnapshotName,
			StagedUpdateStrategyName: testUpdateStrategyName,
		},
	}
}

func getTestClusterResourcePlacement() *placementv1beta1.ClusterResourcePlacement {
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

func getTestClusterSchedulingPolicySnapshot(idx int) *placementv1beta1.ClusterSchedulingPolicySnapshot {
	return &placementv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(placementv1beta1.PolicySnapshotNameFmt, testCRPName, idx),
			Labels: map[string]string{
				"kubernetes-fleet.io/parent-CRP":         testCRPName,
				"kubernetes-fleet.io/is-latest-snapshot": "true",
			},
			Annotations: map[string]string{
				"kubernetes-fleet.io/number-of-clusters": strconv.Itoa(numberOfClustersAnnotation),
			},
		},
		Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
			PolicyHash: []byte("hash"),
		},
	}
}

func getTestClusterResourceBinding(policySnapshotName, targetCluster string) *placementv1beta1.ClusterResourceBinding {
	binding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-" + testResourceSnapshotName + "-" + targetCluster,
			Labels: map[string]string{
				placementv1beta1.CRPTrackingLabel: testCRPName,
			},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:                        placementv1beta1.BindingStateScheduled,
			TargetCluster:                targetCluster,
			SchedulingPolicySnapshotName: policySnapshotName,
		},
	}
	return binding
}

func getTestMemberCluster(idx int, clusterName string, labels map[string]string) *clusterv1beta1.MemberCluster {
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

func getTestClusterStagedUpdateStrategy() *placementv1alpha1.ClusterStagedUpdateStrategy {
	sortingKey := "index"
	return &placementv1alpha1.ClusterStagedUpdateStrategy{
		ObjectMeta: metav1.ObjectMeta{
			Name: testUpdateStrategyName,
		},
		Spec: placementv1alpha1.StagedUpdateStrategySpec{
			Stages: []placementv1alpha1.StageConfig{
				{
					Name: "stage1",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"group":  "prod",
							"region": "eastus",
						},
					},
					SortingLabelKey: &sortingKey,
					AfterStageTasks: []placementv1alpha1.AfterStageTask{
						{
							Type: placementv1alpha1.AfterStageTaskTypeTimedWait,
							WaitTime: metav1.Duration{
								Duration: time.Minute * 10,
							},
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
					AfterStageTasks: []placementv1alpha1.AfterStageTask{
						{
							Type: placementv1alpha1.AfterStageTaskTypeApproval,
						},
					},
				},
			},
		},
	}
}

func getTestClusterResourceSnapshot() *placementv1beta1.ClusterResourceSnapshot {
	clusterResourceSnapshot := &placementv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: testResourceSnapshotName,
			Labels: map[string]string{
				placementv1beta1.CRPTrackingLabel:      testCRPName,
				placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
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

func getTestClusterResourceOverride() *placementv1alpha1.ClusterResourceOverrideSnapshot {
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

func getTestApprovalRequest(name string) *placementv1alpha1.ClusterApprovalRequest {
	return &placementv1alpha1.ClusterApprovalRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				placementv1alpha1.TargetUpdateRunLabel: testUpdateRunName,
			},
		},
	}
}

func validateUpdateRunHasFinalizer(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun) {
	namepsacedName := types.NamespacedName{Name: updateRun.Name}
	Eventually(func() error {
		if err := k8sClient.Get(ctx, namepsacedName, updateRun); err != nil {
			return fmt.Errorf("failed to get clusterStagedUpdateRun %s: %w", namepsacedName, err)
		}
		if !controllerutil.ContainsFinalizer(updateRun, placementv1alpha1.ClusterStagedUpdateRunFinalizer) {
			return fmt.Errorf("finalizer not added to clusterStagedUpdateRun %s", namepsacedName)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "failed to add finalizer to clusterStagedUpdateRun %s", namepsacedName)
}

func validateUpdateRunIsDeleted(ctx context.Context, name types.NamespacedName) {
	Eventually(func() error {
		updateRun := &placementv1alpha1.ClusterStagedUpdateRun{}
		if err := k8sClient.Get(ctx, name, updateRun); !errors.IsNotFound(err) {
			return fmt.Errorf("clusterStagedUpdateRun %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "failed to remove clusterStagedUpdateRun %s ", name)
}

func validateApprovalRequestCount(ctx context.Context, count int) {
	Eventually(func() (int, error) {
		appReqList := &placementv1alpha1.ClusterApprovalRequestList{}
		if err := k8sClient.List(ctx, appReqList); err != nil {
			return -1, err
		}
		return len(appReqList.Items), nil
	}, timeout, interval).Should(Equal(count), "approval requests count mismatch")
}

func getTrueCondition(updateRun *placementv1alpha1.ClusterStagedUpdateRun, condType string) metav1.Condition {
	reason := ""
	switch condType {
	case string(placementv1alpha1.StagedUpdateRunConditionInitialized):
		reason = condition.UpdateRunInitializeSucceededReason
	case string(placementv1alpha1.StagedUpdateRunConditionProgressing):
		reason = condition.UpdateRunStartedReason
	case string(placementv1alpha1.StagedUpdateRunConditionSucceeded):
		reason = condition.UpdateRunSucceededReason
	}
	return metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               condType,
		ObservedGeneration: updateRun.Generation,
		Reason:             reason,
	}
}

func getFalseCondition(updateRun *placementv1alpha1.ClusterStagedUpdateRun, condType string) metav1.Condition {
	reason := ""
	switch condType {
	case string(placementv1alpha1.StagedUpdateRunConditionInitialized):
		reason = condition.UpdateRunInitializeFailedReason
	case string(placementv1alpha1.StagedUpdateRunConditionSucceeded):
		reason = condition.UpdateRunFailedReason
	}
	return metav1.Condition{
		Status:             metav1.ConditionFalse,
		Type:               condType,
		ObservedGeneration: updateRun.Generation,
		Reason:             reason,
	}
}
