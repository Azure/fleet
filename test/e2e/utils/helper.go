/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

var (
	// PollInterval defines the interval time for a poll operation.
	PollInterval = 5 * time.Second
	// PollTimeout defines the time after which the poll operation times out.
	PollTimeout = 60 * time.Second
)

// NewMemberCluster return a new member cluster.
func NewMemberCluster(name string, heartbeat int32, state v1alpha1.ClusterState) *v1alpha1.MemberCluster {
	identity := rbacv1.Subject{
		Name:      name,
		Kind:      "ServiceAccount",
		Namespace: "fleet-system",
	}
	return &v1alpha1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.MemberClusterSpec{
			Identity:               identity,
			State:                  state,
			HeartbeatPeriodSeconds: heartbeat,
		},
	}
}

// NewInternalMemberCluster returns a new internal member cluster.
func NewInternalMemberCluster(name, namespace string) *v1alpha1.InternalMemberCluster {
	return &v1alpha1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

// NewServiceAccount returns a new service account.
func NewServiceAccount(name, namespace string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

// NewNamespace returns a new namespace.
func NewNamespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

// CreateMemberCluster creates MemberCluster and waits for MemberCluster to exist in the hub cluster.
func CreateMemberCluster(ctx context.Context, cluster framework.Cluster, mc *v1alpha1.MemberCluster) {
	klog.Infof("Creating MemberCluster(%s) in %s cluster", mc.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Create(ctx, mc)).Should(gomega.Succeed(), "Failed to create member cluster %s in %s cluster", mc.Name, cluster.ClusterName)
	klog.Infof("Waiting for MemberCluster(%s) to be synced in %s cluster", mc.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		return cluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for member cluster to be created in %s cluster", mc.Name, cluster.ClusterName)
}

// UpdateMemberClusterState updates MemberCluster in the hub cluster.
func UpdateMemberClusterState(ctx context.Context, cluster framework.Cluster, mc *v1alpha1.MemberCluster, state v1alpha1.ClusterState) {
	err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to retrieve member cluster %s in %s cluster", mc.Name, cluster.ClusterName)
	mc.Spec.State = state
	err = cluster.KubeClient.Update(ctx, mc)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to update member cluster %s in %s cluster", mc.Name, cluster.ClusterName)
}

// DeleteMemberCluster deletes MemberCluster in the hub cluster.
func DeleteMemberCluster(ctx context.Context, cluster framework.Cluster, mc *v1alpha1.MemberCluster) {
	klog.Infof("Deleting MemberCluster(%s) in %s cluster", mc.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Delete(ctx, mc)).Should(gomega.Succeed(), "Failed to delete member cluster %s in %s cluster", mc.Name, cluster.ClusterName)
	klog.Infof("Waiting for MemberCluster (%s) to be deleted in %s cluster", mc.Name, cluster.ClusterName)
	gomega.Eventually(func() bool {
		return apierrors.IsNotFound(cluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc))
	}, PollTimeout, PollInterval).Should(gomega.BeTrue(), "Failed to wait for member cluster %s to be deleted in %s cluster", mc.Name, cluster.ClusterName)
}

// WaitConditionMemberCluster waits for MemberCluster to present on th hub cluster with a specific condition.
func WaitConditionMemberCluster(ctx context.Context, cluster framework.Cluster, mc *v1alpha1.MemberCluster, conditionType v1alpha1.MemberClusterConditionType, status metav1.ConditionStatus, customTimeout time.Duration) {
	klog.Infof("Waiting for MemberCluster(%s) condition(%s) status(%s) to be synced", mc.Name, conditionType, status)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name, Namespace: ""}, mc)
		if err != nil {
			return false
		}
		cond := mc.GetCondition(string(conditionType))
		return cond != nil && cond.Status == status
	}, customTimeout, PollInterval).Should(gomega.BeTrue(), "Failed to wait for member cluster %s to have condition %s with status %s", mc.Name, string(conditionType), status)
}

// WaitInternalMemberCluster waits for InternalMemberCluster to present on the hub cluster.
func WaitInternalMemberCluster(ctx context.Context, cluster framework.Cluster, imc *v1alpha1.InternalMemberCluster) {
	klog.Infof("Waiting for InternalMemberCluster(%s) to be synced in the %s cluster", imc.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		return cluster.KubeClient.Get(ctx, types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for internal member cluster %s to be synced in %s cluster", imc.Name, cluster.ClusterName)
}

// WaitConditionInternalMemberCluster waits for InternalMemberCluster to present on the hub cluster with a specific condition.
// Allowing custom timeout as for join cond it needs longer than defined PollTimeout for the member agent to finish joining.
func WaitConditionInternalMemberCluster(ctx context.Context, cluster framework.Cluster, imc *v1alpha1.InternalMemberCluster, conditionType v1alpha1.AgentConditionType, status metav1.ConditionStatus, customTimeout time.Duration) {
	klog.Infof("Waiting for InternalMemberCluster(%s) condition(%s) status(%s) to be synced in the %s cluster", imc.Name, conditionType, status, cluster.ClusterName)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)
		if err != nil {
			return false
		}
		cond := imc.GetConditionWithType(v1alpha1.MemberAgent, string(conditionType))
		return cond != nil && cond.Status == status
	}, customTimeout, PollInterval).Should(gomega.BeTrue(), "Failed to wait for internal member cluster %s to have condition %s with status %s", imc.Name, string(conditionType), status)
}

