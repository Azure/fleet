/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes"

	"go.goms.io/fleet/test/e2e/framework"
)

// TODO (mng): move this test to join/leave tests after those tests finished.
var _ = Describe("Test metrics exposure", func() {
	It("check exposed metrics on hub cluster", func() {
		By("creating cluster REST config")
		clusterConfig := framework.GetClientConfig(HubCluster)
		restConfig, err := clusterConfig.ClientConfig()
		Expect(err).ToNot(HaveOccurred())

		By("creating cluster clientSet")
		clientSet, err := kubernetes.NewForConfig(restConfig)
		Expect(err).ToNot(HaveOccurred())

		By("getting metrics exposed at /metrics endpoint")
		metrics, err := clientSet.RESTClient().Get().AbsPath("/metrics").DoRaw(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(metrics).ToNot(BeEmpty())
	})

	It("check exposed metrics on member cluster", func() {
		By("creating cluster REST config")
		clusterConfig := framework.GetClientConfig(MemberCluster)
		restConfig, err := clusterConfig.ClientConfig()
		Expect(err).ToNot(HaveOccurred())

		By("creating cluster clientSet")
		clientSet, err := kubernetes.NewForConfig(restConfig)
		Expect(err).ToNot(HaveOccurred())

		By("getting metrics exposed at /metrics endpoint")
		data, err := clientSet.RESTClient().Get().AbsPath("/metrics").DoRaw(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(data).ToNot(BeEmpty())
	})
})
