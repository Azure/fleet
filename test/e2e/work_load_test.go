/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/apis/v1alpha1"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

var _ = Describe("workload orchestration testing", func() {
	var (
		crp                   *v1alpha1.ClusterResourcePlacement
		labelKey              = "fleet.azure.com/name"
		labelValue            = "test"
		resourceIgnoreOptions = []cmp.Option{cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "UID", "Annotations", "CreationTimestamp", "ManagedFields"),
			cmpopts.IgnoreFields(metav1.OwnerReference{}, "UID")}
	)

	Context("Test Workload Orchestration", func() {
		It("Apply CRP and check if cluster role gets propagated, update cluster role", func() {
			By("create the resources to be propagated")
			clusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
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
			Expect(HubCluster.KubeClient.Create(ctx, clusterRole)).Should(Succeed(), "Failed to create cluster role %s in %s cluster", clusterRole.Name, HubCluster.ClusterName)

			By("create the cluster resource placement in the hub cluster")
			crp = &v1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crp1"},
				Spec: v1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []v1alpha1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: clusterRole.Labels,
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, crp)).Should(Succeed(), "Failed to create cluster resource placement %s in %s cluster", crp.Name, HubCluster.ClusterName)

			By("check if work gets created for cluster resource placement")
			testutils.WaitWork(ctx, *HubCluster, crp.Name, memberNamespace.Name)

			By("check if cluster resource placement status is updated")
			crpStatus := v1alpha1.ClusterResourcePlacementStatus{
				Conditions: []metav1.Condition{
					{
						Message: "Successfully scheduled resources for placement",
						Reason:  "ScheduleSucceeded",
						Status:  metav1.ConditionTrue,
						Type:    string(v1alpha1.ResourcePlacementConditionTypeScheduled),
					},
					{
						Message: "Successfully applied resources to member clusters",
						Reason:  "ApplySucceeded",
						Status:  metav1.ConditionTrue,
						Type:    string(v1alpha1.ResourcePlacementStatusConditionTypeApplied),
					},
				},
				SelectedResources: []v1alpha1.ResourceIdentifier{
					{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						Name:    clusterRole.Name,
					},
				},
				TargetClusters: []string{"kind-member-testing"},
			}
			testutils.WaitCreateClusterResourcePlacementStatus(ctx, *HubCluster, &types.NamespacedName{Name: crp.Name}, crpStatus, crpStatusCmpOptions, 3*testutils.PollTimeout)

			By("check if cluster role is propagated to member cluster")
			ownerReferences := []metav1.OwnerReference{
				{
					APIVersion:         workapi.GroupVersion.String(),
					BlockOwnerDeletion: pointer.Bool(false),
					Kind:               "AppliedWork",
					Name:               crp.Name,
				},
			}
			expectedClusterRole := clusterRole
			expectedClusterRole.OwnerReferences = ownerReferences
			testutils.CmpClusterRole(ctx, *MemberCluster, &types.NamespacedName{Name: clusterRole.Name}, expectedClusterRole, resourceIgnoreOptions)

			By("update cluster role in Hub cluster")
			rules := []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "list", "watch"},
					APIGroups: []string{""},
					Resources: []string{"secrets"},
				},
			}
			updatedClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name:   clusterRole.Name,
					Labels: map[string]string{labelKey: labelValue, "fleet.azure.com/region": "us"},
				},
				Rules: rules,
			}
			Expect(HubCluster.KubeClient.Update(ctx, updatedClusterRole)).Should(Succeed(), "Failed to update cluster role %s in %s cluster", updatedClusterRole.Name, HubCluster.ClusterName)

			By("check if cluster role got updated in member cluster")
			expectedClusterRole = &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-cluster-role",
					Labels:          updatedClusterRole.Labels,
					OwnerReferences: ownerReferences,
				},
				Rules: rules,
			}
			testutils.CmpClusterRole(ctx, *MemberCluster, &types.NamespacedName{Name: clusterRole.Name}, expectedClusterRole, resourceIgnoreOptions)

			By("delete cluster role on hub cluster")
			Expect(HubCluster.KubeClient.Delete(ctx, clusterRole)).Should(Succeed(), "Failed to delete cluster role %s in %s cluster", clusterRole.Name, HubCluster.ClusterName)
			Eventually(func() bool {
				return apierrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: clusterRole.Name}, clusterRole))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue(), "Failed to wait for cluster role %s to be deleted in %s cluster", clusterRole.Name, HubCluster.ClusterName)

			By("check if cluster role got deleted on member cluster")
			Eventually(func() bool {
				return apierrors.IsNotFound(MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: clusterRole.Name}, clusterRole))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue(), "Failed to wait for cluster role %s to be deleted in %s cluster", clusterRole.Name, MemberCluster.ClusterName)

			By("delete cluster resource placement on hub cluster")
			testutils.DeleteClusterResourcePlacement(ctx, *HubCluster, crp)
		})

		It("Apply CRP selecting namespace by label and check if namespace gets propagated with role, role binding, then update existing role", func() {
			By("create the resources to be propagated")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{labelKey: labelValue},
				},
			}
			testutils.CreateNamespace(ctx, *HubCluster, namespace)

			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-reader",
					Namespace: namespace.Name,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Verbs:     []string{"get", "list", "watch"},
						Resources: []string{"pods"},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, role)).Should(Succeed(), "Failed to create role %s in %s cluster", role.Name, HubCluster.ClusterName)

			roleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "read-pods",
					Namespace: namespace.Name,
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:     "User",
						Name:     "jane",
						APIGroup: "rbac.authorization.k8s.io",
					},
				},
				RoleRef: rbacv1.RoleRef{
					Kind:     "Role",
					Name:     role.Name,
					APIGroup: "rbac.authorization.k8s.io",
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, roleBinding)).Should(Succeed(), "Failed to create role binding %s in %s cluster", roleBinding.Name, HubCluster.ClusterName)

			By("create the cluster resource placement in the hub cluster")
			crp = &v1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crp2"},
				Spec: v1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []v1alpha1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: namespace.Labels,
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, crp)).Should(Succeed(), "Failed to create cluster resource placement %s in %s cluster", crp.Name, HubCluster.ClusterName)

			By("check if work gets created for cluster resource placement")
			testutils.WaitWork(ctx, *HubCluster, crp.Name, memberNamespace.Name)

			By("check if cluster resource placement status is updated")
			crpStatus := v1alpha1.ClusterResourcePlacementStatus{
				Conditions: []metav1.Condition{
					{
						Message: "Successfully scheduled resources for placement",
						Reason:  "ScheduleSucceeded",
						Status:  metav1.ConditionTrue,
						Type:    string(v1alpha1.ResourcePlacementConditionTypeScheduled),
					},
					{
						Message: "Successfully applied resources to member clusters",
						Reason:  "ApplySucceeded",
						Status:  metav1.ConditionTrue,
						Type:    string(v1alpha1.ResourcePlacementStatusConditionTypeApplied),
					},
				},
				SelectedResources: []v1alpha1.ResourceIdentifier{
					{
						Group:     "rbac.authorization.k8s.io",
						Version:   "v1",
						Kind:      "RoleBinding",
						Name:      roleBinding.Name,
						Namespace: roleBinding.Namespace,
					},
					{
						Group:     "rbac.authorization.k8s.io",
						Version:   "v1",
						Kind:      "Role",
						Name:      role.Name,
						Namespace: role.Namespace,
					},
					{
						Version: "v1",
						Kind:    "Namespace",
						Name:    namespace.Name,
					},
				},
				TargetClusters: []string{"kind-member-testing"},
			}
			testutils.WaitCreateClusterResourcePlacementStatus(ctx, *HubCluster, &types.NamespacedName{Name: crp.Name}, crpStatus, crpStatusCmpOptions, 3*testutils.PollTimeout)

			By("check if resources in namespace are propagated to member cluster")
			ownerReferences := []metav1.OwnerReference{
				{
					APIVersion:         workapi.GroupVersion.String(),
					BlockOwnerDeletion: pointer.Bool(false),
					Kind:               "AppliedWork",
					Name:               crp.Name,
				},
			}
			expectedNamespace := namespace
			expectedRole := role
			expectedRoleBinding := roleBinding
			expectedNamespace.OwnerReferences = ownerReferences
			expectedRole.OwnerReferences = ownerReferences
			expectedRoleBinding.OwnerReferences = ownerReferences
			testutils.CmpNamespace(ctx, *MemberCluster, &types.NamespacedName{Name: namespace.Name}, expectedNamespace, resourceIgnoreOptions)
			testutils.CmpRole(ctx, *MemberCluster, &types.NamespacedName{Name: role.Name, Namespace: role.Namespace}, expectedRole, resourceIgnoreOptions)
			testutils.CmpRoleBinding(ctx, *MemberCluster, &types.NamespacedName{Name: roleBinding.Name, Namespace: roleBinding.Namespace}, expectedRoleBinding, resourceIgnoreOptions)

			By("update role in Hub cluster")
			rules := []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Verbs:     []string{"get", "list", "watch", "update"},
					Resources: []string{"pods"},
				},
			}
			updatedRole := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      role.Name,
					Namespace: namespace.Name,
				},
				Rules: rules,
			}
			Expect(HubCluster.KubeClient.Update(ctx, updatedRole)).Should(Succeed(), "Failed to update role %s in %s cluster", updatedRole.Name, HubCluster.ClusterName)
			expectedRole.Rules = rules

			By("check if role got updated in member cluster")
			testutils.CmpRole(ctx, *MemberCluster, &types.NamespacedName{Name: role.Name, Namespace: role.Namespace}, expectedRole, resourceIgnoreOptions)

			By("delete namespace")
			Expect(HubCluster.KubeClient.Delete(context.TODO(), namespace)).Should(Succeed(), "Failed to delete namespace %s in %s cluster", namespace.Name, HubCluster.ClusterName)
			Eventually(func() bool {
				return apierrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: namespace.Name}, namespace))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue(), "Failed to wait for namespace %s to be deleted in %s cluster", namespace.Name, HubCluster.ClusterName)

			By("check if namespace got deleted on member cluster")
			Eventually(func() bool {
				return apierrors.IsNotFound(MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: namespace.Name}, namespace))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue(), "Failed to wait for cluster role %s to be deleted in %s cluster", namespace.Name, MemberCluster.ClusterName)

			By("delete cluster resource placement on hub cluster")
			testutils.DeleteClusterResourcePlacement(ctx, *HubCluster, crp)
		})
	})
})