// CreateClusterRole create cluster role in the hub cluster.
func CreateClusterRole(ctx context.Context, cluster framework.Cluster, cr *rbacv1.ClusterRole) {
	klog.Infof("Creating ClusterRole (%s) in %s cluster", cr.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Create(ctx, cr)).Should(gomega.Succeed(), "Failed to create cluster role %s in %s cluster", cr.Name, cluster.ClusterName)
}

// UpdateClusterRole updates cluster role in hub cluster.
func UpdateClusterRole(ctx context.Context, cluster framework.Cluster, cr *rbacv1.ClusterRole) {
	klog.Infof("Updating ClusterRole (%s) in %s cluster", cr.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Update(ctx, cr)).Should(gomega.Succeed(), "Failed to update cluster role %s in %s cluster", cr.Name, cluster.ClusterName)
}

// WaitCreateClusterRole waits for cluster roles to be created.
func WaitCreateClusterRole(ctx context.Context, cluster framework.Cluster, cr *rbacv1.ClusterRole) {
	klog.Infof("Waiting for ClusterRole(%s) to be created in %s cluster", cr.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		return cluster.KubeClient.Get(ctx, types.NamespacedName{Name: cr.Name}, cr)
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for cluster role %s to be updated in %s cluster", cr.Name, cluster.ClusterName)
}

// WaitUpdateClusterRoleLabels waits for cluster role to be updated to new labels.
func WaitUpdateClusterRoleLabels(ctx context.Context, cluster framework.Cluster, clusterRole, newClusterRole *rbacv1.ClusterRole) {
	klog.Infof("Waiting for ClusterRole(%s) to be updated in %s cluster", clusterRole.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: clusterRole.Name}, clusterRole)
		if err != nil {
			return err
		}
		diff := cmp.Diff(clusterRole.Labels, newClusterRole.Labels)
		if diff != "" {
			return fmt.Errorf("Role(%s) rules mismatch (-want +got):\n%s", clusterRole.Name, diff)
		}
		return nil
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for cluster role %s to be updated in %s cluster", clusterRole.Name, cluster.ClusterName)
}

// DeleteClusterRole deletes cluster role on cluster.
func DeleteClusterRole(ctx context.Context, cluster framework.Cluster, cr *rbacv1.ClusterRole) {
	klog.Infof("Deleting ClusterRole(%s) in %s cluster", cr.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Delete(ctx, cr)).Should(gomega.Succeed(), "Failed to delete cluster role %s in %s cluster", cr.Name, cluster.ClusterName)
	klog.Infof("Waiting for Cluster Role(%s) to be deleted", cr.Name)
	gomega.Eventually(func() bool {
		return apierrors.IsNotFound(cluster.KubeClient.Get(ctx, types.NamespacedName{Name: cr.Name}, cr))
	}, PollTimeout, PollInterval).Should(gomega.BeTrue(), "Failed to wait for cluster role %s to be deleted in %s cluster", cr.Name, cluster.ClusterName)
}

// CreateRole creates role in hub cluster.
func CreateRole(ctx context.Context, cluster framework.Cluster, r *rbacv1.Role) {
	klog.Infof("Creating Role(%s) in %s cluster", r.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Create(ctx, r)).Should(gomega.Succeed(), "Failed to create role %s in %s cluster", r.Name, cluster.ClusterName)
}

// UpdateRole updates role in hub cluster.
func UpdateRole(ctx context.Context, cluster framework.Cluster, r *rbacv1.Role) {
	klog.Infof("Updating Role(%s) in %s cluster", r.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Update(ctx, r)).Should(gomega.Succeed(), "Failed to update role %s in %s cluster", r.Name, cluster.ClusterName)
}

