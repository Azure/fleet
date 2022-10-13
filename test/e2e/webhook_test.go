/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/pkg/utils"
)

const (
	nsKubeSystem  = "kube-system"
	nsFleetSystem = "fleet-system"
)

var (
	whitelistedNamespaces = []corev1.Namespace{
		{ObjectMeta: metav1.ObjectMeta{Name: nsKubeSystem}},
		{ObjectMeta: metav1.ObjectMeta{Name: nsFleetSystem}},
	}
)

var _ = Describe("Hub cluster webhook tests", func() {
	Context("Pod validation", func() {
		It("Operations on Pods within whitelisted namespaces should be admitted", func() {
			for _, ns := range whitelistedNamespaces {
				objKey := client.ObjectKey{Name: utils.RandStr(), Namespace: ns.ObjectMeta.Name}
				ctx = context.Background()
				validPod := &corev1.Pod{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Pod",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      objKey.Name,
						Namespace: objKey.Namespace,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx:1.14.2",
								Ports: []corev1.ContainerPort{
									{
										Name:          "http",
										Protocol:      corev1.ProtocolTCP,
										ContainerPort: 80,
									},
								},
							},
						},
					},
				}

				By(fmt.Sprintf("Creating a pod in namespace (%s)", ns.ObjectMeta.Name), func() {
					err := HubCluster.KubeClient.Create(ctx, validPod)
					if err != nil {
						klog.Fatalf(err.Error())
					}
				})
				By(fmt.Sprintf("Checking if the pod was created in namespace (%s)", ns.ObjectMeta.Name), func() {
					var createdPod corev1.Pod
					err := HubCluster.KubeClient.Get(ctx, objKey, &createdPod)
					Expect(err).ToNot(HaveOccurred())
					Expect(createdPod).ShouldNot(BeNil())
				})
			}
		})
		It("Operations on Pods within non-whitelisted namespaces should be rejected", func() {
			ctx = context.Background()

			// Retrieve list of existing namespaces, remove whitelisted namespaces.
			var nsList corev1.NamespaceList
			err := HubCluster.KubeClient.List(ctx, &nsList)
			Expect(err).ToNot(HaveOccurred())

			// Remove whitelisted namespace from list of existing namespaces.
			for _, ns := range whitelistedNamespaces {
				i := findIndexOfNamespace(ns.Name, nsList)
				if i >= 0 {
					nsList.Items[i] = nsList.Items[len(nsList.Items)-1]
					nsList.Items = nsList.Items[:len(nsList.Items)-1]
				}
			}

			// Attempt creation of pod resource within all non-whitelisted namespaces.
			for _, ns := range nsList.Items {
				objKey := client.ObjectKey{Name: utils.RandStr(), Namespace: ns.ObjectMeta.Name}
				ctx = context.Background()
				pod := &corev1.Pod{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Pod",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      objKey.Name,
						Namespace: objKey.Namespace,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx:1.14.2",
								Ports: []corev1.ContainerPort{
									{
										Name:          "http",
										Protocol:      corev1.ProtocolTCP,
										ContainerPort: 80,
									},
								},
							},
						},
					},
				}

				By(fmt.Sprintf("Expect error during creation of pod in non-whitelisted namespace: %s", ns.ObjectMeta.Name), func() {
					err := HubCluster.KubeClient.Create(ctx, pod)
					Expect(err).Should(HaveOccurred())
				})
			}
		})
	})
})

func findIndexOfNamespace(nsName string, nsList corev1.NamespaceList) int {
	for i, ns := range nsList.Items {
		if ns.Name == nsName {
			return i
		}
	}
	return -1
}
