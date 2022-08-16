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
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("workload orchestration testing", func() {
	var mc *v1alpha1.MemberCluster
	var sa *corev1.ServiceAccount
	var memberIdentity rbacv1.Subject
	var memberNS *corev1.Namespace
	var imc *v1alpha1.InternalMemberCluster
	var cr *rbacv1.ClusterRole
	var crp *v1alpha1.ClusterResourcePlacement
	var work *workapi.Work

	memberNS = NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName))
	clusterRoleName := "test-cluster-role"
	placementName := "resource-label-selector"
	workName := fmt.Sprintf(utils.WorkNameFormat, placementName)

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
			cr = NewClusterRole(clusterRoleName)
			framework.CreateClusterRole(*HubCluster, cr)
		})

		By("create the cluster resource placement in the hub cluster", func() {
			crp = NewClusterResourcePlacement(placementName)
			framework.CreateClusterResourcePlacement(*HubCluster, crp)
			framework.WaitClusterResourcePlacement(*HubCluster, crp)
		})

		By("check if work get created for cluster resource placement", func() {
			work = framework.GetWork(*HubCluster, workName, memberNS.Name)
		})

		By("check if cluster resource placement is updated to Scheduled & Applied", func() {
			framework.WaitConditionClusterResourcePlacement(*HubCluster, crp, string(v1alpha1.ResourcePlacementConditionTypeScheduled), v1.ConditionTrue, 3*framework.PollTimeout)
			framework.WaitConditionClusterResourcePlacement(*HubCluster, crp, string(v1alpha1.ResourcePlacementStatusConditionTypeApplied), v1.ConditionTrue, 3*framework.PollTimeout)
		})
	})

	AfterEach(func() {
		framework.DeleteWork(*HubCluster, work)
		Eventually(func() bool {
			err := HubCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: work.Name, Namespace: work.Namespace}, work)
			return apierrors.IsNotFound(err)
		}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
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
