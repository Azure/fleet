/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"errors"
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
)

var _ = Describe("Resource validation tests for Member Cluster", func() {
	apiName := fmt.Sprintf("abcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789", GinkgoParallelProcess())

	It("should deny creating API with invalid name size", func() {
		selector := invalidWorkResourceSelector()
		// Create the API.
		api := &clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: apiName,
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
		By(fmt.Sprintf("expecting denial of CREATE API %s", apiName))
		err := hubClient.Create(ctx, api)
		var statusErr *k8sErrors.StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
		Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
	})
})
