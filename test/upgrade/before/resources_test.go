/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package before

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	workNamespaceLabelName = "target-test-spec"
)

func workResourceSelector(workNamespaceName string) []placementv1beta1.ClusterResourceSelector {
	return []placementv1beta1.ClusterResourceSelector{
		{
			Group:   "",
			Kind:    "Namespace",
			Version: "v1",
			Name:    workNamespaceName,
		},
	}
}

func appNamespace(workNamespaceName string, crpName string) corev1.Namespace {
	return corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: workNamespaceName,
			Labels: map[string]string{
				workNamespaceLabelName: crpName,
			},
		},
	}
}

func appConfigMap(workNamespaceName, appConfigMapName string) corev1.ConfigMap {
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

func workResourceIdentifiers(workNamespaceName, appConfigMapName string) []placementv1beta1.ResourceIdentifier {
	return []placementv1beta1.ResourceIdentifier{
		{
			Kind:    "Namespace",
			Name:    workNamespaceName,
			Version: "v1",
		},
		{
			Kind:      "ConfigMap",
			Name:      appConfigMapName,
			Version:   "v1",
			Namespace: workNamespaceName,
		},
	}
}
