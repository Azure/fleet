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

package bindingmanager

import (
	"context"
	"flag"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
)

// Note (chenyu1): this package uses envtest-based environment for testing purposes as some of the ops in the logic,
// specifically the dry-run ops, require interaction with a real API server.

var (
	hubCfg    *rest.Config
	hubEnv    *envtest.Environment
	hubClient client.Client

	ctx    context.Context
	cancel context.CancelFunc

	playgroundNamespace = "playground"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Binding Manager Integration Test Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	klog.SetLogger(logger)
	ctrl.SetLogger(logger)

	By("Bootstrapping test environment")
	hubEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("../../../../", "config", "crd", "bases")},
	}

	var err error
	hubCfg, err = hubEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(hubCfg).ToNot(BeNil())

	By("Setting up the scheme")
	Expect(placementv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	By("Building the K8s client")
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(hubClient).ToNot(BeNil())

	By("Creating the test namespace")
	Expect(hubClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: playgroundNamespace},
	})).To(Succeed())
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	By("Tearing down the test environment")
	Expect(hubEnv.Stop()).To(Succeed())
})
