/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.goms.io/fleet/pkg/utils"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("Join/leave member cluster testing", func() {
	var mc *v1alpha1.MemberCluster
	var sa *corev1.ServiceAccount
	var memberIdentity rbacv1.Subject
	var memberNS *corev1.Namespace
	var imc *v1alpha1.InternalMemberCluster

	memberNS = NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName))

	Context("member clusters don't share member identity", func() {
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
				framework.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.ConditionTypeInternalMemberClusterJoin, v1.ConditionTrue, 3*framework.PollTimeout)
			})

		})
		It("leave flow is successful ", func() {

			By("update membercluster in the hub cluster", func() {

				framework.UpdateMemberClusterState(*HubCluster, mc, v1alpha1.ClusterStateLeave)
				framework.WaitMemberCluster(*HubCluster, mc)
			})

			By("check if membercluster condition is updated to Left", func() {
				framework.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionFalse, 3*framework.PollTimeout)
			})

			By("check if internalMemberCluster condition is updated to Joined", func() {
				framework.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.ConditionTypeInternalMemberClusterJoin, v1.ConditionFalse, 3*framework.PollTimeout)
			})

			By("member namespace is deleted from hub cluster", func() {
				Eventually(func() bool {
					err := HubCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: memberNS.Name, Namespace: ""}, memberNS)
					return apierrors.IsNotFound(err)
				}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
			})
			DeferCleanup(func() {
				framework.DeleteMemberCluster(*HubCluster, mc)
				framework.DeleteNamespace(*MemberCluster, memberNS)
			})
		})
	})
})
