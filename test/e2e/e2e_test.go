/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

var (
	hubCluster1Name    = "kind-hub-testing-1"
	hubCluster2Name    = "kind-hub-testing-2"
	memberCluster1Name = "kind-member-testing-1"
	memberCluster2Name = "kind-member-testing-2"
	memberCluster3Name = "kind-member-testing-3"
	sharedMSI          = "shared-msi-test"
	HubCluster1        = framework.NewCluster(hubCluster1Name, scheme)
	HubCluster2        = framework.NewCluster(hubCluster2Name, scheme)
	MemberCluster1     = framework.NewCluster(memberCluster1Name, scheme)
	MemberCluster2     = framework.NewCluster(memberCluster2Name, scheme)
	MemberCluster3     = framework.NewCluster(memberCluster3Name, scheme)
	scheme             = runtime.NewScheme()
	hubURL             string
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "fleet e2e suite")
}

var _ = BeforeSuite(func() {
	kubeconfig := os.Getenv("KUBECONFIG")
	Expect(kubeconfig).ShouldNot(BeEmpty())

	hubURL = os.Getenv("HUB_SERVER_URL")
	Expect(hubURL).ShouldNot(BeEmpty())

	// hub setup
	HubCluster1.HubURL = hubURL
	framework.GetClusterClient(HubCluster1)

	HubCluster2.HubURL = hubURL
	framework.GetClusterClient(HubCluster2)

	//member setup
	MemberCluster1.HubURL = hubURL
	framework.GetClusterClient(MemberCluster1)

	MemberCluster2.HubURL = hubURL
	framework.GetClusterClient(MemberCluster2)

	MemberCluster3.HubURL = hubURL
	framework.GetClusterClient(MemberCluster3)
})
