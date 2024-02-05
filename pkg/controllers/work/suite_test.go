/*
Copyright 2021 The Kubernetes Authors.

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

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package work

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.
var (
	cfg *rest.Config
	// TODO: Separate k8sClient into hub and spoke
	k8sClient      client.Client
	testEnv        *envtest.Environment
	workController *ApplyWorkReconciler
	setupLog       = ctrl.Log.WithName("test")
	ctx            context.Context
	cancel         context.CancelFunc
)

const (
	// number of concurrent reconcile loop for work
	maxWorkConcurrency = 5
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Work-API Controller Suite")
}

// createControllers create the controllers with the supplied config.
func createControllers(ctx context.Context, hubCfg, spokeCfg *rest.Config, setupLog logr.Logger, opts ctrl.Options) (manager.Manager, *ApplyWorkReconciler, error) {
	hubMgr, err := ctrl.NewManager(hubCfg, opts)
	if err != nil {
		setupLog.Error(err, "unable to create hub manager")
		return nil, nil, err
	}

	spokeDynamicClient, err := dynamic.NewForConfig(spokeCfg)
	if err != nil {
		setupLog.Error(err, "unable to create spoke dynamic client")
		return nil, nil, err
	}

	restHTTPClient, err := rest.HTTPClientFor(spokeCfg)
	if err != nil {
		setupLog.Error(err, "unable to create spoke rest http client")
		return nil, nil, err
	}
	restMapper, err := apiutil.NewDynamicRESTMapper(spokeCfg, restHTTPClient)
	if err != nil {
		setupLog.Error(err, "unable to create spoke rest mapper")
		return nil, nil, err
	}

	spokeClient, err := client.New(spokeCfg, client.Options{
		Scheme: opts.Scheme, Mapper: restMapper,
	})
	if err != nil {
		setupLog.Error(err, "unable to create spoke client")
		return nil, nil, err
	}

	workController := NewApplyWorkReconciler(
		hubMgr.GetClient(),
		spokeDynamicClient,
		spokeClient,
		restMapper,
		hubMgr.GetEventRecorderFor("work_controller"),
		maxWorkConcurrency,
		opts.Cache.Namespaces[0],
	)

	if err = workController.SetupWithManager(hubMgr); err != nil {
		setupLog.Error(err, "unable to create the controller", "controller", "Work")
		return nil, nil, err
	}

	if err = workController.Join(ctx); err != nil {
		setupLog.Error(err, "unable to mark the controller joined", "controller", "Work")
		return nil, nil, err
	}

	return hubMgr, workController, nil
}

var _ = BeforeSuite(func() {
	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("../../../", "config", "crd", "bases"),
			filepath.Join("../../../", "test", "integration", "manifests", "resources"),
		},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = fleetv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = kruisev1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	opts := ctrl.Options{
		Scheme: scheme.Scheme,
	}
	k8sClient, err = client.New(cfg, client.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).NotTo(HaveOccurred())

	By("start controllers")
	var hubMgr manager.Manager
	if hubMgr, workController, err = createControllers(ctx, cfg, cfg, setupLog, opts); err != nil {
		setupLog.Error(err, "problem creating controllers")
		os.Exit(1)
	}
	go func() {
		ctx, cancel = context.WithCancel(context.Background())
		if err = hubMgr.Start(ctx); err != nil {
			setupLog.Error(err, "problem running controllers")
			os.Exit(1)
		}
		Expect(err).ToNot(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
