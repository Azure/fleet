/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package resource provides test data for testing.
package resource

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// ServiceResourceContentForTest creates a service for testing.
func ServiceResourceContentForTest(t *testing.T) *fleetv1beta1.ResourceContent {
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-name",
			Namespace: "svc-namespace",
			Annotations: map[string]string{
				"svc-annotation-key": "svc-object-annotation-key-value",
			},
			Labels: map[string]string{
				"region": "east",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"svc-spec-selector-key": "svc-spec-selector-value"},
			Ports: []corev1.ServicePort{
				{
					Name:        "svc-port",
					Protocol:    corev1.ProtocolTCP,
					AppProtocol: ptr.To("svc.com/my-custom-protocol"),
					Port:        9001,
				},
			},
		},
	}
	return CreateResourceContentForTest(t, svc)
}

// DeploymentResourceContentForTest creates a deployment for testing.
func DeploymentResourceContentForTest(t *testing.T) *fleetv1beta1.ResourceContent {
	d := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deployment-name",
			Namespace: "deployment-namespace",
			Labels: map[string]string{
				"app": "nginx",
			},
		},
		Spec: appsv1.DeploymentSpec{
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
							Image: "nginx:1.14.2",
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
	return CreateResourceContentForTest(t, d)
}

// SecretResourceContentForTest creates a secret for testing.
func SecretResourceContentForTest(t *testing.T) *fleetv1beta1.ResourceContent {
	s := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-name",
			Namespace: "secret-namespace",
		},
		Data: map[string][]byte{
			".secret-file": []byte("dmFsdWUtMg0KDQo="),
		},
	}
	return CreateResourceContentForTest(t, s)
}

// NamespaceResourceContentForTest creates a namespace for testing.
func NamespaceResourceContentForTest(t *testing.T) *fleetv1beta1.ResourceContent {
	n := corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "namespace-name",
		},
	}
	return CreateResourceContentForTest(t, n)
}

// ClusterRoleResourceContentForTest creates a clusterRole for testing.
func ClusterRoleResourceContentForTest(t *testing.T) *fleetv1beta1.ResourceContent {
	role := rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "clusterrole-name",
		},
	}
	return CreateResourceContentForTest(t, role)
}

// CreateResourceContentForTest creates a ResourceContent for testing.
func CreateResourceContentForTest(t *testing.T, obj interface{}) *fleetv1beta1.ResourceContent {
	t.Helper()

	want, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&obj)
	if err != nil {
		t.Fatalf("ToUnstructured failed: %v", err)
	}
	delete(want["metadata"].(map[string]interface{}), "creationTimestamp")
	delete(want, "status")

	uWant := unstructured.Unstructured{Object: want}
	rawWant, err := uWant.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	return &fleetv1beta1.ResourceContent{
		RawExtension: runtime.RawExtension{
			Raw: rawWant,
		},
	}
}
