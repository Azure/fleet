/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package framework

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	"go.goms.io/fleet/apis/v1alpha1"
)

// MEMBER CLUSTER

// CreateMemberCluster creates MemberCluster in the hub cluster.
func CreateMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster) {
	ginkgo.By(fmt.Sprintf("Creating MemberCluster(%s/%s)", mc.Namespace, mc.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), mc)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// UpdateMemberCluster updates MemberCluster in the hub cluster.
func UpdateMemberClusterState(cluster Cluster, mc *v1alpha1.MemberCluster, state v1alpha1.ClusterState) {
	err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: mc.Name, Namespace: ""}, mc)
	gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	mc.Spec.State = state
	err = cluster.KubeClient.Update(context.TODO(), mc)
	gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
}

// DeleteMemberCluster deletes MemberCluster in the hub cluster.
func DeleteMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster) {
	ginkgo.By(fmt.Sprintf("Deleting MemberCluster(%s)", mc.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), mc)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// WaitMemberCluster waits for MemberCluster to present on th hub cluster.
func WaitMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster) {
	klog.Infof("Waiting for MemberCluster(%s) to be synced", mc.Name)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: mc.Name, Namespace: ""}, mc)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// WaitStateUpdatedMemberCluster waits for MemberCluster to present on th hub cluster with a specific state.
func WaitStateUpdatedMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster, state v1alpha1.ClusterState) {
	klog.Infof("Waiting for MemberCluster(%s) to be synced", mc.Name)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: mc.Name, Namespace: ""}, mc)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		return mc.Spec.State == state
	}, PollTimeout, PollInterval).Should(gomega.Equal(true))
}

// INTERNAL MEMBER CLUSTER

// CreateInternalMemberCluster creates InternalMemberCluster in the hub cluster.
func CreateInternalMemberCluster(cluster Cluster, imc *v1alpha1.InternalMemberCluster) {
	ginkgo.By(fmt.Sprintf("Creating InternalMemberCluster(%s/%s)", imc.Namespace, imc.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), imc)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// DeleteInternalMemberCluster deletes InternalMemberCluster in the hub cluster.
func DeleteInternalMemberCluster(cluster Cluster, imc *v1alpha1.InternalMemberCluster) {
	ginkgo.By(fmt.Sprintf("Deleting InternalMemberCluster(%s)", imc.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), imc)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// WaitInternalMemberCluster waits for InternalMemberCluster to present on th hub cluster.
func WaitInternalMemberCluster(cluster Cluster, imc *v1alpha1.InternalMemberCluster) {
	klog.Infof("Waiting for InternalMemberCluster(%s) to be synced in the %s cluster", imc.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// WaitStateInternalMemberCluster waits for InternalMemberCluster to have specific state on th hub cluster.
func WaitStateInternalMemberCluster(cluster Cluster, imc *v1alpha1.InternalMemberCluster, state v1alpha1.ClusterState) {
	klog.Infof("Waiting for InternalMemberCluster(%s) to be synced in the %s cluster", imc.Name, cluster.ClusterName)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		return imc.Spec.State == state
	}, PollTimeout, PollInterval).Should(gomega.Equal(true))
}

// MEMBERSHIP

// CreateMembership creates Membership in the member cluster.
func CreateMembership(cluster Cluster, m *v1alpha1.Membership) {
	ginkgo.By(fmt.Sprintf("Creating Membership(%s)", m.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), m)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// UpdateMembershipState updates Membership in the member cluster.
func UpdateMembershipState(cluster Cluster, m *v1alpha1.Membership, state v1alpha1.ClusterState) {
	err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: m.Name, Namespace: m.Namespace}, m)
	gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	m.Spec.State = state
	err = cluster.KubeClient.Update(context.TODO(), m)
	gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
}

// DeleteMembership deletes Membership in the member cluster.
func DeleteMembership(cluster Cluster, m *v1alpha1.Membership) {
	ginkgo.By(fmt.Sprintf("Deleting MemberCluster(%s)", m.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), m)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// WaitMembership waits for Membership to present on th member cluster.
func WaitMembership(cluster Cluster, m *v1alpha1.Membership) {
	klog.Infof("Waiting for Membership(%s) to be synced", m.Name)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: m.Name, Namespace: m.Namespace}, m)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// WaitConditionMembership waits for Membership to present on the member cluster with a specific condition.
// Allowing custom timeout as for join cond it needs longer than defined PollTimeout for the member agent to finish joining.
func WaitConditionMembership(cluster Cluster, m *v1alpha1.Membership, conditionName string, status metav1.ConditionStatus, customTimeout time.Duration) {
	klog.Infof("Waiting for Membership(%s) condition(%s) status(%s) to be synced", m.Name, conditionName, status)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: m.Name, Namespace: m.Namespace}, m)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		cond := m.GetCondition(conditionName)
		return cond != nil && cond.Status == status
	}, customTimeout, PollInterval).Should(gomega.Equal(true))
}
