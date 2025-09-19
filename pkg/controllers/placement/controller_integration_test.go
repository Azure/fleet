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
package placement

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	prometheusclientmodel "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/metrics"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resource"
	"github.com/kubefleet-dev/kubefleet/test/utils/crpstatussync"
	metricsUtils "github.com/kubefleet-dev/kubefleet/test/utils/metrics"
)

const (
	member1Name = "member-1"
	member2Name = "member-2"

	eventuallyTimeout   = time.Second * 10
	consistentlyTimeout = time.Second * 10
	interval            = time.Millisecond * 250
)

var (
	member1Namespace = fmt.Sprintf(utils.NamespaceNameFormat, member1Name)
	member2Namespace = fmt.Sprintf(utils.NamespaceNameFormat, member2Name)

	commonCmpOptions = cmp.Options{
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "UID", "CreationTimestamp", "ManagedFields", "Generation"),
		cmpopts.IgnoreFields(metav1.OwnerReference{}, "UID"),
	}
	cmpPolicySnapshotOptions = cmp.Options{
		commonCmpOptions,
		cmpopts.IgnoreFields(placementv1beta1.ClusterSchedulingPolicySnapshot{}, "TypeMeta"),
	}
	cmpResourceSnapshotOptions = cmp.Options{
		commonCmpOptions,
		cmpopts.IgnoreFields(placementv1beta1.ClusterResourceSnapshot{}, "TypeMeta"),
	}
	cmpCRPOptions = cmp.Options{
		commonCmpOptions,
		cmpopts.IgnoreFields(placementv1beta1.ClusterResourcePlacement{}, "TypeMeta"),
		cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime", "ObservedGeneration"),
		cmpopts.SortSlices(func(c1, c2 metav1.Condition) bool {
			return c1.Type < c2.Type
		}),
		cmpopts.SortSlices(func(c1, c2 placementv1beta1.PerClusterPlacementStatus) bool {
			return c1.ClusterName < c2.ClusterName
		}),
	}
	crpOwnerReference = metav1.OwnerReference{
		APIVersion:         fleetAPIVersion,
		Kind:               "ClusterResourcePlacement",
		Name:               testCRPName,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
)

var (
	crp                 *placementv1beta1.ClusterResourcePlacement
	gotCRP              *placementv1beta1.ClusterResourcePlacement
	gotPolicySnapshot   *placementv1beta1.ClusterSchedulingPolicySnapshot
	gotResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
	member1Binding      *placementv1beta1.ClusterResourceBinding
	member2Binding      *placementv1beta1.ClusterResourceBinding
)

func retrieveAndValidatePolicySnapshot(crp *placementv1beta1.ClusterResourcePlacement, want *placementv1beta1.ClusterSchedulingPolicySnapshot) *placementv1beta1.ClusterSchedulingPolicySnapshot {
	policySnapshotList := &placementv1beta1.ClusterSchedulingPolicySnapshotList{}
	Eventually(func() error {
		if err := k8sClient.List(ctx, policySnapshotList, client.MatchingLabels{placementv1beta1.PlacementTrackingLabel: crp.Name}); err != nil {
			return err
		}
		if len(policySnapshotList.Items) != 1 {
			return fmt.Errorf("got %d, want 1", len(policySnapshotList.Items))
		}
		if diff := cmp.Diff(*want, policySnapshotList.Items[0], cmpPolicySnapshotOptions); diff != "" {
			return fmt.Errorf("clusterSchedulingPolicySnapshot mismatch (-want, +got) :\n%s", diff)
		}
		return nil
	}, eventuallyTimeout, interval).Should(Succeed(), "List() clusterSchedulingPolicySnapshot mismatch")
	return &policySnapshotList.Items[0]
}

func retrieveAndValidateResourceSnapshot(crp *placementv1beta1.ClusterResourcePlacement, want *placementv1beta1.ClusterResourceSnapshot) *placementv1beta1.ClusterResourceSnapshot {
	resourceSnapshotList := &placementv1beta1.ClusterResourceSnapshotList{}
	Eventually(func() error {
		if err := k8sClient.List(ctx, resourceSnapshotList, client.MatchingLabels{placementv1beta1.PlacementTrackingLabel: crp.Name}); err != nil {
			return err
		}
		if len(resourceSnapshotList.Items) != 1 {
			return fmt.Errorf("got %d, want 1", len(resourceSnapshotList.Items))
		}
		if diff := cmp.Diff(*want, resourceSnapshotList.Items[0], cmpResourceSnapshotOptions); diff != "" {
			return fmt.Errorf("resourceSnapshotList mismatch (-want, +got) :\n%s", diff)
		}
		return nil
	}, eventuallyTimeout, interval).Should(Succeed(), "List() resourceSnapshotList mismatch")
	return &resourceSnapshotList.Items[0]
}

func retrieveAndValidateClusterResourcePlacement(crpName string, want *placementv1beta1.ClusterResourcePlacement) *placementv1beta1.ClusterResourcePlacement {
	key := types.NamespacedName{Name: crpName}
	createdCRP := &placementv1beta1.ClusterResourcePlacement{}
	Eventually(func() error {
		if err := k8sClient.Get(ctx, key, createdCRP); err != nil {
			return err
		}
		if diff := cmp.Diff(want, createdCRP, cmpCRPOptions); diff != "" {
			return fmt.Errorf("clusterResourcePlacement mismatch (-want, +got) :\n%s", diff)
		}
		return nil
	}, eventuallyTimeout, interval).Should(Succeed(), "Get() clusterResourcePlacement mismatch")
	return createdCRP
}