// WaitCreateRole waits for role to be created.
func WaitCreateRole(ctx context.Context, cluster framework.Cluster, r *rbacv1.Role) {
	klog.Infof("Waiting for Role(%s) to be created in %s cluster", r.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		return cluster.KubeClient.Get(ctx, types.NamespacedName{Name: r.Name, Namespace: r.Namespace}, r)
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for role %s in %s cluster", r.Name, cluster.ClusterName)
}

// WaitDeleteRole waits for role to be deleted.
func WaitDeleteRole(ctx context.Context, cluster framework.Cluster, r *rbacv1.Role) {
	klog.Infof("Waiting for Role(%s) to be updated in %s cluster", r.Name, cluster.ClusterName)
	gomega.Eventually(func() bool {
		return apierrors.IsNotFound(cluster.KubeClient.Get(ctx, types.NamespacedName{Name: r.Name, Namespace: r.Namespace}, r))
	}, PollTimeout, PollInterval).Should(gomega.BeTrue(), "Failed to wait for role %s to be deleted in %s cluster", r.Name, cluster.ClusterName)
}

// WaitUpdateRoleRules waits for role to be updated to new rules.
func WaitUpdateRoleRules(ctx context.Context, cluster framework.Cluster, role, newRole *rbacv1.Role) {
	klog.Infof("Waiting for Role(%s) to be updated in %s cluster", role.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: role.Name, Namespace: role.Namespace}, role)
		if err != nil {
			return err
		}
		diff := cmp.Diff(role.Rules, newRole.Rules)
		if diff != "" {
			return fmt.Errorf("Role(%s) rules mismatch (-want +got):\n%s", role.Name, diff)
		}
		return nil
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for role %s to be updated in %s cluster", role.Name, cluster.ClusterName)
}

// DeleteRole deletes cluster role on cluster.
func DeleteRole(ctx context.Context, cluster framework.Cluster, r *rbacv1.Role) {
	klog.Infof("Deleting Role(%s) in %s cluster", r.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Delete(ctx, r)).Should(gomega.Succeed(), "Failed to delete role %s in %s cluster", r.Name, cluster.ClusterName)
	klog.Infof("Waiting for Role(%s) to be deleted", r.Name)
	gomega.Eventually(func() bool {
		return apierrors.IsNotFound(cluster.KubeClient.Get(ctx, types.NamespacedName{Name: r.Name, Namespace: r.Namespace}, r))
	}, PollTimeout, PollInterval).Should(gomega.BeTrue(), "Failed to wait for role %s to be deleted  in %s cluster", r.Name, cluster.ClusterName)
}

// CreateRoleBinding creates role binding in hub cluster.
func CreateRoleBinding(ctx context.Context, cluster framework.Cluster, rb *rbacv1.RoleBinding) {
	klog.Infof("Creating Role Binding (%s) in %s cluster", rb.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Create(ctx, rb)).Should(gomega.Succeed(), "Failed to create role binding %s in %s cluster", rb.Name, cluster.ClusterName)
}

// WaitRoleBinding waits for role binding to be created.
func WaitCreateRoleBinding(ctx context.Context, cluster framework.Cluster, rb *rbacv1.RoleBinding) {
	klog.Infof("Waiting for RoleBinding(%s) to be created in %s cluster", rb.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		return cluster.KubeClient.Get(ctx, types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace}, rb)
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for role binding %s to be created in %s cluster", rb.Name, cluster.ClusterName)
}

