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
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("Join/leave member cluster testing", func() {
	var (
		mc1         *v1alpha1.MemberCluster
		sa1         *corev1.ServiceAccount
		member1NS   *corev1.Namespace
		imc1        *v1alpha1.InternalMemberCluster
		membership1 *v1alpha1.Membership
	)

	var (
		mc2         *v1alpha1.MemberCluster
		member2NS   *corev1.Namespace
		imc2        *v1alpha1.InternalMemberCluster
		membership2 *v1alpha1.Membership
		sa2         *corev1.ServiceAccount
	)

	var (
		mc3         *v1alpha1.MemberCluster
		sa3         *corev1.ServiceAccount
		member3NS   *corev1.Namespace
		imc3        *v1alpha1.InternalMemberCluster
		membership3 *v1alpha1.Membership
	)

	var memberIdentity rbacv1.Subject

	member1NS = NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster1.ClusterName))
	member2NS = NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster2.ClusterName))
	member3NS = NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster3.ClusterName))

	// Fleet includes HubCluster1 and MemberCluster1.
	Context("single member cluster", func() {
		BeforeEach(func() {
			memberIdentity = rbacv1.Subject{
				Name:      MemberCluster1.ClusterName,
				Kind:      "ServiceAccount",
				Namespace: "fleet-system",
			}
		})

		It("Join flow is successful ", func() {
			By("Prepare resources in member cluster", func() {
				// create testing NS in member cluster
				framework.CreateNamespace(*MemberCluster1, member1NS)
				framework.WaitNamespace(*MemberCluster1, member1NS)

				sa1 = NewServiceAccount(memberIdentity.Name, member1NS.Name)
				framework.CreateServiceAccount(*MemberCluster1, sa1)
			})

			By("deploy memberCluster in the hub cluster", func() {
				mc1 = NewMemberCluster(MemberCluster1.ClusterName, 60, memberIdentity, v1alpha1.ClusterStateJoin)

				framework.CreateMemberCluster(*HubCluster1, mc1)
				framework.WaitMemberCluster(*HubCluster1, mc1)

				By("check if internalmembercluster created in the hub cluster", func() {
					imc1 = NewInternalMemberCluster(MemberCluster1.ClusterName, member1NS.Name)
					framework.WaitInternalMemberCluster(*HubCluster1, imc1)
				})
			})

			// TODO (mng): removing this when code removal is done
			By("deploy membership in the member cluster", func() {
				membership1 = NewMembership(MemberCluster1.ClusterName, member1NS.Name, string(v1alpha1.ClusterStateJoin))
				framework.CreateMembership(*MemberCluster1, membership1)
				framework.WaitMembership(*MemberCluster1, membership1)
			})

			By("check if membercluster condition is updated to Joined", func() {
				framework.WaitConditionMemberCluster(*HubCluster1, mc1, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionTrue, 3*framework.PollTimeout)
			})

			By("check if internalMemberCluster condition is updated to Joined", func() {
				framework.WaitConditionInternalMemberCluster(*HubCluster1, imc1, v1alpha1.ConditionTypeInternalMemberClusterJoin, v1.ConditionTrue, 3*framework.PollTimeout)
			})

		})
		It("leave flow is successful ", func() {

			By("update membercluster in the hub cluster", func() {

				framework.UpdateMemberClusterState(*HubCluster1, mc1, v1alpha1.ClusterStateLeave)
				framework.WaitMemberCluster(*HubCluster1, mc1)
			})

			// TODO (mng): removing this when code removal is done
			By("update membership in the member cluster", func() {
				framework.UpdateMembershipState(*MemberCluster1, membership1, v1alpha1.ClusterStateLeave)
				framework.WaitMembership(*MemberCluster1, membership1)
			})

			By("check if membercluster condition is updated to Left", func() {
				framework.WaitConditionMemberCluster(*HubCluster1, mc1, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionFalse, 3*framework.PollTimeout)
			})

			By("member namespace is deleted from hub cluster", func() {
				Eventually(func() bool {
					err := HubCluster1.KubeClient.Get(context.TODO(), types.NamespacedName{Name: member1NS.Name, Namespace: ""}, member1NS)
					return apierrors.IsNotFound(err)
				}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
			})

			By("internalMemberCluster is deleted from hub cluster", func() {
				Eventually(func() bool {
					err := HubCluster1.KubeClient.Get(context.TODO(), types.NamespacedName{Name: imc1.Name, Namespace: imc1.Namespace}, imc1)
					return apierrors.IsNotFound(err)
				}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
			})

			DeferCleanup(func() {
				framework.DeleteMemberCluster(*HubCluster1, mc1)
				framework.DeleteNamespace(*MemberCluster1, member1NS)
				framework.DeleteMembership(*MemberCluster1, membership1) // TODO (mng): removing this when code removal is done
			})
		})
	})

	// Fleet includes HubCluster2, MemberCluster2 and MemberCluster3.
	Context("multiple member clusters that share member identity", func() {
		BeforeEach(func() {
			memberIdentity = rbacv1.Subject{
				Name:      sharedMSI,
				Kind:      "ServiceAccount",
				Namespace: "fleet-system",
			}
		})

		It("join flow is successful", func() {
			By("prepare resources in member clusters", func() {
				By("Prepare resources in member cluster 3", func() {
					// create testing NS in member cluster
					framework.CreateNamespace(*MemberCluster3, member3NS)
					framework.WaitNamespace(*MemberCluster3, member3NS)

					sa3 = NewServiceAccount(memberIdentity.Name, member3NS.Name)
					framework.CreateServiceAccount(*MemberCluster3, sa3)
				})

				By("Prepare resources in member cluster 2", func() {
					// create testing NS in member cluster 2
					framework.CreateNamespace(*MemberCluster2, member2NS)
					framework.WaitNamespace(*MemberCluster2, member2NS)

					sa2 = NewServiceAccount(memberIdentity.Name, member2NS.Name)
					framework.CreateServiceAccount(*MemberCluster2, sa2)
				})
			})

			By("deploy memberClusters in hub cluster 2", func() {
				By("deploy memberCluster 3 in the hub cluster 2", func() {
					mc3 = NewMemberCluster(MemberCluster3.ClusterName, 60, memberIdentity, v1alpha1.ClusterStateJoin)
					framework.CreateMemberCluster(*HubCluster2, mc3)
					framework.WaitMemberCluster(*HubCluster2, mc3)
					By("check if internalMemberCluster 3 created in the hub cluster", func() {
						imc3 = NewInternalMemberCluster(MemberCluster3.ClusterName, member3NS.Name)
						framework.WaitInternalMemberCluster(*HubCluster2, imc3)
					})
				})

				By("deploy memberCluster 2 in the hub cluster 2", func() {
					mc2 = NewMemberCluster(MemberCluster2.ClusterName, 60, memberIdentity, v1alpha1.ClusterStateJoin)
					framework.CreateMemberCluster(*HubCluster2, mc2)
					framework.WaitMemberCluster(*HubCluster2, mc2)
					By("check if internalMemberCluster 2 created in the hub cluster", func() {
						imc2 = NewInternalMemberCluster(MemberCluster2.ClusterName, member2NS.Name)
						framework.WaitInternalMemberCluster(*HubCluster2, imc2)
					})
				})
			})

			// TODO (mng): removing this when code removal is done
			By("deploy memberships in the member clusters", func() {
				By("deploy membership 3 in the member cluster 3", func() {
					membership3 = NewMembership(MemberCluster3.ClusterName, member3NS.Name, string(v1alpha1.ClusterStateJoin))
					framework.CreateMembership(*MemberCluster3, membership3)
					framework.WaitMembership(*MemberCluster3, membership3)
				})
				By("deploy membership 2 in the member cluster 2", func() {
					membership2 = NewMembership(MemberCluster2.ClusterName, member2NS.Name, string(v1alpha1.ClusterStateJoin))
					framework.CreateMembership(*MemberCluster2, membership2)
					framework.WaitMembership(*MemberCluster2, membership2)
				})
			})

			By("check if internalMemberCluster conditions are updated to Joined", func() {
				By("check if internalMemberCluster 3 condition is updated to Joined", func() {
					framework.WaitConditionInternalMemberCluster(*HubCluster2, imc3, v1alpha1.ConditionTypeInternalMemberClusterJoin, v1.ConditionTrue, 3*framework.PollTimeout)
				})
				By("check if internalMemberCluster 2 condition is updated to Joined", func() {
					framework.WaitConditionInternalMemberCluster(*HubCluster2, imc2, v1alpha1.ConditionTypeInternalMemberClusterJoin, v1.ConditionTrue, 3*framework.PollTimeout)
				})
			})

			By("check if memberCluster conditions are updated to Joined", func() {
				By("check if memberCluster 3 condition is updated to Joined", func() {
					framework.WaitConditionMemberCluster(*HubCluster2, mc3, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionTrue, 4*framework.PollTimeout)
				})
				By("check if memberCluster 2 condition is updated to Joined", func() {
					framework.WaitConditionMemberCluster(*HubCluster2, mc2, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionTrue, 4*framework.PollTimeout)
				})
			})
		})

		It("leave flow is successful ", func() {
			By("update memberClusters in hub cluster 2", func() {
				By("update memberCluster 3 in the hub cluster 2", func() {
					framework.UpdateMemberClusterState(*HubCluster2, mc3, v1alpha1.ClusterStateLeave)
					framework.WaitMemberCluster(*HubCluster2, mc3)
				})
				By("update memberCluster 2 in the hub cluster 2", func() {
					framework.UpdateMemberClusterState(*HubCluster2, mc2, v1alpha1.ClusterStateLeave)
					framework.WaitMemberCluster(*HubCluster2, mc2)
				})
			})

			// TODO (mng): removing this when code removal is done
			By("update memberships in member clusters", func() {
				By("update membership 3 in the member cluster 3", func() {
					framework.UpdateMembershipState(*MemberCluster3, membership3, v1alpha1.ClusterStateLeave)
					framework.WaitMembership(*MemberCluster3, membership3)
				})
				By("update membership 2 in the member cluster 2", func() {
					framework.UpdateMembershipState(*MemberCluster2, membership2, v1alpha1.ClusterStateLeave)
					framework.WaitMembership(*MemberCluster2, membership2)
				})
			})

			By("check if memberCluster conditions are updated to Left", func() {
				By("check if memberCluster 3 condition is updated to Left", func() {
					framework.WaitConditionMemberCluster(*HubCluster2, mc3, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionFalse, 4*framework.PollTimeout)
				})
				By("check if memberCluster 2 condition is updated to Left", func() {
					framework.WaitConditionMemberCluster(*HubCluster2, mc2, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionFalse, 4*framework.PollTimeout)
				})
			})

			By("check if the internalMemberClusters are deleted from hub cluster 2", func() {
				By("internalMemberCluster 3 is deleted from hub cluster 2", func() {
					Eventually(func() bool {
						err := HubCluster2.KubeClient.Get(context.TODO(), types.NamespacedName{Name: imc3.Name, Namespace: imc3.Namespace}, imc3)
						return apierrors.IsNotFound(err)
					}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
				})
				By("internalMemberCluster 3 is deleted from hub cluster 2", func() {
					Eventually(func() bool {
						err := HubCluster2.KubeClient.Get(context.TODO(), types.NamespacedName{Name: imc2.Name, Namespace: imc2.Namespace}, imc2)
						return apierrors.IsNotFound(err)
					}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
				})
			})

			By("member namespaces are deleted from hub cluster 2", func() {
				By("member namespace 3 is deleted from hub cluster 2", func() {
					Eventually(func() bool {
						err := HubCluster2.KubeClient.Get(context.TODO(), types.NamespacedName{Name: member3NS.Name, Namespace: ""}, member3NS)
						return apierrors.IsNotFound(err)
					}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
				})
				By("member namespace 2 is deleted from hub cluster 2", func() {
					Eventually(func() bool {
						err := HubCluster2.KubeClient.Get(context.TODO(), types.NamespacedName{Name: member2NS.Name, Namespace: ""}, member2NS)
						return apierrors.IsNotFound(err)
					}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
				})
			})

			DeferCleanup(func() {
				framework.DeleteMemberCluster(*HubCluster2, mc3)
				framework.DeleteNamespace(*MemberCluster3, member3NS)
				framework.DeleteMembership(*MemberCluster3, membership3) // TODO (mng): removing this when code removal is done

				framework.DeleteMemberCluster(*HubCluster2, mc2)
				framework.DeleteNamespace(*MemberCluster2, member2NS)
				framework.DeleteMembership(*MemberCluster2, membership2) // TODO (mng): removing this when code removal is done
			})
		})
	})
})
