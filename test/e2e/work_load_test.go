/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/apis/v1alpha1"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

var _ = Describe("workload orchestration testing", func() {
	var mc *v1alpha1.MemberCluster
	var sa *corev1.ServiceAccount
	var imc *v1alpha1.InternalMemberCluster
	var cr *rbacv1.ClusterRole
	var crp *v1alpha1.ClusterResourcePlacement
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()

		By("prepare resources in member cluster")
		// create testing NS in member cluster
		sa = testutils.NewServiceAccount(MemberCluster.ClusterName, memberNamespace.Name)
		testutils.CreateServiceAccount(*MemberCluster, sa)

		By("deploy member cluster in the hub cluster")
		mc = testutils.NewMemberCluster(MemberCluster.ClusterName, 60, v1alpha1.ClusterStateJoin)
		testutils.CreateMemberCluster(*HubCluster, mc)

		By("check if internal member cluster created in the hub cluster")
		imc = testutils.NewInternalMemberCluster(MemberCluster.ClusterName, memberNamespace.Name)
		testutils.WaitInternalMemberCluster(*HubCluster, imc)

		By("check if internal member cluster condition is updated to Joined")
		testutils.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.AgentJoined, v1.ConditionTrue, 3*testutils.PollTimeout)
		By("check if member cluster condition is updated to Joined")
		testutils.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoined, v1.ConditionTrue, 3*testutils.PollTimeout)
	})

	AfterEach(func() {
		testutils.DeleteMemberCluster(ctx, *HubCluster, mc)
		testutils.DeleteServiceAccount(*MemberCluster, sa)
	})

	It("Apply CRP and check if work gets propagated", func() {
		workName := "resource-label-selector"
		labelKey := "fleet.azure.com/name"
		labelValue := "test"
		By("create the resources to be propagated")
		cr = &rbacv1.ClusterRole{
			ObjectMeta: v1.ObjectMeta{
				Name:   "test-cluster-role",
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
			ObjectMeta: v1.ObjectMeta{Name: "resource-label-selector"},
			Spec: v1alpha1.ClusterResourcePlacementSpec{
				ResourceSelectors: []v1alpha1.ClusterResourceSelector{
					{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						LabelSelector: &v1.LabelSelector{
							MatchLabels: map[string]string{"fleet.azure.com/name": "test"},
						},
					},
				},
			},
		}
		testutils.CreateClusterResourcePlacement(*HubCluster, crp)

		By("check if work gets created for cluster resource placement")
		testutils.WaitWork(ctx, *HubCluster, workName, memberNamespace.Name)

		By("check if cluster resource placement is updated to Scheduled & Applied")
		testutils.WaitConditionClusterResourcePlacement(*HubCluster, crp, string(v1alpha1.ResourcePlacementConditionTypeScheduled), v1.ConditionTrue, 3*testutils.PollTimeout)
		testutils.WaitConditionClusterResourcePlacement(*HubCluster, crp, string(v1alpha1.ResourcePlacementStatusConditionTypeApplied), v1.ConditionTrue, 3*testutils.PollTimeout)

		By("check if resource is propagated to member cluster")
		testutils.WaitClusterRole(*MemberCluster, cr)

		By("delete cluster resource placement & cluster role on hub cluster")
		testutils.DeleteClusterResourcePlacement(*HubCluster, crp)
		Eventually(func() bool {
			err := HubCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: crp.Name, Namespace: ""}, crp)
			return apierrors.IsNotFound(err)
		}, testutils.PollTimeout, testutils.PollInterval).Should(Equal(true))
		testutils.DeleteClusterRole(*HubCluster, cr)
	})
})
