/*
Copyright 2026 The KubeFleet Authors.

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

package workgenerator

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/errors"
	"go.goms.io/fleet/pkg/utils/parallelizer"
)

const (
	workOwnerLabelValueLengthLimit = 61
	workOwnerLabelValueHashLength  = 12
)

var (
	workGVK = schema.GroupVersionKind{
		Group:   placementv1alpha1.GroupVersion.Group,
		Version: placementv1alpha1.GroupVersion.Version,
		Kind:    "Work",
	}
)

// areWorksUpToDate checks if the work objects for a placement binding are up-to-date, i.e., if all the work objects
// needed given the current placement binding spec have been created/updated. If so, the work generation can skip
// the work object create/update ops and skip to refreshing the placement binding status.
func areWorksUpToDate(placementBinding placementv1alpha1.PlacementBindingAccessor, works []placementv1alpha1.Work) (bool, error) {
	// Check if the last processed placement resource snapshot name in the placement binding status matches the
	// primary placement resource snapshot name in the placement binding spec. If so, it ensures that all the needed
	// work objects have been created/updated for the placement binding.
	lastProcessedSnapshotName := ""
	if placementBinding.GetStatus().LastProcessedResourceSnapshotName != nil {
		lastProcessedSnapshotName = *placementBinding.GetStatus().LastProcessedResourceSnapshotName
	}
	primarySnapshotName := placementBinding.GetSpec().ResourceSnapshotName

	if lastProcessedSnapshotName != primarySnapshotName {
		return false, nil
	}

	// Do some sanity checks, just to make sure that the cache is up to date.
	if len(works) == 0 {
		// No work objects exist for the placement binding. The cache might not have caught up yet.
		return false, errors.NewTransientError(nil, "no work objects are found; the cache might be stale")
	}

	// Check if all the work objects have been linked to the expected placement resource snapshot (in the spec),
	// and verify that the linked work count recorded on the primary work matches the number of listed works.
	linkedWorkCount := -1
	for idx := range works {
		work := &works[idx]
		annotations := work.GetAnnotations()
		if linked := annotations[placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey]; linked != primarySnapshotName {
			// The work object is linked to a different placement resource snapshot than the one in the placement binding spec.
			// This might happen if the cache is stale.
			return false, errors.NewTransientError(nil, "found a work object that is not linked to the expected primary placement resource snapshot",
				"work", klog.KObj(work), "linkedPlacementResourceSnapshotName", linked, "expectedPlacementResourceSnapshotName", primarySnapshotName)
		}

		linkedWorkCountStr, found := annotations[placementv1alpha1.LinkedWorkCountAnnotationKey]
		if !found {
			continue
		}
		if linkedWorkCount != -1 {
			// At any time there should be exactly one primary placement resource snapshot that has the
			// linked work count annotation.
			return false, errors.NewUnexpectedError(nil, "multiple primary placement resource snapshots have the linked work count annotation",
				"work", klog.KObj(work), "linkedWorkCount", linkedWorkCountStr)
		}
		var err error
		linkedWorkCount, err = strconv.Atoi(linkedWorkCountStr)
		if err != nil || linkedWorkCount < 1 {
			return false, errors.NewUnexpectedError(err, "invalid linked work count annotation on work object",
				"work", klog.KObj(work), "linkedWorkCount", linkedWorkCountStr)
		}
	}
	if linkedWorkCount != len(works) {
		return false, errors.NewTransientError(nil, "the number of work objects is not as expected",
			"expectedWorkCount", linkedWorkCount, "actualWorkCount", len(works))
	}

	// Check if the sync strategy of the placement binding still matches that on the work objects.
	syncStrategy := placementBinding.GetSpec().SyncStrategy
	for idx := range works {
		work := &works[idx]
		if !equality.Semantic.DeepEqual(work.Spec.SyncStrategy, syncStrategy) {
			return false, nil
		}
	}

	return true, nil
}

func (r *Reconciler) refreshWorks(ctx context.Context,
	placementBinding placementv1alpha1.PlacementBindingAccessor,
	sortedPlacementResourceSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor,
	works []placementv1alpha1.Work,
) ([]*placementv1alpha1.Work, bool, error) {
	writtenToStorage := false
	worksToDelete := []*placementv1alpha1.Work{}

	// Build an index of work objects by their names.
	existingWorksByName := make(map[string]*placementv1alpha1.Work, len(works))
	for idx := range works {
		work := &works[idx]
		existingWorksByName[work.GetName()] = work
	}

	seenWorkNames := sets.Set[string]{}
	// First, build a work object for the primary placement resource snapshot. This is considered to be the
	// primary work object for the placement binding.
	//
	// This work object serves as the owner of all other work objects created for this placement binding. KubeFleet
	// leverages this setup to ensure that if a placement binding is deleted, all the work objects created for it
	// will be cleaned up automatically by K8s' built-in GC process.
	//
	// We cannot set the placement binding itself as the owner of the work objects as they might reside in
	// different namespaces, and cross-namespace ownership is not allowed in K8s. The list-then-delete loop has
	// limitations as well, as stale cache might leave some work objects behind.
	primaryPlacementResourceSnapshot := sortedPlacementResourceSnapshots[0]
	primaryWorkToCreateOrUpdate, err := buildWorkObjectFor(primaryPlacementResourceSnapshot, placementBinding, primaryPlacementResourceSnapshot.GetName())
	if err != nil {
		return nil, false, errors.Wraps(err, "failed to build work object for primary placement resource snapshot",
			"primaryPlacementResourceSnapshot", klog.KObj(primaryPlacementResourceSnapshot))
	}
	seenWorkNames.Insert(primaryWorkToCreateOrUpdate.GetName())

	// Then build work objects for any secondary placement resource snapshots. These work objects are considered to
	// be secondary work objects for the placement binding.
	var additionalWorksToCreateOrUpdate []*placementv1alpha1.Work
	for idx := 1; idx < len(sortedPlacementResourceSnapshots); idx++ {
		snapshot := sortedPlacementResourceSnapshots[idx]
		work, err := buildWorkObjectFor(snapshot, placementBinding, primaryPlacementResourceSnapshot.GetName())
		if err != nil {
			return nil, false, errors.Wraps(err, "failed to build work object for placement resource snapshot",
				"placementResourceSnapshot", klog.KObj(snapshot))
		}
		if seenWorkNames.Has(work.GetName()) {
			return nil, false, errors.NewUnexpectedError(nil, "duplicate work object built for placement resource snapshot",
				"work", klog.KObj(work), "placementResourceSnapshot", klog.KObj(snapshot))
		}
		additionalWorksToCreateOrUpdate = append(additionalWorksToCreateOrUpdate, work)
		seenWorkNames.Insert(work.GetName())
	}

	// Add the linked work object count annotation on the primary work object. The count is the total number of
	// work objects created for the placement binding, including the primary work object itself.
	primaryWorkToCreateOrUpdate.GetAnnotations()[placementv1alpha1.LinkedWorkCountAnnotationKey] = fmt.Sprintf("%d", len(additionalWorksToCreateOrUpdate)+1)

	// Check for dangling work objects (those that are no longer linked with any source) and add them to the
	// deletion list.
	for _, work := range existingWorksByName {
		if seenWorkNames.Has(work.GetName()) {
			continue
		}
		klog.V(2).InfoS("A work object is no longer needed; mark it for deletion", "work", klog.KObj(work))
		worksToDelete = append(worksToDelete, work)
	}

	// Issue the delete ops in parallel. The control loop deletes the dangling work objects first to avoid
	// potential conflicts (e.g., creating the same object twice). This is a best-effort attempt as we cannot
	// create/update/delete work objects in a transactional manner.
	if err := r.deleteWorkObjects(ctx, worksToDelete, placementBinding); err != nil {
		return nil, false, errors.Wraps(err, "failed to delete dangling work objects")
	}

	// Create the primary work object first. This is needed as the controller needs its object UID to set
	// owner references on the secondary work objects.
	createdOrUpdatedWorks, primaryWorkObjWrittenToStorage, err := r.createOrUpdateWorkObjects(ctx,
		[]*placementv1alpha1.Work{primaryWorkToCreateOrUpdate}, placementBinding)
	if err != nil {
		return nil, false, errors.Wraps(err, "failed to create or update work object for primary placement resource snapshot",
			"primaryPlacementResourceSnapshot", klog.KObj(primaryPlacementResourceSnapshot))
	}
	ownerWorkObjRef := metav1.NewControllerRef(createdOrUpdatedWorks[0], workGVK)
	writtenToStorage = primaryWorkObjWrittenToStorage

	// Set the owner reference on all secondary work objects.
	for idx := range additionalWorksToCreateOrUpdate {
		work := additionalWorksToCreateOrUpdate[idx]
		work.SetOwnerReferences([]metav1.OwnerReference{*ownerWorkObjRef})
	}

	// Issue the create or update ops for the secondary work objects in parallel.
	additionalCreatedOrUpdatedWorks, additionalCreatedOrUpdated, err := r.createOrUpdateWorkObjects(ctx, additionalWorksToCreateOrUpdate, placementBinding)
	if err != nil {
		return nil, false, errors.Wraps(err, "failed to create or update additional work objects for secondary placement resource snapshots")
	}
	createdOrUpdatedWorks = append(createdOrUpdatedWorks, additionalCreatedOrUpdatedWorks...)
	if !writtenToStorage {
		writtenToStorage = additionalCreatedOrUpdated
	}

	return createdOrUpdatedWorks, writtenToStorage, nil
}

func buildWorkObjectFor(
	placementResourceSnapshot placementv1alpha1.PlacementResourceSnapshotAccessor,
	placementBinding placementv1alpha1.PlacementBindingAccessor,
	primaryPlacementResourceSnapshotName string,
) (*placementv1alpha1.Work, error) {
	snapshotSubIdx := placementResourceSnapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey]
	if len(snapshotSubIdx) == 0 {
		return nil, errors.NewUnexpectedError(nil, "no sub-index label found on the placement resource snapshot")
	}
	derivedFromSnapshotSrcFormatter := &placementResourceSnapshotDerivedFromSourceFormatter{
		snapshotSubIdx: snapshotSubIdx,
	}
	placementBindingSpec := placementBinding.GetSpec()
	placementResourceSnapshotSpec := placementResourceSnapshot.GetSpec()

	workName := uniqueNameForWorkDerivedFromPlacementResourceSnapshot(placementBinding, snapshotSubIdx == "0", derivedFromSnapshotSrcFormatter)

	work := &placementv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: fmt.Sprintf(utils.NamespaceNameFormat, placementBinding.GetSpec().ClusterName),
			Name:      workName,
		},
	}
	updateWorkObjectMetadataAndSpec(
		work,
		placementBinding.GetNamespace(),
		placementBindingSpec.PlacementPolicyName,
		placementBinding.GetName(),
		primaryPlacementResourceSnapshotName,
		derivedFromSnapshotSrcFormatter,
		placementResourceSnapshotSpec.Resources,
		placementBindingSpec.SyncStrategy.DeepCopy(),
	)
	return work, nil
}

func updateWorkObjectMetadataAndSpec(
	work *placementv1alpha1.Work,
	ownerNSName, ownerPlacementPolicyName, ownerPlacementBindingName string,
	primaryPlacementResourceSnapshotName string,
	derivedFromSrcFormatter derivedFromSourceFormatter,
	resources []placementv1alpha1.SnapshottedResource,
	syncStrategy *placementv1alpha1.SyncStrategy,
) {
	// Set annotations on the work object.
	annotations := work.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	// Set the linked to primary placement resource snapshot annotation on the work object.
	annotations[placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey] = primaryPlacementResourceSnapshotName

	// Set the derived from source annotation on the work object.
	//
	// For work objects derived from placement resource snapshots, the annotation is set with the value
	// `placement-resource-snapshot/[SUB-INDEX]`, where `[SUB-INDEX]` is the sub-index of the placement
	// resource snapshot that the work object is derived from.
	//
	// Sub-indices are used here instead of indices to avoid any fluctuations caused by the progression
	// of placement resource snapshots over rollouts.
	annotations[placementv1alpha1.WorkDerivedFromSourceAnnotationKey] = fmt.Sprintf("%s/%s",
		derivedFromSrcFormatter.SourceType(), derivedFromSrcFormatter.SourceID())
	annotations[placementv1alpha1.WorkOwnedByPlacementPolicyAnnotationKey] = ownerPlacementPolicyName
	annotations[placementv1alpha1.WorkOwnedByPlacementBindingAnnotationKey] = ownerPlacementBindingName
	work.SetAnnotations(annotations)

	// Set the owner labels on the work object.
	labels := work.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[placementv1alpha1.WorkOwnerNamespaceLabelKey] = ownerNSName
	labels[placementv1alpha1.WorkOwnedByPlacementPolicyLabelKey] = workOwnerLabelValue(ownerPlacementPolicyName)
	labels[placementv1alpha1.WorkOwnedByPlacementBindingLabelKey] = workOwnerLabelValue(ownerPlacementBindingName)
	work.SetLabels(labels)

	// Set the snapshotted resources on the work object.
	manifests := make([]placementv1alpha1.Manifest, len(resources))
	for i := range resources {
		manifests[i] = placementv1alpha1.Manifest{RawExtension: resources[i].Manifest}
	}
	work.Spec.Manifests = manifests

	// Set the sync strategy on the work object.
	work.Spec.SyncStrategy = syncStrategy
}

func (r *Reconciler) createOrUpdateWorkObjects(
	ctx context.Context,
	worksToCreateOrUpdate []*placementv1alpha1.Work,
	placementBinding placementv1alpha1.PlacementBindingAccessor,
) ([]*placementv1alpha1.Work, bool, error) {
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	createdOrUpdatedWorks := make([]*placementv1alpha1.Work, len(worksToCreateOrUpdate))
	errFlag := parallelizer.NewErrorFlag()
	createdOrUpdated := atomic.Bool{}
	r.parallelizer.ParallelizeUntil(childCtx, len(worksToCreateOrUpdate), func(idx int) {
		work := worksToCreateOrUpdate[idx]

		createdOrUpdatedWork := &placementv1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: work.GetNamespace(),
				Name:      work.GetName(),
			},
		}
		resOp, err := controllerutil.CreateOrUpdate(childCtx, r.hubClient, createdOrUpdatedWork, func() error {
			// Work objects are considered to be fully internal KubeFleet resources; for this reason
			// here the control loop chooses to overwrite the spec, labels, annotations, and owner references of
			// the work object with the latest values instead of attempting to do a merge.
			createdOrUpdatedWork.Spec = work.Spec
			createdOrUpdatedWork.SetLabels(work.GetLabels())
			createdOrUpdatedWork.SetAnnotations(work.GetAnnotations())
			createdOrUpdatedWork.SetOwnerReferences(work.GetOwnerReferences())
			return nil
		})
		if err != nil {
			wrappedErr := errors.Wraps(err, "failed to create or update work object",
				"work", klog.KObj(work), "resOp", resOp)
			errFlag.Raise(wrappedErr)
			childCancel()
			return
		}

		createdOrUpdatedWorks[idx] = createdOrUpdatedWork
		if resOp != controllerutil.OperationResultNone {
			// The work object has been created or updated.
			createdOrUpdated.CompareAndSwap(false, true)
		}
		klog.V(2).InfoS("Successfully created or updated work object",
			"work", klog.KObj(createdOrUpdatedWork), "resOp", resOp,
			"placementBinding", klog.KObj(placementBinding))
	}, "createOrUpdateWorkObjects")
	if err := errFlag.Lower(); err != nil {
		return nil, false, err
	}
	return createdOrUpdatedWorks, createdOrUpdated.Load(), nil
}

func (r *Reconciler) deleteWorkObjects(
	ctx context.Context,
	worksToDelete []*placementv1alpha1.Work,
	placementBinding placementv1alpha1.PlacementBindingAccessor,
) error {
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	errFlag := parallelizer.NewErrorFlag()
	r.parallelizer.ParallelizeUntil(childCtx, len(worksToDelete), func(idx int) {
		work := worksToDelete[idx]

		if err := r.hubClient.Delete(childCtx, work); err != nil && !apierrors.IsNotFound(err) {
			wrappedErr := errors.Wraps(err, "failed to delete work object", "work", klog.KObj(work))
			errFlag.Raise(wrappedErr)
			childCancel()
			return
		}
		klog.V(2).InfoS("Successfully deleted work object",
			"work", klog.KObj(work),
			"placementBinding", klog.KObj(placementBinding))
	}, "deleteWorkObjects")
	return errFlag.Lower()
}

func workOwnerLabelValue(ownerName string) string {
	if len(ownerName) <= workOwnerLabelValueLengthLimit && !strings.Contains(ownerName, ".") {
		return ownerName
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(ownerName)))[:workOwnerLabelValueHashLength]
	name := strings.ReplaceAll(ownerName, ".", "")
	prefixLength := workOwnerLabelValueLengthLimit - workOwnerLabelValueHashLength - 1
	if len(name) > prefixLength {
		name = name[:prefixLength]
	}
	return fmt.Sprintf("%s-%s", name, hash)
}
