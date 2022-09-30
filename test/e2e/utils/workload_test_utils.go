/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"context"
	"fmt"
	"time"

	// Lint check prohibits non "_test" ending files to have dot imports for ginkgo / gomega.
	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

// CmpClusterRole compares actual cluster role with expected cluster role.
func CmpClusterRole(ctx context.Context, cluster framework.Cluster, objectKey *types.NamespacedName, wantClusterRole *rbacv1.ClusterRole, cmpOptions []cmp.Option) {
	gotClusterRole := &rbacv1.ClusterRole{}
	gomega.Eventually(func() error {
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: objectKey.Name}, gotClusterRole); err != nil {
			return err
		}
		if diff := cmp.Diff(wantClusterRole, gotClusterRole, cmpOptions...); diff != "" {
			return fmt.Errorf("cluster role(%s) mismatch (-want +got):\n%s", gotClusterRole.Name, diff)
		}
		return nil
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to compare actual and expected cluster roles in %s cluster", cluster.ClusterName)
}

// CmpNamespace compares actual namespace with expected namespace.
func CmpNamespace(ctx context.Context, cluster framework.Cluster, objectKey *types.NamespacedName, wantNamespace *corev1.Namespace, cmpOptions []cmp.Option) {
	gotNamespace := &corev1.Namespace{}
	gomega.Eventually(func() error {
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: objectKey.Name}, gotNamespace); err != nil {
			return err
		}
		if diff := cmp.Diff(wantNamespace, gotNamespace, cmpOptions...); diff != "" {
			return fmt.Errorf(" namespace(%s) mismatch (-want +got):\n%s", gotNamespace.Name, diff)
		}
		return nil
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to compare actual and expected namespaces in %s cluster", cluster.ClusterName)
}

// CmpRole compares actual role with expected role.
func CmpRole(ctx context.Context, cluster framework.Cluster, objectKey *types.NamespacedName, wantRole *rbacv1.Role, cmpOptions []cmp.Option) {
	gotRole := &rbacv1.Role{}
	gomega.Eventually(func() error {
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: objectKey.Name, Namespace: objectKey.Namespace}, gotRole); err != nil {
			return err
		}
		if diff := cmp.Diff(wantRole, gotRole, cmpOptions...); diff != "" {
			return fmt.Errorf("role(%s) mismatch (-want +got):\n%s", gotRole.Name, diff)
		}
		return nil
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to compare actual and expected roles in %s cluster", cluster.ClusterName)
}

// CmpRoleBinding compares actual role binding with expected role binding.
func CmpRoleBinding(ctx context.Context, cluster framework.Cluster, objectKey *types.NamespacedName, wantRoleBinding *rbacv1.RoleBinding, cmpOptions []cmp.Option) {
	gotRoleBinding := &rbacv1.RoleBinding{}
	gomega.Eventually(func() error {
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: objectKey.Name, Namespace: objectKey.Namespace}, gotRoleBinding); err != nil {
			return err
		}
		if diff := cmp.Diff(wantRoleBinding, gotRoleBinding, cmpOptions...); diff != "" {
			return fmt.Errorf("role binding(%s) mismatch (-want +got):\n%s", gotRoleBinding.Name, diff)
		}
		return nil
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to compare actual and expected role bindings in %s cluster", cluster.ClusterName)
}

// CreateClusterResourcePlacement created ClusterResourcePlacement and waits for ClusterResourcePlacement to exist in hub cluster.
func CreateClusterResourcePlacement(ctx context.Context, cluster framework.Cluster, crp *fleetv1alpha1.ClusterResourcePlacement) {
	gomega.Expect(cluster.KubeClient.Create(ctx, crp)).Should(gomega.Succeed())
	gomega.Eventually(func() error {
		return cluster.KubeClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp)
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Failed to create cluster resource placement %s in %s cluster", crp.Name, cluster.ClusterName)
}

// WaitCreateClusterResourcePlacementStatus waits for ClusterResourcePlacement to present on th hub cluster with a specific status.
func WaitCreateClusterResourcePlacementStatus(ctx context.Context, cluster framework.Cluster, objectKey *types.NamespacedName, wantCRPStatus fleetv1alpha1.ClusterResourcePlacementStatus, crpStatusCmpOptions []cmp.Option, customTimeout time.Duration) {
	gotCRP := &fleetv1alpha1.ClusterResourcePlacement{}
	gomega.Eventually(func() error {
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: objectKey.Name}, gotCRP); err != nil {
			return err
		}
		if statusDiff := cmp.Diff(wantCRPStatus, gotCRP.Status, crpStatusCmpOptions...); statusDiff != "" {
			return fmt.Errorf("cluster resource placment(%s) status mismatch (-want +got):\n%s", gotCRP.Name, statusDiff)
		}
		return nil
	}, customTimeout, PollInterval).Should(gomega.Succeed(), "Failed to wait for cluster resource placement %s status to be updated", gotCRP.Name, cluster.ClusterName)
}

// DeleteClusterResourcePlacement is used delete ClusterResourcePlacement on the hub cluster.
func DeleteClusterResourcePlacement(ctx context.Context, cluster framework.Cluster, crp *fleetv1alpha1.ClusterResourcePlacement) {
	gomega.Expect(cluster.KubeClient.Delete(ctx, crp)).Should(gomega.Succeed(), "Failed to delete cluster resource placement %s in %s cluster", crp.Name, cluster.ClusterName)
	gomega.Eventually(func() bool {
		return apierrors.IsNotFound(cluster.KubeClient.Get(ctx, types.NamespacedName{Name: crp.Name}, crp))
	}, PollTimeout, PollInterval).Should(gomega.BeTrue(), "Failed to wait for cluster resource placement %s to be deleted in %s cluster", crp.Name, cluster.ClusterName)
}
