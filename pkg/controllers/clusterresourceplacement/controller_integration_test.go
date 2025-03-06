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
package clusterresourceplacement

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
	"go.goms.io/fleet/pkg/utils/resource"
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
		Name:               testName,
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

func createSynchronizedClusterResourceBinding(cluster string, policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot, resourceSnapshot *placementv1beta1.ClusterResourceSnapshot) {
	binding := createOverriddenClusterResourceBinding(cluster, policySnapshot, resourceSnapshot)
	cond := metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
		Reason:             condition.WorkSynchronizedReason,
		ObservedGeneration: binding.Generation,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, cond)
	Expect(k8sClient.Status().Update(ctx, binding)).Should(Succeed(), "Failed to update the binding status")
}

var _ = Describe("Test ClusterResourcePlacement Controller", func() {
	Context("When creating new pickAll ClusterResourcePlacement", func() {

		var (
			crp                 *placementv1beta1.ClusterResourcePlacement
			gotCRP              *placementv1beta1.ClusterResourcePlacement
			gotPolicySnapshot   *placementv1beta1.ClusterSchedulingPolicySnapshot
			gotResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
		)

		BeforeEach(func() {
			By("Create a new crp")
			crp = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testName,
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
					Name:       testName,
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
			gotCRP = retrieveAndValidateClusterResourcePlacement(testName, wantCRP)
		})

		AfterEach(func() {
			By("Deleting crp")
			Expect(k8sClient.Delete(ctx, gotCRP)).Should(Succeed())
			retrieveAndValidateCRPDeletion(gotCRP)
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
					Name:       testName,
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
			retrieveAndValidateClusterResourcePlacement(testName, wantCRP)
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
			createOverriddenClusterResourceBinding(member1Name, gotPolicySnapshot, gotResourceSnapshot)

			By("By validating the CRP status")
			wantCRP := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testName,
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
			retrieveAndValidateClusterResourcePlacement(testName, wantCRP)

			By("By creating a synchronized clusterResourceBinding on member-2")
			createSynchronizedClusterResourceBinding(member2Name, gotPolicySnapshot, gotResourceSnapshot)

			wantCRP = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testName,
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
			retrieveAndValidateClusterResourcePlacement(testName, wantCRP)
		})
	})
})
