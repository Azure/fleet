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

package workapplier

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/parallelizer"
)

var (
	statefulSetTemplate = &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx",
			Namespace: nsName,
		},
		Spec: appsv1.StatefulSetSpec{
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

	daemonSetTemplate = &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx",
			Namespace: nsName,
		},
		Spec: appsv1.DaemonSetSpec{
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
							Image: "nginx:latest",
						},
					},
				},
			},
		},
	}

	crdTemplate = &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "foos.example.com",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "Foo",
				ListKind: "FooList",
				Plural:   "foos",
				Singular: "foo",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"field": {
											Type: "string",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	minAvailable = intstr.FromInt32(1)

	pdbTemplate = &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "policy/v1",
			Kind:       "PodDisruptionBudget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-pdb",
			Namespace:  nsName,
			Generation: 2,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
		},
	}
)

// TestTrackDeploymentAvailability tests the trackDeploymentAvailability function.
func TestTrackDeploymentAvailability(t *testing.T) {
	availableDeployWithFixedReplicaCount := deploy.DeepCopy()
	availableDeployWithFixedReplicaCount.Status = appsv1.DeploymentStatus{
		Replicas:          1,
		AvailableReplicas: 1,
		UpdatedReplicas:   1,
	}

	availableDeployWithDefaultReplicaCount := deploy.DeepCopy()
	availableDeployWithDefaultReplicaCount.Spec.Replicas = nil
	availableDeployWithDefaultReplicaCount.Status = appsv1.DeploymentStatus{
		Replicas:          1,
		AvailableReplicas: 1,
		UpdatedReplicas:   1,
	}

	unavailableDeployWithStaleStatus := deploy.DeepCopy()
	unavailableDeployWithStaleStatus.Generation = 2
	unavailableDeployWithStaleStatus.Status = appsv1.DeploymentStatus{
		ObservedGeneration: 1,
		Replicas:           1,
		AvailableReplicas:  1,
		UpdatedReplicas:    1,
	}

	unavailableDeployWithNotEnoughAvailableReplicas := deploy.DeepCopy()
	unavailableDeployWithNotEnoughAvailableReplicas.Spec.Replicas = ptr.To(int32(5))
	unavailableDeployWithNotEnoughAvailableReplicas.Status = appsv1.DeploymentStatus{
		Replicas:          5,
		AvailableReplicas: 2,
		UpdatedReplicas:   5,
	}

	unavailableDeployWithNotEnoughUpdatedReplicas := deploy.DeepCopy()
	unavailableDeployWithNotEnoughUpdatedReplicas.Spec.Replicas = ptr.To(int32(5))
	unavailableDeployWithNotEnoughUpdatedReplicas.Status = appsv1.DeploymentStatus{
		Replicas:          5,
		AvailableReplicas: 5,
		UpdatedReplicas:   2,
	}

	testCases := []struct {
		name                                         string
		deploy                                       *appsv1.Deployment
		wantManifestProcessingAvailabilityResultType ManifestProcessingAvailabilityResultType
	}{
		{
			name:   "available deployment (w/ fixed replica count)",
			deploy: availableDeployWithFixedReplicaCount,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:   "available deployment (w/ default replica count)",
			deploy: availableDeployWithDefaultReplicaCount,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:   "unavailable deployment with stale status",
			deploy: unavailableDeployWithStaleStatus,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
		{
			name:   "unavailable deployment with not enough available replicas",
			deploy: unavailableDeployWithNotEnoughAvailableReplicas,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
		{
			name:   "unavailable deployment with not enough updated replicas",
			deploy: unavailableDeployWithNotEnoughUpdatedReplicas,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotResTyp, err := trackDeploymentAvailability(toUnstructured(t, tc.deploy))
			if err != nil {
				t.Fatalf("trackDeploymentAvailability() = %v, want no error", err)
			}
			if gotResTyp != tc.wantManifestProcessingAvailabilityResultType {
				t.Errorf("manifestProcessingAvailabilityResultType = %v, want %v", gotResTyp, tc.wantManifestProcessingAvailabilityResultType)
			}
		})
	}
}

// TestTrackStatefulSetAvailability tests the trackStatefulSetAvailability function.
func TestTrackStatefulSetAvailability(t *testing.T) {
	availableStatefulSetWithFixedReplicaCount := statefulSetTemplate.DeepCopy()
	availableStatefulSetWithFixedReplicaCount.Status = appsv1.StatefulSetStatus{
		Replicas:          1,
		AvailableReplicas: 1,
		CurrentReplicas:   1,
		UpdatedReplicas:   1,
		CurrentRevision:   "1",
		UpdateRevision:    "1",
	}

	availableStatefulSetWithDefaultReplicaCount := statefulSetTemplate.DeepCopy()
	availableStatefulSetWithDefaultReplicaCount.Spec.Replicas = nil
	availableStatefulSetWithDefaultReplicaCount.Status = appsv1.StatefulSetStatus{
		Replicas:          1,
		AvailableReplicas: 1,
		CurrentReplicas:   1,
		UpdatedReplicas:   1,
		CurrentRevision:   "1",
		UpdateRevision:    "1",
	}

	unavailableStatefulSetWithStaleStatus := statefulSetTemplate.DeepCopy()
	unavailableStatefulSetWithStaleStatus.Generation = 2
	unavailableStatefulSetWithStaleStatus.Status = appsv1.StatefulSetStatus{
		ObservedGeneration: 1,
		Replicas:           1,
		AvailableReplicas:  1,
		CurrentReplicas:    1,
		UpdatedReplicas:    1,
		CurrentRevision:    "1",
		UpdateRevision:     "1",
	}

	unavailableStatefulSetWithNotEnoughAvailableReplicas := statefulSetTemplate.DeepCopy()
	unavailableStatefulSetWithNotEnoughAvailableReplicas.Spec.Replicas = ptr.To(int32(5))
	unavailableStatefulSetWithNotEnoughAvailableReplicas.Status = appsv1.StatefulSetStatus{
		Replicas:          5,
		AvailableReplicas: 2,
		CurrentReplicas:   5,
		UpdatedReplicas:   5,
		CurrentRevision:   "1",
		UpdateRevision:    "1",
	}

	unavailableStatefulSetWithNotEnoughCurrentReplicas := statefulSetTemplate.DeepCopy()
	unavailableStatefulSetWithNotEnoughCurrentReplicas.Spec.Replicas = ptr.To(int32(5))
	unavailableStatefulSetWithNotEnoughCurrentReplicas.Status = appsv1.StatefulSetStatus{
		Replicas:          5,
		AvailableReplicas: 5,
		CurrentReplicas:   2,
		UpdatedReplicas:   5,
		CurrentRevision:   "1",
		UpdateRevision:    "1",
	}

	unavailableStatefulSetWithNotLatestRevision := statefulSetTemplate.DeepCopy()
	unavailableStatefulSetWithNotLatestRevision.Status = appsv1.StatefulSetStatus{
		Replicas:          1,
		AvailableReplicas: 1,
		CurrentReplicas:   1,
		UpdatedReplicas:   1,
		CurrentRevision:   "1",
		UpdateRevision:    "2",
	}

	testCases := []struct {
		name                                         string
		statefulSet                                  *appsv1.StatefulSet
		wantManifestProcessingAvailabilityResultType ManifestProcessingAvailabilityResultType
	}{
		{
			name:        "available stateful set (w/ fixed replica count)",
			statefulSet: availableStatefulSetWithFixedReplicaCount,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:        "available stateful set (w/ default replica count)",
			statefulSet: availableStatefulSetWithDefaultReplicaCount,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:        "unavailable stateful set with stale status",
			statefulSet: unavailableStatefulSetWithStaleStatus,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
		{
			name:        "unavailable stateful set with not enough available replicas",
			statefulSet: unavailableStatefulSetWithNotEnoughAvailableReplicas,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
		{
			name:        "unavailable stateful set with not enough current replicas",
			statefulSet: unavailableStatefulSetWithNotEnoughCurrentReplicas,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
		{
			name:        "unavailable stateful set with not latest revision",
			statefulSet: unavailableStatefulSetWithNotLatestRevision,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotResTyp, err := trackStatefulSetAvailability(toUnstructured(t, tc.statefulSet))
			if err != nil {
				t.Fatalf("trackStatefulSetAvailability() = %v, want no error", err)
			}
			if gotResTyp != tc.wantManifestProcessingAvailabilityResultType {
				t.Errorf("manifestProcessingAvailabilityResultType = %v, want %v", gotResTyp, tc.wantManifestProcessingAvailabilityResultType)
			}
		})
	}
}

// TestTrackDaemonSetAvailability tests the trackDaemonSetAvailability function.
func TestTrackDaemonSetAvailability(t *testing.T) {
	availableDaemonSet := daemonSetTemplate.DeepCopy()
	availableDaemonSet.Status = appsv1.DaemonSetStatus{
		NumberAvailable:        1,
		DesiredNumberScheduled: 1,
		CurrentNumberScheduled: 1,
		UpdatedNumberScheduled: 1,
	}

	unavailableDaemonSetWithStaleStatus := daemonSetTemplate.DeepCopy()
	unavailableDaemonSetWithStaleStatus.Generation = 2
	unavailableDaemonSetWithStaleStatus.Status = appsv1.DaemonSetStatus{
		ObservedGeneration:     1,
		NumberAvailable:        1,
		DesiredNumberScheduled: 1,
		CurrentNumberScheduled: 1,
		UpdatedNumberScheduled: 1,
	}

	unavailableDaemonSetWithNotEnoughAvailablePods := daemonSetTemplate.DeepCopy()
	unavailableDaemonSetWithNotEnoughAvailablePods.Status = appsv1.DaemonSetStatus{
		NumberAvailable:        2,
		DesiredNumberScheduled: 5,
		CurrentNumberScheduled: 5,
		UpdatedNumberScheduled: 5,
	}

	unavailableDaemonSetWithNotEnoughUpdatedPods := daemonSetTemplate.DeepCopy()
	unavailableDaemonSetWithNotEnoughUpdatedPods.Status = appsv1.DaemonSetStatus{
		NumberAvailable:        5,
		DesiredNumberScheduled: 5,
		CurrentNumberScheduled: 5,
		UpdatedNumberScheduled: 6,
	}

	testCases := []struct {
		name                                         string
		daemonSet                                    *appsv1.DaemonSet
		wantManifestProcessingAvailabilityResultType ManifestProcessingAvailabilityResultType
	}{
		{
			name:      "available daemon set",
			daemonSet: availableDaemonSet,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:      "unavailable daemon set with stale status",
			daemonSet: unavailableDaemonSetWithStaleStatus,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
		{
			name:      "unavailable daemon set with not enough available pods",
			daemonSet: unavailableDaemonSetWithNotEnoughAvailablePods,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
		{
			name:      "unavailable daemon set with not enough updated pods",
			daemonSet: unavailableDaemonSetWithNotEnoughUpdatedPods,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotResTyp, err := trackDaemonSetAvailability(toUnstructured(t, tc.daemonSet))
			if err != nil {
				t.Fatalf("trackDaemonSetAvailability() = %v, want no error", err)
			}
			if gotResTyp != tc.wantManifestProcessingAvailabilityResultType {
				t.Errorf("manifestProcessingAvailabilityResultType = %v, want %v", gotResTyp, tc.wantManifestProcessingAvailabilityResultType)
			}
		})
	}
}

// TestTrackServiceAvailability tests the trackServiceAvailability function.
func TestTrackServiceAvailability(t *testing.T) {
	testCases := []struct {
		name                                         string
		service                                      *corev1.Service
		wantManifestProcessingAvailabilityResultType ManifestProcessingAvailabilityResultType
	}{
		{
			name: "untrackable service (external name type)",
			service: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				Spec: corev1.ServiceSpec{
					Type:       corev1.ServiceTypeExternalName,
					ClusterIPs: []string{"192.168.1.1"},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotTrackable,
		},
		{
			name: "available default typed service (IP assigned)",
			service: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				Spec: corev1.ServiceSpec{
					Type:       "",
					ClusterIPs: []string{"192.168.1.1"},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name: "available ClusterIP service (IP assigned)",
			service: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				Spec: corev1.ServiceSpec{
					Type:       corev1.ServiceTypeClusterIP,
					ClusterIP:  "192.168.1.1",
					ClusterIPs: []string{"192.168.1.1"},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name: "available headless service",
			service: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				Spec: corev1.ServiceSpec{
					Type:       corev1.ServiceTypeClusterIP,
					ClusterIPs: []string{"None"},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name: "available node port service (IP assigned)",
			service: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				Spec: corev1.ServiceSpec{
					Type:       corev1.ServiceTypeNodePort,
					ClusterIP:  "13.6.2.2",
					ClusterIPs: []string{"192.168.1.1"},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name: "unavailable ClusterIP service (no IP assigned)",
			service: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "13.6.2.2",
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
		{
			name: "available LoadBalancer service (IP assigned)",
			service: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "10.1.2.4",
							},
						},
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name: "available LoadBalancer service (hostname assigned)",
			service: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								Hostname: "one.microsoft.com",
							},
						},
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name: "unavailable LoadBalancer service (ingress not ready)",
			service: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{},
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotResTyp, err := trackServiceAvailability(toUnstructured(t, tc.service))
			if err != nil {
				t.Errorf("trackServiceAvailability() = %v, want no error", err)
			}
			if gotResTyp != tc.wantManifestProcessingAvailabilityResultType {
				t.Errorf("manifestProcessingAvailabilityResultType = %v, want %v", gotResTyp, tc.wantManifestProcessingAvailabilityResultType)
			}
		})
	}
}

// TestTrackCRDAvailability tests the trackCRDAvailability function.
func TestTrackCRDAvailability(t *testing.T) {
	availableCRD := crdTemplate.DeepCopy()
	availableCRD.Status = apiextensionsv1.CustomResourceDefinitionStatus{
		Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
			{
				Type:   apiextensionsv1.Established,
				Status: apiextensionsv1.ConditionTrue,
			},
			{
				Type:   apiextensionsv1.NamesAccepted,
				Status: apiextensionsv1.ConditionTrue,
			},
		},
	}

	unavailableCRDNotEstablished := crdTemplate.DeepCopy()
	unavailableCRDNotEstablished.Status = apiextensionsv1.CustomResourceDefinitionStatus{
		Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
			{
				Type:   apiextensionsv1.Established,
				Status: apiextensionsv1.ConditionFalse,
			},
			{
				Type:   apiextensionsv1.NamesAccepted,
				Status: apiextensionsv1.ConditionTrue,
			},
		},
	}

	unavailableCRDNameNotAccepted := crdTemplate.DeepCopy()
	unavailableCRDNameNotAccepted.Status = apiextensionsv1.CustomResourceDefinitionStatus{
		Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
			{
				Type:   apiextensionsv1.Established,
				Status: apiextensionsv1.ConditionTrue,
			},
			{
				Type:   apiextensionsv1.NamesAccepted,
				Status: apiextensionsv1.ConditionFalse,
			},
		},
	}

	testCases := []struct {
		name                                         string
		crd                                          *apiextensionsv1.CustomResourceDefinition
		wantManifestProcessingAvailabilityResultType ManifestProcessingAvailabilityResultType
	}{
		{
			name: "available CRD",
			crd:  availableCRD,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name: "unavailable CRD (not established)",
			crd:  unavailableCRDNotEstablished,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
		{
			name: "unavailable CRD (name not accepted)",
			crd:  unavailableCRDNameNotAccepted,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotResTyp, err := trackCRDAvailability(toUnstructured(t, tc.crd))
			if err != nil {
				t.Fatalf("trackCRDAvailability() = %v, want no error", err)
			}
			if gotResTyp != tc.wantManifestProcessingAvailabilityResultType {
				t.Errorf("manifestProcessingAvailabilityResultType = %v, want %v", gotResTyp, tc.wantManifestProcessingAvailabilityResultType)
			}
		})
	}
}

// TestTrackPDBAvailability tests the trackPDBAvailability function.
func TestTrackPDBAvailability(t *testing.T) {
	availablePDB := pdbTemplate.DeepCopy()
	availablePDB.Status = policyv1.PodDisruptionBudgetStatus{
		DisruptionsAllowed: 1,
		CurrentHealthy:     2,
		ObservedGeneration: 2,
		DesiredHealthy:     2,
		ExpectedPods:       1,
		Conditions: []metav1.Condition{
			{
				Type:               policyv1.DisruptionAllowedCondition,
				Status:             metav1.ConditionTrue,
				Reason:             policyv1.SufficientPodsReason,
				ObservedGeneration: 2,
			},
		},
	}
	unavailablePDBInsufficientPods := pdbTemplate.DeepCopy()
	unavailablePDBInsufficientPods.Status = policyv1.PodDisruptionBudgetStatus{
		DisruptionsAllowed: 0,
		CurrentHealthy:     1,
		ObservedGeneration: 2,
		DesiredHealthy:     2,
		ExpectedPods:       1,
		Conditions: []metav1.Condition{
			{
				Type:               policyv1.DisruptionAllowedCondition,
				Status:             metav1.ConditionTrue,
				Reason:             policyv1.SufficientPodsReason,
				ObservedGeneration: 2,
			},
		},
	}

	unavailablePDBStaleCondition := pdbTemplate.DeepCopy()
	unavailablePDBStaleCondition.Status = policyv1.PodDisruptionBudgetStatus{
		DisruptionsAllowed: 1,
		CurrentHealthy:     2,
		ObservedGeneration: 1,
		DesiredHealthy:     2,
		ExpectedPods:       1,
		Conditions: []metav1.Condition{
			{
				Type:               policyv1.DisruptionAllowedCondition,
				Status:             metav1.ConditionTrue,
				Reason:             policyv1.SufficientPodsReason,
				ObservedGeneration: 1,
			},
		},
	}

	testCases := []struct {
		name                                         string
		pdb                                          *policyv1.PodDisruptionBudget
		wantManifestProcessingAvailabilityResultType ManifestProcessingAvailabilityResultType
	}{
		{
			name: "available PDB",
			pdb:  availablePDB,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name: "unavailable PDB (insufficient pods)",
			pdb:  unavailablePDBInsufficientPods,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
		{
			name: "unavailable PDB (stale condition)",
			pdb:  unavailablePDBStaleCondition,
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotResTyp, err := trackPDBAvailability(toUnstructured(t, tc.pdb))
			if err != nil {
				t.Fatalf("trackPDBAvailability() = %v, want no error", err)
			}
			if gotResTyp != tc.wantManifestProcessingAvailabilityResultType {
				t.Errorf("manifestProcessingAvailabilityResultType = %v, want %v", gotResTyp, tc.wantManifestProcessingAvailabilityResultType)
			}
		})
	}
}

// TestTrackInMemberClusterObjAvailabilityByGVR tests the trackInMemberClusterObjAvailabilityByGVR function.
func TestTrackInMemberClusterObjAvailabilityByGVR(t *testing.T) {
	availableDeploy := deploy.DeepCopy()
	availableDeploy.Status = appsv1.DeploymentStatus{
		Replicas:          1,
		AvailableReplicas: 1,
		UpdatedReplicas:   1,
	}

	availableStatefulSet := statefulSetTemplate.DeepCopy()
	availableStatefulSet.Status = appsv1.StatefulSetStatus{
		Replicas:          1,
		AvailableReplicas: 1,
		CurrentReplicas:   1,
		UpdatedReplicas:   1,
		CurrentRevision:   "1",
		UpdateRevision:    "1",
	}

	availableSvc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		Spec: corev1.ServiceSpec{
			Type:       "",
			ClusterIPs: []string{"192.168.1.1"},
		},
	}

	availableDaemonSet := daemonSetTemplate.DeepCopy()
	availableDaemonSet.Status = appsv1.DaemonSetStatus{
		NumberAvailable:        1,
		DesiredNumberScheduled: 1,
		CurrentNumberScheduled: 1,
		UpdatedNumberScheduled: 1,
	}

	availableCRD := crdTemplate.DeepCopy()
	availableCRD.Status = apiextensionsv1.CustomResourceDefinitionStatus{
		Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
			{
				Type:   apiextensionsv1.Established,
				Status: apiextensionsv1.ConditionTrue,
			},
			{
				Type:   apiextensionsv1.NamesAccepted,
				Status: apiextensionsv1.ConditionTrue,
			},
		},
	}

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: nsName,
		},
		Data: map[string]string{
			"key": "value",
		},
	}

	untrackableJob := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
	}

	testCases := []struct {
		name                                         string
		gvr                                          schema.GroupVersionResource
		inMemberClusterObj                           *unstructured.Unstructured
		wantManifestProcessingAvailabilityResultType ManifestProcessingAvailabilityResultType
	}{
		{
			name:               "available deployment",
			gvr:                utils.DeploymentGVR,
			inMemberClusterObj: toUnstructured(t, availableDeploy),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available stateful set",
			gvr:                utils.StatefulSetGVR,
			inMemberClusterObj: toUnstructured(t, availableStatefulSet),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available service",
			gvr:                utils.ServiceGVR,
			inMemberClusterObj: toUnstructured(t, availableSvc),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available daemon set",
			gvr:                utils.DaemonSetGVR,
			inMemberClusterObj: toUnstructured(t, availableDaemonSet),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available custom resource definition",
			gvr:                utils.CustomResourceDefinitionGVR,
			inMemberClusterObj: toUnstructured(t, availableCRD),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "data object (namespace)",
			gvr:                utils.NamespaceGVR,
			inMemberClusterObj: toUnstructured(t, ns.DeepCopy()),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "data object (config map)",
			gvr:                utils.ConfigMapGVR,
			inMemberClusterObj: toUnstructured(t, cm),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "untrackable object (job)",
			gvr:                utils.JobGVR,
			inMemberClusterObj: toUnstructured(t, untrackableJob),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotTrackable,
		},
		{
			name:               "available service account",
			gvr:                utils.ServiceAccountGVR,
			inMemberClusterObj: toUnstructured(t, &corev1.ServiceAccount{}),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available network policy",
			gvr:                utils.NetworkPolicyGVR,
			inMemberClusterObj: toUnstructured(t, &networkingv1.NetworkPolicy{}),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available csi driver",
			gvr:                utils.CSIDriverGVR,
			inMemberClusterObj: toUnstructured(t, &storagev1.CSIDriver{}),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available csi node",
			gvr:                utils.CSINodeGVR,
			inMemberClusterObj: toUnstructured(t, &storagev1.CSINode{}),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available storage class",
			gvr:                utils.StorageClassGVR,
			inMemberClusterObj: toUnstructured(t, &storagev1.StorageClass{}),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available csi storage capacity",
			gvr:                utils.CSIStorageCapacityGVR,
			inMemberClusterObj: toUnstructured(t, &storagev1.CSIStorageCapacity{}),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available controller revision",
			gvr:                utils.ControllerRevisionGVR,
			inMemberClusterObj: toUnstructured(t, &appsv1.ControllerRevision{}),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available ingress class",
			gvr:                utils.IngressClassGVR,
			inMemberClusterObj: toUnstructured(t, &networkingv1.IngressClass{}),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available limit range",
			gvr:                utils.LimitRangeGVR,
			inMemberClusterObj: toUnstructured(t, &corev1.LimitRange{}),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available resource quota",
			gvr:                utils.ResourceQuotaGVR,
			inMemberClusterObj: toUnstructured(t, &corev1.ResourceQuota{}),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
		{
			name:               "available priority class",
			gvr:                utils.PriorityClassGVR,
			inMemberClusterObj: toUnstructured(t, &schedulingv1.PriorityClass{}),
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotResTyp, err := trackInMemberClusterObjAvailabilityByGVR(&tc.gvr, tc.inMemberClusterObj)
			if err != nil {
				t.Fatalf("trackInMemberClusterObjAvailabilityByGVR() = %v, want no error", err)
			}
			if gotResTyp != tc.wantManifestProcessingAvailabilityResultType {
				t.Errorf("manifestProcessingAvailabilityResultType = %v, want %v", gotResTyp, tc.wantManifestProcessingAvailabilityResultType)
			}
		})
	}
}

