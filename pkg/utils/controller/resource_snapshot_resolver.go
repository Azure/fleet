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
	"sort"
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/queue"
	"go.goms.io/fleet/pkg/utils/annotations"
	"go.goms.io/fleet/pkg/utils/labels"
	"go.goms.io/fleet/pkg/utils/resource"
	fleettime "go.goms.io/fleet/pkg/utils/time"
)

// The max size of an object in k8s is 1.5MB because of ETCD limit https://etcd.io/docs/v3.3/dev-guide/limit/.
// We choose 800KB as the soft limit for all the selected resources within one resourceSnapshot object because of this test in k8s which checks
// if object size is greater than 1MB https://github.com/kubernetes/kubernetes/blob/db1990f48b92d603f469c1c89e2ad36da1b74846/test/integration/master/synthetic_master_test.go#L337
var resourceSnapshotResourceSizeLimit = 800 * (1 << 10) // 800KB

type ResourceSnapshotResolver struct {
	Client client.Client
	Scheme *runtime.Scheme

	// Config provides configuration functions for snapshot behavior.
	// If nil, default behavior (no timing restrictions) is used.
	Config *ResourceSnapshotConfig
}

// NewResourceSnapshotResolver creates a new ResourceSnapshotResolver with the universal fields
func NewResourceSnapshotResolver(client client.Client, scheme *runtime.Scheme) *ResourceSnapshotResolver {
	return &ResourceSnapshotResolver{
		Client: client,
		Scheme: scheme,
	}
}

// ResourceSnapshotConfig defines timing parameters for resource snapshot management.
type ResourceSnapshotConfig struct {
	// ResourceSnapshotCreationMinimumInterval is the minimum interval to create a new resourcesnapshot
	// to avoid too frequent updates.
	ResourceSnapshotCreationMinimumInterval time.Duration

	// ResourceChangesCollectionDuration is the duration for collecting resource changes into one snapshot.
	ResourceChangesCollectionDuration time.Duration
}

// NewResourceSnapshotConfig creates a ResourceSnapshotConfig with static values
func NewResourceSnapshotConfig(creationInterval, collectionDuration time.Duration) *ResourceSnapshotConfig {
	return &ResourceSnapshotConfig{
		ResourceSnapshotCreationMinimumInterval: creationInterval,
		ResourceChangesCollectionDuration:       collectionDuration,
	}
}

