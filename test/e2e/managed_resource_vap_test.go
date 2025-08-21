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
	"errors"
	"fmt"
	"net/http"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"

	testutils "go.goms.io/fleet/test/e2e/v1alpha1/utils"
)

const (
	managedByLabel      = "fleet.azure.com/managed-by"
	managedByLabelValue = "arm"
	vapName             = "aks-fleet-managed-by-arm"
	vapBindingName      = "aks-fleet-managed-by-arm"
)

// Helper functions for creating managed resources
func createManagedNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				managedByLabel: managedByLabelValue,
			},
		},
	}
}

func createUnmanagedNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func createManagedResourceQuota(name, namespace string) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				managedByLabel: managedByLabelValue,
			},
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourcePods: resource.MustParse("10"),
			},
		},
	}
}

func createManagedNetworkPolicy(name, namespace string) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				managedByLabel: managedByLabelValue,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
		},
	}
}

func createManagedCRP(name string) *placementv1beta1.ClusterResourcePlacement {
	return &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				managedByLabel: managedByLabelValue,
			},
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-ns",
				},
			},
		},
	}
}

func expectDeniedByVAP(err error) {
	var statusErr *k8sErrors.StatusError
	Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Expected StatusError, got error %s of type %s", err, reflect.TypeOf(err)))
	Expect(statusErr.ErrStatus.Code).To(Equal(int32(http.StatusUnprocessableEntity)), "Expected 422 UnprocessableEntity")
	// ValidatingAdmissionPolicy denials typically contain these patterns
	Expect(statusErr.ErrStatus.Message).To(SatisfyAny(
		ContainSubstring("ValidatingAdmissionPolicy"),
		ContainSubstring("denied the request"),
		ContainSubstring("violates policy"),
	), "Error should indicate policy violation")
}

var _ = Describe("ValidatingAdmissionPolicy for Managed Resources", Label("managedresource"), Ordered, func() {
	BeforeEach(func() {
		Eventually(func() error {
			var vap admissionregistrationv1.ValidatingAdmissionPolicy
			return hubClient.Get(ctx, types.NamespacedName{Name: vapName}, &vap)
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "ValidatingAdmissionPolicy should be installed")

		Eventually(func() error {
			var vapBinding admissionregistrationv1.ValidatingAdmissionPolicyBinding
			return hubClient.Get(ctx, types.NamespacedName{Name: vapBindingName}, &vapBinding)
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "ValidatingAdmissionPolicyBinding should be installed")
	})

	Context("Namespace operations on managed-by label", func() {
		It("should deny CREATE operation on managed namespace for non-system:masters user", func() {
			managedNS := createManagedNamespace("test-managed-ns-create")
			By("expecting denial of CREATE operation on managed namespace")
			err := impersonateHubClient.Create(ctx, managedNS)
			expectDeniedByVAP(err)
		})

		It("should deny UPDATE operation on managed namespace for non-system:masters user", func() {
			managedNS := createManagedNamespace("test-managed-ns-update")
			By("creating managed namespace with system:masters user")
			Expect(hubClient.Create(ctx, managedNS)).To(Succeed())

			Eventually(func() error {
				var ns corev1.Namespace
				if err := hubClient.Get(ctx, types.NamespacedName{Name: managedNS.Name}, &ns); err != nil {
					return err
				}
				ns.Annotations = map[string]string{"test": "annotation"}
				By("expecting denial of UPDATE operation on managed namespace")
				err := impersonateHubClient.Update(ctx, &ns)
				if k8sErrors.IsConflict(err) {
					return err
				}
				expectDeniedByVAP(err)
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())

			Expect(hubClient.Delete(ctx, managedNS)).To(Succeed())
		})

		It("should deny DELETE operation on managed namespace for non-system:masters user", func() {
			managedNS := createManagedNamespace("test-managed-ns-delete")
			By("creating managed namespace with system:masters user")
			Expect(hubClient.Create(ctx, managedNS)).To(Succeed())

			By("expecting denial of DELETE operation on managed namespace")
			err := impersonateHubClient.Delete(ctx, managedNS)
			expectDeniedByVAP(err)

			Expect(hubClient.Delete(ctx, managedNS)).To(Succeed())
		})

		It("should allow CREATE operation on managed namespace for system:masters user", func() {
			managedNS := createManagedNamespace("test-managed-ns-masters")
			By("expecting successful CREATE operation with system:masters user")
			Expect(hubClient.Create(ctx, managedNS)).To(Succeed())

			Expect(hubClient.Delete(ctx, managedNS)).To(Succeed())
		})

		It("should allow operations on unmanaged namespace for non-system:masters user", func() {
			unmanagedNS := createUnmanagedNamespace("test-unmanaged-ns")
			By("expecting successful CREATE operation on unmanaged namespace")
			Expect(impersonateHubClient.Create(ctx, unmanagedNS)).To(Succeed())

			By("expecting successful UPDATE operation on unmanaged namespace")
			Eventually(func() error {
				var ns corev1.Namespace
				if err := impersonateHubClient.Get(ctx, types.NamespacedName{Name: unmanagedNS.Name}, &ns); err != nil {
					return err
				}
				ns.Annotations = map[string]string{"test": "annotation"}
				return impersonateHubClient.Update(ctx, &ns)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())

			By("expecting successful DELETE operation on unmanaged namespace")
			Expect(impersonateHubClient.Delete(ctx, unmanagedNS)).To(Succeed())
		})
	})
})
