/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"fmt"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = ginkgo.Describe("Join member cluster testing", func() {
	var mc *v1alpha1.MemberCluster
	var sa *corev1.ServiceAccount
	var memberIdentity rbacv1.Subject
	var memberNS *corev1.Namespace

	ginkgo.Context("join succeed with member identity is SA", func() {
		memberNS = NewNamespace(fmt.Sprintf("%s-%s", "fleet", MemberCluster.ClusterName))

		ginkgo.BeforeEach(func() {
			memberIdentity = rbacv1.Subject{
				Name:      MemberCluster.ClusterName,
				Kind:      "ServiceAccount",
				Namespace: "fleet-system",
			}
			mc = NewMemberCluster(MemberCluster.ClusterName, 60, memberIdentity, v1alpha1.ClusterStateJoin)
			sa = NewServiceAccount(memberIdentity.Name, memberIdentity.Namespace, []corev1.ObjectReference{})
			framework.CreateServiceAccount(*HubCluster, sa)
		})

		//TODO: in progress implementation
		ginkgo.It("Join flow is succuess ", func() {
			ginkgo.By("deploy memberCluster in the hub cluster", func() {
				framework.CreateMemberCluster(*HubCluster, mc)

				ginkgo.DeferCleanup(func() {
					//TODO: remove framework.DeleteNamespace(*HubCluster, memberNS) after Leave is merged
					framework.DeleteNamespace(*HubCluster, memberNS)
					framework.DeleteServiceAccount(*HubCluster, sa)
					framework.DeleteMemberCluster(*HubCluster, mc)
				})
			})
			ginkgo.By("check if internalmembercluster created in the hub cluster", func() {

			})

			ginkgo.By("deploy membership in the member cluster", func() {

			})

			ginkgo.By("check if internalmembercluster state is updated", func() {

			})

			ginkgo.By("check if membership state is updated", func() {

			})

			ginkgo.By("check if membercluster state is updated", func() {

			})
		})
	})
})