func TestServiceExportAvailability(t *testing.T) {
	svcExportTemplate := &fleetnetworkingv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-svcExport",
			Namespace:   nsName,
			Annotations: map[string]string{},
			Generation:  3,
		},
	}

	testCases := []struct {
		name                                         string
		weight                                       string
		status                                       fleetnetworkingv1alpha1.ServiceExportStatus
		wantManifestProcessingAvailabilityResultType ManifestProcessingAvailabilityResultType
		err                                          error
	}{
		{
			name:   "available svcExport (annotation weight is 0)",
			weight: "0",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionTrue,
						Reason:             "ServiceIsValid",
						ObservedGeneration: 3,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
			err: nil,
		},
		{
			name:   "unavailable svcExport (ServiceExportValid is false)",
			weight: "0",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionFalse,
						Reason:             "ServiceNotFound",
						ObservedGeneration: 3,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			err: nil,
		},
		{
			name:   "unavailable svcExport (different generation, annotation weight is 0)",
			weight: "0",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionTrue,
						Reason:             "ServiceIsValid",
						ObservedGeneration: 2,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			err: nil,
		},
		{
			name:   "available svcExport with no conflict (annotation weight is 1)",
			weight: "1",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionTrue,
						Reason:             "ServiceIsValid",
						ObservedGeneration: 3,
					},
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportConflict),
						Status:             metav1.ConditionFalse,
						Reason:             "NoConflictFound",
						ObservedGeneration: 3,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
			err: nil,
		},
		{
			name:   "unavailable svcExport with conflict (annotation weight is 1)",
			weight: "1",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionTrue,
						Reason:             "ServiceIsValid",
						ObservedGeneration: 3,
					},
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportConflict),
						Status:             metav1.ConditionTrue,
						Reason:             "ConflictFound",
						ObservedGeneration: 3,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			err: nil,
		},
		{
			name:   "unavailable invalid svcExport (annotation weight is 1)",
			weight: "1",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionFalse,
						Reason:             "ServiceIneligible",
						ObservedGeneration: 3,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			err: nil,
		},
		{
			name:   "unavailable svcExport (different generation, annotation weight is 1)",
			weight: "1",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionTrue,
						Reason:             "ServiceIsValid",
						ObservedGeneration: 3,
					},
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportConflict),
						Status:             metav1.ConditionTrue,
						Reason:             "ConflictFound",
						ObservedGeneration: 2,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			err: nil,
		},
		{
			name:   "unavailable svcExport (no annotation weight, no conflict condition)",
			weight: "",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionTrue,
						Reason:             "ServiceIsValid",
						ObservedGeneration: 3,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			err: nil,
		},
		{
			name:   "available svcExport (no annotation weight)",
			weight: "",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionTrue,
						Reason:             "ServiceIsValid",
						ObservedGeneration: 3,
					},
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportConflict),
						Status:             metav1.ConditionFalse,
						Reason:             "NoConflictFound",
						ObservedGeneration: 3,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeAvailable,
			err: nil,
		},
		{
			name:   "unavailable svcExport (no annotation weight with conflict)",
			weight: "",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionTrue,
						Reason:             "ServiceIsValid",
						ObservedGeneration: 3,
					},
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportConflict),
						Status:             metav1.ConditionTrue,
						Reason:             "ConflictFound",
						ObservedGeneration: 3,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			err: nil,
		},
		{
			name:   "unavailable svcExport (no annotation weight, different generation)",
			weight: "",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionTrue,
						Reason:             "ServiceIsValid",
						ObservedGeneration: 3,
					},
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportConflict),
						Status:             metav1.ConditionFalse,
						Reason:             "NoConflictFound",
						ObservedGeneration: 2,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			err: nil,
		},
		{
			name:   "unavailable svcExport (no annotation weight, invalid service export)",
			weight: "",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionFalse,
						Reason:             "ServiceIsNotValid",
						ObservedGeneration: 3,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			err: nil,
		},
		{
			name:   "unavailable svcExport (invalid weight)",
			weight: "a",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionTrue,
						Reason:             "ServiceIsValid",
						ObservedGeneration: 3,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			err: fmt.Errorf("the weight annotation is not a valid integer: a"),
		},
		{
			name:   "unavailable svcExport (out of range weight)",
			weight: "1002",
			status: fleetnetworkingv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetnetworkingv1alpha1.ServiceExportValid),
						Status:             metav1.ConditionTrue,
						Reason:             "ServiceIsValid",
						ObservedGeneration: 3,
					},
				},
			},
			wantManifestProcessingAvailabilityResultType: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
			err: fmt.Errorf("the weight annotation is not in the range [0, 1000]: 1002"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svcExport := svcExportTemplate.DeepCopy()
			if tc.weight != "" {
				svcExport.Annotations[objectmeta.ServiceExportAnnotationWeight] = tc.weight
			}
			svcExport.Status = tc.status
			gotResTyp, err := trackServiceExportAvailability(toUnstructured(t, svcExport))

			// Check for errors
			if err != nil {
				if tc.err == nil || err.Error() != tc.err.Error() {
					t.Fatalf("trackServiceExportAvailability() = %v, want %v", err, tc.err)
				}
			} else if tc.err != nil {
				t.Fatalf("trackServiceExportAvailability() = nil, want error: %v", tc.err)
			}

			// Check the result type
			if gotResTyp != tc.wantManifestProcessingAvailabilityResultType {
				t.Errorf("manifestProcessingAvailabilityResultType = %v, want %v", gotResTyp, tc.wantManifestProcessingAvailabilityResultType)
			}
		})
	}
}

