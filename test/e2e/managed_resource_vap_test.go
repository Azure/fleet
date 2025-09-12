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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	managedByLabel      = "fleet.azure.com/managed-by"
	managedByLabelValue = "arm"
	vapName             = "aks-fleet-managed-by-arm"
)

var managedByLabelMap = map[string]string{
	managedByLabel: managedByLabelValue,
}

// Helper functions for creating managed resources
func createUnmanagedNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}
func createManagedNamespace(name string) *corev1.Namespace {
	ns := createUnmanagedNamespace(name)
	ns.Labels = managedByLabelMap
	return ns
}

func createManagedResourceQuota(ns, name string) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    managedByLabelMap,
		},
	}
}

func createManagedNetworkPolicy(ns, name string) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    managedByLabelMap,
		},
	}
}

func createManagedCRP(name string) *placementv1beta1.ClusterResourcePlacement {
	return &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: managedByLabelMap,
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
				},
			},
		},
	}
}

func expectDeniedByVAP(err error) {
	var statusErr *k8sErrors.StatusError
	Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Expected StatusError, got error %s of type %s", err, reflect.TypeOf(err)))
	Expect(statusErr.ErrStatus.Code).To(Equal(int32(http.StatusForbidden)), "Expected HTTP 403 Forbidden")
	// ValidatingAdmissionPolicy denials typically contain these patterns
	Expect(statusErr.ErrStatus.Message).To(SatisfyAny(
		ContainSubstring("ValidatingAdmissionPolicy"),
		ContainSubstring("denied the request"),
		ContainSubstring("violates policy"),
	), "Error should indicate policy violation")
}

