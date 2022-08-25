/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

var _ = Describe("workload orchestration testing", func() {
	var (
		mc       *v1alpha1.MemberCluster
		sa       *corev1.ServiceAccount
		memberNS *corev1.Namespace
		imc      *v1alpha1.InternalMemberCluster
		ctx      context.Context
	)

	labelKey := "fleet.azure.com/name"
	labelValue := "test"

	selectedResource1 := v1alpha1.ResourceIdentifier{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "ClusterRole",
		Name:    "test-cluster-role",
	}
	selectedResource2 := v1alpha1.ResourceIdentifier{
		Group:     "rbac.authorization.k8s.io",
		Version:   "v1",
		Kind:      "Role",
		Name:      "test-pod-reader",
		Namespace: "test-namespace",
	}
	selectedResource3 := v1alpha1.ResourceIdentifier{
		Group:     "rbac.authorization.k8s.io",
		Version:   "v1",
		Kind:      "RoleBinding",
		Name:      "read-pods",
		Namespace: "test-namespace",
	}
	selectedResource4 := v1alpha1.ResourceIdentifier{
		Version: "v1",
		Kind:    "Namespace",
		Name:    "test-namespace",
	}
	selectedResource5 := v1alpha1.ResourceIdentifier{
		Group:     "rbac.authorization.k8s.io",
		Version:   "v1",
		Kind:      "Role",
		Name:      "test-service-reader",
		Namespace: "test-namespace",
	}
	selectedResource6 := v1alpha1.ResourceIdentifier{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "ClusterRole",
		Name:    "new-cluster-role",
	}

	conditionStatus := []v1.ConditionStatus{v1.ConditionTrue, v1.ConditionTrue}
	conditionType := []string{string(v1alpha1.ResourcePlacementConditionTypeScheduled), string(v1alpha1.ResourcePlacementStatusConditionTypeApplied)}
	targetClusters := []string{"kind-member-testing"}

	BeforeEach(func() {
		ctx = context.TODO()
		memberNS = testutils.NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName), nil)
		By("prepare resources in member cluster")
		// create testing NS in member cluster
		testutils.CreateNamespace(ctx, *MemberCluster, memberNS)
		sa = testutils.NewServiceAccount(MemberCluster.ClusterName, memberNS.Name)
		testutils.CreateServiceAccount(ctx, *MemberCluster, sa)

		By("deploy member cluster in the hub cluster")
		mc = testutils.NewMemberCluster(MemberCluster.ClusterName, 60, v1alpha1.ClusterStateJoin)
		testutils.CreateMemberCluster(ctx, *HubCluster, mc)

		By("check if internal member cluster created in the hub cluster")
		imc = testutils.NewInternalMemberCluster(MemberCluster.ClusterName, memberNS.Name)
		testutils.WaitInternalMemberCluster(ctx, *HubCluster, imc)

		By("check if internal member cluster condition is updated to Joined")
		testutils.WaitConditionInternalMemberCluster(ctx, *HubCluster, imc, v1alpha1.AgentJoined, v1.ConditionTrue, 3*testutils.PollTimeout)
		By("check if member cluster condition is updated to Joined")
		testutils.WaitConditionMemberCluster(ctx, *HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoined, v1.ConditionTrue, 3*testutils.PollTimeout)
	})

	AfterEach(func() {
		testutils.DeleteMemberCluster(ctx, *HubCluster, mc)
		testutils.DeleteNamespace(ctx, *MemberCluster, memberNS)
	})

	It("Apply CRP and check if cluster role gets propagated, update cluster role", func() {
		By("create the resources to be propagated")
		clusterRole := &rbacv1.ClusterRole{
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
		testutils.CreateClusterRole(ctx, *HubCluster, clusterRole)

		workName := "test-crp1"
		By("create the cluster resource placement in the hub cluster")
		crp := &v1alpha1.ClusterResourcePlacement{
			ObjectMeta: v1.ObjectMeta{Name: "test-crp1"},
			Spec: v1alpha1.ClusterResourcePlacementSpec{
				ResourceSelectors: []v1alpha1.ClusterResourceSelector{
					{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						LabelSelector: &v1.LabelSelector{
							MatchLabels: map[string]string{labelKey: labelValue},
						},
					},
				},
			},
		}

		selectedResources := []v1alpha1.ResourceIdentifier{selectedResource1}

		testutils.CreateClusterResourcePlacement(ctx, *HubCluster, crp)

		By("check if work gets created for cluster resource placement")
		testutils.WaitWork(*HubCluster, workName, memberNS.Name)

		By("check if cluster resource placement status is updated")
		testutils.WaitCreateClusterResourcePlacement(ctx, *HubCluster, crp, conditionType, conditionStatus, selectedResources, targetClusters, 3*testutils.PollTimeout)

		By("check if cluster role is propagated to member cluster")
		testutils.WaitCreateClusterRole(ctx, *MemberCluster, clusterRole)

		By("edit cluster role in Hub cluster")
		newLabelKey := "fleet.azure.com/region"
		newLabelValue := "us"

		newClusterRole := &rbacv1.ClusterRole{
			ObjectMeta: v1.ObjectMeta{
				Name:   "test-cluster-role",
				Labels: map[string]string{labelKey: labelValue, newLabelKey: newLabelValue},
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "list", "watch"},
					APIGroups: []string{""},
					Resources: []string{"secrets"},
				},
			},
		}
		testutils.UpdateClusterRole(ctx, *HubCluster, newClusterRole)
		clusterRole = &rbacv1.ClusterRole{
			ObjectMeta: v1.ObjectMeta{
				Name: "test-cluster-role",
			},
		}

		By("check if cluster resource placement status is synced")
		testutils.WaitCreateClusterResourcePlacement(ctx, *HubCluster, crp, conditionType, conditionStatus, selectedResources, targetClusters, 3*testutils.PollTimeout)

		By("check if cluster role got updated in member cluster")
		testutils.WaitUpdateClusterRoleLabels(ctx, *MemberCluster, clusterRole, newClusterRole)

		By("delete cluster resource placement on hub cluster")
		testutils.DeleteClusterResourcePlacement(ctx, *HubCluster, crp)

		By("delete cluster role")
		testutils.DeleteClusterRole(ctx, *HubCluster, clusterRole)
	})

	It("Apply CRP selecting namespace by label and check if namespace gets propagated with role, role binding, then update existing role", func() {
		By("create the resources to be propagated")
		resourceNamespace := testutils.NewNamespace("test-namespace", map[string]string{labelKey: labelValue})
		testutils.CreateNamespace(ctx, *HubCluster, resourceNamespace)

		role := &rbacv1.Role{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-pod-reader",
				Namespace: "test-namespace",
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Verbs:     []string{"get", "list", "watch"},
					Resources: []string{"pods"},
				},
			},
		}
		testutils.CreateRole(ctx, *HubCluster, role)

		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: v1.ObjectMeta{
				Name:      "read-pods",
				Namespace: "test-namespace",
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
				Name:     "test-pod-reader",
				APIGroup: "rbac.authorization.k8s.io",
			},
		}
		testutils.CreateRoleBinding(ctx, *HubCluster, roleBinding)

		workName := "test-crp2"
		By("create the cluster resource placement in the hub cluster")
		crp := &v1alpha1.ClusterResourcePlacement{
			ObjectMeta: v1.ObjectMeta{Name: "test-crp2"},
			Spec: v1alpha1.ClusterResourcePlacementSpec{
				ResourceSelectors: []v1alpha1.ClusterResourceSelector{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						LabelSelector: &v1.LabelSelector{
							MatchLabels: map[string]string{labelKey: labelValue},
						},
					},
				},
			},
		}
		testutils.CreateClusterResourcePlacement(ctx, *HubCluster, crp)

		By("check if work gets created for cluster resource placement")
		testutils.WaitWork(*HubCluster, workName, memberNS.Name)

		By("check if cluster resource placement status is updated")
		selectedResources := []v1alpha1.ResourceIdentifier{selectedResource2, selectedResource3, selectedResource4}
		testutils.WaitCreateClusterResourcePlacement(ctx, *HubCluster, crp, conditionType, conditionStatus, selectedResources, targetClusters, 3*testutils.PollTimeout)

		By("check if resources in namespace are propagated to member cluster")
		testutils.WaitCreateNamespace(ctx, *MemberCluster, resourceNamespace)
		testutils.WaitCreateRole(ctx, *MemberCluster, role)
		testutils.WaitCreateRoleBinding(ctx, *MemberCluster, roleBinding)

		By("edit role in Hub cluster")
		updatedRole := &rbacv1.Role{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-pod-reader",
				Namespace: "test-namespace",
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Verbs:     []string{"get", "list", "watch", "update"},
					Resources: []string{"pods"},
				},
			},
		}
		testutils.UpdateRole(ctx, *HubCluster, updatedRole)
		role = &rbacv1.Role{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-pod-reader",
				Namespace: "test-namespace",
			},
		}

		By("check if cluster resource placement status is synced")
		testutils.WaitCreateClusterResourcePlacement(ctx, *HubCluster, crp, conditionType, conditionStatus, selectedResources, targetClusters, 3*testutils.PollTimeout)

		By("check if role got updated in member cluster")
		testutils.WaitUpdateRoleRules(ctx, *MemberCluster, role, updatedRole)

		By("delete cluster resource placement on hub cluster")
		testutils.DeleteClusterResourcePlacement(ctx, *HubCluster, crp)

		By("delete namespace")
		testutils.DeleteNamespace(ctx, *HubCluster, resourceNamespace)
	})

	It("Apply CRP selecting namespace by name and check if namespace gets propagated with role, add new role then delete new role", func() {
		By("create the resources to be propagated")
		resourceNamespace := testutils.NewNamespace("test-namespace", map[string]string{labelKey: labelValue})
		testutils.CreateNamespace(ctx, *HubCluster, resourceNamespace)

		role := &rbacv1.Role{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-pod-reader",
				Namespace: "test-namespace",
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Verbs:     []string{"get", "list", "watch"},
					Resources: []string{"pods"},
				},
			},
		}
		testutils.CreateRole(ctx, *HubCluster, role)

		workName := "test-crp3"
		By("create the cluster resource placement in the hub cluster")
		crp := &v1alpha1.ClusterResourcePlacement{
			ObjectMeta: v1.ObjectMeta{Name: "test-crp3"},
			Spec: v1alpha1.ClusterResourcePlacementSpec{
				ResourceSelectors: []v1alpha1.ClusterResourceSelector{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "test-namespace",
					},
				},
			},
		}

		testutils.CreateClusterResourcePlacement(ctx, *HubCluster, crp)

		By("check if work gets created for cluster resource placement")
		testutils.WaitWork(*HubCluster, workName, memberNS.Name)

		By("check if cluster resource placement status is updated")
		var selectedResources []v1alpha1.ResourceIdentifier
		selectedResources = []v1alpha1.ResourceIdentifier{selectedResource2, selectedResource4}
		testutils.WaitCreateClusterResourcePlacement(ctx, *HubCluster, crp, conditionType, conditionStatus, selectedResources, targetClusters, 3*testutils.PollTimeout)

		By("check if resources in namespace are propagated to member cluster")
		testutils.WaitCreateNamespace(ctx, *MemberCluster, resourceNamespace)
		testutils.WaitCreateRole(ctx, *MemberCluster, role)

		By("Add new role which should be selected by cluster resource placement")
		newRole := &rbacv1.Role{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-service-reader",
				Namespace: "test-namespace",
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Verbs:     []string{"get", "list", "watch"},
					Resources: []string{"services"},
				},
			},
		}

		testutils.CreateRole(ctx, *HubCluster, newRole)

		By("check if cluster resource placement status is selecting new role")
		selectedResources = []v1alpha1.ResourceIdentifier{selectedResource2, selectedResource4, selectedResource5}
		testutils.WaitCreateClusterResourcePlacement(ctx, *HubCluster, crp, conditionType, conditionStatus, selectedResources, targetClusters, 3*testutils.PollTimeout)

		By("check if new role in namespace is propagated to member cluster")
		testutils.WaitCreateRole(ctx, *MemberCluster, newRole)

		By("delete new role in hub cluster")
		testutils.DeleteRole(ctx, *HubCluster, newRole)

		By("check if cluster resource placement status is updated after deleting new role")
		selectedResources = []v1alpha1.ResourceIdentifier{selectedResource2, selectedResource4}
		testutils.WaitCreateClusterResourcePlacement(ctx, *HubCluster, crp, conditionType, conditionStatus, selectedResources, targetClusters, 3*testutils.PollTimeout)

		By("check if role is deleted on member cluster")
		testutils.WaitDeleteRole(ctx, *MemberCluster, newRole)

		By("delete cluster resource placement on hub cluster")
		testutils.DeleteClusterResourcePlacement(ctx, *HubCluster, crp)

		By("delete namespace")
		testutils.DeleteNamespace(ctx, *HubCluster, resourceNamespace)
	})

	It("Apply CRP which selects cluster role by name and namespace by label", func() {
		By("create the resources to be propagated")
		clusterRole := &rbacv1.ClusterRole{
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
		testutils.CreateClusterRole(ctx, *HubCluster, clusterRole)

		resourceNamespace := testutils.NewNamespace("test-namespace", map[string]string{labelKey: labelValue})
		testutils.CreateNamespace(ctx, *HubCluster, resourceNamespace)

		role := &rbacv1.Role{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-pod-reader",
				Namespace: "test-namespace",
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Verbs:     []string{"get", "list", "watch"},
					Resources: []string{"pods"},
				},
			},
		}

		testutils.CreateRole(ctx, *HubCluster, role)
		workName := "test-crp4"
		By("create the cluster resource placement in the hub cluster")
		crp := &v1alpha1.ClusterResourcePlacement{
			ObjectMeta: v1.ObjectMeta{Name: "test-crp4"},
			Spec: v1alpha1.ClusterResourcePlacementSpec{
				ResourceSelectors: []v1alpha1.ClusterResourceSelector{
					{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						Name:    "test-cluster-role",
					},
					{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						LabelSelector: &v1.LabelSelector{
							MatchLabels: map[string]string{labelKey: labelValue},
						},
					},
				},
			},
		}

		testutils.CreateClusterResourcePlacement(ctx, *HubCluster, crp)

		By("check if work gets created for cluster resource placement")
		testutils.WaitWork(*HubCluster, workName, memberNS.Name)

		By("check if cluster resource placement status is updated")
		selectedResources := []v1alpha1.ResourceIdentifier{selectedResource1, selectedResource2, selectedResource4}
		testutils.WaitCreateClusterResourcePlacement(ctx, *HubCluster, crp, conditionType, conditionStatus, selectedResources, targetClusters, 3*testutils.PollTimeout)

		By("check if cluster role, role & role binding are propagated to member cluster")
		testutils.WaitCreateNamespace(ctx, *MemberCluster, resourceNamespace)
		testutils.WaitCreateClusterRole(ctx, *MemberCluster, clusterRole)
		testutils.WaitCreateRole(ctx, *MemberCluster, role)

		By("delete cluster resource placement on hub cluster")
		testutils.DeleteClusterResourcePlacement(ctx, *HubCluster, crp)

		By("delete cluster role")
		testutils.DeleteClusterRole(ctx, *HubCluster, clusterRole)

		By("delete namespace")
		testutils.DeleteNamespace(ctx, *HubCluster, resourceNamespace)
	})

	It("Apply CRP selecting cluster role, add new cluster role with different label and update CRP to select new cluster role", func() {
		By("create the resources to be propagated")
		clusterRole := &rbacv1.ClusterRole{
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
		testutils.CreateClusterRole(ctx, *HubCluster, clusterRole)

		workName := "test-crp5"
		By("create the cluster resource placement in the hub cluster")
		crp := &v1alpha1.ClusterResourcePlacement{
			ObjectMeta: v1.ObjectMeta{Name: "test-crp5"},
			Spec: v1alpha1.ClusterResourcePlacementSpec{
				ResourceSelectors: []v1alpha1.ClusterResourceSelector{
					{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						LabelSelector: &v1.LabelSelector{
							MatchLabels: map[string]string{labelKey: labelValue},
						},
					},
				},
			},
		}

		testutils.CreateClusterResourcePlacement(ctx, *HubCluster, crp)

		By("check if work gets created for cluster resource placement")
		testutils.WaitWork(*HubCluster, workName, memberNS.Name)

		By("check if cluster resource placement status is updated")
		var selectedResources []v1alpha1.ResourceIdentifier
		selectedResources = []v1alpha1.ResourceIdentifier{selectedResource1}
		testutils.WaitCreateClusterResourcePlacement(ctx, *HubCluster, crp, conditionType, conditionStatus, selectedResources, targetClusters, 3*testutils.PollTimeout)

		By("check if cluster role is propagated to member cluster")
		testutils.WaitCreateClusterRole(ctx, *MemberCluster, clusterRole)

		By("create new cluster role")
		newLabelKey := "fleet.azure.com/region"
		newLabelValue := "us"
		newClusterRole := &rbacv1.ClusterRole{
			ObjectMeta: v1.ObjectMeta{
				Name:   "new-cluster-role",
				Labels: map[string]string{newLabelKey: newLabelValue},
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "list", "watch"},
					APIGroups: []string{""},
					Resources: []string{"pods"},
				},
			},
		}
		testutils.CreateClusterRole(ctx, *HubCluster, newClusterRole)

		By("check if cluster resource placement status is not updated")
		testutils.WaitCreateClusterResourcePlacement(ctx, *HubCluster, crp, conditionType, conditionStatus, selectedResources, targetClusters, 3*testutils.PollTimeout)

		By("update cluster resource placement to select new role")
		newCrp := &v1alpha1.ClusterResourcePlacement{
			ObjectMeta: v1.ObjectMeta{Name: "test-crp5"},
			Spec: v1alpha1.ClusterResourcePlacementSpec{
				ResourceSelectors: []v1alpha1.ClusterResourceSelector{
					{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						LabelSelector: &v1.LabelSelector{
							MatchLabels: map[string]string{labelKey: labelValue},
						},
					},
					{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						LabelSelector: &v1.LabelSelector{
							MatchLabels: map[string]string{newLabelKey: newLabelValue},
						},
					},
				},
			},
		}
		newCrp.SetResourceVersion(crp.GetResourceVersion())
		testutils.UpdateClusterResourcePlacement(ctx, *HubCluster, newCrp)

		By("check if cluster resource placement status is updated")
		selectedResources = []v1alpha1.ResourceIdentifier{selectedResource1, selectedResource6}
		testutils.WaitCreateClusterResourcePlacement(ctx, *HubCluster, newCrp, conditionType, conditionStatus, selectedResources, targetClusters, 3*testutils.PollTimeout)

		By("check if new cluster role is propagated to member cluster")
		testutils.WaitCreateClusterRole(ctx, *MemberCluster, newClusterRole)

		By("delete cluster resource placement on hub cluster")
		testutils.DeleteClusterResourcePlacement(ctx, *HubCluster, newCrp)

		By("delete cluster role")
		testutils.DeleteClusterRole(ctx, *HubCluster, clusterRole)
	})
})
