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

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/kubefleet-dev/kubefleet/pkg/admissionpolicymanager"
)

var _ = Describe("deny service account writes and token requests in restricted namespaces via VAP", Ordered, func() {
	svcAccountName := fmt.Sprintf(svcAccountNameTemplate, GinkgoParallelProcess())
	svcAccountToAddName := "added-sa"
	kubeSystemNamespaceName := "kube-system"

	var svcAccount *corev1.ServiceAccount
	BeforeAll(func() {
		if !EnabledVAPGenerators.Has(admissionpolicymanager.SvcAccountsAndTokenRequestsVAPGeneratorName) {
			Skip("VAP required for this test is not enabled; skip the test")
		}

		// Create a service account in the kube-system namespace.
		svcAccount = &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcAccountName,
				Namespace: kubeSystemNamespaceName,
			},
		}
		Expect(hubClient.Create(ctx, svcAccount)).Should(Succeed(), "Failed to create service account")
	})

	AfterAll(func() {
		// Ensure the removal of the created service account.
		Eventually(func() error {
			svcAccount := corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcAccountName,
					Namespace: kubeSystemNamespaceName,
				},
			}
			if err := hubClient.Delete(ctx, &svcAccount); err != nil {
				if k8sErrors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("failed to delete service account: %w", err)
			}

			if err := hubClient.Get(ctx, types.NamespacedName{Name: svcAccountName, Namespace: kubeSystemNamespaceName}, &corev1.ServiceAccount{}); err != nil {
				if k8sErrors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("failed to retrieve service account: %w", err)
			}
			return fmt.Errorf("service account still exists")
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete service account")
	})

	It("should deny creation of service accounts in kube-system namespace for non-whitelisted users", func() {
		newSvcAccount := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcAccountToAddName,
				Namespace: kubeSystemNamespaceName,
			},
		}
		wantErrMsg := "writing service accounts in reserved namespaces or requesting tokens from such service accounts is disallowed"
		Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Create(ctx, newSvcAccount), wantErrMsg)).Should(Succeed(), "Failed to deny creation of service account in kube-system namespace")
	})

	It("should deny access to service account token subresource in restricted namespace for non-whitelisted users", func() {
		tokenRequest := &authenticationv1.TokenRequest{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: kubeSystemNamespaceName,
				Name:      svcAccountName,
			},
			Spec: authenticationv1.TokenRequestSpec{
				Audiences:         []string{"experimental"},
				ExpirationSeconds: ptr.To(int64(3600)),
			},
		}
		wantErrMsg := "writing service accounts in reserved namespaces or requesting tokens from such service accounts is disallowed"
		Expect(checkIfStatusErrorWithMessage(impersonateHubClient.SubResource("token").Create(ctx, svcAccount, tokenRequest), wantErrMsg)).Should(Succeed(), "Failed to deny access to service account token subresource in restricted namespace")
	})
})

var _ = Describe("deny pod and replica set creation in non-reserved namespaces via VAP", Ordered, func() {
	podName := "dummy-pod"
	replicaSetName := "dummy-replica-set"

	BeforeAll(func() {
		if !EnabledVAPGenerators.Has(admissionpolicymanager.PodsAndReplicaSetsVAPGeneratorName) {
			Skip("VAP required for this test is not enabled; skip the test")
		}
	})

	AfterAll(func() {
		// Ensure the removal of the pod in case the test fails and the object was inadvertently created.
		Eventually(func() error {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: "default",
				},
			}
			if err := hubClient.Delete(ctx, &pod); err != nil {
				if k8sErrors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("failed to delete pod: %w", err)
			}

			if err := hubClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: "default"}, &corev1.Pod{}); err != nil {
				if k8sErrors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("failed to retrieve pod: %w", err)
			}
			return fmt.Errorf("pod still exists")
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete pod")

		// Ensure the removal of the replica set in case the test fails and the object was inadvertently created.
		Eventually(func() error {
			replicaSet := appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      replicaSetName,
					Namespace: "default",
				},
			}
			if err := hubClient.Delete(ctx, &replicaSet); err != nil {
				if k8sErrors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("failed to delete replica set: %w", err)
			}

			if err := hubClient.Get(ctx, types.NamespacedName{Name: replicaSetName, Namespace: "default"}, &appsv1.ReplicaSet{}); err != nil {
				if k8sErrors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("failed to retrieve replica set: %w", err)
			}
			return fmt.Errorf("replica set still exists")
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete replica set")
	})

	It("should deny creation of pods in the default namespace for non-whitelisted users", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "nginx",
						Image: "nginx",
					},
				},
			},
		}
		wantErrMsg := "creating pods and replicas is disallowed in the fleet hub cluster"
		Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Create(ctx, pod), wantErrMsg)).Should(Succeed(), "Failed to deny creation of pod in default namespace")
	})

	It("should deny creation of replica sets in the default namespace for non-whitelisted users", func() {
		replicaSet := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      replicaSetName,
				Namespace: "default",
			},
			Spec: appsv1.ReplicaSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "nginx"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": "nginx"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx",
							},
						},
					},
				},
			},
		}
		wantErrMsg := "creating pods and replicas is disallowed in the fleet hub cluster"
		Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Create(ctx, replicaSet), wantErrMsg)).Should(Succeed(), "Failed to deny creation of replica set in default namespace")
	})
})
