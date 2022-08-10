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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
)

// CreateNamespace create namespace.
func CreateNamespace(cluster Cluster, ns *corev1.Namespace) {
	ginkgo.By(fmt.Sprintf("Creating Namespace(%s)", ns.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), ns)
		gomega.Expect(err).Should(gomega.Succeed())
	})
}

// WaitNamespace wait Namespace to present on th member cluster.
func WaitNamespace(cluster Cluster, nc *corev1.Namespace) {
	klog.Infof("Waiting for Namespace(%s) to be synced", nc.Name)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: nc.Name, Namespace: ""}, nc)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// DeleteNamespace delete namespace.
func DeleteNamespace(cluster Cluster, ns *corev1.Namespace) {
	ginkgo.By(fmt.Sprintf("Deleting Namespace(%s)", ns.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), ns)
		if err != nil && !apierrors.IsNotFound(err) {
			gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
		}
	})
}
