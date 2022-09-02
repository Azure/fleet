/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/test/e2e/framework"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

var (
	hubClusterName    = "kind-hub-testing"
	memberClusterName = "kind-member-testing"
	HubCluster        = framework.NewCluster(hubClusterName, scheme)
	MemberCluster     = framework.NewCluster(memberClusterName, scheme)
	hubURL            string
	scheme            = runtime.NewScheme()

	// This namespace will store Member cluster-related CRs, such as v1alpha1.MemberCluster
	memberNamespace = testutils.NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName))

	// This namespace in HubCluster will store v1alpha1.Work to simulate Work-related features in Hub Cluster.
	workNamespace = testutils.NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName))

	// This namespace in MemberCluster will store resources created from the Work-api.
	workResourceNamespace = testutils.NewNamespace("resource-namespace")

	// Used to decode an unstructured object.
	genericCodecs = serializer.NewCodecFactory(scheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(workv1alpha1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "fleet e2e suite")
}

var _ = BeforeSuite(func() {
	kubeconfig := os.Getenv("KUBECONFIG")
	Expect(kubeconfig).ShouldNot(BeEmpty(), "Failure to retrieve kubeconfig")

	hubURL = os.Getenv("HUB_SERVER_URL")
	Expect(hubURL).ShouldNot(BeEmpty(), "Failure to retrieve Hub URL")

	// hub setup
	HubCluster.HubURL = hubURL
	framework.GetClusterClient(HubCluster)

	//member setup
	MemberCluster.HubURL = hubURL
	framework.GetClusterClient(MemberCluster)

	testutils.CreateNamespace(*MemberCluster, memberNamespace)

	testutils.CreateNamespace(*HubCluster, workNamespace)
	testutils.CreateNamespace(*MemberCluster, workResourceNamespace)
})

var _ = AfterSuite(func() {
	testutils.DeleteNamespace(*MemberCluster, memberNamespace)

	testutils.DeleteNamespace(*HubCluster, workNamespace)
	testutils.DeleteNamespace(*MemberCluster, workResourceNamespace)
})
