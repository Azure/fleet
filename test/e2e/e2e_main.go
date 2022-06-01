/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"os"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
)

var (
	Kubeconfig string
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "fleet e2e suite")
}

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	return nil
}, func(bytes []byte) {
	Kubeconfig = os.Getenv("KUBECONFIG")
	gomega.Expect(Kubeconfig).ShouldNot(gomega.BeEmpty())

	var err error
	gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

})

var _ = ginkgo.SynchronizedAfterSuite(func() {

}, func() {

})
