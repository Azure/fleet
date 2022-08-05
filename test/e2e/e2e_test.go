/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"embed"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

var (
	hubClusterName    = "kind-hub-testing"
	memberClusterName = "kind-member-testing"
	HubCluster        = framework.NewCluster(hubClusterName, scheme)
	MemberCluster     = framework.NewCluster(memberClusterName, scheme)
	hubURL            string
	scheme            = runtime.NewScheme()
	genericCodecs     = serializer.NewCodecFactory(scheme)
	genericCodec      = genericCodecs.UniversalDeserializer()

	//go:embed manifests
	testManifestFiles embed.FS
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(workv1alpha1.AddToScheme(scheme))
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
	MemberCluster.HubURL = hubURL
	framework.GetClusterClient(MemberCluster)

})
