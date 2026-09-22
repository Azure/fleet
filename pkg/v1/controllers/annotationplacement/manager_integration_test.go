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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

// These specs run the controller the way the hub agent would: registered with a manager, fed
// annotated resources through the channel the resource watcher writes to, and reading generated
// policies from the manager's cache. They are what proves the watch on the generated policies --
// the reconciler's own tests call Reconcile directly and never see a policy event.
var _ = Describe("annotation based placement under a manager", Ordered, func() {
	var (
		managerCtx    context.Context
		stopManager   context.CancelFunc
		managerDone   chan struct{}
		sourceQueue   *SourceQueue
		configMap     *corev1.ConfigMap
		managedEvents *events.FakeRecorder
	)

	// report hands the resource watcher's view of a resource to the controller, as the hub agent's
	// change detector would on every event for an annotated resource.
	report := func(object client.Object) {
		source := &unstructured.Unstructured{}
		content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
		Expect(err).Should(Succeed())
		source.SetUnstructuredContent(content)
		source.SetGroupVersionKind(configMapGVK)
		sourceQueue.Add(source)
	}

	managedPolicy := func() (kfplacementv1alpha1.PlacementPolicyAccessor, error) {
		policy := &kfplacementv1alpha1.PlacementPolicy{}
		key := client.ObjectKey{Namespace: configMap.Namespace, Name: generatedPolicyName(configMapGVK, configMap.Namespace, configMap.Name)}
		return policy, hubClient.Get(ctx, key, policy)
	}

	BeforeAll(func() {
		managerCtx, stopManager = context.WithCancel(ctx)
		mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
			Scheme:  scheme.Scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
		})
		Expect(err).Should(Succeed())

		sourceQueue = &SourceQueue{}
		managedEvents = events.NewFakeRecorder(100)
		managed := &Reconciler{
			Client:     mgr.GetClient(),
			APIReader:  mgr.GetAPIReader(),
			RESTMapper: mgr.GetRESTMapper(),
			Recorder:   managedEvents,
		}
		Expect(managed.SetupWithManager(mgr, sourceQueue)).Should(Succeed())
		managerDone = make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(managerDone)
			Expect(mgr.Start(managerCtx)).Should(Succeed())
		}()

		configMapCount++
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("managed-%d", configMapCount),
				Namespace: "default",
				Annotations: map[string]string{
					kfplacementv1alpha1.ClusterSelectorsAnnotation: "env=staging,count=All",
				},
			},
		}
		Expect(hubClient.Create(ctx, configMap)).Should(Succeed())
	})

	AfterAll(func() {
		Expect(client.IgnoreNotFound(hubClient.Delete(ctx, configMap))).Should(Succeed())
		stopManager()
		// Start's own assertion must land while this spec tree is still running.
		Eventually(managerDone, eventuallyTimeout).Should(BeClosed())
	})

	It("should generate a policy for a resource the watcher reports", func() {
		report(configMap)
		Eventually(func() error {
			_, err := managedPolicy()
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	})

	It("should restore a generated policy someone edits, with no event on the resource", func() {
		policy, err := managedPolicy()
		Expect(err).Should(Succeed())
		policy.GetSpec().ClusterSelectors = nil
		Expect(hubClient.Update(ctx, policy)).Should(Succeed())

		Eventually(func() ([]kfplacementv1alpha1.ClusterSelector, error) {
			policy, err := managedPolicy()
			if err != nil {
				return nil, err
			}
			return policy.GetSpec().ClusterSelectors, nil
		}, eventuallyTimeout, eventuallyInterval).Should(HaveLen(1), "the watch on generated policies must bring an edited policy back")
	})

	It("should recreate a generated policy someone deletes, with no event on the resource", func() {
		policy, err := managedPolicy()
		Expect(err).Should(Succeed())
		deletedUID := policy.GetUID()
		Expect(hubClient.Delete(ctx, policy)).Should(Succeed())

		Eventually(func() (bool, error) {
			policy, err := managedPolicy()
			if err != nil {
				return false, err
			}
			return policy.GetUID() != deletedUID, nil
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue(), "the watch on generated policies must bring a deleted policy back")
	})

	It("should delete the policy once the watcher reports the annotation gone", func() {
		Expect(hubClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)).Should(Succeed())
		configMap.Annotations = nil
		Expect(hubClient.Update(ctx, configMap)).Should(Succeed())
		report(configMap)

		Eventually(func() bool {
			_, err := managedPolicy()
			return apierrors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	})
})
