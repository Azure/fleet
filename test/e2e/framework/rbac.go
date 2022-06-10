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
	corev1 "k8s.io/api/core/v1"
)

// CreateServiceAccount create serviceaccount.
func CreateServiceAccount(cluster Cluster, sa *corev1.ServiceAccount) {
	ginkgo.By(fmt.Sprintf("Creating ServiceAccount(%s)", sa.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), sa)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// DeleteServiceAccount delete serviceaccount.
func DeleteServiceAccount(cluster Cluster, sa *corev1.ServiceAccount) {
	ginkgo.By(fmt.Sprintf("Delete ServiceAccount(%s)", sa.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), sa)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}
