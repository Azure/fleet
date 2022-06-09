/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"os"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"go.goms.io/fleet/test/e2e/framework"
)

var (
	HubClusterName    = "hub-testing"
	MemberClusterName = "member-testing"
	HubCluster        = framework.NewCluster(HubClusterName)
	MemberCluster     = framework.NewCluster(MemberClusterName)
	genericScheme     = runtime.NewScheme()
)

func init() {
	utilruntime.Must(scheme.AddToScheme(genericScheme))
}

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "fleet e2e suite")
}

var _ = ginkgo.BeforeSuite(func() {
	kubeconfig := os.Getenv("KUBECONFIG")
	gomega.Expect(kubeconfig).ShouldNot(gomega.BeEmpty())
	// hub setup
	framework.GetClusterDynamicClient(HubCluster)
	framework.GetClusterClient(HubCluster)

	//member setup
	framework.GetClusterDynamicClient(MemberCluster)
	framework.GetClusterClient(MemberCluster)

})

var _ = ginkgo.SynchronizedAfterSuite(func() {

}, func() {

})
