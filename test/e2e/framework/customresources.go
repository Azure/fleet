/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package framework

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	"go.goms.io/fleet/apis/v1alpha1"
)

// ##MEMBER CLUSTER##

// CreateMemberCluster create MemberCluster in the hub cluster.
func CreateMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster) {
	ginkgo.By(fmt.Sprintf("Creating MemberCluster(%s/%s)", mc.Namespace, mc.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), mc)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// UpdateMemberCluster update MemberCluster in the hub cluster.
func UpdateMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster) {
	ginkgo.By(fmt.Sprintf("Updating MemberCluster(%s)", mc.Name), func() {
		err := cluster.KubeClient.Status().Update(context.TODO(), mc)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// DeleteMemberCluster delete MemberCluster in the hub cluster.
func DeleteMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster) {
	ginkgo.By(fmt.Sprintf("Deleting MemberCluster(%s)", mc.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), mc)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// WaitMemberCluster wait MemberCluster to present on th hub cluster.
func WaitMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster) {
	klog.Infof("Waiting for MemberCluster(%s) to be synced", mc.Name)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: mc.Name, Namespace: ""}, mc)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// WaitStateUpdatedMemberCluster wait MemberCluster to present on th hub cluster.
func WaitStateUpdatedMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster, state v1alpha1.ClusterState) {
	klog.Infof("Waiting for MemberCluster(%s) to be synced", mc.Name)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: mc.Name, Namespace: ""}, mc)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		return mc.Spec.State == state
	}, PollTimeout, PollInterval).Should(gomega.Equal(true))
}

// ##INTERNAL MEMBER CLUSTER##

// CreateInternalMemberCluster create InternalMemberCluster in the hub cluster.
func CreateInternalMemberCluster(cluster Cluster, imc *v1alpha1.InternalMemberCluster) {
	ginkgo.By(fmt.Sprintf("Creating InternalMemberCluster(%s/%s)", imc.Namespace, imc.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), imc)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// DeleteInternalMemberCluster delete InternalMemberCluster in the hub cluster.
func DeleteInternalMemberCluster(cluster Cluster, imc *v1alpha1.InternalMemberCluster) {
	ginkgo.By(fmt.Sprintf("Deleting InternalMemberCluster(%s)", imc.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), imc)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// WaitInternalMemberCluster wait InternalMemberCluster to present on th hub cluster.
func WaitInternalMemberCluster(cluster Cluster, imc *v1alpha1.InternalMemberCluster) {
	klog.Infof("Waiting for InternalMemberCluster(%s) to be synced in the %s cluster", imc.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// WaitForInternalMemberClusterCondition wait InternalMemberCluster to have specific condition on th hub cluster.
func WaitForInternalMemberClusterCondition(cluster Cluster, imc *v1alpha1.InternalMemberCluster, conditionType string) {
	klog.Infof("Waiting for InternalMemberCluster(%s) to be synced in the %s cluster", imc.Name, cluster.ClusterName)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
		condition := meta.FindStatusCondition(imc.Status.Conditions, conditionType)
		return condition != nil
	}, PollTimeout, PollInterval).Should(gomega.Equal(true))
}

// ##MEMBERSHIP##

// CreateMembership create Membership in the member cluster.
func CreateMembership(cluster Cluster, m *v1alpha1.Membership) {
	ginkgo.By(fmt.Sprintf("Creating Membership(%s)", m.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), m)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// UpdateMembership update Membership in the member cluster.
func UpdateMembership(cluster Cluster, m *v1alpha1.Membership) {
	ginkgo.By(fmt.Sprintf("Updating Membership(%s)", m.Name), func() {
		err := cluster.KubeClient.Status().Update(context.TODO(), m)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// DeleteMembership delete Membership in the member cluster.
func DeleteMembership(cluster Cluster, m *v1alpha1.Membership) {
	ginkgo.By(fmt.Sprintf("Deleting MemberCluster(%s)", m.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), m)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// WaitMembership wait Membership to present on th member cluster.
func WaitMembership(cluster Cluster, m *v1alpha1.Membership) {
	klog.Infof("Waiting for Membership(%s) to be synced", m.Name)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: m.Name, Namespace: m.Namespace}, m)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// WaitStateUpdatedMembership wait Membership to present on th member cluster.
func WaitStateUpdatedMembership(cluster Cluster, m *v1alpha1.Membership, conditionType string) {
	klog.Infof("Waiting for Membership(%s) to be synced", m.Name)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: m.Name, Namespace: m.Namespace}, m)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		condition := meta.FindStatusCondition(m.Status.Conditions, conditionType)
		return condition != nil
	}, PollTimeout, PollInterval).Should(gomega.Equal(true))
}
