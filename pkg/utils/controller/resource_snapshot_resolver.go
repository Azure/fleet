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
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// FetchLatestMasterResourceSnapshot fetches the master ResourceSnapshot for a given placement key.
func FetchLatestMasterResourceSnapshot(ctx context.Context, k8Client client.Reader, placementKey types.NamespacedName) (fleetv1beta1.ResourceSnapshotObj, error) {
	// Extract namespace and name from the placement key
	namespace := placementKey.Namespace
	name := placementKey.Name
	var resourceSnapshotList fleetv1beta1.ResourceSnapshotObjList
	var listOptions []client.ListOption
	listOptions = append(listOptions, client.MatchingLabels{
		fleetv1beta1.PlacementTrackingLabel: name,
		fleetv1beta1.IsLatestSnapshotLabel:  "true",
	})
	// Check if the key contains a namespace separator
	if namespace != "" {
		// This is a namespaced ResourceSnapshotList
		resourceSnapshotList = &fleetv1beta1.ResourceSnapshotList{}
		listOptions = append(listOptions, client.InNamespace(namespace))
	} else {
		resourceSnapshotList = &fleetv1beta1.ClusterResourceSnapshotList{}
	}
	if err := k8Client.List(ctx, resourceSnapshotList, listOptions...); err != nil {
		klog.ErrorS(err, "Failed to list the resourceSnapshots associated with the placement", "placement", placementKey)
		return nil, NewAPIServerError(true, err)
	}
	items := resourceSnapshotList.GetResourceSnapshotObjs()
	if len(items) == 0 {
		klog.V(2).InfoS("No resourceSnapshots found for the placement", "placement", placementKey)
		return nil, nil
	}

	// Look for the master resourceSnapshot.
	var masterResourceSnapshot fleetv1beta1.ResourceSnapshotObj
	for i, resourceSnapshot := range items {
		anno := resourceSnapshot.GetAnnotations()
		// only master has this annotation
		if len(anno[fleetv1beta1.ResourceGroupHashAnnotation]) != 0 {
			masterResourceSnapshot = items[i]
			break
		}
	}
	// It is possible that no master resourceSnapshot is found, e.g., when the new resourceSnapshot is created but not yet marked as the latest.
	if masterResourceSnapshot == nil {
		return nil, fmt.Errorf("no masterResourceSnapshot found for the placement %v", placementKey)
	}
	klog.V(2).InfoS("Found the latest associated masterResourceSnapshot", "placement", placementKey, "masterResourceSnapshot", klog.KObj(masterResourceSnapshot))
	return masterResourceSnapshot, nil
}