func retrieveAndValidateCRPDeletion(crp *placementv1beta1.ClusterResourcePlacement) {
	By("Checking the policy snapshots")
	policySnapshotList := &placementv1beta1.ClusterSchedulingPolicySnapshotList{}
	Eventually(func() error {
		if err := k8sClient.List(ctx, policySnapshotList, client.MatchingLabels{placementv1beta1.PlacementTrackingLabel: crp.Name}); err != nil {
			return err
		}
		if len(policySnapshotList.Items) != 0 {
			return fmt.Errorf("policySnapshotList len got %v, want 0", len(policySnapshotList.Items))
		}
		return nil
	}, eventuallyTimeout, interval).Should(Succeed(), "List() clusterSchedulingPolicySnapshot mismatch")

	By("Checking the resource snapshots")
	resourceSnapshotList := &placementv1beta1.ClusterResourceSnapshotList{}
	Eventually(func() error {
		if err := k8sClient.List(ctx, resourceSnapshotList, client.MatchingLabels{placementv1beta1.PlacementTrackingLabel: crp.Name}); err != nil {
			return err
		}
		if len(resourceSnapshotList.Items) != 0 {
			return fmt.Errorf("resourceSnapshotList len got %d, want 0", len(resourceSnapshotList.Items))
		}
		return nil
	}, eventuallyTimeout, interval).Should(Succeed(), "List() resourceSnapshotList mismatch")

	By("Checking the CRP")
	key := types.NamespacedName{Name: crp.Name}
	createdCRP := &placementv1beta1.ClusterResourcePlacement{}
	Eventually(func() bool {
		err := k8sClient.Get(ctx, key, createdCRP)
		return errors.IsNotFound(err)
	}, eventuallyTimeout, interval).Should(BeTrue(), "Get() clusterResourcePlacement, want not found")
}

func createOverriddenClusterResourceBinding(cluster string, policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot, resourceSnapshot *placementv1beta1.ClusterResourceSnapshot) *placementv1beta1.ClusterResourceBinding {
	binding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-" + cluster,
			Labels: map[string]string{
				placementv1beta1.PlacementTrackingLabel: resourceSnapshot.Labels[placementv1beta1.PlacementTrackingLabel],
			},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:                        placementv1beta1.BindingStateBound,
			ResourceSnapshotName:         resourceSnapshot.Name,
			SchedulingPolicySnapshotName: policySnapshot.Name,
			TargetCluster:                cluster,
		},
	}
	Expect(k8sClient.Create(ctx, binding)).Should(Succeed(), "Failed to create clusterResourceBinding")
	cond := metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
		Reason:             condition.RolloutStartedReason,
		ObservedGeneration: binding.Generation,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, cond)
	cond = metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(placementv1beta1.ResourceBindingOverridden),
		Reason:             condition.OverriddenSucceededReason,
		ObservedGeneration: binding.Generation,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, cond)
	Expect(k8sClient.Status().Update(ctx, binding)).Should(Succeed(), "Failed to update the binding status")
	return binding
}

func createSynchronizedClusterResourceBinding(cluster string, policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot, resourceSnapshot *placementv1beta1.ClusterResourceSnapshot) *placementv1beta1.ClusterResourceBinding {
	binding := createOverriddenClusterResourceBinding(cluster, policySnapshot, resourceSnapshot)
	cond := metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
		Reason:             condition.WorkSynchronizedReason,
		ObservedGeneration: binding.Generation,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, cond)
	Expect(k8sClient.Status().Update(ctx, binding)).Should(Succeed(), "Failed to update the binding status")
	return binding
}

func updateClusterResourceBindingWithSynchronized(binding *placementv1beta1.ClusterResourceBinding) *placementv1beta1.ClusterResourceBinding {
	cond := metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
		Reason:             condition.WorkSynchronizedReason,
		ObservedGeneration: binding.Generation,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, cond)
	Expect(k8sClient.Status().Update(ctx, binding)).Should(Succeed(), "Failed to update the binding status")
	return binding
}

func updateClusterResourceBindingWithAvailable(binding *placementv1beta1.ClusterResourceBinding) *placementv1beta1.ClusterResourceBinding {
	cond := metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(placementv1beta1.ResourceBindingApplied),
		Reason:             condition.ApplySucceededReason,
		ObservedGeneration: binding.Generation,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, cond)
	cond = metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(placementv1beta1.ResourceBindingAvailable),
		Reason:             condition.AvailableReason,
		ObservedGeneration: binding.Generation,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, cond)
	Expect(k8sClient.Status().Update(ctx, binding)).Should(Succeed(), "Failed to update the binding status")
	return binding
}

func updateClusterResourceBindingWithReportDiff(binding *placementv1beta1.ClusterResourceBinding) *placementv1beta1.ClusterResourceBinding {
	cond := metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
		Reason:             condition.WorkSynchronizedReason,
		ObservedGeneration: binding.Generation,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, cond)
	cond = metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(placementv1beta1.PerClusterDiffReportedConditionType),
		Reason:             condition.DiffReportedStatusTrueReason,
		ObservedGeneration: binding.Generation,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, cond)
	Expect(k8sClient.Status().Update(ctx, binding)).Should(Succeed(), "Failed to update the binding status")
	return binding
}

func checkClusterSchedulingPolicySnapshot() *placementv1beta1.ClusterSchedulingPolicySnapshot {
	policyHash, err := resource.HashOf(crp.Spec.Policy)
	Expect(err).Should(Succeed(), "failed to create policy hash")

	wantPolicySnapshot := placementv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(placementv1beta1.PolicySnapshotNameFmt, crp.Name, 0),
			Labels: map[string]string{
				placementv1beta1.PlacementTrackingLabel: crp.Name,
				placementv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
				placementv1beta1.PolicyIndexLabel:       strconv.Itoa(0),
			},
			Annotations: map[string]string{
				placementv1beta1.CRPGenerationAnnotation: strconv.Itoa(int(crp.Generation)),
			},
			OwnerReferences: []metav1.OwnerReference{
				crpOwnerReference,
			},
		},
		Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
			PolicyHash: []byte(policyHash),
		},
	}
	return retrieveAndValidatePolicySnapshot(crp, &wantPolicySnapshot)
}

