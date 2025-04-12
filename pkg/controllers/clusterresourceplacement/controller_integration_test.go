/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package clusterresourceplacement

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	prometheusclientmodel "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller/metrics"
	"go.goms.io/fleet/pkg/utils/resource"
	metricsUtils "go.goms.io/fleet/test/utils/metrics"
)

const (
	member1Name = "member-1"
	member2Name = "member-2"

	eventuallyTimeout = time.Second * 10
	interval          = time.Millisecond * 250
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
		cmpopts.SortSlices(func(c1, c2 placementv1beta1.ResourcePlacementStatus) bool {
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

func retrieveAndValidatePolicySnapshot(crp *placementv1beta1.ClusterResourcePlacement, want *placementv1beta1.ClusterSchedulingPolicySnapshot) *placementv1beta1.ClusterSchedulingPolicySnapshot {
	policySnapshotList := &placementv1beta1.ClusterSchedulingPolicySnapshotList{}
	Eventually(func() error {
		if err := k8sClient.List(ctx, policySnapshotList, client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crp.Name}); err != nil {
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
		if err := k8sClient.List(ctx, resourceSnapshotList, client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crp.Name}); err != nil {
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
		if err := k8sClient.List(ctx, policySnapshotList, client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crp.Name}); err != nil {
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
		if err := k8sClient.List(ctx, resourceSnapshotList, client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crp.Name}); err != nil {
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
				placementv1beta1.CRPTrackingLabel: resourceSnapshot.Labels[placementv1beta1.CRPTrackingLabel],
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

func createAvailableClusterResourceBinding(cluster string, policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot, resourceSnapshot *placementv1beta1.ClusterResourceSnapshot) *placementv1beta1.ClusterResourceBinding {
	binding := createSynchronizedClusterResourceBinding(cluster, policySnapshot, resourceSnapshot)
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
		Type:               string(placementv1beta1.ResourcesDiffReportedConditionType),
		Reason:             condition.DiffReportedStatusTrueReason,
		ObservedGeneration: binding.Generation,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, cond)
	Expect(k8sClient.Status().Update(ctx, binding)).Should(Succeed(), "Failed to update the binding status")
	return binding
}

var _ = Describe("Test ClusterResourcePlacement Controller", func() {
	Context("When creating new pickAll ClusterResourcePlacement", func() {
		var (
			customRegistry      *prometheus.Registry
			crp                 *placementv1beta1.ClusterResourcePlacement
			gotCRP              *placementv1beta1.ClusterResourcePlacement
			gotPolicySnapshot   *placementv1beta1.ClusterSchedulingPolicySnapshot
			gotResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
			member1Binding      *placementv1beta1.ClusterResourceBinding
			member2Binding      *placementv1beta1.ClusterResourceBinding
		)

		BeforeEach(func() {
			// Create a test registry
			customRegistry = prometheus.NewRegistry()
			Expect(customRegistry.Register(metrics.FleetPlacementCompleteLastTimeStampSeconds)).Should(Succeed())
			Expect(customRegistry.Register(metrics.FleetPlacementStatusLastTimeStampSeconds)).Should(Succeed())
			metrics.FleetPlacementCompleteLastTimeStampSeconds.Reset()
			metrics.FleetPlacementStatusLastTimeStampSeconds.Reset()

			By("Create a new crp")
			crp = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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

			By("By checking clusterSchedulingPolicySnapshot")
			policyHash, err := resource.HashOf(crp.Spec.Policy)
			Expect(err).Should(Succeed(), "failed to create policy hash")

			wantPolicySnapshot := placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.PolicySnapshotNameFmt, crp.Name, 0),
					Labels: map[string]string{
						placementv1beta1.CRPTrackingLabel:      crp.Name,
						placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
						placementv1beta1.PolicyIndexLabel:      strconv.Itoa(0),
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
			gotPolicySnapshot = retrieveAndValidatePolicySnapshot(crp, &wantPolicySnapshot)

			By("By checking clusterResourceSnapshot")
			emptyResources := &placementv1beta1.ResourceSnapshotSpec{
				SelectedResources: []placementv1beta1.ResourceContent{},
			}
			jsonBytes, err := resource.HashOf(emptyResources)
			Expect(err).Should(Succeed(), "Failed to create resource hash")

			wantResourceSnapshot := &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crp.Name, 0),
					Labels: map[string]string{
						placementv1beta1.CRPTrackingLabel:      crp.Name,
						placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
						placementv1beta1.ResourceIndexLabel:    strconv.Itoa(0),
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
			gotResourceSnapshot = retrieveAndValidateResourceSnapshot(crp, wantResourceSnapshot)
			By("By checking CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: SchedulingUnknownReason,
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

			Expect(customRegistry.Unregister(metrics.FleetPlacementCompleteLastTimeStampSeconds)).Should(BeTrue())
			Expect(customRegistry.Unregister(metrics.FleetPlacementStatusLastTimeStampSeconds)).Should(BeTrue())
		})

		It("None of the clusters are selected", func() {
			By("By updating clusterSchedulingPolicySnapshot status to schedule success")
			scheduledCondition := metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Reason:             ResourceScheduleSucceededReason,
				ObservedGeneration: gotCRP.Generation,
			}
			meta.SetStatusCondition(&gotPolicySnapshot.Status.Conditions, scheduledCondition)
			gotPolicySnapshot.Status.ObservedCRPGeneration = gotCRP.Generation
			Expect(k8sClient.Status().Update(ctx, gotPolicySnapshot)).Should(Succeed(), "Failed to update the policy snapshot status")

			By("By validating the CRP status has only scheduling condition")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: ResourceScheduleSucceededReason,
						},
					},
				},
			}
			retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement complete metric was emitted with isCompleted False")
			wantCompleteMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("isCompleted"), Value: ptr.To("false")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementCompleteMetric(customRegistry, wantCompleteMetrics)

			By("Ensure placement status metric was emitted")
			// A metric for every !true (nil, unknown, false) status as the CRP reconciles.
			// In this case, no clusters are selected therefore with pickAll policy therefore there is nothing to rollout
			// so the RolloutStarted condition is nil.
			wantStatusMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To("nil")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(customRegistry, wantStatusMetrics)
		})

		It("Clusters are not selected", func() {
			By("By updating clusterSchedulingPolicySnapshot status to scheduling failed ")
			scheduledCondition := metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Reason:             ResourceScheduleFailedReason,
				ObservedGeneration: gotCRP.Generation,
			}
			meta.SetStatusCondition(&gotPolicySnapshot.Status.Conditions, scheduledCondition)
			gotPolicySnapshot.Status.ObservedCRPGeneration = gotCRP.Generation
			gotPolicySnapshot.Status.ClusterDecisions = []placementv1beta1.ClusterDecision{
				{
					ClusterName: member1Name,
					Selected:    false,
					Reason:      "invalid",
				},
				{
					ClusterName: member2Name,
					Selected:    false,
					Reason:      "invalid",
				},
			}
			Expect(k8sClient.Status().Update(ctx, gotPolicySnapshot)).Should(Succeed(), "Failed to update the policy snapshot status")

			By("By validating the CRP status has only scheduling condition")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionFalse,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: ResourceScheduleFailedReason,
						},
					},
				},
			}
			retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement complete metric was emitted with isCompleted False")
			wantCompleteMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("isCompleted"), Value: ptr.To("false")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementCompleteMetric(customRegistry, wantCompleteMetrics)

			By("Ensure placement status metric was emitted")
			// A metric for the different reconcilations for all the !true statuses as the CRP reconciles.
			// In this case we have 2 metrics for 1 condition type as Scheduled goes from `Unknown` to `False`.
			wantStatusMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionFalse))},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(customRegistry, wantStatusMetrics)
		})

		It("Clusters are selected and resources are applied successfully", func() {
			By("By updating clusterSchedulingPolicySnapshot status to schedule success")
			scheduledCondition := metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Reason:             ResourceScheduleSucceededReason,
				ObservedGeneration: gotCRP.Generation,
			}
			meta.SetStatusCondition(&gotPolicySnapshot.Status.Conditions, scheduledCondition)
			gotPolicySnapshot.Status.ObservedCRPGeneration = gotCRP.Generation
			gotPolicySnapshot.Status.ClusterDecisions = []placementv1beta1.ClusterDecision{
				{
					ClusterName: member1Name,
					Selected:    true,
					Reason:      "valid",
				},
				{
					ClusterName: member2Name,
					Selected:    true,
					Reason:      "valid",
				},
			}
			Expect(k8sClient.Status().Update(ctx, gotPolicySnapshot)).Should(Succeed(), "Failed to update the policy snapshot status")

			By("By creating clusterResourceBinding on member-1")
			member1Binding = createOverriddenClusterResourceBinding(member1Name, gotPolicySnapshot, gotResourceSnapshot)

			By("By validating the CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: ResourceScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
							Reason: condition.RolloutStartedUnknownReason,
						},
					},
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{
							ClusterName: member1Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedUnknownReason,
								},
							},
						},
						{
							ClusterName: member2Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedUnknownReason,
								},
							},
						},
					},
				},
			}
			retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement complete metric was emitted with isCompleted False")
			wantCompleteMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("isCompleted"), Value: ptr.To("false")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementCompleteMetric(customRegistry, wantCompleteMetrics)

			By("Ensure placement status metric was emitted")
			// There are two metrics because 1 member cluster has an unknown RolloutStarted status.
			// We emit the !true status for CRP as it reconciles. In this case, CRP is still rolling out.
			wantStatusMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(customRegistry, wantStatusMetrics)

			By("By creating a synchronized clusterResourceBinding on member-2")
			member2Binding = createSynchronizedClusterResourceBinding(member2Name, gotPolicySnapshot, gotResourceSnapshot)

			wantCRP = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: ResourceScheduleSucceededReason,
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
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{

							ClusterName: member1Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedUnknownReason,
								},
							},
						},
						{
							ClusterName: member2Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.ResourcesAppliedConditionType),
									Reason: condition.ApplyPendingReason,
								},
							},
						},
					},
				},
			}
			crp = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement complete metric was emitted with isCompleted False")
			checkPlacementCompleteMetric(customRegistry, wantCompleteMetrics)

			By("Ensure placement status metric was emitted")
			wantStatusMetrics = append(wantStatusMetrics, &prometheusclientmodel.Metric{
				Label: []*prometheusclientmodel.LabelPair{
					{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
					{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
					{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType))},
					{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
				},
				Gauge: &prometheusclientmodel.Gauge{
					Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
				},
			})
			checkPlacementStatusMetric(customRegistry, wantStatusMetrics)
		})

		It("Emit metrics when CRP spec updates with different generations", func() {
			By("By updating clusterSchedulingPolicySnapshot status to schedule success")
			scheduledCondition := metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Reason:             ResourceScheduleSucceededReason,
				ObservedGeneration: gotCRP.Generation,
			}
			meta.SetStatusCondition(&gotPolicySnapshot.Status.Conditions, scheduledCondition)
			gotPolicySnapshot.Status.ObservedCRPGeneration = gotCRP.Generation
			gotPolicySnapshot.Status.ClusterDecisions = []placementv1beta1.ClusterDecision{
				{
					ClusterName: member1Name,
					Selected:    true,
					Reason:      "valid",
				},
				{
					ClusterName: member2Name,
					Selected:    true,
					Reason:      "valid",
				},
			}
			Expect(k8sClient.Status().Update(ctx, gotPolicySnapshot)).Should(Succeed(), "Failed to update the policy snapshot status")

			By("By creating clusterResourceBinding on member-1")
			member1Binding = createOverriddenClusterResourceBinding(member1Name, gotPolicySnapshot, gotResourceSnapshot)

			By("By validating the CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: ResourceScheduleSucceededReason,
						},
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
							Reason: condition.RolloutStartedUnknownReason,
						},
					},
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{
							ClusterName: member1Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedUnknownReason,
								},
							},
						},
						{
							ClusterName: member2Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedUnknownReason,
								},
							},
						},
					},
				},
			}
			retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement complete metric was emitted with isCompleted False")
			wantCompleteMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("isCompleted"), Value: ptr.To("false")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementCompleteMetric(customRegistry, wantCompleteMetrics)

			By("Ensure placement status metric was emitted")
			wantStatusMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(customRegistry, wantStatusMetrics)

			By("By creating a synchronized clusterResourceBinding on member-2")
			member2Binding = createSynchronizedClusterResourceBinding(member2Name, gotPolicySnapshot, gotResourceSnapshot)

			wantCRP = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: ResourceScheduleSucceededReason,
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
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{

							ClusterName: member1Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedUnknownReason,
								},
							},
						},
						{
							ClusterName: member2Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.ResourcesAppliedConditionType),
									Reason: condition.ApplyPendingReason,
								},
							},
						},
					},
				},
			}
			crp = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement complete metric was emitted with isCompleted False")
			checkPlacementCompleteMetric(customRegistry, wantCompleteMetrics)

			By("Ensure placement status metric was emitted")
			wantStatusMetrics = append(wantStatusMetrics, &prometheusclientmodel.Metric{
				Label: []*prometheusclientmodel.LabelPair{
					{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
					{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
					{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType))},
					{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
				},
				Gauge: &prometheusclientmodel.Gauge{
					Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
				},
			})
			checkPlacementStatusMetric(customRegistry, wantStatusMetrics)

			By("Update CRP spec to add another resource selector")
			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors,
				placementv1beta1.ClusterResourceSelector{
					Group:   corev1.GroupName,
					Version: "v1",
					Kind:    "Namespace",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"region": "west"},
					},
				})
			Expect(k8sClient.Update(ctx, crp)).Should(Succeed(), "Failed to update crp")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRPName}, crp)).Should(BeNil(), "Get() clusterResourcePlacement mismatch")

			By("By validating the CRP status with new spec")
			wantCRP = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionUnknown,
							Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason:             SchedulingUnknownReason,
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
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement complete metric was emitted with isCompleted False")
			checkPlacementCompleteMetric(customRegistry, wantCompleteMetrics)

			By("Ensure placement status metric was emitted with different generations")
			// When a CRP spec is updated, the generation of the CRP changes. Therefore, the observed generation for the conditions will also change.
			// Should have multiples of same condition type with different generations.
			// In this case we have 2 metrics for Scheduled condition type as crp generation goes from 1 to 2.
			wantStatusMetrics = append(wantStatusMetrics, &prometheusclientmodel.Metric{
				Label: []*prometheusclientmodel.LabelPair{
					{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
					{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
					{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
					{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
				},
				Gauge: &prometheusclientmodel.Gauge{
					Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
				},
			})
			checkPlacementStatusMetric(customRegistry, wantStatusMetrics)
		})

		It("Emit metrics for complete CRP", func() {
			By("By checking CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: SchedulingUnknownReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("By updating clusterSchedulingPolicySnapshot status to schedule success")
			scheduledCondition := metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Reason:             ResourceScheduleSucceededReason,
				ObservedGeneration: gotCRP.Generation,
			}
			meta.SetStatusCondition(&gotPolicySnapshot.Status.Conditions, scheduledCondition)
			gotPolicySnapshot.Status.ObservedCRPGeneration = gotCRP.Generation
			gotPolicySnapshot.Status.ClusterDecisions = []placementv1beta1.ClusterDecision{
				{
					ClusterName: member1Name,
					Selected:    true,
					Reason:      "valid",
				},
				{
					ClusterName: member2Name,
					Selected:    true,
					Reason:      "valid",
				},
			}
			Expect(k8sClient.Status().Update(ctx, gotPolicySnapshot)).Should(Succeed(), "Failed to update the policy snapshot status")

			By("By creating a synchronized clusterResourceBinding on member-1")
			member1Binding = createAvailableClusterResourceBinding(member1Name, gotPolicySnapshot, gotResourceSnapshot)

			By("By creating a synchronized clusterResourceBinding on member-2")
			member2Binding = createAvailableClusterResourceBinding(member2Name, gotPolicySnapshot, gotResourceSnapshot)

			wantCRP = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: ResourceScheduleSucceededReason,
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
							Type:   string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
							Reason: condition.ApplySucceededReason,
						},
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementAvailableConditionType),
							Reason: condition.AvailableReason,
						},
					},
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{

							ClusterName: member1Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourcesAppliedConditionType),
									Reason: condition.ApplySucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourcesAvailableConditionType),
									Reason: condition.AvailableReason,
								},
							},
						},
						{
							ClusterName: member2Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourcesAppliedConditionType),
									Reason: condition.ApplySucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourcesAvailableConditionType),
									Reason: condition.AvailableReason,
								},
							},
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement complete metric was emitted with isCompleted True")
			wantCompleteMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("isCompleted"), Value: ptr.To("false")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("isCompleted"), Value: ptr.To("true")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementCompleteMetric(customRegistry, wantCompleteMetrics)
		})
	})

	Context("When creating a ReportDiff ClusterResourcePlacement", func() {
		var (
			customRegistry      *prometheus.Registry
			crp                 *placementv1beta1.ClusterResourcePlacement
			gotCRP              *placementv1beta1.ClusterResourcePlacement
			gotPolicySnapshot   *placementv1beta1.ClusterSchedulingPolicySnapshot
			gotResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
			member1Binding      *placementv1beta1.ClusterResourceBinding
			member2Binding      *placementv1beta1.ClusterResourceBinding
		)

		BeforeEach(func() {
			// Create a test registry
			customRegistry = prometheus.NewRegistry()
			Expect(customRegistry.Register(metrics.FleetPlacementCompleteLastTimeStampSeconds)).Should(Succeed())
			Expect(customRegistry.Register(metrics.FleetPlacementStatusLastTimeStampSeconds)).Should(Succeed())
			metrics.FleetPlacementCompleteLastTimeStampSeconds.Reset()
			metrics.FleetPlacementStatusLastTimeStampSeconds.Reset()

			By("Create a new crp")
			crp = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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

			By("By checking clusterSchedulingPolicySnapshot")
			policyHash, err := resource.HashOf(crp.Spec.Policy)
			Expect(err).Should(Succeed(), "failed to create policy hash")

			wantPolicySnapshot := placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.PolicySnapshotNameFmt, crp.Name, 0),
					Labels: map[string]string{
						placementv1beta1.CRPTrackingLabel:      crp.Name,
						placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
						placementv1beta1.PolicyIndexLabel:      strconv.Itoa(0),
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
			gotPolicySnapshot = retrieveAndValidatePolicySnapshot(crp, &wantPolicySnapshot)

			By("By checking clusterResourceSnapshot")
			emptyResources := &placementv1beta1.ResourceSnapshotSpec{
				SelectedResources: []placementv1beta1.ResourceContent{},
			}
			jsonBytes, err := resource.HashOf(emptyResources)
			Expect(err).Should(Succeed(), "Failed to create resource hash")

			wantResourceSnapshot := &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crp.Name, 0),
					Labels: map[string]string{
						placementv1beta1.CRPTrackingLabel:      crp.Name,
						placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
						placementv1beta1.ResourceIndexLabel:    strconv.Itoa(0),
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
			gotResourceSnapshot = retrieveAndValidateResourceSnapshot(crp, wantResourceSnapshot)
			By("By checking CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: SchedulingUnknownReason,
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

			Expect(customRegistry.Unregister(metrics.FleetPlacementCompleteLastTimeStampSeconds)).Should(BeTrue())
			Expect(customRegistry.Unregister(metrics.FleetPlacementStatusLastTimeStampSeconds)).Should(BeTrue())
		})

		It("Emit metrics for ReportDiff Incomplete CRP", func() {
			By("By checking CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: SchedulingUnknownReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("By updating clusterSchedulingPolicySnapshot status to schedule success")
			scheduledCondition := metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Reason:             ResourceScheduleSucceededReason,
				ObservedGeneration: gotCRP.Generation,
			}
			meta.SetStatusCondition(&gotPolicySnapshot.Status.Conditions, scheduledCondition)
			gotPolicySnapshot.Status.ObservedCRPGeneration = gotCRP.Generation
			gotPolicySnapshot.Status.ClusterDecisions = []placementv1beta1.ClusterDecision{
				{
					ClusterName: member1Name,
					Selected:    true,
					Reason:      "valid",
				},
				{
					ClusterName: member2Name,
					Selected:    true,
					Reason:      "valid",
				},
			}
			Expect(k8sClient.Status().Update(ctx, gotPolicySnapshot)).Should(Succeed(), "Failed to update the policy snapshot status")

			By("By creating a synchronized clusterResourceBinding on member-1")
			member1Binding = createSynchronizedClusterResourceBinding(member1Name, gotPolicySnapshot, gotResourceSnapshot)

			By("By creating a synchronized clusterResourceBinding on member-2")
			member2Binding = createSynchronizedClusterResourceBinding(member2Name, gotPolicySnapshot, gotResourceSnapshot)

			wantCRP = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: ResourceScheduleSucceededReason,
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
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementDiffReportedConditionType),
							Reason: condition.DiffReportedStatusUnknownReason,
						},
					},
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{

							ClusterName: member1Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.ResourcesDiffReportedConditionType),
									Reason: condition.DiffReportedStatusUnknownReason,
								},
							},
						},
						{
							ClusterName: member2Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedReason,
								},
								{
									Status: metav1.ConditionUnknown,
									Type:   string(placementv1beta1.ResourcesDiffReportedConditionType),
									Reason: condition.DiffReportedStatusUnknownReason,
								},
							},
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement complete metric was emitted with isCompleted True")
			wantCompleteMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("isCompleted"), Value: ptr.To("false")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementCompleteMetric(customRegistry, wantCompleteMetrics)

			By("Ensure placement status metric was emitted")
			wantStatusMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementScheduledConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(crp.Generation, 10))},
						{Name: ptr.To("conditionType"), Value: ptr.To(string(placementv1beta1.ClusterResourcePlacementDiffReportedConditionType))},
						{Name: ptr.To("status"), Value: ptr.To(string(corev1.ConditionUnknown))},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementStatusMetric(customRegistry, wantStatusMetrics)
		})

		It("Emit metrics for ReportDiff Complete CRP", func() {
			By("By checking CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionUnknown,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: SchedulingUnknownReason,
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("By updating clusterSchedulingPolicySnapshot status to schedule success")
			scheduledCondition := metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Reason:             ResourceScheduleSucceededReason,
				ObservedGeneration: gotCRP.Generation,
			}
			meta.SetStatusCondition(&gotPolicySnapshot.Status.Conditions, scheduledCondition)
			gotPolicySnapshot.Status.ObservedCRPGeneration = gotCRP.Generation
			gotPolicySnapshot.Status.ClusterDecisions = []placementv1beta1.ClusterDecision{
				{
					ClusterName: member1Name,
					Selected:    true,
					Reason:      "valid",
				},
				{
					ClusterName: member2Name,
					Selected:    true,
					Reason:      "valid",
				},
			}
			Expect(k8sClient.Status().Update(ctx, gotPolicySnapshot)).Should(Succeed(), "Failed to update the policy snapshot status")

			By("By creating a synchronized clusterResourceBinding on member-1")
			member1Binding = createSynchronizedClusterResourceBinding(member1Name, gotPolicySnapshot, gotResourceSnapshot)
			member1Binding = updateClusterResourceBindingWithReportDiff(member1Binding)

			By("By creating a synchronized clusterResourceBinding on member-2")
			member2Binding = createSynchronizedClusterResourceBinding(member2Name, gotPolicySnapshot, gotResourceSnapshot)
			member2Binding = updateClusterResourceBindingWithReportDiff(member2Binding)

			wantCRP = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{placementv1beta1.ClusterResourcePlacementCleanupFinalizer},
				},
				Spec: crp.Spec,
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionTrue,
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Reason: ResourceScheduleSucceededReason,
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
					},
					PlacementStatuses: []placementv1beta1.ResourcePlacementStatus{
						{

							ClusterName: member1Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourcesDiffReportedConditionType),
									Reason: condition.DiffReportedStatusTrueReason,
								},
							},
						},
						{
							ClusterName: member2Name,
							Conditions: []metav1.Condition{
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceScheduledConditionType),
									Reason: condition.ScheduleSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceRolloutStartedConditionType),
									Reason: condition.RolloutStartedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceOverriddenConditionType),
									Reason: condition.OverriddenSucceededReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourceWorkSynchronizedConditionType),
									Reason: condition.WorkSynchronizedReason,
								},
								{
									Status: metav1.ConditionTrue,
									Type:   string(placementv1beta1.ResourcesDiffReportedConditionType),
									Reason: condition.DiffReportedStatusTrueReason,
								},
							},
						},
					},
				},
			}
			gotCRP = retrieveAndValidateClusterResourcePlacement(testCRPName, wantCRP)

			By("Ensure placement complete metric was emitted with isCompleted True")
			wantCompleteMetrics := []*prometheusclientmodel.Metric{
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("isCompleted"), Value: ptr.To("false")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
				{
					Label: []*prometheusclientmodel.LabelPair{
						{Name: ptr.To("name"), Value: ptr.To(crp.Name)},
						{Name: ptr.To("isCompleted"), Value: ptr.To("true")},
					},
					Gauge: &prometheusclientmodel.Gauge{
						Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
					},
				},
			}
			checkPlacementCompleteMetric(customRegistry, wantCompleteMetrics)
		})
	})
})

func checkPlacementCompleteMetric(registry *prometheus.Registry, wantMetrics []*prometheusclientmodel.Metric) {
	metricFamilies, err := registry.Gather()
	Expect(err).Should(Succeed())
	var placementCompleteMetrics []*prometheusclientmodel.Metric
	for _, mf := range metricFamilies {
		if mf.GetName() == "fleet_workload_placement_complete_last_timestamp_seconds" {
			placementCompleteMetrics = mf.GetMetric()
		}
	}
	Expect(cmp.Diff(placementCompleteMetrics, wantMetrics, metricsUtils.MetricsCmpOptions...)).Should(BeEmpty(), "Placement complete metrics do not match diff (-got, +want):")
}

func checkPlacementStatusMetric(registry *prometheus.Registry, wantMetrics []*prometheusclientmodel.Metric) {
	metricFamilies, err := registry.Gather()
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
