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
package v1beta1

import (
	"errors"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

var _ = Describe("Test MemberCluster CEL validations", func() {
	Context("Name length validation", func() {
		It("should deny creating MemberCluster with name longer than 63 characters", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"
			memberCluster := &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Identity: rbacv1.Subject{
						Name:      "fleet-member-agent-cluster-1",
						Kind:      "ServiceAccount",
						Namespace: "fleet-system",
						APIGroup:  "",
					},
				},
			}
			
			By(fmt.Sprintf("expecting denial of CREATE MemberCluster with name length %d > 63", len(name)))
			err := hubClient.Create(ctx, memberCluster)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Create MemberCluster call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should allow creating MemberCluster with name of 63 characters", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-12345"
			memberCluster := &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Identity: rbacv1.Subject{
						Name:      "fleet-member-agent-cluster-1",
						Kind:      "ServiceAccount",
						Namespace: "fleet-system",
						APIGroup:  "",
					},
				},
			}
			
			By(fmt.Sprintf("expecting success when creating MemberCluster with name length = 63"))
			Expect(hubClient.Create(ctx, memberCluster)).Should(Succeed())
			Expect(hubClient.Delete(ctx, memberCluster)).Should(Succeed())
		})
	})

	Context("Taint validations", func() {
		It("should deny creating MemberCluster with duplicate taints", func() {
			memberCluster := &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("duplicate-taints-%d", GinkgoParallelProcess()),
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Identity: rbacv1.Subject{
						Name:      "fleet-member-agent-cluster-1",
						Kind:      "ServiceAccount",
						Namespace: "fleet-system",
						APIGroup:  "",
					},
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key1",
							Value:  "value2",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			}
			
			By("expecting denial of CREATE MemberCluster with duplicate taints")
			err := hubClient.Create(ctx, memberCluster)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Create MemberCluster call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("taints must be unique"))
		})

		It("should allow creating MemberCluster with unique taints", func() {
			memberCluster := &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("unique-taints-%d", GinkgoParallelProcess()),
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Identity: rbacv1.Subject{
						Name:      "fleet-member-agent-cluster-1",
						Kind:      "ServiceAccount",
						Namespace: "fleet-system",
						APIGroup:  "",
					},
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key2",
							Value:  "value2",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			}
			
			By("expecting success when creating MemberCluster with unique taints")
			Expect(hubClient.Create(ctx, memberCluster)).Should(Succeed())
			Expect(hubClient.Delete(ctx, memberCluster)).Should(Succeed())
		})

		It("should deny creating MemberCluster with invalid taint key format", func() {
			memberCluster := &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("invalid-taint-key-%d", GinkgoParallelProcess()),
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Identity: rbacv1.Subject{
						Name:      "fleet-member-agent-cluster-1",
						Kind:      "ServiceAccount",
						Namespace: "fleet-system",
						APIGroup:  "",
					},
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "invalid_key!",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			}
			
			By("expecting denial of CREATE MemberCluster with invalid taint key format")
			err := hubClient.Create(ctx, memberCluster)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Create MemberCluster call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("taint key must be a valid label name"))
		})

		It("should deny creating MemberCluster with invalid taint value format", func() {
			memberCluster := &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("invalid-taint-value-%d", GinkgoParallelProcess()),
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Identity: rbacv1.Subject{
						Name:      "fleet-member-agent-cluster-1",
						Kind:      "ServiceAccount",
						Namespace: "fleet-system",
						APIGroup:  "",
					},
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "valid-key",
							Value:  "invalid value!",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			}
			
			By("expecting denial of CREATE MemberCluster with invalid taint value format")
			err := hubClient.Create(ctx, memberCluster)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Create MemberCluster call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("taint value must be a valid label value"))
		})

		It("should allow creating MemberCluster with valid taint formats", func() {
			memberCluster := &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("valid-taints-%d", GinkgoParallelProcess()),
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Identity: rbacv1.Subject{
						Name:      "fleet-member-agent-cluster-1",
						Kind:      "ServiceAccount",
						Namespace: "fleet-system",
						APIGroup:  "",
					},
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "valid.key",
							Value:  "valid-value",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "another.valid-key",
							Value:  "valid_value.123",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			}
			
			By("expecting success when creating MemberCluster with valid taint formats")
			Expect(hubClient.Create(ctx, memberCluster)).Should(Succeed())
			Expect(hubClient.Delete(ctx, memberCluster)).Should(Succeed())
		})
	})
})