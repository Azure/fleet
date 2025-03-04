/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package after

import (
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

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
