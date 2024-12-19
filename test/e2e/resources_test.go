/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	workNamespaceNameTemplate         = "application-%d"
	appConfigMapNameTemplate          = "app-config-%d"
	appSecretNameTemplate             = "app-secret-%d" // #nosec G101
	crpNameTemplate                   = "crp-%d"
	crpNameWithSubIndexTemplate       = "crp-%d-%d"
	croNameTemplate                   = "cro-%d"
	roNameTemplate                    = "ro-%d"
	mcNameTemplate                    = "mc-%d"
	internalServiceExportNameTemplate = "ise-%d"
	internalServiceImportNameTemplate = "isi-%d"
	endpointSliceExportNameTemplate   = "ep-%d"
	crpEvictionNameTemplate           = "crpe-%d"

	customDeletionBlockerFinalizer = "custom-deletion-blocker-finalizer"
	workNamespaceLabelName         = "process"
)

func workResourceSelector() []placementv1beta1.ClusterResourceSelector {
	return []placementv1beta1.ClusterResourceSelector{
		{
			Group:   "",
			Kind:    "Namespace",
			Version: "v1",
			Name:    fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
		},
	}
}

func configMapSelector() []placementv1alpha1.ResourceSelector {
	return []placementv1alpha1.ResourceSelector{
		{
			Group:   "",
			Kind:    "ConfigMap",
			Version: "v1",
			Name:    fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess()),
		},
	}
}

func invalidWorkResourceSelector() []placementv1beta1.ClusterResourceSelector {
	return []placementv1beta1.ClusterResourceSelector{
		{
			Group:   "",
			Kind:    "Namespace",
			Version: "v1",
			Name:    fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"test-key": "test-value"},
			},
		},
	}
}

func appNamespace() corev1.Namespace {
	return corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
			Labels: map[string]string{
				workNamespaceLabelName: strconv.Itoa(GinkgoParallelProcess()),
			},
		},
	}
}

func appConfigMap() corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess()),
			Namespace: fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess()),
		},
		Data: map[string]string{
			"data": "test",
		},
	}
}

func internalServiceExport(name, namespace string) fleetnetworkingv1alpha1.InternalServiceExport {
	return fleetnetworkingv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: fleetnetworkingv1alpha1.InternalServiceExportSpec{
			Ports: []fleetnetworkingv1alpha1.ServicePort{
				{
					Protocol: corev1.ProtocolTCP,
					Port:     4848,
				},
			},
			ServiceReference: fleetnetworkingv1alpha1.ExportedObjectReference{
				NamespacedName:  "test-svc",
				ResourceVersion: "test-resource-version",
				ClusterID:       "member-1",
				ExportedSince:   metav1.NewTime(time.Now().Round(time.Second)),
			},
		},
	}
}

func internalServiceImport(name, namespace string) fleetnetworkingv1alpha1.InternalServiceImport {
	return fleetnetworkingv1alpha1.InternalServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: fleetnetworkingv1alpha1.InternalServiceImportSpec{
			ServiceImportReference: fleetnetworkingv1alpha1.ExportedObjectReference{
				ClusterID:       "test-cluster-id",
				Kind:            "test-kind",
				Namespace:       "test-ns",
				Name:            "test-name",
				ResourceVersion: "1",
				Generation:      1,
				UID:             "test-id",
				NamespacedName:  "test-ns/test-name",
			},
		},
	}
}

func endpointSliceExport(name, namespace string) fleetnetworkingv1alpha1.EndpointSliceExport {
	protocol := corev1.ProtocolTCP
	return fleetnetworkingv1alpha1.EndpointSliceExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: fleetnetworkingv1alpha1.EndpointSliceExportSpec{
			AddressType: "IPv4",
			Endpoints: []fleetnetworkingv1alpha1.Endpoint{
				{
					Addresses: []string{"test-address-1"},
				},
			},
			Ports: []discoveryv1.EndpointPort{
				{
					Name:     ptr.To("http"),
					Protocol: &protocol,
					Port:     ptr.To(int32(80)),
				},
			},
			EndpointSliceReference: fleetnetworkingv1alpha1.ExportedObjectReference{
				ClusterID:       "test-cluster-id",
				Kind:            "test-kind",
				Namespace:       "test-ns",
				Name:            "test-name",
				ResourceVersion: "1",
				Generation:      1,
				UID:             "test-id",
				NamespacedName:  "test-ns/test-name",
			},
			OwnerServiceReference: fleetnetworkingv1alpha1.OwnerServiceReference{
				Namespace:      "test-ns",
				Name:           "test-name",
				NamespacedName: "test-ns/test-name",
			},
		},
	}
}
