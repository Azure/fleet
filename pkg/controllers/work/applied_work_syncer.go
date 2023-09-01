/*
Copyright 2021 The Kubernetes Authors.

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

package work

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// generateDiff check the difference between what is supposed to be applied  (tracked by the work CR status)
// and what was applied in the member cluster (tracked by the appliedWork CR).
// What is in the `appliedWork` but not in the `work` should be deleted from the member cluster
// What is in the `work` but not in the `appliedWork` should be added to the appliedWork status
func (r *ApplyWorkReconciler) generateDiff(ctx context.Context, work *fleetv1beta1.Work, appliedWork *fleetv1beta1.AppliedWork) ([]fleetv1beta1.AppliedResourceMeta, []fleetv1beta1.AppliedResourceMeta, error) {
	var staleRes, newRes []fleetv1beta1.AppliedResourceMeta
	// for every resource applied in cluster, check if it's still in the work's manifest condition
	// we keep the applied resource in the appliedWork status even if it is not applied successfully
	// to make sure that it is safe to delete the resource from the member cluster.
	for _, resourceMeta := range appliedWork.Status.AppliedResources {
		resStillExist := false
		for _, manifestCond := range work.Status.ManifestConditions {
			if isSameResourceIdentifier(resourceMeta.WorkResourceIdentifier, manifestCond.Identifier) {
				resStillExist = true
				break
			}
		}
		if !resStillExist {
			klog.V(2).InfoS("find an orphaned resource in the member cluster",
				"parent resource", work.GetName(), "orphaned resource", resourceMeta.WorkResourceIdentifier)
			staleRes = append(staleRes, resourceMeta)
		}
	}
	// add every resource in the work's manifest condition that is applied successfully back to the appliedWork status
	for _, manifestCond := range work.Status.ManifestConditions {
		ac := meta.FindStatusCondition(manifestCond.Conditions, ConditionTypeApplied)
		if ac == nil {
			// should not happen
			klog.ErrorS(fmt.Errorf("resource is missing  applied condition"), "applied condition missing", "resource", manifestCond.Identifier)
			continue
		}
		// we only add the applied one to the appliedWork status
		if ac.Status == metav1.ConditionTrue {
			resRecorded := false
			// we update the identifier
			// TODO: this UID may not be the current one if the resource is deleted and recreated
			for _, resourceMeta := range appliedWork.Status.AppliedResources {
				if isSameResourceIdentifier(resourceMeta.WorkResourceIdentifier, manifestCond.Identifier) {
					resRecorded = true
					newRes = append(newRes, fleetv1beta1.AppliedResourceMeta{
						WorkResourceIdentifier: manifestCond.Identifier,
						UID:                    resourceMeta.UID,
					})
					break
				}
			}
			if !resRecorded {
				klog.V(2).InfoS("discovered a new manifest resource",
					"parent Work", work.GetName(), "manifest", manifestCond.Identifier)
				obj, err := r.spokeDynamicClient.Resource(schema.GroupVersionResource{
					Group:    manifestCond.Identifier.Group,
					Version:  manifestCond.Identifier.Version,
					Resource: manifestCond.Identifier.Resource,
				}).Namespace(manifestCond.Identifier.Namespace).Get(ctx, manifestCond.Identifier.Name, metav1.GetOptions{})
				switch {
				case apierrors.IsNotFound(err):
					klog.V(2).InfoS("the new manifest resource is already deleted", "parent Work", work.GetName(), "manifest", manifestCond.Identifier)
					continue
				case err != nil:
					klog.ErrorS(err, "failed to retrieve the manifest", "parent Work", work.GetName(), "manifest", manifestCond.Identifier)
					return nil, nil, err
				}
				newRes = append(newRes, fleetv1beta1.AppliedResourceMeta{
					WorkResourceIdentifier: manifestCond.Identifier,
					UID:                    obj.GetUID(),
				})
			}
		}
	}
	return newRes, staleRes, nil
}

func (r *ApplyWorkReconciler) deleteStaleManifest(ctx context.Context, staleManifests []fleetv1beta1.AppliedResourceMeta, owner metav1.OwnerReference) error {
	var errs []error

	for _, staleManifest := range staleManifests {
		gvr := schema.GroupVersionResource{
			Group:    staleManifest.Group,
			Version:  staleManifest.Version,
			Resource: staleManifest.Resource,
		}
		uObj, err := r.spokeDynamicClient.Resource(gvr).Namespace(staleManifest.Namespace).
			Get(ctx, staleManifest.Name, metav1.GetOptions{})
		if err != nil {
			// It is possible that the staled manifest was already deleted but the status wasn't updated to reflect that yet.
			if apierrors.IsNotFound(err) {
				klog.V(2).InfoS("the staled manifest already deleted", "manifest", staleManifest, "owner", owner)
				continue
			}
			klog.ErrorS(err, "failed to get the staled manifest", "manifest", staleManifest, "owner", owner)
			errs = append(errs, err)
			continue
		}
		existingOwners := uObj.GetOwnerReferences()
		newOwners := make([]metav1.OwnerReference, 0)
		found := false
		for index, r := range existingOwners {
			if isReferSameObject(r, owner) {
				found = true
				newOwners = append(newOwners, existingOwners[:index]...)
				newOwners = append(newOwners, existingOwners[index+1:]...)
			}
		}
		if !found {
			klog.V(2).InfoS("the stale manifest is not owned by this work, skip", "manifest", staleManifest, "owner", owner)
			continue
		}
		if len(newOwners) == 0 {
			klog.V(2).InfoS("delete the staled manifest", "manifest", staleManifest, "owner", owner)
			err = r.spokeDynamicClient.Resource(gvr).Namespace(staleManifest.Namespace).
				Delete(ctx, staleManifest.Name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				klog.ErrorS(err, "failed to delete the staled manifest", "manifest", staleManifest, "owner", owner)
				errs = append(errs, err)
			}
		} else {
			klog.V(2).InfoS("remove the owner reference from the staled manifest", "manifest", staleManifest, "owner", owner)
			uObj.SetOwnerReferences(newOwners)
			_, err = r.spokeDynamicClient.Resource(gvr).Namespace(staleManifest.Namespace).Update(ctx, uObj, metav1.UpdateOptions{FieldManager: workFieldManagerName})
			if err != nil {
				klog.ErrorS(err, "failed to remove the owner reference from manifest", "manifest", staleManifest, "owner", owner)
				errs = append(errs, err)
			}
		}
	}
	return utilerrors.NewAggregate(errs)
}

// isSameResourceIdentifier returns true if a and b identifies the same object.
func isSameResourceIdentifier(a, b fleetv1beta1.WorkResourceIdentifier) bool {
	// compare GVKNN but ignore the Ordinal and Resource
	return a.Group == b.Group && a.Version == b.Version && a.Kind == b.Kind && a.Namespace == b.Namespace && a.Name == b.Name
}
