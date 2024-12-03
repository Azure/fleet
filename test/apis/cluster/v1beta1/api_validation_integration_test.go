/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package v1beta1

import (
	"errors"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

var _ = Describe("Test cluster v1beta1 API validation", func() {
	Context("Test MemberCluster API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"
			// Create the API.
			memberClusterName := &clusterv1beta1.MemberCluster{
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
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, memberClusterName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {
			var name = "-abcdef-123456789-123456789-123456789-123456789-123456789"
			// Create the API.
			memberClusterName := &clusterv1beta1.MemberCluster{
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
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			err := hubClient.Create(ctx, memberClusterName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-"
			// Create the API.
			memberClusterName := &clusterv1beta1.MemberCluster{
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
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			err := hubClient.Create(ctx, memberClusterName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {
			var name = "a_bcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"
			// Create the API.
			memberClusterName := &clusterv1beta1.MemberCluster{
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
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			err := hubClient.Create(ctx, memberClusterName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})
	})

	Context("Test Member Cluster creation API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			var name = "abc-123456789-123456789-123456789-123456789-123456789-123456789"
			// Create the API.
			memberClusterName := &clusterv1beta1.MemberCluster{
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
			Expect(hubClient.Create(ctx, memberClusterName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, memberClusterName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			var name = "abc-123456789"
			// Create the API.
			memberClusterName := &clusterv1beta1.MemberCluster{
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
			Expect(hubClient.Create(ctx, memberClusterName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, memberClusterName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			var name = "123-123456789"
			// Create the API.
			memberClusterName := &clusterv1beta1.MemberCluster{
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
			Expect(hubClient.Create(ctx, memberClusterName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, memberClusterName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			var name = "123456789-abc"
			// Create the API.
			memberClusterName := &clusterv1beta1.MemberCluster{
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
			Expect(hubClient.Create(ctx, memberClusterName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, memberClusterName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			var name = "123456789-123"
			// Create the API.
			memberClusterName := &clusterv1beta1.MemberCluster{
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
			Expect(hubClient.Create(ctx, memberClusterName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, memberClusterName)).Should(Succeed())
		})
	})
})
