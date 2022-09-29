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
	// Lint check prohibits non "_test" ending files to have dot imports for ginkgo / gomega.
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

// CmpClusterRole compares actual cluster role with expected cluster role.
func CmpClusterRole(ctx context.Context, cluster framework.Cluster, actualClusterRole, expectedClusterRole *rbacv1.ClusterRole, cmpOptions []cmp.Option) {
	gomega.Eventually(func() error {
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: actualClusterRole.Name}, actualClusterRole); err != nil {
			return err
		}
		if diff := cmp.Diff(expectedClusterRole, actualClusterRole, cmpOptions...); diff != "" {
			return fmt.Errorf("cluster role(%s) mismatch (-want +got):\n%s", actualClusterRole.Name, diff)
		}
		return nil
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for cluster role %s to be updated in %s cluster", actualClusterRole.Name, cluster.ClusterName)
}

// CmpNamespace compares actual namespace with expected namespace.
func CmpNamespace(ctx context.Context, cluster framework.Cluster, actualNamespace, expectedNamespace *corev1.Namespace, cmpOptions []cmp.Option) {
	gomega.Eventually(func() error {
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: actualNamespace.Name}, actualNamespace); err != nil {
			return err
		}
		if diff := cmp.Diff(expectedNamespace, actualNamespace, cmpOptions...); diff != "" {
			return fmt.Errorf(" namespace(%s) mismatch (-want +got):\n%s", actualNamespace.Name, diff)
		}
		return nil
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to compare actual and expected namespaces in %s cluster", cluster.ClusterName)
}

// CmpRole compares actual role with expected role.
func CmpRole(ctx context.Context, cluster framework.Cluster, actualRole, expectedRole *rbacv1.Role, cmpOptions []cmp.Option) {
	gomega.Eventually(func() error {
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: actualRole.Name, Namespace: actualRole.Namespace}, actualRole); err != nil {
			return err
		}
		if diff := cmp.Diff(expectedRole, actualRole, cmpOptions...); diff != "" {
			return fmt.Errorf("role(%s) mismatch (-want +got):\n%s", actualRole.Name, diff)
		}
		return nil
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to compare actual and expected roles in %s cluster", cluster.ClusterName)
}

// CmpRoleBinding compares actual role binding with expected role binding.
func CmpRoleBinding(ctx context.Context, cluster framework.Cluster, actualRoleBinding, expectedRoleBinding *rbacv1.RoleBinding, cmpOptions []cmp.Option) {
	gomega.Eventually(func() error {
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: actualRoleBinding.Name, Namespace: actualRoleBinding.Namespace}, actualRoleBinding); err != nil {
			return err
		}
		if diff := cmp.Diff(expectedRoleBinding, actualRoleBinding, cmpOptions...); diff != "" {
			return fmt.Errorf("role binding(%s) mismatch (-want +got):\n%s", actualRoleBinding.Name, diff)
		}
		return nil
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to compare actual and expected role bindings in %s cluster", cluster.ClusterName)
}

// CreateClusterResourcePlacement created ClusterResourcePlacement and waits for ClusterResourcePlacement to exist in hub cluster.
func CreateClusterResourcePlacement(ctx context.Context, cluster framework.Cluster, crp *v1alpha1.ClusterResourcePlacement) {
	gomega.Expect(cluster.KubeClient.Create(ctx, crp)).Should(gomega.Succeed())
	gomega.Eventually(func() error {
		return cluster.KubeClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to create cluster resource placement %s in %s cluster", crp.Name, cluster.ClusterName)
}

// WaitCreateClusterResourcePlacementStatus waits for ClusterResourcePlacement to present on th hub cluster with a specific status.
func WaitCreateClusterResourcePlacementStatus(ctx context.Context, cluster framework.Cluster, crp *v1alpha1.ClusterResourcePlacement, status v1alpha1.ClusterResourcePlacementStatus, customTimeout time.Duration) {
	gomega.Eventually(func() error {
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp); err != nil {
			return err
		}
		ignoreOption := cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")
		statusDiff := cmp.Diff(status, crp.Status, ignoreOption)
		if statusDiff != "" {
			return fmt.Errorf("cluster resource placment(%s) status mismatch (-want +got):\n%s", crp.Name, statusDiff)
		}
		return nil
	}, customTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for cluster resource placement %s status to be updated", crp.Name, cluster.ClusterName)
}

// DeleteClusterResourcePlacement is used delete ClusterResourcePlacement on the hub cluster.
func DeleteClusterResourcePlacement(ctx context.Context, cluster framework.Cluster, crp *v1alpha1.ClusterResourcePlacement) {
	gomega.Expect(cluster.KubeClient.Delete(ctx, crp)).Should(gomega.Succeed(), "Failed to delete cluster resource placement %s in %s cluster", crp.Name, cluster.ClusterName)
	gomega.Eventually(func() bool {
		return apierrors.IsNotFound(cluster.KubeClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp))
	}, PollTimeout, PollInterval).Should(gomega.BeTrue(), "Failed to wait for cluster resource placement %s to be deleted in %s cluster", crp.Name, cluster.ClusterName)
}