// TestTrackInMemberClusterObjAvailability tests the trackInMemberClusterObjAvailability method.
func TestTrackInMemberClusterObjAvailability(t *testing.T) {
	ctx := context.Background()
	workRef := klog.KRef(memberReservedNSName, workName)

	availableDeploy := deploy.DeepCopy()
	availableDeploy.Status = appsv1.DeploymentStatus{
		Replicas:          1,
		AvailableReplicas: 1,
		UpdatedReplicas:   1,
	}

	unavailableDaemonSet := daemonSetTemplate.DeepCopy()
	unavailableDaemonSet.Status = appsv1.DaemonSetStatus{
		NumberAvailable:        1,
		DesiredNumberScheduled: 1,
		CurrentNumberScheduled: 1,
		UpdatedNumberScheduled: 2,
	}

	untrackableJob := &batchv1.Job{}

	testCases := []struct {
		name        string
		bundles     []*manifestProcessingBundle
		wantBundles []*manifestProcessingBundle
	}{
		{
			name: "mixed",
			bundles: []*manifestProcessingBundle{
				// The IDs are set purely for the purpose of sorting the results.

				// An available deployment.
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 0,
					},
					gvr:                &utils.DeploymentGVR,
					inMemberClusterObj: toUnstructured(t, availableDeploy),
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
				},
				// A failed to get applied service.
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					gvr:                &utils.ServiceGVR,
					inMemberClusterObj: nil,
					applyResTyp:        ManifestProcessingApplyResultTypeFailedToApply,
				},
				// An unavailable daemon set.
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 2,
					},
					gvr:                &utils.DaemonSetGVR,
					inMemberClusterObj: toUnstructured(t, unavailableDaemonSet),
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
				},
				// An untrackable job.
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 3,
					},
					gvr:                &utils.JobGVR,
					inMemberClusterObj: toUnstructured(t, untrackableJob),
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
				},
			},
			wantBundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 0,
					},
					gvr:                &utils.DeploymentGVR,
					inMemberClusterObj: toUnstructured(t, availableDeploy),
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeAvailable,
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					gvr:                &utils.ServiceGVR,
					inMemberClusterObj: nil,
					applyResTyp:        ManifestProcessingApplyResultTypeFailedToApply,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeSkipped,
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 2,
					},
					gvr:                &utils.DaemonSetGVR,
					inMemberClusterObj: toUnstructured(t, unavailableDaemonSet),
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 3,
					},
					gvr:                &utils.JobGVR,
					inMemberClusterObj: toUnstructured(t, untrackableJob),
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeNotTrackable,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Reconciler{
				parallelizer: parallelizer.NewParallelizer(2),
			}

			r.trackInMemberClusterObjAvailability(ctx, tc.bundles, workRef)

			// A special less func to sort the bundles by their ordinal.
			lessFuncManifestProcessingBundle := func(i, j *manifestProcessingBundle) bool {
				return i.id.Ordinal < j.id.Ordinal
			}
			if diff := cmp.Diff(
				tc.bundles, tc.wantBundles,
				cmp.AllowUnexported(manifestProcessingBundle{}),
				cmpopts.SortSlices(lessFuncManifestProcessingBundle),
			); diff != "" {
				t.Errorf("bundles mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}