var _ = Describe("ValidatingAdmissionPolicy for Managed Resources", Label("managedresource"), Ordered, func() {
	It("The VAP and its binding should exist", func() {
		var vap admissionregistrationv1.ValidatingAdmissionPolicy
		Expect(sysMastersClient.Get(ctx, types.NamespacedName{Name: vapName}, &vap)).Should(Succeed(), "ValidatingAdmissionPolicy should be installed")

		var vapBinding admissionregistrationv1.ValidatingAdmissionPolicyBinding
		Expect(sysMastersClient.Get(ctx, types.NamespacedName{Name: vapName}, &vapBinding)).Should(Succeed(), "ValidatingAdmissionPolicyBinding should be installed")
	})

	It("should allow operations on unmanaged namespace for non-system:masters user", func() {
		unmanagedNS := createUnmanagedNamespace("test-unmanaged-ns")
		By("expecting successful CREATE operation on unmanaged namespace")
		Expect(notMasterUser.Create(ctx, unmanagedNS)).To(Succeed())

		By("expecting successful UPDATE operation on unmanaged namespace")
		Eventually(func() error {
			var ns corev1.Namespace
			if err := notMasterUser.Get(ctx, types.NamespacedName{Name: unmanagedNS.Name}, &ns); err != nil {
				return err
			}
			ns.Annotations = map[string]string{"test": "annotation"}
			return notMasterUser.Update(ctx, &ns)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())

		By("expecting successful DELETE operation on unmanaged namespace")
		Expect(notMasterUser.Delete(ctx, unmanagedNS)).To(Succeed())
	})

	Context("When the namespace doesn't exist", func() {
		It("should deny CREATE operation on managed namespace for non-system:masters user", func() {
			managedNS := createManagedNamespace("test-managed-ns-create")
			By("expecting denial of CREATE operation on managed namespace")
			err := notMasterUser.Create(ctx, managedNS)
			expectDeniedByVAP(err)
		})

		It("should allow CREATE operation on managed namespace for system:masters user", func() {
			managedNS := createManagedNamespace("test-managed-ns-masters")
			By("expecting successful CREATE operation with system:masters user")
			Expect(sysMastersClient.Create(ctx, managedNS)).To(Succeed())

			Expect(sysMastersClient.Delete(ctx, managedNS)).To(Succeed())
		})
	})

	Context("When the namespace exists", Ordered, func() {
		managedNS := createManagedNamespace("test-managed-ns-update")
		BeforeAll(func() {
			err := sysMastersClient.Delete(ctx, managedNS)
			if err != nil {
				Expect(k8sErrors.IsNotFound(err)).To(BeTrue())
			}
			Expect(sysMastersClient.Create(ctx, managedNS)).To(Succeed())
			var ns corev1.Namespace
			err = sysMastersClient.Get(ctx, types.NamespacedName{Name: managedNS.Name}, &ns)
			Expect(err).To(BeNil())
			Expect(ns.Labels).To(HaveKeyWithValue(managedByLabel, managedByLabelValue))
		})

		It("should deny DELETE operation on managed namespace for non-system:masters user", func() {
			By("expecting denial of DELETE operation on managed namespace")
			err := notMasterUser.Delete(ctx, managedNS)
			expectDeniedByVAP(err)
		})

		It("should deny UPDATE operation on managed namespace for non-system:masters user", func() {
			var updateErr error
			Eventually(func() error {
				var ns corev1.Namespace
				if err := sysMastersClient.Get(ctx, types.NamespacedName{Name: managedNS.Name}, &ns); err != nil {
					return err
				}
				ns.Annotations = map[string]string{"test": "annotation"}
				By("expecting denial of UPDATE operation on managed namespace")
				updateErr = notMasterUser.Update(ctx, &ns)
				if k8sErrors.IsConflict(updateErr) {
					return updateErr
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())

			expectDeniedByVAP(updateErr)
		})

		It("should allow UPDATE operation on managed namespace for system:masters user", func() {
			var updateErr error
			Eventually(func() error {
				var ns corev1.Namespace
				if err := sysMastersClient.Get(ctx, types.NamespacedName{Name: managedNS.Name}, &ns); err != nil {
					return err
				}
				ns.Annotations = map[string]string{"test": "annotation"}
				By("expecting denial of UPDATE operation on managed namespace")
				updateErr = notMasterUser.Update(ctx, &ns)
				if k8sErrors.IsConflict(updateErr) {
					return updateErr
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		Context("For other resources in scope", func() {

			It("should deny creating managed resource quotas", func() {
				rq := createManagedResourceQuota("default", "default")
				err := notMasterUser.Create(ctx, rq)
				expectDeniedByVAP(err)
			})

			It("should deny creating managed network policy", func() {
				np := createManagedNetworkPolicy("default", "default")
				err := notMasterUser.Create(ctx, np)
				expectDeniedByVAP(err)
			})

			It("should deny creating managed CRP", func() {
				crp := createManagedCRP("test-crp")
				err := notMasterUser.Create(ctx, crp)
				expectDeniedByVAP(err)
			})

			It("general expected behavior of other resources", func() {
				rq := createManagedResourceQuota("default", "default")
				np := createManagedNetworkPolicy("default", "default")
				crp := createManagedCRP("test-crp")
				err := sysMastersClient.Create(ctx, rq)
				Expect(err).To(BeNil(), "system:masters user should create managed ResourceQuota")
				err = sysMastersClient.Create(ctx, np)
				Expect(err).To(BeNil(), "system:masters user should create managed NetworkPolicy")
				err = sysMastersClient.Create(ctx, crp)
				Expect(err).To(BeNil(), "system:masters user should create managed CRP")

				var updateErr error
				Eventually(func() error {
					var urq corev1.ResourceQuota
					if err := sysMastersClient.Get(ctx, types.NamespacedName{Name: "default", Namespace: "default"}, &urq); err != nil {
						return err
					}
					urq.Annotations = map[string]string{"test": "annotation"}
					By("expecting denial of UPDATE operation on managed namespace")
					updateErr = notMasterUser.Update(ctx, &urq)
					if k8sErrors.IsConflict(updateErr) {
						return updateErr
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed())
				expectDeniedByVAP(updateErr)

				err = notMasterUser.Delete(ctx, np)
				expectDeniedByVAP(err)
				err = notMasterUser.Delete(ctx, crp)
				expectDeniedByVAP(err)

				err = sysMastersClient.Delete(ctx, rq)
				Expect(err).To(BeNil(), "system:masters user should create managed ResourceQuota")
				err = sysMastersClient.Delete(ctx, np)
				Expect(err).To(BeNil(), "system:masters user should create managed NetworkPolicy")
				err = sysMastersClient.Delete(ctx, crp)
				Expect(err).To(BeNil(), "system:masters user should create managed CRP")
			})
		})

		AfterAll(func() {
			err := sysMastersClient.Delete(ctx, managedNS)
			if err != nil {
				Expect(k8sErrors.IsNotFound(err)).To(BeTrue())
			}
		})
	})

})
