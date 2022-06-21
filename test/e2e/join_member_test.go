/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("Join member cluster testing", func() {
	var mc *v1alpha1.MemberCluster
	var sa *corev1.ServiceAccount
	var memberIdentity rbacv1.Subject
	var memberNS *corev1.Namespace
	var imc *v1alpha1.InternalMemberCluster
	var membership *v1alpha1.Membership

	memberNS = NewNamespace(fmt.Sprintf("%s-%s", "fleet", MemberCluster.ClusterName))

	BeforeEach(func() {
		memberIdentity = rbacv1.Subject{
			Name:      MemberCluster.ClusterName,
			Kind:      "ServiceAccount",
			Namespace: "fleet-system",
		}
	})

	It("Join flow is succuess ", func() {
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

		By("deploy membership in the member cluster", func() {
			membership = NewMembership(MemberCluster.ClusterName, memberNS.Name, MemberCluster.HubURL, string(v1alpha1.ClusterStateJoin))
			framework.CreateMembership(*MemberCluster, membership)
			framework.WaitMembership(*MemberCluster, membership)
		})

		By("check if membership state is updated", func() {
			framework.WaitStateUpdatedMembership(*MemberCluster, membership, v1alpha1.ConditionTypeMembershipJoin)
		})

		By("check if membercluster state is updated", func() {
			framework.WaitStateUpdatedMemberCluster(*HubCluster, mc, v1alpha1.ClusterStateJoin)
		})
	})
	It("leave flow is succuess ", func() {

		By("update membership in the member cluster", func() {
			membership.Spec.State = v1alpha1.ClusterStateLeave
			framework.CreateMembership(*MemberCluster, membership)
			framework.WaitMembership(*MemberCluster, membership)
		})

		By("check if membership state is updated", func() {
			framework.WaitStateUpdatedMembership(*MemberCluster, membership, v1alpha1.ConditionTypeMembershipJoin)
		})

		By("check if membercluster state is updated", func() {
			framework.WaitMemberCluster(*HubCluster, mc)
		})

		By("check if membercluster state is updated", func() {
			framework.WaitStateUpdatedMemberCluster(*HubCluster, mc, v1alpha1.ClusterStateLeave)
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
			framework.DeleteMembership(*MemberCluster, membership)
		})
	})

})
