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

package workapplier

import (
	"context"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

// retrieveLinkedAndLeftOverWorks retrieves two sets of work objects:
//
// a) linked work objects: these work objects share the same owner placement binding as the primary work object, and
//
//	are all linked to the same primary placement resource snapshot as the primary work object. In other words,
//	these work objects together form a consistent state of manifests that the work applier should process.
//
// b) leftover work objects: these work objects are also owned by the same placement binding as the primary work
//
//	object, but are no longer linked to the current primary placement resource snapshot and are considered stale.
//	All manifests in these work objects should be cleaned up, unless they are also referenced by another linked work
//	object.
//
// This method will return an error if it observed any incomplete/inconsistent state among the two sets. Specifically,
// it will return a transient error when:
//
// a) the number of linked work objects does not match the linked work count recorded on the primary work object, or
//
//	any of the linked work objects is not linked to the same primary placement resource snapshot as the primary
//	work object.
//
// b) any of the leftover work objects hasn't been marked for deletion.
//
// Note that the first work object in the array of linked work objects is always the primary work object.
func (r *Reconciler) retrieveLinkedAndLeftOverWorks(ctx context.Context,
	primaryWork *placementv1alpha1.Work) ([]*placementv1alpha1.Work, []*placementv1alpha1.Work, error) {
	ownedBy := primaryWork.GetLabels()[placementv1alpha1.WorkOwnedByPlacementBindingLabelKey]
	if ownedBy == "" {
		return nil, nil, errors.NewUnexpectedError(nil, "the primary work is missing the owner placement binding label")
	}
	// An empty owner namespace signals a cluster-scoped placement binding.
	ownerNS := primaryWork.GetLabels()[placementv1alpha1.WorkOwnerNamespaceLabelKey]

	wantLinkedWorkCountVal := primaryWork.GetAnnotations()[placementv1alpha1.LinkedWorkCountAnnotationKey]
	wantLinkedWorkCount, err := strconv.Atoi(wantLinkedWorkCountVal)
	if err != nil {
		return nil, nil, errors.NewUnexpectedError(err, "failed to parse the linked work count annotation on the primary work",
			"linkedWorkCount", wantLinkedWorkCountVal)
	}

	wantPrimarySnapshotName := primaryWork.GetAnnotations()[placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey]
	if wantPrimarySnapshotName == "" {
		return nil, nil, errors.NewUnexpectedError(nil, "the primary work is missing the primary placement resource snapshot annotation")
	}

	// List all work objects owned by the same placement binding as the primary work.
	ownedByFieldVal := fmt.Sprintf(fieldindexers.WorkOwnedByPlacementBindingCustomFieldValFmt, ownerNS, ownedBy)
	workList := &placementv1alpha1.WorkList{}
	if err := r.hubClient.List(ctx, workList,
		client.InNamespace(primaryWork.Namespace),
		client.MatchingFields{fieldindexers.WorkOwnedByPlacementBindingCustomFieldName: ownedByFieldVal},
	); err != nil {
		return nil, nil, errors.NewAPIServerError(err, "failed to list the work objects owned by the placement binding", true,
			"ownerPlacementBinding", ownedByFieldVal)
	}

	var linkedWorks []*placementv1alpha1.Work
	var leftOverWorks []*placementv1alpha1.Work

	linkedWorks = append(linkedWorks, primaryWork)
	for idx := range workList.Items {
		linkedWork := &workList.Items[idx]
		primarySnapshotName := linkedWork.GetAnnotations()[placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey]
		if primarySnapshotName != wantPrimarySnapshotName {
			// This work is not linked to the same primary placement resource snapshot as the primary work; add it
			// to the set of leftover works.
			leftOverWorks = append(leftOverWorks, linkedWork)
			continue
		}

		if linkedWork.Name != primaryWork.Name {
			// This work is linked to the same primary placement resource snapshot as the primary work; add it
			// to the set of linked works.
			linkedWorks = append(linkedWorks, linkedWork)
		}
	}

	// Verify that the number of linked work objects matches the count recorded on the primary work.
	if len(linkedWorks) != wantLinkedWorkCount {
		return nil, nil, errors.NewTransientError(nil, "the number of linked work objects found is inconsistent with the count recorded on the primary work",
			"ownerPlacementBinding", ownedByFieldVal,
			"observedLinkedWorkCount", len(linkedWorks), "wantLinkedWorkCount", wantLinkedWorkCount)
	}

	// Verify that all leftover work objects have been marked for deletion.
	for idx := range leftOverWorks {
		leftOverWork := leftOverWorks[idx]
		if leftOverWork.DeletionTimestamp == nil {
			return nil, nil, errors.NewTransientError(nil, "a leftover work object has not been marked for deletion as expected",
				"leftOverWork", klog.KObj(leftOverWork))
		}
	}

	return linkedWorks, leftOverWorks, nil
}

// addCleanupFinalizerTo adds the cleanup finalizer to the given work objects, so that the applied resources
// on the member cluster side can be cleaned up before the work objects are removed.
func (r *Reconciler) addCleanupFinalizerTo(ctx context.Context, linkedWorks []*placementv1alpha1.Work) error {
	for idx := range linkedWorks {
		linkedWork := linkedWorks[idx]
		if controllerutil.ContainsFinalizer(linkedWork, workApplierCleanupFinalizer) {
			continue
		}

		controllerutil.AddFinalizer(linkedWork, workApplierCleanupFinalizer)
		if err := r.hubClient.Update(ctx, linkedWork); err != nil {
			// Reset the finalizer list to its previous state.
			controllerutil.RemoveFinalizer(linkedWork, workApplierCleanupFinalizer)
			return errors.NewAPIServerError(err, "failed to add the cleanup finalizer to the work", false,
				"work", klog.KObj(linkedWork))
		}
		klog.V(2).InfoS("Added the cleanup finalizer to the work", "work", klog.KObj(linkedWork))
	}
	return nil
}

func (r *Reconciler) ensureAppliedWorks(ctx context.Context, works []*placementv1alpha1.Work) ([]*placementv1alpha1.AppliedWork, error) {
	appliedWorks := make([]*placementv1alpha1.AppliedWork, 0, len(works))
	for idx := range works {
		work := works[idx]

		// Check if an appliedWork object already exists for the primary work object.
		//
		// Since we only create an appliedWork object after adding the finalizer to the primary work object,
		// it might appear that if the finalizer is absent, the appliedWork object should not exist. This
		// is not the case with the work applier though, as the controller features a
		// Leave method that will strip all work objects off their finalizers, which is called when the
		// member cluster leaves the fleet. If the member cluster chooses to later re-join the fleet, the controller
		// might see a work object with no finalizer but with an appliedWork object. Because of this, here we always
		// check for the existence of the appliedWork object, with or without the finalizer.
		appliedWork := &placementv1alpha1.AppliedWork{}
		err := r.spokeClient.Get(ctx, types.NamespacedName{Name: work.Name}, appliedWork)
		switch {
		case err == nil:
			// The AppliedWork already exists; no further action is needed.
			klog.V(2).InfoS("Found an appliedWork object for the work object", "appliedWork", klog.KObj(appliedWork))
			appliedWorks = append(appliedWorks, appliedWork)
			continue
		case !apierrors.IsNotFound(err):
			return nil, errors.NewAPIServerError(err, "failed to retrieve the appliedWork object", true)
		}

		// The appliedWork object does not exist; create one.
		appliedWork = &placementv1alpha1.AppliedWork{
			ObjectMeta: metav1.ObjectMeta{
				Name: work.Name,
			},
			Spec: placementv1alpha1.AppliedWorkSpec{
				WorkName:      work.Name,
				WorkNamespace: work.Namespace,
			},
		}
		if err := r.spokeClient.Create(ctx, appliedWork); err != nil {
			// Note that the controller must retry on AppliedWork AlreadyExists errors; otherwise the
			// controller will run the reconciliation loop with an AppliedWork that has no UID,
			// which might lead to takeover failures in later steps.
			return nil, errors.NewAPIServerError(err, "failed to create an appliedWork object for the work object", false,
				"appliedWork", klog.KObj(appliedWork))
		}
		klog.V(2).InfoS("Created an appliedWork object for the work object", "appliedWork", klog.KObj(appliedWork))
		appliedWorks = append(appliedWorks, appliedWork)
	}

	return appliedWorks, nil
}
