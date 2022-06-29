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
	hubClusterName     = "kind-hub-testing"
	memberClusterName  = "kind-member-testing-1"
	memberCluster2Name = "kind-member-testing-2"
	sharedMSI          = "shared-msi-test"
	HubCluster         = framework.NewCluster(hubClusterName, scheme)
	MemberCluster1     = framework.NewCluster(memberClusterName, scheme)
	MemberCluster2     = framework.NewCluster(memberCluster2Name, scheme)
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
	HubCluster.HubURL = hubURL
	framework.GetClusterClient(HubCluster)

	//member setup
	MemberCluster1.HubURL = hubURL
	framework.GetClusterClient(MemberCluster1)

	MemberCluster2.HubURL = hubURL
	framework.GetClusterClient(MemberCluster2)
})
