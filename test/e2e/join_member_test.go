/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("Join member cluster testing", func() {

	ginkgo.Context("join succeed", func() {

	})

	ginkgo.BeforeEach(func() {

	})

	//TODO: in progress implementation
	ginkgo.It("deploy memberCluster in the hub cluster", func() {

		ginkgo.By("check if internalmembercluster created in the hub cluster", func() {

		})

		ginkgo.By("deploy membership in the member cluster", func() {

		})

		ginkgo.By("check if internalmembercluster state is updated", func() {

		})

		ginkgo.By("check if membership state is updated", func() {

		})

		ginkgo.By("check if membercluster state is updated", func() {

		})
	})
})
