/*
Copyright 2026 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package workgenerator

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

const (
	memberCluster1Name = "member-1"
	memberCluster2Name = "member-2"

	// The namespace in which namespace-scoped placement objects (e.g., PlacementBinding and
	// PlacementResourceSnapshot objects) are created.
	appNamespaceName = "app"

	workerCount             = 4
	maxConcurrentReconciles = 4
)

var (
	hubCfg    *rest.Config
	hubEnv    *envtest.Environment
	hubClient client.Client
	mgr       manager.Manager

	ctx    context.Context
	cancel context.CancelFunc

	memberClusterNames = []string{memberCluster1Name, memberCluster2Name}
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Work Generator Integration Test Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	By("Bootstrapping test environment")
	hubEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	hubCfg, err = hubEnv.Start()
	Expect(err).Should(Succeed())
	Expect(hubCfg).NotTo(BeNil())

	By("Setting up the scheme")
	Expect(placementv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())

	By("Starting the controller manager")
	mgr, err = ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).Should(Succeed())

	// Use the same (cache-backed) client as the controller to avoid cache delays when verifying results.
	By("Setting the hub client to the one used by the controller manager")
	hubClient = mgr.GetClient()

	// The field indexes must be set up before the manager starts.
	By("Setting up the field indexes")
	Expect(fieldindexers.SetupWithHubControllerManager(ctx, mgr)).Should(Succeed())

	By("Setting up the work generator")
	Expect(New(mgr.GetClient(), workerCount).SetupWithManager(mgr, maxConcurrentReconciles)).Should(Succeed())

	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).Should(Succeed(), "Failed to run manager")
	}()
	Expect(mgr.GetCache().WaitForCacheSync(ctx)).Should(BeTrue(), "Failed to sync the manager cache")

	By("Creating the application namespace")
	Expect(hubClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: appNamespaceName},
	})).Should(Succeed(), "Failed to create the application namespace")

	By("Creating the reserved namespaces for member clusters")
	for _, clusterName := range memberClusterNames {
		Expect(hubClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf(utils.NamespaceNameFormat, clusterName)},
		})).Should(Succeed(), "Failed to create the reserved namespace for member cluster %s", clusterName)
	}
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()

	By("Tearing down the test environment")
	Expect(hubEnv.Stop()).Should(Succeed())
})
