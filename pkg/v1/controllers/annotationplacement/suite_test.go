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

package annotationplacement

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

var configMapGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

var (
	testEnv       *envtest.Environment
	restConfig    *rest.Config
	hubClient     client.Client
	reconciler    *Reconciler
	eventRecorder *events.FakeRecorder
	ctx           context.Context
	cancel        context.CancelFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Annotation Based Placement Controller Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping the test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	var err error
	restConfig, err = testEnv.Start()
	Expect(err).Should(Succeed())
	Expect(restConfig).NotTo(BeNil())

	Expect(kfplacementv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())

	hubClient, err = client.New(restConfig, client.Options{Scheme: scheme.Scheme})
	Expect(err).Should(Succeed())

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	Expect(err).Should(Succeed())
	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	Expect(err).Should(Succeed())

	// The recorder is buffered generously: an unread event blocks the reconciler that records it,
	// which would surface as a timeout somewhere unrelated rather than as a failed expectation.
	eventRecorder = events.NewFakeRecorder(100)
	reconciler = &Reconciler{
		// The envtest client reads straight from the API server, so it serves as the API reader
		// too; there is no cache to fall behind here.
		Client:     hubClient,
		APIReader:  hubClient,
		RESTMapper: restmapper.NewDiscoveryRESTMapper(groupResources),
		Recorder:   eventRecorder,
	}
})

var _ = AfterSuite(func() {
	defer func() {
		Expect(testEnv.Stop()).Should(Succeed())
	}()
	cancel()
})
