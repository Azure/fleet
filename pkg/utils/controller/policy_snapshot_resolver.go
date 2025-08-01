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
)

// DeletePolicySnapshots deletes all the policy snapshots owned by the placement.
// For cluster-scoped placements (ClusterResourcePlacement), it deletes ClusterSchedulingPolicySnapshots.
// For namespaced placements (ResourcePlacement), it deletes SchedulingPolicySnapshots.
func DeletePolicySnapshots(ctx context.Context, k8Client client.Client, placementObj fleetv1beta1.PlacementObj) error {
	placementKObj := klog.KObj(placementObj)
	var policySnapshotObj fleetv1beta1.PolicySnapshotObj
	deleteOptions := []client.DeleteAllOfOption{
		client.MatchingLabels{fleetv1beta1.PlacementTrackingLabel: placementObj.GetName()},
	}
	// Set up the appropriate snapshot type and delete options based on placement scope
	if placementObj.GetNamespace() != "" {
		// This is a namespaced ResourcePlacement - delete SchedulingPolicySnapshots
		policySnapshotObj = &fleetv1beta1.SchedulingPolicySnapshot{}
		deleteOptions = append(deleteOptions, client.InNamespace(placementObj.GetNamespace()))
	} else {
		// This is a cluster-scoped ClusterResourcePlacement - delete ClusterSchedulingPolicySnapshots
		policySnapshotObj = &fleetv1beta1.ClusterSchedulingPolicySnapshot{}
	}
	policySnapshotKObj := klog.KObj(policySnapshotObj)

	// Perform the delete operation
	if err := k8Client.DeleteAllOf(ctx, policySnapshotObj, deleteOptions...); err != nil {
		klog.ErrorS(err, "Failed to delete policy snapshots", "policySnapshot", policySnapshotKObj, "placement", placementKObj)
		return NewAPIServerError(false, err)
	}

	klog.V(2).InfoS("Deleted policy snapshots", "policySnapshot", policySnapshotKObj, "placement", placementKObj)
	return nil
}

// BuildPolicySnapshot builds and returns a policy snapshot for the given placement, policy snapshot index, and policy hash.
// For cluster-scoped placements, it returns a ClusterSchedulingPolicySnapshot.
// For namespaced placements, it returns a SchedulingPolicySnapshot.
func BuildPolicySnapshot(placementObj fleetv1beta1.PlacementObj, policySnapshotIndex int, policyHash string) fleetv1beta1.PolicySnapshotObj {
	var snapshot fleetv1beta1.PolicySnapshotObj
	labels := map[string]string{
		fleetv1beta1.PlacementTrackingLabel: placementObj.GetName(),
		fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
		fleetv1beta1.PolicyIndexLabel:       strconv.Itoa(policySnapshotIndex),
	}
	annotations := map[string]string{
		fleetv1beta1.CRPGenerationAnnotation: strconv.FormatInt(placementObj.GetGeneration(), 10),
	}
	// Add NumberOfClusters annotation if placement is selectN type
	if spec := placementObj.GetPlacementSpec(); spec.Policy != nil &&
		spec.Policy.PlacementType == fleetv1beta1.PickNPlacementType &&
		spec.Policy.NumberOfClusters != nil {
		annotations[fleetv1beta1.NumberOfClustersAnnotation] = strconv.Itoa(int(*spec.Policy.NumberOfClusters))
	}

	spec := fleetv1beta1.SchedulingPolicySnapshotSpec{
		Policy:     placementObj.GetPlacementSpec().Policy,
		PolicyHash: []byte(policyHash),
	}
	// Set the name following the convention: {PlacementName}-{index}
	name := fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, placementObj.GetName(), policySnapshotIndex)
	if placementObj.GetNamespace() != "" {
		// This is a namespaced ResourcePlacement - create SchedulingPolicySnapshot
		snapshot = &fleetv1beta1.SchedulingPolicySnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   placementObj.GetNamespace(),
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: spec,
		}
	} else {
		// This is a cluster-scoped ClusterResourcePlacement - create ClusterSchedulingPolicySnapshot
		snapshot = &fleetv1beta1.ClusterSchedulingPolicySnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: spec,
		}
	}
	return snapshot
}

// FetchLatestPolicySnapshot fetches the latest policy snapshot for a given placement.
// For cluster-scoped placements, it fetches ClusterSchedulingPolicySnapshot.
// For namespaced placements, it fetches SchedulingPolicySnapshot.
func FetchLatestPolicySnapshot(ctx context.Context, k8Client client.Reader, placementKey types.NamespacedName) (fleetv1beta1.PolicySnapshotList, error) {
	namespace := placementKey.Namespace
	name := placementKey.Name

	var policySnapshotList fleetv1beta1.PolicySnapshotList
	var listOptions []client.ListOption
	listOptions = append(listOptions, client.MatchingLabels{
		fleetv1beta1.PlacementTrackingLabel: name,
		fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
	})

	if namespace != "" {
		// This is a namespaced SchedulingPolicySnapshotList
		policySnapshotList = &fleetv1beta1.SchedulingPolicySnapshotList{}
		listOptions = append(listOptions, client.InNamespace(namespace))
	} else {
		// This is a cluster-scoped ClusterSchedulingPolicySnapshotList
		policySnapshotList = &fleetv1beta1.ClusterSchedulingPolicySnapshotList{}
	}

	if err := k8Client.List(ctx, policySnapshotList, listOptions...); err != nil {
		klog.ErrorS(err, "Failed to list the policySnapshots associated with the placement", "placement", placementKey)
		return nil, err
	}
	return policySnapshotList, nil
}

// ListPolicySnapshots lists all policy snapshots associated with a placement key.
// For cluster-scoped placements, it lists ClusterSchedulingPolicySnapshot.
// For namespaced placements, it lists SchedulingPolicySnapshot.
func ListPolicySnapshots(ctx context.Context, k8Client client.Reader, placementKey types.NamespacedName) (fleetv1beta1.PolicySnapshotList, error) {
	namespace := placementKey.Namespace
	name := placementKey.Name

	var policySnapshotList fleetv1beta1.PolicySnapshotList
	var listOptions []client.ListOption
	listOptions = append(listOptions, client.MatchingLabels{
		fleetv1beta1.PlacementTrackingLabel: name,
	})

	if namespace != "" {
		// This is a namespaced SchedulingPolicySnapshotList
		policySnapshotList = &fleetv1beta1.SchedulingPolicySnapshotList{}
		listOptions = append(listOptions, client.InNamespace(namespace))
	} else {
		// This is a cluster-scoped ClusterSchedulingPolicySnapshotList
		policySnapshotList = &fleetv1beta1.ClusterSchedulingPolicySnapshotList{}
	}

	if err := k8Client.List(ctx, policySnapshotList, listOptions...); err != nil {
		klog.ErrorS(err, "Failed to list the policySnapshots associated with the placement", "placement", placementKey)
		return nil, err
	}
	return policySnapshotList, nil
}