// GetOrCreateResourceSnapshot gets or creates a resource snapshot for the given placement.
// It returns the latest resource snapshot if it exists and is up to date, otherwise it creates a new one.
// It also returns the ctrl.Result to indicate whether the request should be requeued or not.
// Note: when the ctrl.Result.Requeue is true, it still returns the current latest resourceSnapshot so that
// placement can update the rollout status.
func (r *ResourceSnapshotResolver) GetOrCreateResourceSnapshot(ctx context.Context, placement fleetv1beta1.PlacementObj, envelopeObjCount int, resourceSnapshotSpec *fleetv1beta1.ResourceSnapshotSpec, revisionHistoryLimit int) (ctrl.Result, fleetv1beta1.ResourceSnapshotObj, error) {
	placementKObj := klog.KObj(placement)
	resourceHash, err := resource.HashOf(resourceSnapshotSpec)
	if err != nil {
		klog.ErrorS(err, "Failed to generate resource hash", "placement", placementKObj)
		return ctrl.Result{}, nil, NewUnexpectedBehaviorError(err)
	}

	// latestResourceSnapshotIndex should be -1 when there is no snapshot.
	latestResourceSnapshot, latestResourceSnapshotIndex, err := r.lookupLatestResourceSnapshot(ctx, placement)
	if err != nil {
		return ctrl.Result{}, nil, err
	}

	latestResourceSnapshotHash := ""
	numberOfSnapshots := -1
	if latestResourceSnapshot != nil {
		latestResourceSnapshotHash, err = annotations.ParseResourceGroupHashFromAnnotation(latestResourceSnapshot)
		if err != nil {
			klog.ErrorS(err, "Failed to get the ResourceGroupHashAnnotation", "resourceSnapshot", klog.KObj(latestResourceSnapshot))
			return ctrl.Result{}, nil, NewUnexpectedBehaviorError(err)
		}
		numberOfSnapshots, err = annotations.ExtractNumberOfResourceSnapshotsFromResourceSnapshot(latestResourceSnapshot)
		if err != nil {
			klog.ErrorS(err, "Failed to get the NumberOfResourceSnapshotsAnnotation", "resourceSnapshot", klog.KObj(latestResourceSnapshot))
			return ctrl.Result{}, nil, NewUnexpectedBehaviorError(err)
		}
	}

	shouldCreateNewMasterResourceSnapshot := true
	// This index indicates the selected resource in the split selectedResourceList, if this index is zero we start
	// from creating the master resourceSnapshot if it's greater than zero it means that the master resourceSnapshot
	// got created but not all sub-indexed resourceSnapshots have been created yet. It covers the corner case where the
	// controller crashes in the middle.
	resourceSnapshotStartIndex := 0
	if latestResourceSnapshot != nil && latestResourceSnapshotHash == resourceHash {
		if err := r.ensureLatestResourceSnapshot(ctx, latestResourceSnapshot); err != nil {
			return ctrl.Result{}, nil, err
		}
		// check to see all that the master cluster resource snapshot and sub-indexed snapshots belonging to the same group index exists.
		resourceSnapshotList, err := ListAllResourceSnapshotWithAnIndex(ctx, r.Client, latestResourceSnapshot.GetLabels()[fleetv1beta1.ResourceIndexLabel], placement.GetName(), placement.GetNamespace())
		if err != nil {
			klog.ErrorS(err, "Failed to list the latest group resourceSnapshots associated with the placement", "placement", placementKObj)
			return ctrl.Result{}, nil, NewAPIServerError(true, err)
		}
		if len(resourceSnapshotList.GetResourceSnapshotObjs()) == numberOfSnapshots {
			klog.V(2).InfoS("resourceSnapshots have not changed", "placement", placementKObj, "resourceSnapshot", klog.KObj(latestResourceSnapshot))
			return ctrl.Result{}, latestResourceSnapshot, nil
		}
		// we should not create a new master cluster resource snapshot.
		shouldCreateNewMasterResourceSnapshot = false
		// set resourceSnapshotStartIndex to start from this index, so we don't try to recreate existing sub-indexed cluster resource snapshots.
		resourceSnapshotStartIndex = len(resourceSnapshotList.GetResourceSnapshotObjs())
	}

	// Need to create new snapshot when 1) there is no snapshots or 2) the latest snapshot hash != current one.
	// mark the last resource snapshot as inactive if it is different from what we have now or 3) when some
	// sub-indexed cluster resource snapshots belonging to the same group have not been created, the master
	// cluster resource snapshot should exist and be latest.
	if latestResourceSnapshot != nil && latestResourceSnapshotHash != resourceHash && latestResourceSnapshot.GetLabels()[fleetv1beta1.IsLatestSnapshotLabel] == strconv.FormatBool(true) {
		// When the latest resource snapshot without the isLastest label, it means it fails to create the new
		// resource snapshot in the last reconcile and we don't need to check and delay the request.
		res, error := r.shouldCreateNewResourceSnapshotNow(ctx, latestResourceSnapshot)
		if error != nil {
			return ctrl.Result{}, nil, error
		}
		if res.RequeueAfter > 0 {
			// If the latest resource snapshot is not ready to be updated, we requeue the request.
			return res, latestResourceSnapshot, nil
		}
		shouldCreateNewMasterResourceSnapshot = true
		// set the latest label to false first to make sure there is only one or none active resource snapshot
		labels := latestResourceSnapshot.GetLabels()
		labels[fleetv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(false)
		latestResourceSnapshot.SetLabels(labels)
		if err := r.Client.Update(ctx, latestResourceSnapshot); err != nil {
			klog.ErrorS(err, "Failed to set the isLatestSnapshot label to false", "resourceSnapshot", klog.KObj(latestResourceSnapshot))
			return ctrl.Result{}, nil, NewUpdateIgnoreConflictError(err)
		}
		klog.V(2).InfoS("Marked the existing resourceSnapshot as inactive", "placement", placementKObj, "resourceSnapshot", klog.KObj(latestResourceSnapshot))
	}

	// only delete redundant resource snapshots and increment the latest resource snapshot index if new master resource snapshot is to be created.
	if shouldCreateNewMasterResourceSnapshot {
		// delete redundant snapshot revisions before creating a new master resource snapshot to guarantee that the number of snapshots won't exceed the limit.
		if err := r.deleteRedundantResourceSnapshots(ctx, placement, revisionHistoryLimit); err != nil {
			return ctrl.Result{}, nil, err
		}
		latestResourceSnapshotIndex++
	}
	// split selected resources as list of lists.
	selectedResourcesList := SplitSelectedResources(resourceSnapshotSpec.SelectedResources, resourceSnapshotResourceSizeLimit)
	var resourceSnapshot fleetv1beta1.ResourceSnapshotObj
	for i := resourceSnapshotStartIndex; i < len(selectedResourcesList); i++ {
		if i == 0 {
			resourceSnapshot = BuildMasterResourceSnapshot(latestResourceSnapshotIndex, len(selectedResourcesList), envelopeObjCount, placement.GetName(), placement.GetNamespace(), resourceHash, selectedResourcesList[i])
			latestResourceSnapshot = resourceSnapshot
		} else {
			resourceSnapshot = BuildSubIndexResourceSnapshot(latestResourceSnapshotIndex, i-1, placement.GetName(), placement.GetNamespace(), selectedResourcesList[i])
		}
		if err = r.createResourceSnapshot(ctx, placement, resourceSnapshot); err != nil {
			return ctrl.Result{}, nil, err
		}
	}
	// shouldCreateNewMasterResourceSnapshot is used here to be defensive in case of the regression.
	if shouldCreateNewMasterResourceSnapshot && len(selectedResourcesList) == 0 {
		resourceSnapshot = BuildMasterResourceSnapshot(latestResourceSnapshotIndex, 1, envelopeObjCount, placement.GetName(), placement.GetNamespace(), resourceHash, []fleetv1beta1.ResourceContent{})
		latestResourceSnapshot = resourceSnapshot
		if err = r.createResourceSnapshot(ctx, placement, resourceSnapshot); err != nil {
			return ctrl.Result{}, nil, err
		}
	}
	return ctrl.Result{}, latestResourceSnapshot, nil
}

// lookupLatestResourceSnapshot finds the latest snapshots and.
// There will be only one active resource snapshot if exists.
// It first checks whether there is an active resource snapshot.
// If not, it finds the one whose resourceIndex label is the largest.
// The resource index will always start from 0.
// lookupLatestResourceSnapshot finds the latest resource snapshots for the given placement.
// It works with both cluster-scoped (ClusterResourcePlacement) and namespace-scoped (ResourcePlacement) placements.
// There will be only one active resource snapshot if exists.
// It first checks whether there is an active resource snapshot.
// If not, it finds the one whose resourceIndex label is the largest.
// The resource index will always start from 0.
// Return error when 1) cannot list the snapshots 2) there are more than one active resource snapshots 3) snapshot has the
// invalid label value.
// 2 & 3 should never happen.
func (r *ResourceSnapshotResolver) lookupLatestResourceSnapshot(ctx context.Context, placement fleetv1beta1.PlacementObj) (fleetv1beta1.ResourceSnapshotObj, int, error) {
	placementKObj := klog.KObj(placement)

	// Use the existing FetchLatestMasterResourceSnapshot function to get the master snapshot
	masterSnapshot, err := FetchLatestMasterResourceSnapshot(ctx, r.Client, types.NamespacedName{Namespace: placement.GetNamespace(), Name: placement.GetName()})
	if err != nil {
		return nil, -1, err
	}
	if masterSnapshot != nil {
		// Extract resource index from the master snapshot
		resourceIndex, err := labels.ExtractResourceIndexFromResourceSnapshot(masterSnapshot)
		if err != nil {
			klog.ErrorS(err, "Failed to parse the resource index label", "resourceSnapshot", klog.KObj(masterSnapshot))
			return nil, -1, NewUnexpectedBehaviorError(err)
		}
		return masterSnapshot, resourceIndex, nil
	}
	// When there are no active snapshots, find the first snapshot who has the largest resource index.
	// It should be rare only when placement is crashed before creating the new active snapshot.
	sortedList, err := r.listSortedResourceSnapshots(ctx, placement)
	if err != nil {
		return nil, -1, err
	}
	if len(sortedList.GetResourceSnapshotObjs()) == 0 {
		// The resource index of the first snapshot will start from 0.
		return nil, -1, nil
	}
	latestSnapshot := sortedList.GetResourceSnapshotObjs()[len(sortedList.GetResourceSnapshotObjs())-1]
	resourceIndex, err := labels.ExtractResourceIndexFromResourceSnapshot(latestSnapshot)
	if err != nil {
		klog.ErrorS(err, "Failed to parse the resource index label", "placement", placementKObj, "resourceSnapshot", klog.KObj(latestSnapshot))
		return nil, -1, NewUnexpectedBehaviorError(err)
	}
	return latestSnapshot, resourceIndex, nil
}

// listSortedResourceSnapshots returns the resource snapshots sorted by its index and its subindex.
// Now works with both cluster-scoped and namespaced resource snapshots using interface types.
// The resourceSnapshot is less than the other one when resourceIndex is less.
// When the resourceIndex is equal, then order by the subindex.
// Note: the snapshot does not have subindex is the largest of a group and there should be only one in a group.
func (r *ResourceSnapshotResolver) listSortedResourceSnapshots(ctx context.Context, placementObj fleetv1beta1.PlacementObj) (fleetv1beta1.ResourceSnapshotObjList, error) {
	placementKey := types.NamespacedName{
		Namespace: placementObj.GetNamespace(),
		Name:      placementObj.GetName(),
	}

	snapshotList, err := ListAllResourceSnapshots(ctx, r.Client, placementKey)
	if err != nil {
		klog.ErrorS(err, "Failed to list all resourceSnapshots", "placement", klog.KObj(placementObj))
		return nil, NewAPIServerError(true, err)
	}

	items := snapshotList.GetResourceSnapshotObjs()
	var errs []error
	sort.Slice(items, func(i, j int) bool {
		iKObj := klog.KObj(items[i])
		jKObj := klog.KObj(items[j])
		ii, err := labels.ExtractResourceIndexFromResourceSnapshot(items[i])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the resource index label", "placement", klog.KObj(placementObj), "resourceSnapshot", iKObj)
			errs = append(errs, err)
		}
		ji, err := labels.ExtractResourceIndexFromResourceSnapshot(items[j])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the resource index label", "placement", klog.KObj(placementObj), "resourceSnapshot", jKObj)
			errs = append(errs, err)
		}
		if ii != ji {
			return ii < ji
		}

		iDoesExist, iSubindex, err := annotations.ExtractSubindexFromResourceSnapshot(items[i])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the subindex index", "placement", klog.KObj(placementObj), "resourceSnapshot", iKObj)
			errs = append(errs, err)
		}
		jDoesExist, jSubindex, err := annotations.ExtractSubindexFromResourceSnapshot(items[j])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the subindex index", "placement", klog.KObj(placementObj), "resourceSnapshot", jKObj)
			errs = append(errs, err)
		}

		// Both of the snapshots do not have subindex, which should not happen.
		if !iDoesExist && !jDoesExist {
			klog.ErrorS(err, "There are more than one resource snapshot which do not have subindex in a group", "placement", klog.KObj(placementObj), "resourceSnapshot", iKObj, "resourceSnapshot", jKObj)
			errs = append(errs, err)
		}

		if !iDoesExist { // check if it's the first snapshot
			return false
		}
		if !jDoesExist { // check if it's the first snapshot
			return true
		}
		return iSubindex < jSubindex
	})

	if len(errs) > 0 {
		return nil, NewUnexpectedBehaviorError(utilerrors.NewAggregate(errs))
	}

	return snapshotList, nil
}

