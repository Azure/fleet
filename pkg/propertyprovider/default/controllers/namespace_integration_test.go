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

package controllers

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	eventuallyTimeout  = time.Second * 10
	eventuallyInterval = time.Millisecond * 250
)

var _ = Describe("Test Namespace Controller", func() {
	var (
		namespaceName  = "test-namespace"
		workName       = "test-work"
		namespace      *corev1.Namespace
		wantNamespaces = map[string]string{
			// default namespaces created in member cluster
			"default":         "",
			"kube-node-lease": "",
			"kube-public":     "",
			"kube-system":     "",
		}
	)

	Context("Namespace tracking", Ordered, func() {
		It("should track new namespace without owner references", func() {
			// Create a namespace without owner references
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}

			Expect(k8sClient.Create(ctx, namespace)).Should(Succeed(), "failed to create namespace")
			wantNamespaces[namespaceName] = ""
			By("By validating namespaces")
			validateNamespaces(wantNamespaces)
		})

		It("should ignore irrelevant updates like labels and annotations", func() {
			// Update the namespace with labels and annotations (should not trigger reconciliation)
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace); err != nil {
					return err
				}
				namespace.Labels = map[string]string{"test-label": "test-value"}
				namespace.Annotations = map[string]string{"test-annotation": "test-value"}
				return k8sClient.Update(ctx, namespace)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "failed to update the namespace")

			// Namespace should still be tracked without owner reference
			wantNamespaces[namespaceName] = ""
			By("By validating namespaces")
			validateNamespaces(wantNamespaces)
		})

		It("should track namespace when owner references are added", func() {
			// Update the namespace with owner reference
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace); err != nil {
					return err
				}
				namespace.OwnerReferences = []metav1.OwnerReference{
					{
						APIVersion: placementv1beta1.GroupVersion.String(),
						Kind:       placementv1beta1.AppliedWorkKind,
						Name:       workName,
						UID:        "test-uuid",
					},
				}
				return k8sClient.Update(ctx, namespace)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "failed to update the namespace")

			wantNamespaces[namespaceName] = workName
			By("By validating namespaces")
			validateNamespaces(wantNamespaces)
		})

		It("should track namespace when owner references are modified", func() {
			newWorkName := "new-work"
			// Update the namespace with a different owner reference
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace); err != nil {
					return err
				}
				namespace.OwnerReferences = []metav1.OwnerReference{
					{
						APIVersion: placementv1beta1.GroupVersion.String(),
						Kind:       placementv1beta1.AppliedWorkKind,
						Name:       newWorkName,
						UID:        "new-test-uuid",
					},
				}
				return k8sClient.Update(ctx, namespace)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "failed to update the namespace")

			wantNamespaces[namespaceName] = newWorkName
			By("By validating namespaces with updated owner reference")
			validateNamespaces(wantNamespaces)
		})

		It("should untrack namespaces when ns is marked for deletion", func() {
			Expect(k8sClient.Delete(ctx, namespace)).Should(Succeed())

			// The namespace will be in the terminating state because of missing namespace controller.
			// This should trigger the update event filter (oldNs.DeletionTimestamp == nil && newNs.DeletionTimestamp != nil)
			// which will cause reconciliation and untrack the namespace.
			By("By validating namespaces are untracked when transitioning to terminating state")
			delete(wantNamespaces, namespaceName)
			validateNamespaces(wantNamespaces)
		})

		It("should untrack namespaces when ns is deleted", func() {
			Eventually(func() error {
				ns, err := clientset.CoreV1().Namespaces().Get(ctx, namespaceName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Clear the finalizers in the spec
				ns.Spec.Finalizers = []corev1.FinalizerName{}

				// Update the /finalize subresource specifically
				// This is the only way to bypass the block in envtest
				_, err = clientset.CoreV1().Namespaces().Finalize(ctx, ns, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "failed to remove the finalizer")

			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace); !errors.IsNotFound(err) {
					return fmt.Errorf("Namespaces still exists or an unexpected error occurred: %w", err)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "failed to remove the namespace")

			// Verify the namespace gets untracked
			By("By validating namespaces")
			validateNamespaces(wantNamespaces)

		})
	})
})

func validateNamespaces(wantNamespaces map[string]string) {
	Eventually(func() error {
		gotNamespaces, gotReachLimit := tracker.ListNamespaces()
		if diff := cmp.Diff(wantNamespaces, gotNamespaces); diff != "" {
			return fmt.Errorf("ListNamespaces() namespaces mismatch (-want +got):\n%s", diff)
		}

		if gotReachLimit != false {
			return fmt.Errorf("ListNamespaces() reachLimit = %v, want false", gotReachLimit)
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "failed to validate the namespaces")
}
