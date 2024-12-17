/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	workName = "work-1"

	deployName    = "deploy-1"
	configMapName = "configmap-1"
	nsName        = "ns-1"
)

var (
	deploy = &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: nsName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "nginx",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "nginx",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}
	deployUnstructured *unstructured.Unstructured
	deployJSON         []byte

	ns = &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	nsUnstructured *unstructured.Unstructured
	nsJSON         []byte
)

var (
	appliedWorkOwnerRef = &metav1.OwnerReference{
		APIVersion: "placement.kubernetes-fleet.io/v1beta1",
		Kind:       "AppliedWork",
		Name:       workName,
		UID:        "0",
	}
)

var (
	ignoreFieldTypeMetaInNamespace = cmpopts.IgnoreFields(corev1.Namespace{}, "TypeMeta")
)

func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := fleetv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs (placement/v1beta1) to the runtime scheme: %v", err)
	}

	// Initialize the resource templates used for tests.
	initializeResourceTemplates()

	os.Exit(m.Run())
}

func initializeResourceTemplates() {
	var err error

	// Regular objects.
	// Deployment.
	deployGenericMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deploy)
	if err != nil {
		log.Fatalf("failed to convert deployment to unstructured: %v", err)
	}
	deployUnstructured = &unstructured.Unstructured{Object: deployGenericMap}

	deployJSON, err = deployUnstructured.MarshalJSON()
	if err != nil {
		log.Fatalf("failed to marshal deployment to JSON: %v", err)
	}

	// Namespace.
	nsGenericMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ns)
	if err != nil {
		log.Fatalf("failed to convert namespace to unstructured: %v", err)
	}
	nsUnstructured = &unstructured.Unstructured{Object: nsGenericMap}
	nsJSON, err = nsUnstructured.MarshalJSON()
	if err != nil {
		log.Fatalf("failed to marshal namespace to JSON: %v", err)
	}
}
