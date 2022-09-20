package e2eJoinLeavePlacement

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

var (
	scheme            = runtime.NewScheme()
	hubURL            string
	hubClusterName    = "kind-hub-testing"
	memberClusterName = "kind-member-testing"
	HubCluster        = framework.NewCluster(hubClusterName, scheme)
	MemberCluster     = framework.NewCluster(memberClusterName, scheme)
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(workv1alpha1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "fleet join leave placement e2e suite")
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
})
