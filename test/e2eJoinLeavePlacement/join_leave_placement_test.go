/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2eJoinLeavePlacement

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	testutils "go.goms.io/fleet/test/e2e/utils"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/apis/v1alpha1"
)

var (
	mc                 *v1alpha1.MemberCluster
	mcStatusCmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
		cmpopts.IgnoreFields(v1alpha1.AgentStatus{}, "LastReceivedHeartbeat"),
		cmpopts.IgnoreTypes(v1alpha1.ResourceUsage{}), cmpopts.SortSlices(func(ref1, ref2 metav1.Condition) bool { return ref1.Type < ref2.Type }),
	}
)

var _ = Describe("workload orchestration testing with join/leave", func() {

	It("Test join and leave with CRP", func() {
		ctx = context.Background()
		//cprName := "join-leave-test"
		//labelKey := "fleet.azure.com/name"
		//labelValue := "test"

		By("deploy member cluster in the hub cluster")
		identity := rbacv1.Subject{
			Name:      "member-agent-sa",
			Kind:      "ServiceAccount",
			Namespace: "fleet-system",
		}
		mc = &v1alpha1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: MemberCluster.ClusterName,
			},
			Spec: v1alpha1.MemberClusterSpec{
				Identity:               identity,
				State:                  v1alpha1.ClusterStateJoin,
				HeartbeatPeriodSeconds: 60,
			},
		}
		Expect(HubCluster.KubeClient.Create(ctx, mc)).Should(Succeed(), "Failed to create member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)

		By("check if member cluster status is updated to joined")
		wantMCStatus := v1alpha1.MemberClusterStatus{
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
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Failed to wait for member cluster %s to have status %s", mc.Name, wantMCStatus)
	})
})
