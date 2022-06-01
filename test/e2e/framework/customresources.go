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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis/v1alpha1"
)

// CreateMemberCluster create MemberCluster in the hub cluster.
func CreateMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster) {
	ginkgo.By(fmt.Sprintf("Creating MemberCluster(%s/%s)", mc.Namespace, mc.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), mc, &client.CreateOptions{})
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

// CreateInternalMemberCluster create InternalMemberCluster in the hub cluster.
func CreateInternalMemberCluster(cluster Cluster, imc *v1alpha1.InternalMemberCluster) {
	ginkgo.By(fmt.Sprintf("Creating InternalMemberCluster(%s/%s)", imc.Namespace, imc.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), imc, &client.CreateOptions{})
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
