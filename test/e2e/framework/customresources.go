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
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
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

// WaitConditionMemberCluster waits for MemberCluster to present on th hub cluster with a specific condition.
func WaitConditionMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster, conditionName string, status metav1.ConditionStatus, customTimeout time.Duration) {
	klog.Infof("Waiting for MemberCluster(%s) condition(%s) status(%s) to be synced", mc.Name, conditionName, status)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: mc.Name, Namespace: ""}, mc)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		cond := mc.GetCondition(conditionName)
		return cond != nil && cond.Status == status
	}, customTimeout, PollInterval).Should(gomega.Equal(true))
}

// INTERNAL MEMBER CLUSTER

// WaitInternalMemberCluster waits for InternalMemberCluster to present on th hub cluster.
func WaitInternalMemberCluster(cluster Cluster, imc *v1alpha1.InternalMemberCluster) {
	klog.Infof("Waiting for InternalMemberCluster(%s) to be synced in the %s cluster", imc.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// WaitConditionInternalMemberCluster waits for InternalMemberCluster to present on the hub cluster with a specific condition.
// Allowing custom timeout as for join cond it needs longer than defined PollTimeout for the member agent to finish joining.
func WaitConditionInternalMemberCluster(cluster Cluster, imc *v1alpha1.InternalMemberCluster, conditionType v1alpha1.AgentConditionType, status metav1.ConditionStatus, customTimeout time.Duration) {
	klog.Infof("Waiting for InternalMemberCluster(%s) condition(%s) status(%s) to be synced in the %s cluster", imc.Name, conditionType, status, cluster.ClusterName)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		cond := imc.GetConditionWithType(v1alpha1.MemberAgent, string(conditionType))
		return cond != nil && cond.Status == status
	}, customTimeout, PollInterval).Should(gomega.Equal(true))
}

// CreateClusterRole create cluster role in the hub cluster
func CreateClusterRole(cluster Cluster, cr *rbacv1.ClusterRole) {
	ginkgo.By(fmt.Sprintf("Creating ClusterRole (%s)", cr.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), cr)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// WaitClusterRole waits for cluster roles to be created.
func WaitClusterRole(cluster Cluster, cr *rbacv1.ClusterRole) {
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ""}, cr)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// DeleteClusterRole deletes cluster role on cluster.
func DeleteClusterRole(cluster Cluster, cr *rbacv1.ClusterRole) {
	ginkgo.By(fmt.Sprintf("Deleting ClusterRole(%s)", cr.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), cr)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// CreateClusterResourcePlacement created cluster resource placement on hub cluster.
func CreateClusterResourcePlacement(cluster Cluster, crp *v1alpha1.ClusterResourcePlacement) {
	ginkgo.By(fmt.Sprintf("Creating Cluster Resource Placement (%s)", crp.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), crp)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// WaitClusterResourcePlacement waits for ClusterResourcePlacement to present on th hub cluster.
func WaitClusterResourcePlacement(cluster Cluster, crp *v1alpha1.ClusterResourcePlacement) {
	klog.Infof("Waiting for Cluster Resource Placement (%s) to be synced", crp.Name)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: crp.Name, Namespace: ""}, crp)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// WaitConditionClusterResourcePlacement waits for ClusterResourcePlacement to present on th hub cluster with a specific condition.
func WaitConditionClusterResourcePlacement(cluster Cluster, crp *v1alpha1.ClusterResourcePlacement, conditionName string, status metav1.ConditionStatus, customTimeout time.Duration) {
	klog.Infof("Waiting for ClusterResourcePlacement(%s) condition(%s) status(%s) to be synced", crp.Name, conditionName, status)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: crp.Name, Namespace: ""}, crp)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		cond := crp.GetCondition(conditionName)
		return cond != nil && cond.Status == status
	}, customTimeout, PollInterval).Should(gomega.Equal(true))
}

// DeleteClusterResourcePlacement is used delete cluster resource placement on the hub cluster.
func DeleteClusterResourcePlacement(cluster Cluster, crp *v1alpha1.ClusterResourcePlacement) {
	controllerutil.RemoveFinalizer(crp, utils.PlacementFinalizer)
	ginkgo.By(fmt.Sprintf("Deleting ClusterResourcePlacement(%s)", crp.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), crp)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// WaitWork waits for Work to be present on the hub cluster.
func WaitWork(cluster Cluster, workName, workNamespace string) {
	var work workapi.Work
	klog.Infof("Waiting for Work(%s/%s) to be synced", workName, workNamespace)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: workName, Namespace: workNamespace}, &work)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}
