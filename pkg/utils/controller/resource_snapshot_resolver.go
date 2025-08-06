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
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/labels"
)

// FetchAllResourceSnapshotsAlongWithMaster fetches the group of resourceSnapshot or resourceSnapshots using the latest master resourceSnapshot.
func FetchAllResourceSnapshotsAlongWithMaster(ctx context.Context, k8Client client.Reader, placementKey string, masterResourceSnapshot fleetv1beta1.ResourceSnapshotObj) (map[string]fleetv1beta1.ResourceSnapshotObj, error) {
	resourceSnapshots := make(map[string]fleetv1beta1.ResourceSnapshotObj)
	resourceSnapshots[masterResourceSnapshot.GetName()] = masterResourceSnapshot

	// check if there are more snapshot in the same index group
	countAnnotation := masterResourceSnapshot.GetAnnotations()[fleetv1beta1.NumberOfResourceSnapshotsAnnotation]
	snapshotCount, err := strconv.Atoi(countAnnotation)
	if err != nil || snapshotCount < 1 {
		return nil, NewUnexpectedBehaviorError(fmt.Errorf(
			"master resource snapshot %s for placement %s has an invalid snapshot count %d or err %w", masterResourceSnapshot.GetName(), placementKey, snapshotCount, err))
	}

	if snapshotCount > 1 {
		// fetch all the resource snapshot in the same index group
		index, err := labels.ExtractResourceIndexFromResourceSnapshot(masterResourceSnapshot)
		if err != nil {
			klog.ErrorS(err, "Master resource snapshot has invalid resource index", "resourceSnapshot", klog.KObj(masterResourceSnapshot))
			return nil, NewUnexpectedBehaviorError(err)
		}

		// Extract namespace and name from the placement key
		namespace, name, err := ExtractNamespaceNameFromKey(queue.PlacementKey(placementKey))
		if err != nil {
			return nil, err
		}
		var resourceSnapshotList fleetv1beta1.ResourceSnapshotObjList
		if resourceSnapshotList, err = ListAllResourceSnapshotWithAnIndex(ctx, k8Client, strconv.Itoa(index), name, namespace); err != nil {
			klog.ErrorS(err, "Failed to list all the resource snapshot", "placement", placementKey, "resourceSnapshotIndex", index)
			return nil, NewAPIServerError(true, err)
		}
		//insert all the resource snapshot into the map
		items := resourceSnapshotList.GetResourceSnapshotObjs()

		for i := 0; i < len(items); i++ {
			resourceSnapshots[items[i].GetName()] = items[i]
		}
	}

	// check if all the resource snapshots are created since that may take a while but the rollout controller may update the resource binding on master snapshot creation
	if len(resourceSnapshots) != snapshotCount {
		misMatchErr := fmt.Errorf("%w: resource snapshots are still being created for the masterResourceSnapshot %s, total snapshot in the index group = %d, num Of existing snapshot in the group= %d, placement = %s",
			errResourceNotFullyCreated, masterResourceSnapshot.GetName(), snapshotCount, len(resourceSnapshots), placementKey)
		klog.ErrorS(misMatchErr, "Resource snapshot are not ready", "placement", placementKey)
		return nil, NewExpectedBehaviorError(misMatchErr)
	}
	return resourceSnapshots, nil
}

