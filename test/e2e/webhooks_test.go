/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
)

var _ = Describe("Hub cluster webhook testing", func() {
	Context("Pod Webhook", func() {
		It("todo", func() {
			ctx = context.Background()
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testDep",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: pointer.Int32(1),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-podTemp-name",
							Namespace: "default",
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "web",
									Image: "nginx:1.12",
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
					},
					Strategy:                appsv1.DeploymentStrategy{},
					MinReadySeconds:         0,
					RevisionHistoryLimit:    nil,
					Paused:                  false,
					ProgressDeadlineSeconds: nil,
				},
			}

			By("creating a deployment with a pod in a non-approved namespace", func() {
				err := HubCluster.KubeClient.Create(ctx, dep)
				if err == nil {
					klog.Fatalf(err.Error())
				}
			})
		})
	})
})
