package e2e

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/apis/v1alpha1"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

var _ = Describe("workload orchestration testing with join/leave", Serial, Ordered, func() {
	var (
		crp *v1alpha1.ClusterResourcePlacement
		ctx context.Context

		mcStatusCmpOptions = []cmp.Option{
			cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
			cmpopts.IgnoreFields(v1alpha1.AgentStatus{}, "LastReceivedHeartbeat"),
			cmpopts.IgnoreTypes(v1alpha1.ResourceUsage{}), cmpopts.SortSlices(func(ref1, ref2 metav1.Condition) bool { return ref1.Type < ref2.Type }),
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
		Eventually(func() error {
			if err := HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc); err != nil {
				return err
			}
			if statusDiff := cmp.Diff(wantMCStatus, mc.Status, mcStatusCmpOptions...); statusDiff != "" {
				return fmt.Errorf("member cluster(%s) status mismatch (-want +got):\n%s", mc.Name, statusDiff)
			}
			return nil
		}, 3*testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Failed to wait for member cluster %s to have status %s", mc.Name, wantMCStatus)

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
		testutils.CreateClusterRole(*HubCluster, cr)

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
		testutils.CreateClusterResourcePlacement(*HubCluster, crp)

		By("verify the resource is not propagated to member cluster")
		Consistently(func() error {
			return MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cr.Name}, cr)
		}, testutils.PollTimeout, testutils.PollInterval).ShouldNot(Succeed(), "Failed to verify cluster role %s is not propagated to %s cluster", cr.Name, MemberCluster.ClusterName)

		By("update member cluster in the hub cluster to join")
		Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)).Should(Succeed(), "Failed to retrieve member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)
		mc.Spec.State = v1alpha1.ClusterStateJoin
		Expect(HubCluster.KubeClient.Update(ctx, mc)).Should(Succeed(), "Failed to update member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)

		By("check if member cluster condition is updated to Joined")
		wantMCStatus = v1alpha1.MemberClusterStatus{
			AgentStatus: imcJoinedAgentStatus,
			Conditions:  mcJoinedConditions,
		}
		Eventually(func() error {
			if err := HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc); err != nil {
				return err
			}
			if statusDiff := cmp.Diff(wantMCStatus, mc.Status, mcStatusCmpOptions...); statusDiff != "" {
				return fmt.Errorf("member cluster(%s) status mismatch (-want +got):\n%s", mc.Name, statusDiff)
			}
			return nil
		}, 3*testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Failed to wait for member cluster %s to have status %s", mc.Name, wantMCStatus)

		By("verify that the cluster resource placement is applied")
		testutils.WaitConditionClusterResourcePlacement(*HubCluster, crp, string(v1alpha1.ResourcePlacementStatusConditionTypeApplied), metav1.ConditionTrue, testutils.PollTimeout)

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
		Eventually(func() error {
			if err := HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc); err != nil {
				return err
			}
			if statusDiff := cmp.Diff(wantMCStatus, mc.Status, mcStatusCmpOptions...); statusDiff != "" {
				return fmt.Errorf("member cluster(%s) status mismatch (-want +got):\n%s", mc.Name, statusDiff)
			}
			return nil
		}, 3*testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Failed to wait for internal member cluster %s to have status %s", mc.Name, wantMCStatus)

		By("verify that the resource is still on the member cluster")
		Consistently(func() error {
			return MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cr.Name}, cr)
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Failed to verify cluster role %s is still on %s cluster", cr.Name, MemberCluster.ClusterName)

		By("delete the crp from the hub")
		testutils.DeleteClusterResourcePlacement(*HubCluster, crp)

		By("verify that the resource is still on the member cluster")
		Consistently(func() error {
			return MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: ""}, cr)
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Failed to verify cluster role %s is still on %s cluster", cr.Name, MemberCluster.ClusterName)

		By("delete cluster role on hub cluster")
		testutils.DeleteClusterRole(*HubCluster, cr)

		By("update member cluster in the hub cluster to join")
		Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)).Should(Succeed(), "Failed to retrieve member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)
		mc.Spec.State = v1alpha1.ClusterStateJoin
		Expect(HubCluster.KubeClient.Update(ctx, mc)).Should(Succeed(), "Failed to update member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)

		By("check if member cluster condition is updated to Joined")
		wantMCStatus = v1alpha1.MemberClusterStatus{
			AgentStatus: imcJoinedAgentStatus,
			Conditions:  mcJoinedConditions,
		}
		Eventually(func() error {
			if err := HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc); err != nil {
				return err
			}
			if statusDiff := cmp.Diff(wantMCStatus, mc.Status, mcStatusCmpOptions...); statusDiff != "" {
				return fmt.Errorf("member cluster(%s) status mismatch (-want +got):\n%s", mc.Name, statusDiff)
			}
			return nil
		}, 3*testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Failed to wait for member cluster %s to have status %s", mc.Name, wantMCStatus)
	})
})
