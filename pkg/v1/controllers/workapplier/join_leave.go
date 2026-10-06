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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

// Join sets the work applier to work.
//
// As a member cluster side controller, work applier can only start reconciling work objects after the
// member cluster has successfully joined the fleet.
func (r *Reconciler) Join() error {
	if !r.ready.Load() {
		klog.InfoS("Setting the work applier as ready")
	}
	r.ready.Store(true)
	return nil
}

// Leave performs cleanup ops when a member cluster leaves the fleet.
//
// For availability reasons, when a member cluster leaves the fleet, all the resources are left intact on the member
// cluster.
func (r *Reconciler) Leave(ctx context.Context) error {
	if r.ready.Load() {
		klog.InfoS("Setting the work applier as not ready")
	}
	r.ready.Store(false)

	// List all the work objects.
	workList := &placementv1alpha1.WorkList{}
	if err := r.hubUncachedReader.List(ctx, workList, client.InNamespace(r.workNSName)); err != nil {
		return errors.NewAPIServerError(err, "failed to list the work objects", false)
	}

	// For each work object, if it has the cleanup finalizer, find the corresponding appliedWork object, and
	// delete it with the Orphan propagation policy.
	//
	// It is OK if the appliedWork object hasn't been created yet.
	deletePolicy := metav1.DeletePropagationOrphan
	for idx := range workList.Items {
		work := &workList.Items[idx]
		if !controllerutil.ContainsFinalizer(work, workApplierCleanupFinalizer) {
			continue
		}

		appliedWork := &placementv1alpha1.AppliedWork{
			ObjectMeta: metav1.ObjectMeta{Name: work.Name},
		}
		if err := r.spokeClient.Delete(ctx, appliedWork, &client.DeleteOptions{PropagationPolicy: &deletePolicy}); err != nil {
			if apierrors.IsNotFound(err) {
				klog.V(2).InfoS("The appliedWork object does not exist; skipping deletion",
					"appliedWork", klog.KObj(appliedWork), "work", klog.KObj(work))
				continue
			}
			return errors.NewAPIServerError(err, "failed to delete the appliedWork object", false,
				"appliedWork", klog.KObj(appliedWork), "work", klog.KObj(work))
		}
		klog.V(2).InfoS("Deleted the appliedWork object with the Orphan propagation policy",
			"appliedWork", klog.KObj(appliedWork), "work", klog.KObj(work))
	}

	// Drop the cleanup finalizer from all the work objects.
	for idx := range workList.Items {
		work := &workList.Items[idx]
		if !controllerutil.ContainsFinalizer(work, workApplierCleanupFinalizer) {
			continue
		}

		controllerutil.RemoveFinalizer(work, workApplierCleanupFinalizer)
		if err := r.hubClient.Update(ctx, work); err != nil {
			return errors.NewAPIServerError(err, "failed to remove the cleanup finalizer from the work", false,
				"work", klog.KObj(work))
		}
	}

	klog.V(2).InfoS("Removed the cleanup finalizer from all the work objects", "workCount", len(workList.Items))
	return nil
}
