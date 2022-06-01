/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package framework

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"go.goms.io/fleet/apis/v1alpha1"
)

var (
	fleetResource = schema.GroupVersionResource{Group: "fleet.azure.com", Version: "v1", Resource: "memberclusters"}
)

// CreateMemberCluster create MemberCluster in the hub cluster.
func CreateMemberCluster(cluster Cluster, mc *v1alpha1.MemberCluster) {
	ginkgo.By(fmt.Sprintf("Creating MemberCluster(%s/%s)", mc.Namespace, mc.Name), func() {
		obj, err := toUnstructured(mc)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

		_, err = cluster.DynamicClientSet.Resource(fleetResource).Create(context.TODO(), obj, metav1.CreateOptions{})
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// RemoveMemberCluster delete MemberCluster in the hub cluster.
func RemoveMemberCluster(cluster Cluster, namespace, name string) {
	ginkgo.By(fmt.Sprintf("Removing MemberCluster(%s/%s)", namespace, name), func() {
		err := cluster.DynamicClientSet.Resource(fleetResource).Delete(context.TODO(), name, metav1.DeleteOptions{})
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	})
}

// toUnstructured converts a typed object to an unstructured object.
func toUnstructured(obj interface{}) (*unstructured.Unstructured, error) {
	uncastObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: uncastObj}, nil
}
