/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	testutils "go.goms.io/fleet/test/e2e/utils"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.goms.io/fleet/apis/v1alpha1"
)

var _ = Describe("workload orchestration testing", func() {
	var crp *v1alpha1.ClusterResourcePlacement
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	AfterEach(func() {
		testutils.DeleteClusterResourcePlacement(*HubCluster, crp)
	})

	Context("Test Workload Orchestration", func() {
		It("Apply CRP and check if work gets propagated", func() {
			workName := "resource-label-selector"
			labelKey := "fleet.azure.com/name"
			labelValue := "test"
			By("create the resources to be propagated")
			cr := &rbacv1.ClusterRole{
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
			testutils.WaitConditionClusterResourcePlacement(*HubCluster, crp, string(v1alpha1.ResourcePlacementConditionTypeScheduled), v1.ConditionTrue, testutils.PollTimeout)
			testutils.WaitConditionClusterResourcePlacement(*HubCluster, crp, string(v1alpha1.ResourcePlacementStatusConditionTypeApplied), v1.ConditionTrue, testutils.PollTimeout)

			By("check if resource is propagated to member cluster")
			testutils.WaitClusterRole(*MemberCluster, cr)

			By("delete cluster role on hub cluster")
			testutils.DeleteClusterRole(*HubCluster, cr)
		})
	})
})
