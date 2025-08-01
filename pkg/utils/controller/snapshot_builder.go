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

package controller

import (
	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// SplitSelectedResources splits selected resources into separate lists
// so that the total size of each split list of selected Resources is within the size limit.
func SplitSelectedResources(selectedResources []fleetv1beta1.ResourceContent, snapshotSizeLimit int) [][]fleetv1beta1.ResourceContent {
	var selectedResourcesList [][]fleetv1beta1.ResourceContent
	i := 0
	for i < len(selectedResources) {
		j := i
		currentSize := 0
		var snapshotResources []fleetv1beta1.ResourceContent
		for j < len(selectedResources) {
			currentSize += len(selectedResources[j].Raw)
			if currentSize > snapshotSizeLimit {
				break
			}
			snapshotResources = append(snapshotResources, selectedResources[j])
			j++
		}
		// Any selected resource will always be less than 1.5MB since that's the ETCD limit. In this case an individual
		// selected resource crosses the size limit.
		if len(snapshotResources) == 0 && len(selectedResources[j].Raw) > snapshotSizeLimit {
			snapshotResources = append(snapshotResources, selectedResources[j])
			j++
		}
		selectedResourcesList = append(selectedResourcesList, snapshotResources)
		i = j
	}
	return selectedResourcesList
}
