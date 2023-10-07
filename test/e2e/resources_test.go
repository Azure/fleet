/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	workNamespaceNameTemplate = "work-%d"
	appConfigMapNameTemplate  = "app-config-%d"
	crpNameTemplate           = "crp-%d"

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

func workNamespace() corev1.Namespace {
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
