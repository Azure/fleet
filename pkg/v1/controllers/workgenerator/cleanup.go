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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/errors"
)

func (r *Reconciler) addPlacementBindingCleanupFinalizer(ctx context.Context, placementBinding placementv1alpha1.PlacementBindingAccessor) error {
	if controllerutil.ContainsFinalizer(placementBinding, workGeneratorCleanupFinalizer) {
		return nil
	}
	controllerutil.AddFinalizer(placementBinding, workGeneratorCleanupFinalizer)
	if err := r.hubClient.Update(ctx, placementBinding); err != nil {
		return errors.NewAPIServerError(err, "failed to add cleanup finalizer to placement binding", false)
	}
	return nil
}

// cleanupWorks deletes the primary Work object owned by a placement binding in the reserved namespace of the
// target cluster; all the other Work objects are cleaned up via owner-reference cascade deletion.
func (r *Reconciler) cleanupWorks(ctx context.Context, placementBinding placementv1alpha1.PlacementBindingAccessor) error {
	if !controllerutil.ContainsFinalizer(placementBinding, workGeneratorCleanupFinalizer) {
		// The cleanup finalizer has been dropped; no cleanup is needed.
		return nil
	}

	derivedFromSourceFormatter := &placementResourceSnapshotDerivedFromSourceFormatter{
		snapshotSubIdx: "0",
	}
	workName := uniqueNameForWorkDerivedFromPlacementResourceSnapshot(placementBinding, true, derivedFromSourceFormatter)
	workForPrimaryResSnapshot := &placementv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: fmt.Sprintf(utils.NamespaceNameFormat, placementBinding.GetSpec().ClusterName),
			Name:      workName,
		},
	}
	if err := r.hubClient.Delete(ctx, workForPrimaryResSnapshot); err != nil && !apierrors.IsNotFound(err) {
		return errors.NewAPIServerError(err, "failed to delete work object for primary placement resource snapshot", false,
			"work", klog.KObj(workForPrimaryResSnapshot))
	}
	// This work object is set as the owner of all other work objects created for this placement binding;
	// no further cleanup is needed.

	// If the binding has been suspended, unset its last processed resource snapshot name.
	if placementBinding.GetSpec().Suspended && placementBinding.GetDeletionTimestamp().IsZero() {
		placementBinding.GetStatus().LastProcessedResourceSnapshotName = nil

		if err := r.hubClient.Status().Update(ctx, placementBinding); err != nil {
			return errors.NewAPIServerError(err, "failed to update placement binding status to reset last processed resource snapshot name", false)
		}
	}

	// Remove the cleanup finalizer from the placement binding.
	controllerutil.RemoveFinalizer(placementBinding, workGeneratorCleanupFinalizer)
	if err := r.hubClient.Update(ctx, placementBinding); err != nil {
		return errors.NewAPIServerError(err, "failed to remove cleanup finalizer from placement binding", false)
	}
	return nil
}