// ensureLatestResourceSnapshot ensures the latest resourceSnapshot has the isLatest label, working with interface types.
func (r *ResourceSnapshotResolver) ensureLatestResourceSnapshot(ctx context.Context, latest fleetv1beta1.ResourceSnapshotObj) error {
	labels := latest.GetLabels()
	if labels[fleetv1beta1.IsLatestSnapshotLabel] == strconv.FormatBool(true) {
		return nil
	}
	// It could happen when the controller just sets the latest label to false for the old snapshot, and fails to
	// create a new resource snapshot.
	// And then the customers revert back their resource to the old one again.
	// In this case, the "latest" snapshot without isLatest label has the same resource hash as the current one.
	labels[fleetv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(true)
	latest.SetLabels(labels)
	if err := r.Client.Update(ctx, latest); err != nil {
		klog.ErrorS(err, "Failed to update the resourceSnapshot", "resourceSnapshot", klog.KObj(latest))
		return NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("ResourceSnapshot's IsLatestSnapshotLabel was updated to true", "resourceSnapshot", klog.KObj(latest))
	return nil
}

// shouldCreateNewResourceSnapshotNow checks whether it is ready to create the new resource snapshot to avoid too frequent creation
// based on the configured resourceSnapshotCreationMinimumInterval and resourceChangesCollectionDuration.
func (r *ResourceSnapshotResolver) shouldCreateNewResourceSnapshotNow(ctx context.Context, latestResourceSnapshot fleetv1beta1.ResourceSnapshotObj) (ctrl.Result, error) {
	if r.Config != nil && r.Config.ResourceSnapshotCreationMinimumInterval <= 0 && r.Config.ResourceChangesCollectionDuration <= 0 {
		return ctrl.Result{}, nil
	}

	// We respect the ResourceChangesCollectionDuration to allow the controller to bundle all the resource changes into one snapshot.
	snapshotKObj := klog.KObj(latestResourceSnapshot)
	now := time.Now()
	nextResourceSnapshotCandidateDetectionTime, err := annotations.ExtractNextResourceSnapshotCandidateDetectionTimeFromResourceSnapshot(latestResourceSnapshot)
	if nextResourceSnapshotCandidateDetectionTime.IsZero() || err != nil {
		if err != nil {
			klog.ErrorS(NewUnexpectedBehaviorError(err), "Failed to get the NextResourceSnapshotCandidateDetectionTimeAnnotation", "resourceSnapshot", snapshotKObj)
		}
		// If the annotation is not set, set next resource snapshot candidate detection time is now.
		if latestResourceSnapshot.GetAnnotations() == nil {
			latestResourceSnapshot.SetAnnotations(make(map[string]string))
		}
		latestResourceSnapshot.GetAnnotations()[fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation] = now.Format(time.RFC3339)
		if err := r.Client.Update(ctx, latestResourceSnapshot); err != nil {
			klog.ErrorS(err, "Failed to update the NextResourceSnapshotCandidateDetectionTime annotation", "resourceSnapshot", snapshotKObj)
			return ctrl.Result{}, NewUpdateIgnoreConflictError(err)
		}
		nextResourceSnapshotCandidateDetectionTime = now
		klog.V(2).InfoS("Updated the NextResourceSnapshotCandidateDetectionTime annotation", "resourceSnapshot", snapshotKObj, "nextResourceSnapshotCandidateDetectionTimeAnnotation", now.Format(time.RFC3339))
	}
	nextCreationTime := fleettime.MaxTime(nextResourceSnapshotCandidateDetectionTime.Add(r.Config.ResourceChangesCollectionDuration), latestResourceSnapshot.GetCreationTimestamp().Add(r.Config.ResourceSnapshotCreationMinimumInterval))
	if now.Before(nextCreationTime) {
		// If the next resource snapshot creation time is not reached, we requeue the request to avoid too frequent update.
		klog.V(2).InfoS("Delaying the new resourceSnapshot creation",
			"resourceSnapshot", snapshotKObj, "nextCreationTime", nextCreationTime, "latestResourceSnapshotCreationTime", latestResourceSnapshot.GetCreationTimestamp(),
			"resourceSnapshotCreationMinimumInterval", r.Config.ResourceSnapshotCreationMinimumInterval, "resourceChangesCollectionDuration", r.Config.ResourceChangesCollectionDuration,
			"afterDuration", nextCreationTime.Sub(now))
		return ctrl.Result{RequeueAfter: nextCreationTime.Sub(now)}, nil
	}
	return ctrl.Result{}, nil
}

// deleteRedundantResourceSnapshots handles multiple snapshots in a group.
func (r *ResourceSnapshotResolver) deleteRedundantResourceSnapshots(ctx context.Context, placementObj fleetv1beta1.PlacementObj, revisionHistoryLimit int) error {
	sortedList, err := r.listSortedResourceSnapshots(ctx, placementObj)
	if err != nil {
		return err
	}

	items := sortedList.GetResourceSnapshotObjs()
	if len(items) < revisionHistoryLimit {
		// If the number of existing snapshots is less than the limit no matter how many snapshots in a group, we don't
		// need to delete any snapshots.
		// Skip the checking and deleting.
		return nil
	}

	placementKObj := klog.KObj(placementObj)
	lastGroupIndex := -1
	groupCounter := 0

	// delete the snapshots from the end as there are could be multiple snapshots in a group in order to keep the latest
	// snapshots from the end.
	for i := len(items) - 1; i >= 0; i-- {
		snapshotKObj := klog.KObj(items[i])
		ii, err := labels.ExtractResourceIndexFromResourceSnapshot(items[i])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the resource index label", "placement", placementKObj, "resourceSnapshot", snapshotKObj)
			return NewUnexpectedBehaviorError(err)
		}
		if ii != lastGroupIndex {
			groupCounter++
			lastGroupIndex = ii
		}
		if groupCounter < revisionHistoryLimit { // need to reserve one slot for the new snapshot
			// When the number of group is less than the revision limit, skipping deleting the snapshot.
			continue
		}
		if err := r.Client.Delete(ctx, items[i]); err != nil && !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to delete resourceSnapshot", "placement", placementKObj, "resourceSnapshot", snapshotKObj)
			return NewAPIServerError(false, err)
		}
	}
	if groupCounter-revisionHistoryLimit > 0 {
		// We always delete before creating a new snapshot, the snapshot group size should never exceed the limit
		// as there is no finalizer added and the object should be deleted immediately.
		klog.Warning("The number of resourceSnapshot groups exceeds the revisionHistoryLimit and it should never happen", "placement", placementKObj, "numberOfSnapshotGroups", groupCounter, "revisionHistoryLimit", revisionHistoryLimit)
	}
	return nil
}

