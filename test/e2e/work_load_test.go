package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("workload orchestration testing", func() {
	var mc *v1alpha1.MemberCluster
	var sa *corev1.ServiceAccount
	var memberIdentity rbacv1.Subject
	var memberNS *corev1.Namespace
	var imc *v1alpha1.InternalMemberCluster
	var cr *rbacv1.ClusterRole
	var crp *v1alpha1.ClusterResourcePlacement

	memberNS = NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName))

	BeforeEach(func() {
		memberIdentity = rbacv1.Subject{
			Name:      MemberCluster.ClusterName,
			Kind:      "ServiceAccount",
			Namespace: "fleet-system",
		}
	})

	It("Join flow is successful ", func() {
		By("Prepare resources in member cluster", func() {
			// create testing NS in member cluster
			//framework.DeleteNamespace(*MemberCluster, memberNS)
			framework.CreateNamespace(*MemberCluster, memberNS)
			framework.WaitNamespace(*MemberCluster, memberNS)

			sa = NewServiceAccount(memberIdentity.Name, memberNS.Name)
			framework.CreateServiceAccount(*MemberCluster, sa)
		})

		By("deploy memberCluster in the hub cluster", func() {
			mc = NewMemberCluster(MemberCluster.ClusterName, 60, memberIdentity, v1alpha1.ClusterStateJoin)

			framework.CreateMemberCluster(*HubCluster, mc)
			framework.WaitMemberCluster(*HubCluster, mc)

			By("check if internalmembercluster created in the hub cluster", func() {
				imc = NewInternalMemberCluster(MemberCluster.ClusterName, memberNS.Name)
				framework.WaitInternalMemberCluster(*HubCluster, imc)
			})
		})

		By("check if membercluster condition is updated to Joined", func() {
			framework.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionTrue, 3*framework.PollTimeout)
		})

		By("check if internalMemberCluster condition is updated to Joined", func() {
			framework.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.AgentJoined, v1.ConditionTrue, 3*framework.PollTimeout)
		})

		By("create the resources to be propagated", func() {
			cr = NewClusterRole("test-cluster-role")
			framework.CreateClusterRole(*HubCluster, cr)
		})

		By("create the cluster resource placement in the hub cluster", func() {
			crp = NewClusterResourcePlacement("resource-label-selector")
			framework.CreateClusterResourcePlacement(*HubCluster, crp)
			framework.WaitClusterResourcePlacement(*HubCluster, crp)
		})

		By("check if cluster resource placement is updated to Scheduled", func() {
			framework.WaitConditionClusterResourcePlacement(*HubCluster, crp, string(v1alpha1.ResourcePlacementConditionTypeScheduled), v1.ConditionTrue, 3*framework.PollTimeout)
		})

		By("check if cluster resource placement is updated to Applied", func() {
			framework.WaitConditionClusterResourcePlacement(*HubCluster, crp, string(v1alpha1.ResourcePlacementStatusConditionTypeApplied), v1.ConditionTrue, 3*framework.PollTimeout)
		})

		//By("clean up", func() {
		//	framework.DeleteMemberCluster(*HubCluster, mc)
		//	Eventually(func() bool {
		//		err := HubCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: memberNS.Name, Namespace: ""}, memberNS)
		//		return apierrors.IsNotFound(err)
		//	}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
		//	framework.DeleteNamespace(*MemberCluster, memberNS)
		//	Eventually(func() bool {
		//		err := MemberCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: memberNS.Name, Namespace: ""}, memberNS)
		//		return apierrors.IsNotFound(err)
		//	}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
		//})

	})
})