func checkClusterResourceSnapshot() *placementv1beta1.ClusterResourceSnapshot {
	emptyResources := &placementv1beta1.ResourceSnapshotSpec{
		SelectedResources: []placementv1beta1.ResourceContent{},
	}
	jsonBytes, err := resource.HashOf(emptyResources)
	Expect(err).Should(Succeed(), "Failed to create resource hash")

	wantResourceSnapshot := &placementv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crp.Name, 0),
			Labels: map[string]string{
				placementv1beta1.PlacementTrackingLabel: crp.Name,
				placementv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
				placementv1beta1.ResourceIndexLabel:     strconv.Itoa(0),
			},
			Annotations: map[string]string{
				placementv1beta1.NumberOfResourceSnapshotsAnnotation: strconv.Itoa(1),
				placementv1beta1.ResourceGroupHashAnnotation:         jsonBytes,
				placementv1beta1.NumberOfEnvelopedObjectsAnnotation:  strconv.Itoa(0),
			},
			OwnerReferences: []metav1.OwnerReference{
				crpOwnerReference,
			},
		},
		Spec: placementv1beta1.ResourceSnapshotSpec{
			SelectedResources: []placementv1beta1.ResourceContent{},
		},
	}
	return retrieveAndValidateResourceSnapshot(crp, wantResourceSnapshot)
}

func updateClusterSchedulingPolicySnapshotStatus(status metav1.ConditionStatus, clustersSelected bool) *placementv1beta1.ClusterSchedulingPolicySnapshot {
	reason := condition.ResourceScheduleSucceededReason
	if status == metav1.ConditionFalse {
		reason = condition.ResourceScheduleFailedReason
	}

	// Update scheduling condition
	scheduledCondition := metav1.Condition{
		Type:               string(placementv1beta1.PolicySnapshotScheduled),
		Status:             status,
		Reason:             reason,
		ObservedGeneration: gotCRP.Generation,
	}
	meta.SetStatusCondition(&gotPolicySnapshot.Status.Conditions, scheduledCondition)
	gotPolicySnapshot.Status.ObservedCRPGeneration = gotCRP.Generation

	// Only update ClusterDecisions if clustersSelected is true
	if clustersSelected {
		// Build cluster decisions
		reasonStr := "valid"
		selected := true
		if status == metav1.ConditionFalse {
			selected = false
			reasonStr = "invalid"
		}
		gotPolicySnapshot.Status.ClusterDecisions = []placementv1beta1.ClusterDecision{
			{
				ClusterName: member1Name,
				Selected:    selected,
				Reason:      reasonStr,
			},
			{
				ClusterName: member2Name,
				Selected:    selected,
				Reason:      reasonStr,
			},
		}
	}

	// Apply status update
	Expect(k8sClient.Status().Update(ctx, gotPolicySnapshot)).Should(Succeed(), "Failed to update the policy snapshot status")
	return retrieveAndValidatePolicySnapshot(gotCRP, gotPolicySnapshot)
}

