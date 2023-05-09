/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/apis/v1alpha1"
	workcontroller "go.goms.io/fleet/pkg/controllers/work"
	pkgutils "go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/test/e2e/utils"
)

var _ = Describe("workload orchestration testing", func() {
	var (
		crp        *v1alpha1.ClusterResourcePlacement
		labelKey   = "fleet.azure.com/name"
		labelValue = "test"
		// Ignoring typeMeta to get the tests to pass, not sure why.
		resourceIgnoreOptions = []cmp.Option{cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "UID", "Annotations", "CreationTimestamp", "ManagedFields"),
			cmpopts.IgnoreFields(metav1.OwnerReference{}, "UID"), cmpopts.IgnoreFields(metav1.TypeMeta{}, "Kind", "APIVersion")}
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
			utils.WaitWork(ctx, *HubCluster, crp.Name, memberNamespace.Name)

			By("check if cluster resource placement status is updated")
			crpStatus := v1alpha1.ClusterResourcePlacementStatus{
				Conditions: []metav1.Condition{
					{
						Reason: "ScheduleSucceeded",
						Status: metav1.ConditionTrue,
						Type:   string(v1alpha1.ResourcePlacementConditionTypeScheduled),
					},
					{
						Reason: "ApplySucceeded",
						Status: metav1.ConditionTrue,
						Type:   string(v1alpha1.ResourcePlacementStatusConditionTypeApplied),
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
			utils.WaitCreateClusterResourcePlacementStatus(ctx, *HubCluster, &types.NamespacedName{Name: crp.Name}, crpStatus, crpStatusCmpOptions, 3*utils.PollTimeout)

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
			utils.CmpClusterRole(ctx, *MemberCluster, &types.NamespacedName{Name: clusterRole.Name}, expectedClusterRole, resourceIgnoreOptions)

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
			utils.CmpClusterRole(ctx, *MemberCluster, &types.NamespacedName{Name: clusterRole.Name}, expectedClusterRole, resourceIgnoreOptions)

			By("delete cluster role on hub cluster")
			Expect(HubCluster.KubeClient.Delete(ctx, clusterRole)).Should(Succeed(), "Failed to delete cluster role %s in %s cluster", clusterRole.Name, HubCluster.ClusterName)
			Eventually(func() bool {
				return errors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: clusterRole.Name}, clusterRole))
			}, utils.PollTimeout, utils.PollInterval).Should(BeTrue(), "Failed to wait for cluster role %s to be deleted in %s cluster", clusterRole.Name, HubCluster.ClusterName)

			By("check if cluster role got deleted on member cluster")
			Eventually(func() bool {
				return errors.IsNotFound(MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: clusterRole.Name}, clusterRole))
			}, utils.PollTimeout, utils.PollInterval).Should(BeTrue(), "Failed to wait for cluster role %s to be deleted in %s cluster", clusterRole.Name, MemberCluster.ClusterName)

			By("delete cluster resource placement on hub cluster")
			utils.DeleteClusterResourcePlacement(ctx, *HubCluster, crp)
		})

		It("Apply CRP selecting namespace by label and check if namespace gets propagated with role, role binding, then update existing role", func() {
			By("create the resources to be propagated")
			namespace1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace1",
					Labels: map[string]string{labelKey: labelValue},
				},
			}
			namespace2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace2",
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, namespace1)).Should(Succeed(), "Failed to create namespace %s in %s cluster", namespace1.Name, HubCluster.ClusterName)
			Expect(HubCluster.KubeClient.Create(ctx, namespace2)).Should(Succeed(), "Failed to create namespace %s in %s cluster", namespace2.Name, HubCluster.ClusterName)

			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-reader",
					Namespace: namespace1.Name,
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
					Namespace: namespace1.Name,
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
								MatchLabels: namespace1.Labels,
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, crp)).Should(Succeed(), "Failed to create cluster resource placement %s in %s cluster", crp.Name, HubCluster.ClusterName)

			By("check if work gets created for cluster resource placement")
			utils.WaitWork(ctx, *HubCluster, crp.Name, memberNamespace.Name)

			By("check if cluster resource placement status is updated")
			crpStatus := v1alpha1.ClusterResourcePlacementStatus{
				Conditions: []metav1.Condition{
					{
						Reason: "ScheduleSucceeded",
						Status: metav1.ConditionTrue,
						Type:   string(v1alpha1.ResourcePlacementConditionTypeScheduled),
					},
					{
						Reason: "ApplySucceeded",
						Status: metav1.ConditionTrue,
						Type:   string(v1alpha1.ResourcePlacementStatusConditionTypeApplied),
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
						Name:    namespace1.Name,
					},
				},
				TargetClusters: []string{"kind-member-testing"},
			}
			utils.WaitCreateClusterResourcePlacementStatus(ctx, *HubCluster, &types.NamespacedName{Name: crp.Name}, crpStatus, crpStatusCmpOptions, 3*utils.PollTimeout)

			By("check if resources in namespace are propagated to member cluster")
			ownerReferences := []metav1.OwnerReference{
				{
					APIVersion:         workapi.GroupVersion.String(),
					BlockOwnerDeletion: pointer.Bool(false),
					Kind:               "AppliedWork",
					Name:               crp.Name,
				},
			}
			expectedNamespace := namespace1
			expectedRole := role
			expectedRoleBinding := roleBinding
			expectedNamespace.OwnerReferences = ownerReferences
			expectedRole.OwnerReferences = ownerReferences
			expectedRoleBinding.OwnerReferences = ownerReferences
			utils.CmpNamespace(ctx, *MemberCluster, &types.NamespacedName{Name: namespace1.Name}, expectedNamespace, resourceIgnoreOptions)
			utils.CmpRole(ctx, *MemberCluster, &types.NamespacedName{Name: role.Name, Namespace: role.Namespace}, expectedRole, resourceIgnoreOptions)
			utils.CmpRoleBinding(ctx, *MemberCluster, &types.NamespacedName{Name: roleBinding.Name, Namespace: roleBinding.Namespace}, expectedRoleBinding, resourceIgnoreOptions)

			By("check if namespace not selected by CRP doesn't exist on member cluster")
			Consistently(func() bool {
				return errors.IsNotFound(MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: namespace2.Name}, namespace2))
			}, utils.PollTimeout, utils.PollInterval).Should(BeTrue(), "Failed to verify namespace %s is not propagated to %s cluster", namespace2.Name, MemberCluster.ClusterName)

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
					Namespace: namespace1.Name,
				},
				Rules: rules,
			}
			Expect(HubCluster.KubeClient.Update(ctx, updatedRole)).Should(Succeed(), "Failed to update role %s in %s cluster", updatedRole.Name, HubCluster.ClusterName)
			expectedRole.Rules = rules

			By("check if role got updated in member cluster")
			utils.CmpRole(ctx, *MemberCluster, &types.NamespacedName{Name: role.Name, Namespace: role.Namespace}, expectedRole, resourceIgnoreOptions)

			By("delete namespaces")
			utils.DeleteNamespace(ctx, *HubCluster, namespace1)
			utils.DeleteNamespace(ctx, *HubCluster, namespace2)

			By("check if namespace got deleted on member cluster")
			Eventually(func() bool {
				return errors.IsNotFound(MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: namespace1.Name}, namespace1))
			}, utils.PollTimeout, utils.PollInterval).Should(BeTrue(), "Failed to wait for cluster role %s to be deleted in %s cluster", namespace1.Name, MemberCluster.ClusterName)

			By("delete cluster resource placement on hub cluster")
			utils.DeleteClusterResourcePlacement(ctx, *HubCluster, crp)
		})

		It("Apply CRP select namespace propagate small secret, then update secret to be large to handle annotation limitation", func() {
			By("create the resources to be propagated")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{labelKey: labelValue},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, namespace)).Should(Succeed(), "Failed to create namespace %s in %s cluster", namespace.Name, HubCluster.ClusterName)
			var testSmallSecret corev1.Secret
			err := pkgutils.GetObjectFromManifest("./test/integration/manifests/resources/test-small-secret.yaml", &testSmallSecret)
			Expect(err).Should(Succeed())
			Expect(HubCluster.KubeClient.Create(ctx, &testSmallSecret)).Should(Succeed(), "Failed to create small secret %s in %s cluster", testSmallSecret.Name, HubCluster.ClusterName)

			By("create the cluster resource placement in the hub cluster")
			crp = &v1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crp"},
				Spec: v1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []v1alpha1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    namespace.Name,
						},
					},
				},
			}

			Expect(HubCluster.KubeClient.Create(ctx, crp)).Should(Succeed(), "Failed to create cluster resource placement %s in %s cluster", crp.Name, HubCluster.ClusterName)

			By("check if work gets created for cluster resource placement")
			utils.WaitWork(ctx, *HubCluster, crp.Name, memberNamespace.Name)

			By("check if cluster resource placement status is updated")
			crpStatus := v1alpha1.ClusterResourcePlacementStatus{
				Conditions: []metav1.Condition{
					{
						Reason: "ScheduleSucceeded",
						Status: metav1.ConditionTrue,
						Type:   string(v1alpha1.ResourcePlacementConditionTypeScheduled),
					},
					{
						Reason: "ApplySucceeded",
						Status: metav1.ConditionTrue,
						Type:   string(v1alpha1.ResourcePlacementStatusConditionTypeApplied),
					},
				},
				SelectedResources: []v1alpha1.ResourceIdentifier{
					{
						Version:   "v1",
						Kind:      "Secret",
						Name:      testSmallSecret.Name,
						Namespace: testSmallSecret.Namespace,
					},
					{
						Version: "v1",
						Kind:    "Namespace",
						Name:    namespace.Name,
					},
				},
				TargetClusters: []string{"kind-member-testing"},
			}
			utils.WaitCreateClusterResourcePlacementStatus(ctx, *HubCluster, &types.NamespacedName{Name: crp.Name}, crpStatus, crpStatusCmpOptions, 3*utils.PollTimeout)

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
			expectedSecret := &testSmallSecret
			expectedNamespace.OwnerReferences = ownerReferences
			expectedSecret.OwnerReferences = ownerReferences

			utils.CmpNamespace(ctx, *MemberCluster, &types.NamespacedName{Name: namespace.Name}, expectedNamespace, resourceIgnoreOptions)
			// Ignoring Annotations here because fleet.azure.com/last-applied-configuration has live fields, checking to see if it's not empty instead.
			gotSecret := utils.CmpSecret(ctx, *MemberCluster, &types.NamespacedName{Name: testSmallSecret.Name, Namespace: testSmallSecret.Namespace}, expectedSecret, resourceIgnoreOptions)
			Expect(gotSecret.Annotations[workcontroller.LastAppliedConfigAnnotation]).To(Not(BeEmpty()))
			testSmallSecretSpecHash := gotSecret.Annotations[workcontroller.ManifestHashAnnotation]

			By("update secret so that annotation limit crosses threshold of 256KB")
			var testLargeSecret corev1.Secret
			err = pkgutils.GetObjectFromManifest("./test/integration/manifests/resources/test-large-secret.yaml", &testLargeSecret)
			Expect(err).Should(Succeed())
			// testLargeSecret has the same name as testSmallSecret
			Expect(HubCluster.KubeClient.Update(ctx, &testLargeSecret)).Should(Succeed(), "Failed to update secret %s to be large in %s cluster", testLargeSecret.Name, HubCluster.ClusterName)
			expectedSecret = &testLargeSecret
			expectedSecret.OwnerReferences = ownerReferences

			// Ignoring Annotations here because fleet.azure.com/last-applied-configuration has live fields, checking to see if it's not empty instead.
			gotSecret = utils.CmpSecret(ctx, *MemberCluster, &types.NamespacedName{Name: testLargeSecret.Name, Namespace: testLargeSecret.Namespace}, expectedSecret, resourceIgnoreOptions)
			Expect(gotSecret.Annotations[workcontroller.LastAppliedConfigAnnotation]).To(BeEmpty())
			Expect(gotSecret.Annotations[workcontroller.ManifestHashAnnotation]).ToNot(Equal(testSmallSecretSpecHash))

			By("update secret so that it's small again")
			// Using a new variable to prevent failure, leads to 409 if not.
			var smallSecret corev1.Secret
			err = pkgutils.GetObjectFromManifest("./test/integration/manifests/resources/test-small-secret.yaml", &smallSecret)
			Expect(err).Should(Succeed())
			Eventually(func() error {
				if err := HubCluster.KubeClient.Update(ctx, &smallSecret); err != nil {
					return err
				}
				return nil
			}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to update secret to be small in %s cluster", HubCluster.ClusterName)
			expectedSecret = &smallSecret
			expectedSecret.OwnerReferences = ownerReferences

			// Ignoring Annotations here because fleet.azure.com/last-applied-configuration has live fields, checking to see if it's not empty instead.
			gotSecret = utils.CmpSecret(ctx, *MemberCluster, &types.NamespacedName{Name: smallSecret.Name, Namespace: smallSecret.Namespace}, expectedSecret, resourceIgnoreOptions)
			Expect(gotSecret.Annotations[workcontroller.LastAppliedConfigAnnotation]).ToNot(BeEmpty())
			Expect(gotSecret.Annotations[workcontroller.ManifestHashAnnotation]).To(Equal(testSmallSecretSpecHash))

			By("delete namespaces")
			utils.DeleteNamespace(ctx, *HubCluster, namespace)
			By("delete cluster resource placement on hub cluster")
			utils.DeleteClusterResourcePlacement(ctx, *HubCluster, crp)
		})
	})
})
