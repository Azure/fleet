/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"errors"
	"fmt"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"reflect"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Resource validation tests for Member Cluster", func() {
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
		err := hubClient.Create(ctx, memberClusterName)
		var statusErr *k8serrors.StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
		Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
	})

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
		Expect(impersonateHubClient.Create(ctx, memberClusterName)).Should(Succeed())
		Expect(impersonateHubClient.Get(ctx, types.NamespacedName{Name: memberClusterName.Name}, memberClusterName)).Should(Succeed())
		Expect(impersonateHubClient.Delete(ctx, memberClusterName)).Should(Succeed())
		ensureMemberClusterAndRelatedResourcesDeletion(name)
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
		Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')"))
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
		Expect(impersonateHubClient.Create(ctx, memberClusterName)).Should(Succeed())
		Expect(impersonateHubClient.Get(ctx, types.NamespacedName{Name: memberClusterName.Name}, memberClusterName)).Should(Succeed())
		Expect(impersonateHubClient.Delete(ctx, memberClusterName)).Should(Succeed())
		ensureMemberClusterAndRelatedResourcesDeletion(name)
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
		Expect(impersonateHubClient.Create(ctx, memberClusterName)).Should(Succeed())
		Expect(impersonateHubClient.Get(ctx, types.NamespacedName{Name: memberClusterName.Name}, memberClusterName)).Should(Succeed())
		Expect(impersonateHubClient.Delete(ctx, memberClusterName)).Should(Succeed())
		ensureMemberClusterAndRelatedResourcesDeletion(name)
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
		Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')"))
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
		Expect(impersonateHubClient.Create(ctx, memberClusterName)).Should(Succeed())
		Expect(impersonateHubClient.Get(ctx, types.NamespacedName{Name: memberClusterName.Name}, memberClusterName)).Should(Succeed())
		Expect(impersonateHubClient.Delete(ctx, memberClusterName)).Should(Succeed())
		ensureMemberClusterAndRelatedResourcesDeletion(name)
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
		Expect(impersonateHubClient.Create(ctx, memberClusterName)).Should(Succeed())
		Expect(impersonateHubClient.Get(ctx, types.NamespacedName{Name: memberClusterName.Name}, memberClusterName)).Should(Succeed())
		Expect(impersonateHubClient.Delete(ctx, memberClusterName)).Should(Succeed())
		ensureMemberClusterAndRelatedResourcesDeletion(name)
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
		Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')"))
	})
})
