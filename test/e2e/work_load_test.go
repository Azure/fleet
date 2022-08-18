package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("workload orchestration testing", func() {
	var mc *v1alpha1.MemberCluster
	var sa *corev1.ServiceAccount
	var memberNS *corev1.Namespace
	var imc *v1alpha1.InternalMemberCluster
	var cr *rbacv1.ClusterRole
	var crp *v1alpha1.ClusterResourcePlacement

	BeforeEach(func() {
		memberNS = NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName))
		By("prepare resources in member cluster")
		// create testing NS in member cluster
		framework.CreateNamespace(*MemberCluster, memberNS)
		sa = NewServiceAccount(MemberCluster.ClusterName, memberNS.Name)
		framework.CreateServiceAccount(*MemberCluster, sa)

		By("deploy member cluster in the hub cluster")
		mc = NewMemberCluster(MemberCluster.ClusterName, 60, v1alpha1.ClusterStateJoin)
		framework.CreateMemberCluster(*HubCluster, mc)

		By("check if internal member cluster created in the hub cluster")
		imc = &v1alpha1.InternalMemberCluster{
			ObjectMeta: v1.ObjectMeta{
				Name:      MemberCluster.ClusterName,
				Namespace: memberNS.Name,
			},
		}
		framework.WaitInternalMemberCluster(*HubCluster, imc)

		By("check if internal member cluster condition is updated to Joined")
		framework.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.AgentJoined, v1.ConditionTrue, 3*framework.PollTimeout)
		By("check if member cluster condition is updated to Joined")
		framework.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionTrue, 3*framework.PollTimeout)
	})

	It("Apply CRP and check if work gets propagated", func() {
		workName := fmt.Sprintf(utils.WorkNameFormat, "resource-label-selector")
		By("create the resources to be propagated")
		cr = &rbacv1.ClusterRole{
			ObjectMeta: v1.ObjectMeta{
				Name:   "test-cluster-role",
				Labels: map[string]string{"fleet.azure.com/name": "test"},
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "list", "watch"},
					APIGroups: []string{""},
					Resources: []string{"secrets"},
				},
			},
		}
		framework.CreateClusterRole(*HubCluster, cr)

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
		framework.CreateClusterResourcePlacement(*HubCluster, crp)

		By("check if work gets created for cluster resource placement")
		framework.WaitWork(*HubCluster, workName, memberNS.Name)

		By("check if cluster resource placement is updated to Scheduled & Applied")
		framework.WaitConditionClusterResourcePlacement(*HubCluster, crp, string(v1alpha1.ResourcePlacementConditionTypeScheduled), v1.ConditionTrue, 3*framework.PollTimeout)
		framework.WaitConditionClusterResourcePlacement(*HubCluster, crp, string(v1alpha1.ResourcePlacementStatusConditionTypeApplied), v1.ConditionTrue, 3*framework.PollTimeout)

		By("check if resource is propagated to member cluster")
		framework.WaitClusterRole(*MemberCluster, cr)

		By("delete cluster resource placement & cluster role on hub cluster")
		framework.DeleteClusterResourcePlacement(*HubCluster, crp)
		Eventually(func() bool {
			err := HubCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: crp.Name, Namespace: ""}, crp)
			return apierrors.IsNotFound(err)
		}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
		framework.DeleteClusterRole(*HubCluster, cr)
	})

	AfterEach(func() {
		framework.DeleteMemberCluster(*HubCluster, mc)
		Eventually(func() bool {
			err := HubCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: memberNS.Name, Namespace: ""}, memberNS)
			return apierrors.IsNotFound(err)
		}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
		framework.DeleteNamespace(*MemberCluster, memberNS)
		Eventually(func() bool {
			err := MemberCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: memberNS.Name, Namespace: ""}, memberNS)
			return apierrors.IsNotFound(err)
		}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
	})
})