// FetchLatestMasterResourceSnapshot fetches the master ResourceSnapshot for a given placement key.
func FetchLatestMasterResourceSnapshot(ctx context.Context, k8Client client.Reader, placementKey types.NamespacedName) (fleetv1beta1.ResourceSnapshotObj, error) {
	resourceSnapshotList, err := ListLatestResourceSnapshots(ctx, k8Client, placementKey)
	if err != nil {
		return nil, err
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
	// The master is always the first to be created in the resourcegroup so it should be found.
	if masterResourceSnapshot == nil {
		return nil, NewUnexpectedBehaviorError(fmt.Errorf("no masterResourceSnapshot found for the placement %v", placementKey))
	}
	klog.V(2).InfoS("Found the latest associated masterResourceSnapshot", "placement", placementKey, "masterResourceSnapshot", klog.KObj(masterResourceSnapshot))
	return masterResourceSnapshot, nil
}

// ListLatestResourceSnapshots lists the latest resource snapshots associated with a placement key.
// For cluster-scoped placements, it lists ClusterResourceSnapshots.
// For namespaced placements, it lists ResourceSnapshots.
func ListLatestResourceSnapshots(ctx context.Context, k8Client client.Reader, placementKey types.NamespacedName) (fleetv1beta1.ResourceSnapshotObjList, error) {
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
	return resourceSnapshotList, nil
}

// ListAllResourceSnapshots lists all resource snapshots associated with a placement key (not just the latest ones).
// This is useful for sorting and cleanup operations that need to process all snapshots.
func ListAllResourceSnapshots(ctx context.Context, k8Client client.Reader, placementKey types.NamespacedName) (fleetv1beta1.ResourceSnapshotObjList, error) {
	// Extract namespace and name from the placement key
	namespace := placementKey.Namespace
	name := placementKey.Name
	var resourceSnapshotList fleetv1beta1.ResourceSnapshotObjList
	var listOptions []client.ListOption
	listOptions = append(listOptions, client.MatchingLabels{
		fleetv1beta1.PlacementTrackingLabel: name,
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
		klog.ErrorS(err, "Failed to list all resourceSnapshots associated with the placement", "placement", placementKey)
		return nil, NewAPIServerError(true, err)
	}
	return resourceSnapshotList, nil
}

// ListAllResourceSnapshotWithAnIndex lists all the resourceSnapshots associated with a placement key and a resourceSnapshotIndex.
// For cluster-scoped placements, it lists ClusterResourceSnapshot.
// For namespaced placements, it lists ResourceSnapshot.
// It returns a ResourceSnapshotObjList which contains all the resourceSnapshots in the same index group
func ListAllResourceSnapshotWithAnIndex(ctx context.Context, k8Client client.Reader, resourceSnapshotIndex, placementName, placementNamespace string) (fleetv1beta1.ResourceSnapshotObjList, error) {
	var resourceSnapshotList fleetv1beta1.ResourceSnapshotObjList
	var listOptions []client.ListOption
	listOptions = append(listOptions, client.MatchingLabels{
		fleetv1beta1.PlacementTrackingLabel: placementName,
		fleetv1beta1.ResourceIndexLabel:     resourceSnapshotIndex,
	})
	// Check if the key contains a namespace separator
	if placementNamespace != "" {
		// This is a namespaced ResourceSnapshotList
		resourceSnapshotList = &fleetv1beta1.ResourceSnapshotList{}
		listOptions = append(listOptions, client.InNamespace(placementNamespace))
	} else {
		resourceSnapshotList = &fleetv1beta1.ClusterResourceSnapshotList{}
	}
	if err := k8Client.List(ctx, resourceSnapshotList, listOptions...); err != nil {
		klog.ErrorS(err, "Failed to list the resourceSnapshots associated with the placement for the given index",
			"resourceSnapshotIndex", resourceSnapshotIndex, "placementName", placementName, "placementNamespace", placementNamespace)
		return nil, NewAPIServerError(true, err)
	}

	return resourceSnapshotList, nil
}

// DeleteResourceSnapshots deletes all the resource snapshots owned by the placement.
// For cluster-scoped placements (ClusterResourcePlacement), it deletes ClusterResourceSnapshots.
// For namespaced placements (ResourcePlacement), it deletes ResourceSnapshots.
func DeleteResourceSnapshots(ctx context.Context, k8Client client.Client, placementObj fleetv1beta1.PlacementObj) error {
	placementKObj := klog.KObj(placementObj)
	var resourceSnapshotObj fleetv1beta1.ResourceSnapshotObj
	deleteOptions := []client.DeleteAllOfOption{
		client.MatchingLabels{fleetv1beta1.PlacementTrackingLabel: placementObj.GetName()},
	}
	// Set up the appropriate snapshot type and delete options based on placement scope
	if placementObj.GetNamespace() != "" {
		// This is a namespaced ResourcePlacement - delete ResourceSnapshots
		resourceSnapshotObj = &fleetv1beta1.ResourceSnapshot{}
		deleteOptions = append(deleteOptions, client.InNamespace(placementObj.GetNamespace()))
	} else {
		// This is a cluster-scoped ClusterResourcePlacement - delete ClusterResourceSnapshots
		resourceSnapshotObj = &fleetv1beta1.ClusterResourceSnapshot{}
	}
	resourceSnapshotKObj := klog.KObj(resourceSnapshotObj)

	// Perform the delete operation
	if err := k8Client.DeleteAllOf(ctx, resourceSnapshotObj, deleteOptions...); err != nil {
		klog.ErrorS(err, "Failed to delete resourceSnapshots", "resourceSnapshot", resourceSnapshotKObj, "placement", placementKObj)
		return NewAPIServerError(false, err)
	}

	klog.V(2).InfoS("Deleted resourceSnapshots", "resourceSnapshot", resourceSnapshotKObj, "placement", placementKObj)
	return nil
}

// BuildMasterResourceSnapshot builds and returns the master resource snapshot for the latest resource snapshot index and selected resources.
// If the placement is namespace-scoped, it creates a namespace-scoped ResourceSnapshot; otherwise, it creates a cluster-scoped ClusterResourceSnapshot.
func BuildMasterResourceSnapshot(placementObj fleetv1beta1.PlacementObj, latestResourceSnapshotIndex, resourceSnapshotCount, envelopeObjCount int, resourceHash string, selectedResources []fleetv1beta1.ResourceContent) fleetv1beta1.ResourceSnapshotObj {
	labels := map[string]string{
		fleetv1beta1.PlacementTrackingLabel: placementObj.GetName(),
		fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
		fleetv1beta1.ResourceIndexLabel:     strconv.Itoa(latestResourceSnapshotIndex),
	}
	annotations := map[string]string{
		fleetv1beta1.ResourceGroupHashAnnotation:         resourceHash,
		fleetv1beta1.NumberOfResourceSnapshotsAnnotation: strconv.Itoa(resourceSnapshotCount),
		fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  strconv.Itoa(envelopeObjCount),
	}
	spec := fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources: selectedResources,
	}

	// If namespace is provided, create a namespace-scoped ResourceSnapshot
	if placementObj.GetNamespace() != "" {
		return &fleetv1beta1.ResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, placementObj.GetName(), latestResourceSnapshotIndex),
				Namespace:   placementObj.GetNamespace(),
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: spec,
		}
	}

	// Otherwise, create a cluster-scoped ClusterResourceSnapshot
	return &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, placementObj.GetName(), latestResourceSnapshotIndex),
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: spec,
	}
}
