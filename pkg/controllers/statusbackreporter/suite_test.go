/*
Copyright 2025 The KubeFleet Authors.

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

package statusbackreporter

import (
	"context"
	"flag"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"go.goms.io/fleet/pkg/utils/parallelizer"
)

const (
	defaultWorkerCount = 4
)

const (
	memberReservedNSName = "fleet-member-experimental"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.
var (
	hubCfg             *rest.Config
	hubEnv             *envtest.Environment
	hubClient          client.Client
	hubMgr             manager.Manager
	statusBackReporter *Reconciler

	ctx    context.Context
	cancel context.CancelFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Status Back-Reporter Integration Test Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("Bootstrapping test environments")
	hubEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("../../../", "config", "crd", "bases"),
			filepath.Join("../../../", "test", "manifests"),
		},
	}

	var err error
	hubCfg, err = hubEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(hubCfg).ToNot(BeNil())

	// The schemes have been set up in the TestMain method.

	By("Building the K8s clients")
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(hubClient).ToNot(BeNil())

	hubDynamicClient, err := dynamic.NewForConfig(hubCfg)
	Expect(err).ToNot(HaveOccurred())
	Expect(hubDynamicClient).ToNot(BeNil())

	// Create the reserved namespace for KubeFleet member cluster.
	memberReservedNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberReservedNSName,
		},
	}
	Expect(hubClient.Create(ctx, &memberReservedNS)).To(Succeed())

	By("Setting up the controller and the controller manager for member cluster 1")
	hubMgr, err = ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).ToNot(HaveOccurred())

	statusBackReporter = NewReconciler(
		hubClient,
		hubDynamicClient,
		parallelizer.NewParallelizer(defaultWorkerCount),
	)
	Expect(statusBackReporter.SetupWithManager(hubMgr)).To(Succeed())

	go func() {
		defer GinkgoRecover()
		Expect(hubMgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	By("Tearing down the test environment")
	Expect(hubEnv.Stop()).To(Succeed())
})
