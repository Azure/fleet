/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	workNamespaceName = "work"
	appConfigMapName  = "app-config"
	crpName           = "test-crp"

	customDeletionBlockerFinalizer = "custom-deletion-blocker-finalizer"
)

var (
	workResourceSelector = []placementv1beta1.ClusterResourceSelector{
		{
			Group:   "",
			Kind:    "Namespace",
			Version: "v1",
			Name:    "work",
		},
	}
)

func workNamespace() corev1.Namespace {
	return corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: workNamespaceName,
		},
	}
}

func appConfigMap() corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appConfigMapName,
			Namespace: workNamespaceName,
		},
		Data: map[string]string{
			"data": "test",
		},
	}
}
