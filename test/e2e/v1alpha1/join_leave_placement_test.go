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

package v1alpha1

import (
	"context"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubefleet-dev/kubefleet/apis/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/test/e2e/v1alpha1/utils"
)

// Serial - Ginkgo will guarantee that these specs will never run in parallel with other specs.
// This test cannot be run in parallel with other specs in the suite as it's leaving, joining, leaving and joining again.
var _ = Describe("workload orchestration testing with join/leave", Serial, func() {
	var (
		crp *v1alpha1.ClusterResourcePlacement
		ctx context.Context

		mcStatusCmpOptions = []cmp.Option{
			cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
			cmpopts.IgnoreFields(v1alpha1.AgentStatus{}, "LastReceivedHeartbeat"),
			cmpopts.IgnoreTypes(v1alpha1.ResourceUsage{}),
			sortOption,
		}
	)

	It("Test join and leave with CRP", func() {
		ctx = context.Background()
		cprName := "join-leave-test"
		labelKey := "fleet.azure.com/name"
		labelValue := "test"

		By("update member cluster in the hub cluster to leave")
		Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)).Should(Succeed(), "Failed to retrieve member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)
		mc.Spec.State = v1alpha1.ClusterStateLeave
		Expect(HubCluster.KubeClient.Update(ctx, mc)).Should(Succeed(), "Failed to update member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)

		By("check if member cluster status is updated to Left")
		wantMCStatus := v1alpha1.MemberClusterStatus{
			AgentStatus: imcLeftAgentStatus,
			Conditions:  mcLeftConditions,
		}
		utils.CheckMemberClusterStatus(ctx, *HubCluster, &types.NamespacedName{Name: mc.Name}, wantMCStatus, mcStatusCmpOptions)

		By("create the resources to be propagated")
		cr := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "jlp-test-cluster-role",
				Labels: map[string]string{labelKey: labelValue},
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "list", "watch"},
					APIGroups: []string{""},
					Resources: []string{"secrets"},
				},
			},
		}
		Expect(HubCluster.KubeClient.Create(ctx, cr)).Should(Succeed(), "Failed to create cluster role %s in %s cluster", cr.Name, HubCluster.ClusterName)

		By("create the cluster resource placement in the hub cluster")
		crp = &v1alpha1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: cprName,
			},
			Spec: v1alpha1.ClusterResourcePlacementSpec{
				ResourceSelectors: []v1alpha1.ClusterResourceSelector{
					{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: cr.Labels,
						},
					},
				},
			},
		}
		Expect(HubCluster.KubeClient.Create(ctx, crp)).Should(Succeed(), "Failed to create cluster resource placement %s in %s cluster", crp.Name, HubCluster.ClusterName)

		By("verify the resource is not propagated to member cluster")
		Consistently(func() bool {
			return apierrors.IsNotFound(MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cr.Name}, cr))
		}, utils.PollTimeout, utils.PollInterval).Should(BeTrue(), "Failed to verify cluster role %s is not propagated to %s cluster", cr.Name, MemberCluster.ClusterName)

		By("update member cluster in the hub cluster to join")
		Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)).Should(Succeed(), "Failed to retrieve member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)
		mc.Spec.State = v1alpha1.ClusterStateJoin
		Expect(HubCluster.KubeClient.Update(ctx, mc)).Should(Succeed(), "Failed to update member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)

		By("check if member cluster condition is updated to Joined")
		wantMCStatus = v1alpha1.MemberClusterStatus{
			AgentStatus: imcJoinedAgentStatus,
			Conditions:  mcJoinedConditions,
		}
		utils.CheckMemberClusterStatus(ctx, *HubCluster, &types.NamespacedName{Name: mc.Name}, wantMCStatus, mcStatusCmpOptions)

		By("verify that the cluster resource placement is applied")
		crpStatus := v1alpha1.ClusterResourcePlacementStatus{
			Conditions: []metav1.Condition{
				{
					Reason: "ScheduleSucceeded",
					Status: metav1.ConditionTrue,
					Type:   string(v1alpha1.ResourcePlacementConditionTypeScheduled),
				},
				{
					Reason: "ApplySucceeded",
					Status: metav1.ConditionTrue,
					Type:   string(v1alpha1.ResourcePlacementStatusConditionTypeApplied),
				},
			},
			SelectedResources: []v1alpha1.ResourceIdentifier{
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    cr.Name,
				},
			},
			TargetClusters: []string{"kind-member-testing"},
		}
		utils.WaitCreateClusterResourcePlacementStatus(ctx, *HubCluster, &types.NamespacedName{Name: crp.Name}, crpStatus, crpStatusCmpOptions, 3*utils.PollTimeout)

		By("verify the resource is propagated to member cluster")
		Expect(MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cr.Name}, cr)).Should(Succeed(), "Failed to verify cluster role %s is propagated to %s cluster", cr.Name, MemberCluster.ClusterName)

		By("update member cluster in the hub cluster to leave")
		Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)).Should(Succeed(), "Failed to retrieve member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)
		mc.Spec.State = v1alpha1.ClusterStateLeave
		Expect(HubCluster.KubeClient.Update(ctx, mc)).Should(Succeed(), "Failed to update member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)

		By("verify that member cluster is marked as left")
		wantMCStatus = v1alpha1.MemberClusterStatus{
			AgentStatus: imcLeftAgentStatus,
			Conditions:  mcLeftConditions,
		}
		utils.CheckMemberClusterStatus(ctx, *HubCluster, &types.NamespacedName{Name: mc.Name}, wantMCStatus, mcStatusCmpOptions)

		By("verify that the resource is still on the member cluster")
		Consistently(func() error {
			return MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cr.Name}, cr)
		}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to verify cluster role %s is still on %s cluster", cr.Name, MemberCluster.ClusterName)

		By("delete the crp from the hub")
		utils.DeleteClusterResourcePlacement(ctx, *HubCluster, crp)

		By("verify that the resource is still on the member cluster")
		Consistently(func() error {
			return MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: ""}, cr)
		}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to verify cluster role %s is still on %s cluster", cr.Name, MemberCluster.ClusterName)

		By("delete cluster role on hub cluster")
		Expect(HubCluster.KubeClient.Delete(ctx, cr)).Should(Succeed(), "Failed to delete cluster role %s in %s cluster", cr.Name, HubCluster.ClusterName)

		By("delete cluster role on member cluster")
		Expect(MemberCluster.KubeClient.Delete(ctx, cr)).Should(Succeed(), "Failed to delete cluster role %s in %s cluster", cr.Name, MemberCluster.ClusterName)

		By("update member cluster in the hub cluster to join")
		Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)).Should(Succeed(), "Failed to retrieve member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)
		mc.Spec.State = v1alpha1.ClusterStateJoin
		Expect(HubCluster.KubeClient.Update(ctx, mc)).Should(Succeed(), "Failed to update member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)

		By("check if member cluster condition is updated to Joined")
		wantMCStatus = v1alpha1.MemberClusterStatus{
			AgentStatus: imcJoinedAgentStatus,
			Conditions:  mcJoinedConditions,
		}
		utils.CheckMemberClusterStatus(ctx, *HubCluster, &types.NamespacedName{Name: mc.Name}, wantMCStatus, mcStatusCmpOptions)
	})
})
