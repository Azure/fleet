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