// createResourceSnapshot sets placement owner reference on the resource snapshot and creates it.
// Now supports both cluster-scoped and namespace-scoped placements using interface types.
func (r *ResourceSnapshotResolver) createResourceSnapshot(ctx context.Context, placementObj fleetv1beta1.PlacementObj, resourceSnapshot fleetv1beta1.ResourceSnapshotObj) error {
	resourceSnapshotKObj := klog.KObj(resourceSnapshot)
	if err := controllerutil.SetControllerReference(placementObj, resourceSnapshot, r.Scheme); err != nil {
		klog.ErrorS(err, "Failed to set owner reference", "resourceSnapshot", resourceSnapshotKObj)
		// should never happen
		return NewUnexpectedBehaviorError(err)
	}
	if err := r.Client.Create(ctx, resourceSnapshot); err != nil {
		klog.ErrorS(err, "Failed to create new resourceSnapshot", "resourceSnapshot", resourceSnapshotKObj)
		return NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Created new resourceSnapshot", "placement", klog.KObj(placementObj), "resourceSnapshot", resourceSnapshotKObj)
	return nil
}

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

// buildMasterResourceSnapshot builds and returns the master resource snapshot for the latest resource snapshot index and selected resources.
func BuildMasterResourceSnapshot(latestResourceSnapshotIndex, resourceSnapshotCount, envelopeObjCount int, placementName, placementNamespace, resourceHash string, selectedResources []fleetv1beta1.ResourceContent) fleetv1beta1.ResourceSnapshotObj {
	labels := map[string]string{
		fleetv1beta1.PlacementTrackingLabel: placementName,
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
	if placementNamespace == "" {
		// Cluster-scoped placement
		return &fleetv1beta1.ClusterResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, placementName, latestResourceSnapshotIndex),
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: spec,
		}
	} else {
		// Namespace-scoped placement
		return &fleetv1beta1.ResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, placementName, latestResourceSnapshotIndex),
				Namespace:   placementNamespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: spec,
		}
	}
}