// CreateClusterResourcePlacement created ClusterResourcePlacement and waits for ClusterResourcePlacement to exist in hub cluster.
func CreateClusterResourcePlacement(ctx context.Context, cluster framework.Cluster, crp *v1alpha1.ClusterResourcePlacement) {
	klog.Infof("Creating ClusterResourcePlacement(%s) in %s cluster", crp.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Create(ctx, crp)).Should(gomega.Succeed())
	klog.Infof("Waiting for ClusterResourcePlacement(%s) to be created", crp.Name)
	gomega.Eventually(func() error {
		return cluster.KubeClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to create cluster resource placement %s in %s cluster", crp.Name, cluster.ClusterName)
}

// WaitCreateClusterResourcePlacement waits for ClusterResourcePlacement to present on th hub cluster with a specific status.
func WaitCreateClusterResourcePlacement(ctx context.Context, cluster framework.Cluster, crp *v1alpha1.ClusterResourcePlacement, conditionType []string,
	conditionStatus []metav1.ConditionStatus, selectedResources []v1alpha1.ResourceIdentifier, targetClusters []string, customTimeout time.Duration) {
	klog.Infof("Waiting for ClusterResourcePlacement(%s) status to be synced in %s cluster", crp.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)
		if err != nil {
			return err
		}
		for i := range conditionType {
			condition := crp.GetCondition(conditionType[i])
			if condition != nil && condition.Status != conditionStatus[i] {
				return fmt.Errorf("cluster resource placement(%s) failed to %s resources", crp.Name, conditionType)
			}
		}
		less := func(a, b v1alpha1.ResourceIdentifier) bool { return a.Name < b.Name }
		selectedResourceDiff := cmp.Diff(selectedResources, crp.Status.SelectedResources, cmpopts.SortSlices(less))
		if selectedResourceDiff != "" {
			return fmt.Errorf("cluster resource placment(%s) selected resources mismatch (-want +got):\n%s", crp.Name, selectedResourceDiff)
		}
		targetClustersDiff := cmp.Diff(targetClusters, crp.Status.TargetClusters)
		if targetClustersDiff != "" {
			return fmt.Errorf("cluster resource placment(%s) target clusters mismatch (-want +got):\n%s", crp.Name, targetClustersDiff)
		}
		if crp.Status.FailedResourcePlacements != nil {
			return fmt.Errorf("cluster resource placement(%s) failed resource placements should be nil", crp.Name)
		}
		return nil
	}, customTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for cluster resource placement %s to be created in %s cluster", crp.Name, cluster.ClusterName)
}

// UpdateClusterResourcePlacement updates cluster resource placement in hub cluster.
func UpdateClusterResourcePlacement(ctx context.Context, cluster framework.Cluster, crp *v1alpha1.ClusterResourcePlacement) {
	klog.Infof("Updating Cluster Resource Placement(%s) in %s cluster", crp.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Update(ctx, crp)).Should(gomega.Succeed(), "Failed to update cluster resource placement %s in %s cluster", crp.Name, cluster.ClusterName)
}

// DeleteClusterResourcePlacement is used delete ClusterResourcePlacement on the hub cluster.
func DeleteClusterResourcePlacement(ctx context.Context, cluster framework.Cluster, crp *v1alpha1.ClusterResourcePlacement) {
	klog.Infof("Deleting ClusterResourcePlacement(%s) in %s cluster", crp.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Delete(ctx, crp)).Should(gomega.Succeed(), "Failed to delete cluster resource placement %s in %s cluster", crp.Name, cluster.ClusterName)
	klog.Infof("Waiting for Cluster Resource Placement(%s) to be deleted", crp.Name)
	gomega.Eventually(func() bool {
		return apierrors.IsNotFound(cluster.KubeClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp))
	}, PollTimeout, PollInterval).Should(gomega.BeTrue(), "Failed to wait for cluster resource placement %s to be deleted in %s cluster", crp.Name, cluster.ClusterName)
}

// WaitWork waits for Work to be present on the hub cluster.
func WaitWork(cluster framework.Cluster, workName, workNamespace string) {
	var work workapi.Work
	klog.Infof("Waiting for Work(%s/%s) to be synced", workName, workNamespace)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: workName, Namespace: workNamespace}, &work)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// CreateNamespace create namespace and waits for namespace to exist.
func CreateNamespace(ctx context.Context, cluster framework.Cluster, ns *corev1.Namespace) {
	klog.Infof("Creating Namespace(%s) in %s cluster", ns.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Create(ctx, ns)).Should(gomega.Succeed(), "Failed to create namespace %s in %s cluster", ns.Name, cluster.ClusterName)
	klog.Infof("Waiting for Namespace(%s) to be created in %s cluster", ns.Name)
	gomega.Eventually(func() error {
		return cluster.KubeClient.Get(ctx, types.NamespacedName{Name: ns.Name}, ns)
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for namespace %s to be created in %s cluster", ns.Name, cluster.ClusterName)
}

// WaitCreateNamespace waits for namespace to be created.
func WaitCreateNamespace(ctx context.Context, cluster framework.Cluster, ns *corev1.Namespace) {
	klog.Infof("Waiting for Namespace(%s) to be created in %s cluster", ns.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		return cluster.KubeClient.Get(ctx, types.NamespacedName{Name: ns.Name}, ns)
	}, PollTimeout, PollInterval).Should(gomega.Succeed())
}

// DeleteNamespace delete namespace.
func DeleteNamespace(ctx context.Context, cluster framework.Cluster, ns *corev1.Namespace) {
	klog.Infof("Deleting Namespace(%s) in %s cluster", ns.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Delete(context.TODO(), ns)).Should(gomega.Succeed(), "Failed to delete namespace %s in %s cluster", ns.Name, cluster.ClusterName)
	klog.Infof("Waiting for Namespace(%s) to be deleted in %s cluster", ns.Name, cluster.ClusterName)
	gomega.Eventually(func() bool {
		return apierrors.IsNotFound(cluster.KubeClient.Get(ctx, types.NamespacedName{Name: ns.Name}, ns))
	}, PollTimeout, PollInterval).Should(gomega.BeTrue(), "Failed to wait for namespace %s to be deleted in %s cluster", ns.Name, cluster.ClusterName)
}

// CreateServiceAccount creates service account.
func CreateServiceAccount(ctx context.Context, cluster framework.Cluster, sa *corev1.ServiceAccount) {
	klog.Infof("Creating ServiceAccount(%s) in %s cluster", sa.Name, cluster.ClusterName)
	gomega.Expect(cluster.KubeClient.Create(ctx, sa)).Should(gomega.Succeed(), "Failed to create service account %s in %s cluster")
}