var _ = Describe("Test ClusterResourcePlacement Controller", func() {
	Context("When creating new pickAll ClusterResourcePlacement", func() {
		BeforeEach(func() {
			// Reset metric before each test
			metrics.FleetPlacementStatusLastTimeStampSeconds.Reset()

			By("Create a new crp")
			crp = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"region": "east"},
							},
						},
					},
					RevisionHistoryLimit: ptr.To(int32(1)),
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed(), "Failed to create crp")

			By("Check clusterSchedulingPolicySnapshot")
			gotPolicySnapshot = checkClusterSchedulingPolicySnapshot()

			By("Check clusterResourceSnapshot")
			gotResourceSnapshot = checkClusterResourceSnapshot()

			By("Validate CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.SchedulingUnknownReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(crp.Name, wantCRP)
		})

		AfterEach(func() {
			By("Deleting crp")
			Expect(k8sClient.Delete(ctx, gotCRP)).Should(Succeed())
			retrieveAndValidateCRPDeletion(gotCRP)

			By("Deleting clusterResourceBindings")
			if member1Binding != nil {
				Expect(k8sClient.Delete(ctx, member1Binding)).Should(Succeed())
			}
			if member2Binding != nil {
				Expect(k8sClient.Delete(ctx, member2Binding)).Should(Succeed())
			}
		})

		It("None of the clusters are selected", func() {
			By("Update clusterSchedulingPolicySnapshot status to schedule success")
			updateClusterSchedulingPolicySnapshotStatus(metav1.ConditionTrue, false)

			By("Validate the CRP status has only scheduling condition")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.ResourceScheduleSucceededReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric was emitted")
			// A metric for every !true (nil, unknown, false) status as the CRP reconciles.
			// In this case, no clusters are selected therefore with pickAll policy therefore there is nothing to rollout
			// so the RolloutStarted condition is nil.
			wantMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.SchedulingUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To("nil")},
						{Name: ptr.To("reason"), Value: ptr.To("nil")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(wantMetrics)
		})

		It("Clusters are not selected", func() {
			By("Update clusterSchedulingPolicySnapshot status to schedule failed")
			updateClusterSchedulingPolicySnapshotStatus(metav1.ConditionFalse, true)

			By("Validate the CRP status has only scheduling condition")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionFalse,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.ResourceScheduleFailedReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric was emitted")
			// A metric for the different reconcilations for all the !true statuses as the CRP reconciles.
			// In this case we have 2 metrics for 1 condition type as Scheduled goes from `Unknown` to `False`.
			wantMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.SchedulingUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionFalse))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.ResourceScheduleFailedReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(wantMetrics)
		})

		It("Clusters are selected and resources are applied successfully", func() {
			By("Update clusterSchedulingPolicySnapshot status to schedule success")
			updateClusterSchedulingPolicySnapshotStatus(metav1.ConditionTrue, true)

			By("Create an overridden clusterResourceBinding on member-1")
			member1Binding = createOverriddenClusterResourceBinding(member1Name, gotPolicySnapshot, gotResourceSnapshot)

			By("Validate the CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.ResourceScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
							Reason: condition.RolloutStartedUnknownReason,
						},
					},
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           member1Name,
							ObservedResourceIndex: "0",
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedUnknownReason,
								},
							},
						},
						{
							ClusterName:           member2Name,
							ObservedResourceIndex: "", // Empty as the binding is not created yet.
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedUnknownReason,
								},
							},
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric was emitted")
			// There are two metrics because 1 member cluster has an unknown RolloutStarted status.
			// We emit the !true status for CRP as it reconciles. In this case, CRP is still rolling out.
			wantMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.SchedulingUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.RolloutStartedUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(wantMetrics)

			By("Create a synchronized clusterResourceBinding on member-2")
			member2Binding = createSynchronizedClusterResourceBinding(member2Name, gotPolicySnapshot, gotResourceSnapshot)

			By("Validate the CRP status")
			wantCRP.Status.Conditions = []metav1.Condition{
				{
					Status: metav1.ConditionTrue,
					Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
					Reason: condition.ResourceScheduleSucceededReason,
				},
				{
					Status: metav1.ConditionTrue,
					Type:   string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Reason: condition.RolloutStartedReason,
				},
				{
					Status: metav1.ConditionTrue,
					Type:   string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
					Reason: condition.OverrideNotSpecifiedReason,
				},
				{
					Status: metav1.ConditionUnknown,
					Type:   string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
					Reason: condition.WorkSynchronizedUnknownReason,
				},
			}
			wantCRP.Status.PerClusterPlacementStatuses = []placementv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           member1Name,
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterScheduledConditionType),
							Reason: condition.ScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
							Reason: condition.RolloutStartedReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
							Reason: condition.OverriddenSucceededReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
							Reason: condition.WorkSynchronizedUnknownReason,
						},
					},
				},
				{
					ClusterName:           member2Name,
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterScheduledConditionType),
							Reason: condition.ScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
							Reason: condition.RolloutStartedReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
							Reason: condition.OverriddenSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
							Reason: condition.WorkSynchronizedReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.PerClusterAppliedConditionType),
							Reason: condition.ApplyPendingReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric was emitted")
			wantMetrics = append(wantMetrics, &prometheusclientmodel.Metric{
				Label: []*prometheusclientmodel.LabelPair{
					{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
					{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
					{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
					{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType))},
					{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					{Name: ptr.To("reason"), Value: ptr.To(condition.WorkSynchronizedUnknownReason)},
				},
				Gauge: &prometheusclientmodel.Gauge{
					Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
				},
			})
			checkPlacementStatusMetric(wantMetrics)
		})

		It("Emit metrics when CRP spec updates with different generations", func() {
			By("Update clusterSchedulingPolicySnapshot status to schedule success")
			gotPolicySnapshot = updateClusterSchedulingPolicySnapshotStatus(metav1.ConditionTrue, true)

			By("Create an overridden clusterResourceBinding on member-1")
			member1Binding = createOverriddenClusterResourceBinding(member1Name, gotPolicySnapshot, gotResourceSnapshot)

			By("Validate the CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.ResourceScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
							Reason: condition.RolloutStartedUnknownReason,
						},
					},
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           member1Name,
							ObservedResourceIndex: "0",
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedUnknownReason,
								},
							},
						},
						{
							ClusterName:           member2Name,
							ObservedResourceIndex: "", // Empty as the binding is not created yet.
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedUnknownReason,
								},
							},
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric was emitted for 1st generation")
			wantMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.SchedulingUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.RolloutStartedUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(wantMetrics)

			By("Create a synchronized clusterResourceBinding on member-2")
			member2Binding = createSynchronizedClusterResourceBinding(member2Name, gotPolicySnapshot, gotResourceSnapshot)

			By("Validate the CRP status with updated binding")
			wantCRP.Status.Conditions = []metav1.Condition{
				{
					Status: metav1.ConditionTrue,
					Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
					Reason: condition.ResourceScheduleSucceededReason,
				},
				{
					Status: metav1.ConditionTrue,
					Type:   string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Reason: condition.RolloutStartedReason,
				},
				{
					Status: metav1.ConditionTrue,
					Type:   string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
					Reason: condition.OverrideNotSpecifiedReason,
				},
				{
					Status: metav1.ConditionUnknown,
					Type:   string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
					Reason: condition.WorkSynchronizedUnknownReason,
				},
			}
			wantCRP.Status.PerClusterPlacementStatuses = []placementv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           member1Name,
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterScheduledConditionType),
							Reason: condition.ScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
							Reason: condition.RolloutStartedReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
							Reason: condition.OverriddenSucceededReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
							Reason: condition.WorkSynchronizedUnknownReason,
						},
					},
				},
				{
					ClusterName:           member2Name,
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterScheduledConditionType),
							Reason: condition.ScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
							Reason: condition.RolloutStartedReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
							Reason: condition.OverriddenSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
							Reason: condition.WorkSynchronizedReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.PerClusterAppliedConditionType),
							Reason: condition.ApplyPendingReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric was emitted for 1st generation")
			wantMetrics = append(wantMetrics, &prometheusclientmodel.Metric{
				Label: []*prometheusclientmodel.LabelPair{
					{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
					{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
					{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
					{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType))},
					{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					{Name: ptr.To("reason"), Value: ptr.To(condition.WorkSynchronizedUnknownReason)},
				},
				Gauge: &prometheusclientmodel.Gauge{
					Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
				},
			})
			checkPlacementStatusMetric(wantMetrics)

			By("Update CRP spec to add another resource selector")
			gotCRP.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors,
				placementv1beta1.ResourceSelectorTerm{
					Group:   corev1.GroupName,
					Version: "v1",
					Kind:    "Namespace",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"region": "west"},
					},
				})
			Expect(k8sClient.Update(ctx, gotCRP)).Should(Succeed(), "Failed to update crp")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRPName}, crp)).Should(BeNil(), "Get() clusterResourcePlacement mismatch")

			By("Validate CRP status with new spec")
			wantCRP.Spec.ResourceSelectors = gotCRP.Spec.ResourceSelectors
			wantCRP.Status.Conditions = []metav1.Condition{
				{
					Status:             metav1.ConditionUnknown,
					Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
					Reason:             condition.SchedulingUnknownReason,
					ObservedGeneration: 2,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: 1,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
					Reason:             condition.OverrideNotSpecifiedReason,
					ObservedGeneration: 1,
				},
				{
					Status:             metav1.ConditionUnknown,
					Type:               string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
					Reason:             condition.WorkSynchronizedUnknownReason,
					ObservedGeneration: 1,
				},
			}
			wantCRP.Status.PerClusterPlacementStatuses = nil
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric was emitted with different generations")
			// When a CRP spec is updated, the generation of the CRP changes. Therefore, the observed generation for the conditions will also change.
			// Should have multiples of same condition type with different generations.
			// In this case we have 2 metrics for Scheduled condition type as crp generation goes from 1 to 2.
			wantMetrics = append(wantMetrics, &prometheusclientmodel.Metric{
				Label: []*prometheusclientmodel.LabelPair{
					{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
					{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
					{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
					{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
					{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					{Name: ptr.To("reason"), Value: ptr.To(condition.SchedulingUnknownReason)},
				},
				Gauge: &prometheusclientmodel.Gauge{
					Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
				},
			})
			checkPlacementStatusMetric(wantMetrics)

			By("Update clusterSchedulingPolicySnapshot status to schedule success")
			// Update the annotation to match the CRP generation, which is now 2
			gotPolicySnapshot.Annotations[placementv1beta1.CRPGenerationAnnotation] = strconv.FormatInt(gotCRP.Generation, 10)
			gotPolicySnapshot = retrieveAndValidatePolicySnapshot(gotCRP, gotPolicySnapshot)
			gotPolicySnapshot = updateClusterSchedulingPolicySnapshotStatus(metav1.ConditionTrue, true)

			By("Validate CRP status with new observed generations for conditions")
			wantCRP.Status.Conditions = []metav1.Condition{
				{
					Status:             metav1.ConditionTrue,
					Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
					Reason:             condition.ResourceScheduleSucceededReason,
					ObservedGeneration: 2,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: 2,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
					Reason:             condition.OverrideNotSpecifiedReason,
					ObservedGeneration: 2,
				},
				{
					Status:             metav1.ConditionUnknown,
					Type:               string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
					Reason:             condition.WorkSynchronizedUnknownReason,
					ObservedGeneration: 2,
				},
			}
			wantCRP.Status.PerClusterPlacementStatuses = []placementv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           member1Name,
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.PerClusterScheduledConditionType),
							Reason:             condition.ScheduleSucceededReason,
							ObservedGeneration: 2,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.PerClusterRolloutStartedConditionType),
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: 2,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.PerClusterOverriddenConditionType),
							Reason:             condition.OverriddenSucceededReason,
							ObservedGeneration: 2,
						},
						{
							Status:             metav1.ConditionUnknown,
							Type:               string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
							Reason:             condition.WorkSynchronizedUnknownReason,
							ObservedGeneration: 2,
						},
					},
				},
				{
					ClusterName:           member2Name,
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.PerClusterScheduledConditionType),
							Reason:             condition.ScheduleSucceededReason,
							ObservedGeneration: 2,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.PerClusterRolloutStartedConditionType),
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: 2,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.PerClusterOverriddenConditionType),
							Reason:             condition.OverriddenSucceededReason,
							ObservedGeneration: 2,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
							Reason:             condition.WorkSynchronizedReason,
							ObservedGeneration: 2,
						},
						{
							Status:             metav1.ConditionUnknown,
							Type:               string(placementv1beta1.PerClusterAppliedConditionType),
							Reason:             condition.ApplyPendingReason,
							ObservedGeneration: 2,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric was emitted with different generations")
			// When a CRP spec is updated, the generation of the CRP changes. Therefore, the observed generation for the conditions will also change.
			// Should have multiples of same condition type with different generations.
			// In this case we have 2 metrics for different condition types as crp updates and its generation goes from 1 to 2.
			wantMetrics = append(wantMetrics, &prometheusclientmodel.Metric{
				Label: []*prometheusclientmodel.LabelPair{
					{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
					{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
					{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
					{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType))},
					{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					{Name: ptr.To("reason"), Value: ptr.To(condition.WorkSynchronizedUnknownReason)},
				},
				Gauge: &prometheusclientmodel.Gauge{
					Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
				},
			})
			checkPlacementStatusMetric(wantMetrics)
		})

		It("Emit metrics for complete CRP", func() {
			By("Update clusterSchedulingPolicySnapshot status to schedule success")
			updateClusterSchedulingPolicySnapshotStatus(metav1.ConditionTrue, true)

			By("Create a synchronized clusterResourceBinding on member-1")
			member1Binding = createSynchronizedClusterResourceBinding(member1Name, gotPolicySnapshot, gotResourceSnapshot)

			By("Create an overridden clusterResourceBinding on member-2")
			member2Binding = createOverriddenClusterResourceBinding(member2Name, gotPolicySnapshot, gotResourceSnapshot)

			By("Validate the CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.ResourceScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
							Reason: condition.RolloutStartedReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
							Reason: condition.OverrideNotSpecifiedReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
							Reason: condition.WorkSynchronizedUnknownReason,
						},
					},
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           member1Name,
							ObservedResourceIndex: "0",
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.ResourceBindingApplied),
									Reason: condition.ApplyPendingReason,
								},
							},
						},
						{
							ClusterName:           member2Name,
							ObservedResourceIndex: "0",
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedUnknownReason,
								},
							},
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric was emitted")
			wantMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.SchedulingUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.RolloutStartedUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.WorkSynchronizedUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(wantMetrics)

			By("Update to a synchronized clusterResourceBinding on member-1")
			member1Binding = updateClusterResourceBindingWithSynchronized(member1Binding)

			By("Update to a synchronized clusterResourceBinding on member-2")
			member2Binding = updateClusterResourceBindingWithSynchronized(member2Binding)

			By("Validate CRP status with apply pending condition")
			wantCondition := metav1.Condition{
				Status: metav1.ConditionTrue,
				Type:   string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
				Reason: condition.WorkSynchronizedReason,
			}
			meta.SetStatusCondition(&wantCRP.Status.Conditions, wantCondition)
			wantCondition.Type = string(placementv1beta1.ResourceBindingWorkSynchronized)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[0].Conditions, wantCondition)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[1].Conditions, wantCondition)

			wantCondition = metav1.Condition{
				Status: metav1.ConditionUnknown,
				Type:   string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
				Reason: condition.ApplyPendingReason,
			}
			meta.SetStatusCondition(&wantCRP.Status.Conditions, wantCondition)
			wantCondition.Type = string(placementv1beta1.ResourceBindingApplied)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[0].Conditions, wantCondition)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[1].Conditions, wantCondition)

			By("Ensure placement status applied metric was emitted")
			wantMetrics = append(wantMetrics, &prometheusclientmodel.Metric{
				Label: []*prometheusclientmodel.LabelPair{
					{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
					{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
					{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
					{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementAppliedConditionType))},
					{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					{Name: ptr.To("reason"), Value: ptr.To(condition.ApplyPendingReason)},
				},
				Gauge: &prometheusclientmodel.Gauge{
					Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
				},
			})

			By("Update to an available clusterResourceBinding on member-1")
			member1Binding = updateClusterResourceBindingWithAvailable(member1Binding)

			By("Update to an available clusterResourceBinding on member-2")
			member2Binding = updateClusterResourceBindingWithAvailable(member2Binding)

			By("Validate CRP status with all true conditions")
			wantCondition = metav1.Condition{
				Status: metav1.ConditionTrue,
				Type:   string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
				Reason: condition.ApplySucceededReason,
			}
			meta.SetStatusCondition(&wantCRP.Status.Conditions, wantCondition)
			wantCondition.Type = string(placementv1beta1.ResourceBindingApplied)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[0].Conditions, wantCondition)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[1].Conditions, wantCondition)

			wantCondition = metav1.Condition{
				Status: metav1.ConditionTrue,
				Type:   string(placementv1beta1.ClusterResourcePlacementAvailableConditionType),
				Reason: condition.AvailableReason,
			}
			meta.SetStatusCondition(&wantCRP.Status.Conditions, wantCondition)
			wantCondition.Type = string(placementv1beta1.ResourceBindingAvailable)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[0].Conditions, wantCondition)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[1].Conditions, wantCondition)
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status completed metric was emitted")
			wantMetrics = append(wantMetrics, &prometheusclientmodel.Metric{
				Label: []*prometheusclientmodel.LabelPair{
					{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
					{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
					{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
					{Name: ptr.To("conditionType"), Value: ptr.To("Completed")},
					{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionTrue))},
					{Name: ptr.To("reason"), Value: ptr.To("Completed")},
				},
				Gauge: &prometheusclientmodel.Gauge{
					Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
				},
			})
			checkPlacementStatusMetric(wantMetrics)
		})
	})

	Context("When creating a ReportDiff ClusterResourcePlacement", func() {
		BeforeEach(func() {
			// Reset metric before each test
			metrics.FleetPlacementStatusLastTimeStampSeconds.Reset()

			By("Create a new crp")
			crp = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"region": "east"},
							},
						},
					},
					RevisionHistoryLimit: ptr.To(int32(1)),
					Strategy: placementv1beta1.RolloutStrategy{
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							Type: placementv1beta1.ApplyStrategyTypeReportDiff,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed(), "Failed to create crp")

			By("Check clusterSchedulingPolicySnapshot")
			gotPolicySnapshot = checkClusterSchedulingPolicySnapshot()

			By("Check clusterResourceSnapshot")
			gotResourceSnapshot = checkClusterResourceSnapshot()

			By("Check CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.SchedulingUnknownReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(crp.Name, wantCRP)
		})

		AfterEach(func() {
			By("Deleting crp")
			Expect(k8sClient.Delete(ctx, gotCRP)).Should(Succeed())
			retrieveAndValidateCRPDeletion(gotCRP)

			By("Deleting clusterResourceBindings")
			Expect(k8sClient.Delete(ctx, member1Binding)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, member2Binding)).Should(Succeed())
		})

		It("Emit metrics for ReportDiff Incomplete CRP", func() {
			By("Update clusterSchedulingPolicySnapshot status to schedule success")
			updateClusterSchedulingPolicySnapshotStatus(metav1.ConditionTrue, true)

			By("Create an overridden clusterResourceBinding on member-1")
			member1Binding = createOverriddenClusterResourceBinding(member1Name, gotPolicySnapshot, gotResourceSnapshot)

			By("Create an overridden clusterResourceBinding on member-2")
			member2Binding = createOverriddenClusterResourceBinding(member2Name, gotPolicySnapshot, gotResourceSnapshot)

			By("Validate CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.ResourceScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
							Reason: condition.RolloutStartedReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
							Reason: condition.OverrideNotSpecifiedReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
							Reason: condition.WorkSynchronizedUnknownReason,
						},
					},
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           member1Name,
							ObservedResourceIndex: "0",
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedUnknownReason,
								},
							},
						},
						{
							ClusterName:           member2Name,
							ObservedResourceIndex: "0",
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedUnknownReason,
								},
							},
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric was emitted")
			wantMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.SchedulingUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.RolloutStartedUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.WorkSynchronizedUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(wantMetrics)

			By("Update to a synchronized clusterResourceBinding on member-1")
			member1Binding = updateClusterResourceBindingWithSynchronized(member1Binding)

			By("Update to a synchronized clusterResourceBinding on member-2")
			member2Binding = updateClusterResourceBindingWithSynchronized(member2Binding)

			By("Validate CRP status")
			wantCondition := metav1.Condition{
				Status: metav1.ConditionTrue,
				Type:   string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
				Reason: condition.WorkSynchronizedReason,
			}
			meta.SetStatusCondition(&wantCRP.Status.Conditions, wantCondition)
			wantCondition.Type = string(placementv1beta1.PerClusterWorkSynchronizedConditionType)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[0].Conditions, wantCondition)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[1].Conditions, wantCondition)
			wantCondition = metav1.Condition{
				Status: metav1.ConditionUnknown,
				Type:   string(placementv1beta1.ClusterResourcePlacementDiffReportedConditionType),
				Reason: condition.DiffReportedStatusUnknownReason,
			}
			meta.SetStatusCondition(&wantCRP.Status.Conditions, wantCondition)
			wantCondition.Type = string(placementv1beta1.PerClusterDiffReportedConditionType)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[0].Conditions, wantCondition)
			meta.SetStatusCondition(&wantCRP.Status.PerClusterPlacementStatuses[1].Conditions, wantCondition)
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric for reportDiff was emitted")
			wantMetrics = append(wantMetrics, &prometheusclientmodel.Metric{
				Label: []*prometheusclientmodel.LabelPair{
					{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
					{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
					{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
					{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementDiffReportedConditionType))},
					{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					{Name: ptr.To("reason"), Value: ptr.To(condition.DiffReportedStatusUnknownReason)},
				},
				Gauge: &prometheusclientmodel.Gauge{
					Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
				},
			})
			checkPlacementStatusMetric(wantMetrics)
		})

		It("Emit metrics for ReportDiff Complete CRP", func() {
			By("Update clusterSchedulingPolicySnapshot status to schedule success")
			updateClusterSchedulingPolicySnapshotStatus(metav1.ConditionTrue, true)

			By("Create a synchronized clusterResourceBinding on member-1")
			member1Binding = createOverriddenClusterResourceBinding(member1Name, gotPolicySnapshot, gotResourceSnapshot)

			By("Create a synchronized clusterResourceBinding on member-2")
			member2Binding = createOverriddenClusterResourceBinding(member2Name, gotPolicySnapshot, gotResourceSnapshot)

			By("Validate CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.ResourceScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
							Reason: condition.RolloutStartedReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
							Reason: condition.OverrideNotSpecifiedReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
							Reason: condition.WorkSynchronizedUnknownReason,
						},
					},
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           member1Name,
							ObservedResourceIndex: "0",
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedUnknownReason,
								},
							},
						},
						{
							ClusterName:           member2Name,
							ObservedResourceIndex: "0",
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedUnknownReason,
								},
							},
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric was emitted")
			wantMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.SchedulingUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.RolloutStartedUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.WorkSynchronizedUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(wantMetrics)

			By("Create a reportDiff clusterResourceBinding on member-1")
			member1Binding = updateClusterResourceBindingWithReportDiff(member1Binding)

			By("Create a reportDiff clusterResourceBinding on member-2")
			member2Binding = updateClusterResourceBindingWithReportDiff(member2Binding)

			By("Validate CRP status")
			wantCRP.Status.Conditions = []metav1.Condition{
				{
					Status: metav1.ConditionTrue,
					Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
					Reason: condition.ResourceScheduleSucceededReason,
				},
				{
					Status: metav1.ConditionTrue,
					Type:   string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Reason: condition.RolloutStartedReason,
				},
				{
					Status: metav1.ConditionTrue,
					Type:   string(placementv1beta1.ClusterResourcePlacementOverriddenConditionType),
					Reason: condition.OverrideNotSpecifiedReason,
				},
				{
					Status: metav1.ConditionTrue,
					Type:   string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
					Reason: condition.WorkSynchronizedReason,
				},
				{
					Status: metav1.ConditionTrue,
					Type:   string(placementv1beta1.ClusterResourcePlacementDiffReportedConditionType),
					Reason: condition.DiffReportedStatusTrueReason,
				},
			}
			wantCRP.Status.PerClusterPlacementStatuses = []placementv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           member1Name,
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterScheduledConditionType),
							Reason: condition.ScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
							Reason: condition.RolloutStartedReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
							Reason: condition.OverriddenSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
							Reason: condition.WorkSynchronizedReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterDiffReportedConditionType),
							Reason: condition.DiffReportedStatusTrueReason,
						},
					},
				},
				{
					ClusterName:           member2Name,
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterScheduledConditionType),
							Reason: condition.ScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
							Reason: condition.RolloutStartedReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterOverriddenConditionType),
							Reason: condition.OverriddenSucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
							Reason: condition.WorkSynchronizedReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.PerClusterDiffReportedConditionType),
							Reason: condition.DiffReportedStatusTrueReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status complete metric was emitted")
			wantMetrics = append(wantMetrics,
				&prometheusclientmodel.Metric{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To("Completed")},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionTrue))},
						{Name: ptr.To("reason"), Value: ptr.To("Completed")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			)
			checkPlacementStatusMetric(wantMetrics)
		})
	})

	Context("When creating an ClusterResourcePlacement with user error", func() {
		BeforeEach(func() {
			// Reset metric before each test
			metrics.FleetPlacementStatusLastTimeStampSeconds.Reset()
		})

		AfterEach(func() {
			By("Deleting crp")
			Expect(k8sClient.Delete(ctx, gotCRP)).Should(Succeed())
			retrieveAndValidateCRPDeletion(gotCRP)
		})

		It("Emit metrics for CRP with invalid resource selector", func() {
			By("Create a new crp with an invalid resource selector")
			crp = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "InvalidKind", // Invalid kind to trigger user error
							Name:    "invalid-resource",
						},
					},
					RevisionHistoryLimit: ptr.To(int32(1)),
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed(), "Failed to create crp with user error")

			By("Validate CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionFalse,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.InvalidResourceSelectorsReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(crp.Name, wantCRP)

			By("Ensure placement status metric was emitted")
			wantMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionFalse))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.InvalidResourceSelectorsReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(wantMetrics)
		})
	})

	Context("When creating an ClusterResourcePlacement with External RolloutStrategy", func() {
		BeforeEach(func() {
			// Reset metric before each test
			metrics.FleetPlacementStatusLastTimeStampSeconds.Reset()

			By("Create a new crp with external rollout strategy")
			crp = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"region": "east"},
							},
						},
					},
					RevisionHistoryLimit: ptr.To(int32(1)),
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.ExternalRolloutStrategyType,
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed(), "Failed to create crp with user error")

			By("Check clusterSchedulingPolicySnapshot")
			gotPolicySnapshot = checkClusterSchedulingPolicySnapshot()

			By("Check clusterResourceSnapshot")
			gotResourceSnapshot = checkClusterResourceSnapshot()

			By("Validate CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.SchedulingUnknownReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(crp.Name, wantCRP)
		})

		AfterEach(func() {
			By("Deleting crp")
			Expect(k8sClient.Delete(ctx, gotCRP)).Should(Succeed())
			retrieveAndValidateCRPDeletion(gotCRP)
		})

		It("Emit metrics for CRP with external rollout strategy", func() {
			By("Ensure placement status metric was emitted")
			wantMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
						{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
						{Name: ptr.To("reason"), Value: ptr.To(condition.SchedulingUnknownReason)},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(wantMetrics)

			By("Update clusterSchedulingPolicySnapshot status to schedule success")
			updateClusterSchedulingPolicySnapshotStatus(metav1.ConditionTrue, true)

			By("Validate the CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.ResourceScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
							Reason: condition.RolloutControlledByExternalControllerReason,
						},
					},
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName: member1Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedUnknownReason,
								},
							},
						},
						{
							ClusterName: member2Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.PerClusterScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Reason: condition.RolloutStartedUnknownReason,
								},
							},
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement status metric for rollout external was emitted")
			wantMetrics = append(wantMetrics, &prometheusclientmodel.Metric{
				Label: []*prometheusclientmodel.LabelPair{
					{Name: ptr.To("namespace"), Value: ptr.To(gotCRP.Namespace)},
					{Name: ptr.To("name"), Value: ptr.To(gotCRP.Name)},
					{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(gotCRP.Generation, 10))},
					{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType))},
					{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					{Name: ptr.To("reason"), Value: ptr.To(condition.RolloutControlledByExternalControllerReason)},
				},
				Gauge: &prometheusclientmodel.Gauge{
					Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
				},
			})
			checkPlacementStatusMetric(wantMetrics)
		})
	})

	Context("When creating a ClusterResourcePlacement with NamespaceAccessible StatusReportingScope and nonexistent target namespace", func() {
		BeforeEach(func() {
			By("Create a new crp with NamespaceAccessible StatusReportingScope targeting nonexistent namespace")
			crp = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Namespace",
							Name:    "nonexistent-namespace", // This namespace doesn't exist.
						},
					},
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed(), "Failed to create crp with NamespaceAccessible scope")
		})

		AfterEach(func() {
			By("Deleting crp")
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
			}
			Expect(k8sClient.Delete(ctx, crp)).Should(Succeed())
			retrieveAndValidateCRPDeletion(crp)
		})

		It("Should handle missing target namespace", func() {
			By("Validate CRP status includes StatusSynced condition")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.SchedulingUnknownReason,
						},
						{
							Status: metav1.ConditionFalse,
							Type:   string(placementv1beta1.ClusterResourcePlacementStatusSyncedConditionType),
							Reason: condition.StatusSyncFailedReason,
						},
					},
				},
			}
			retrieveAndValidateClusterResourcePlacement(crp.Name, wantCRP)

			// Namespace doesn't exist, so no CRPS should be created - sanity check.
			By("Ensure no ClusterResourcePlacementStatus is created in the nonexistent namespace")
			Consistently(func() bool {
				crpStatus := &placementv1beta1.ClusterResourcePlacementStatus{}
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testCRPName, Namespace: "nonexistent-namespace"}, crpStatus))
			}, consistentlyTimeout, interval).Should(BeTrue(), "ClusterResourcePlacementStatus should not exist in nonexistent namespace")
		})
	})

	Context("When creating an ClusterResourcePlacement with NamespaceAccessible StatusReportingScope with invalid resource selector", func() {
		namespaceName := "status-namespace"
		BeforeEach(func() {
			By("Create a namespace for status reporting")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).Should(Succeed(), "Failed to create status namespace")
			// Wait for namespace creation
			Eventually(func() error {
				ns := &corev1.Namespace{}
				return k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, ns)
			}, eventuallyTimeout, interval).Should(Succeed(), "Failed to get status namespace")

			By("Create a new crp with an invalid resource selector")
			crp = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Namespace",
							Name:    namespaceName,
						},
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "InvalidKind", // Invalid kind to trigger user error.
							Name:    "invalid-resource",
						},
					},
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed(), "Failed to create crp with user error")
		})

		AfterEach(func() {
			By("Deleting crp")
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
			}
			Expect(k8sClient.Delete(ctx, crp)).Should(Succeed())
			retrieveAndValidateCRPDeletion(crp)

			// Need to manually clean up CRPS in test environment https://book.kubebuilder.io/reference/envtest#testing-considerations.
			By("Deleting crps")
			crps := &placementv1beta1.ClusterResourcePlacementStatus{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testCRPName,
					Namespace: namespaceName,
				},
			}
			Expect(k8sClient.Delete(ctx, crps)).Should(Succeed())
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crps.Name, Namespace: crps.Namespace}, crps))
			}, eventuallyTimeout, interval).Should(BeTrue(), "ClusterResourcePlacementStatus should be deleted")

			By("Deleting status namespace")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			// Namespace cleanup is not supported in envtest https://book.kubebuilder.io/reference/envtest#namespace-usage-limitation.
			Expect(k8sClient.Delete(ctx, namespace)).Should(Succeed())
		})

		It("Should handle invalid resource selector and create CRPS", func() {
			By("Validate CRP status includes both Scheduled and StatusSynced conditions")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.PlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						// ObservedResourceIndex is not set because resource selector is invalid.
						{
							Status: metav1.ConditionFalse,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: condition.InvalidResourceSelectorsReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementStatusSyncedConditionType),
							Reason: condition.StatusSyncSucceededReason,
						},
					},
				},
			}
			retrieveAndValidateClusterResourcePlacement(crp.Name, wantCRP)

			By("Validate CRPS is created and matches CRP status")
			crpsMatchesActual := crpstatussync.CRPSStatusMatchesCRPActual(ctx, k8sClient, testCRPName, namespaceName)
			Eventually(crpsMatchesActual, eventuallyTimeout, interval).Should(Succeed(), "ClusterResourcePlacementStatus should be created and match expected structure")
		})
	})
})

func checkPlacementStatusMetric(wantMetrics []*prometheusclientmodel.Metric) {
	metricFamilies, err := ctrlmetrics.Registry.Gather()
	Expect(err).Should(Succeed())
	var placementStatusMetrics []*prometheusclientmodel.Metric
	for _, mf := range metricFamilies {
		if mf.GetName() == "fleet_workload_placement_status_last_timestamp_seconds" {
			placementStatusMetrics = mf.GetMetric()
		}
	}
	// Sort the emitted metrics for comparison
	Expect(cmp.Diff(placementStatusMetrics, wantMetrics, metricsUtils.MetricsCmpOptions...)).Should(BeEmpty(), "Placement status metrics do not match diff (-got, +want):")
}