// BuildSubIndexResourceSnapshot builds and returns the sub index resource snapshot for both cluster-scoped and namespace-scoped placements.
// Returns a ClusterResourceSnapshot for cluster-scoped placements (empty namespace) or ResourceSnapshot for namespace-scoped placements.
func BuildSubIndexResourceSnapshot(latestResourceSnapshotIndex, resourceSnapshotSubIndex int, placementName, placementNamespace string, selectedResources []fleetv1beta1.ResourceContent) fleetv1beta1.ResourceSnapshotObj {
	labels := map[string]string{
		fleetv1beta1.PlacementTrackingLabel: placementName,
		fleetv1beta1.ResourceIndexLabel:     strconv.Itoa(latestResourceSnapshotIndex),
	}
	annotations := map[string]string{
		fleetv1beta1.SubindexOfResourceSnapshotAnnotation: strconv.Itoa(resourceSnapshotSubIndex),
	}
	spec := fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources: selectedResources,
	}
	if placementNamespace == "" {
		// Cluster-scoped placement
		return &fleetv1beta1.ClusterResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, placementName, latestResourceSnapshotIndex, resourceSnapshotSubIndex),
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: spec,
		}
	} else {
		// Namespace-scoped placement
		return &fleetv1beta1.ResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, placementName, latestResourceSnapshotIndex, resourceSnapshotSubIndex),
				Namespace:   placementNamespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: spec,
		}
	}
}
