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

// Package bindingmanager provides utilities for managing the binding manager role for placement policies.
package bindingmanager

import (
	"context"
	"reflect"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils/errors"
)

// ClaimRoleAs adds an object reference (under the management of a controller) as the binding manager for
// a placement policy.
//
// This function returns (true, nil), if the binding manager role has been successfully claimed;
// (false, nil) if another source currently holds the binding manager role; and (false, err) if an error occurs
// during the attempt.
func ClaimRoleAs(
	ctx context.Context,
	hubClient client.Client,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	controllerName string,
	objectRef placementv1alpha1.ObjectReference,
) (bool, error) {
	if placementPolicy == nil || reflect.ValueOf(placementPolicy).IsNil() {
		return false, errors.NewUnexpectedError(nil, "the placement policy is nil")
	}
	if len(controllerName) == 0 {
		return false, errors.NewUnexpectedError(nil, "no controller name is provided")
	}
	// The name, API version, and kind fields are required by the API definition.
	if len(objectRef.Name) == 0 || len(objectRef.APIVersion) == 0 || len(objectRef.Kind) == 0 {
		return false, errors.NewUnexpectedError(nil, "the object reference is incomplete")
	}

	bindingManager := placementPolicy.GetStatus().BindingManager
	bindingManagerCopy := bindingManager.DeepCopy()
	if bindingManager == nil {
		bindingManager = &placementv1alpha1.BindingManager{
			ControllerName: controllerName,
			ObjectRefs: []placementv1alpha1.ObjectReference{
				objectRef,
			},
		}
		placementPolicy.GetStatus().BindingManager = bindingManager
		if err := hubClient.Status().Update(ctx, placementPolicy); err != nil {
			// Reset the binding manager to its previous state.
			placementPolicy.GetStatus().BindingManager = bindingManagerCopy
			return false, errors.NewAPIServerError(err, "failed to update placement policy status", false)
		}
		klog.V(2).InfoS("Successfully claimed the binding manager role", "placementPolicy", klog.KObj(placementPolicy), "controllerName", controllerName, "objectRef", objectRef)
		return true, nil
	}

	if bindingManager.ControllerName != controllerName {
		klog.V(2).InfoS("The binding manager role has already been claimed by another controller; should retry later",
			"placementPolicy", klog.KObj(placementPolicy),
			"currentControllerName", bindingManager.ControllerName,
			"applicantControllerName", controllerName, "applicantObjectRef", objectRef)
		return false, nil
	}

	found := false
	for idx := range bindingManager.ObjectRefs {
		if bindingManager.ObjectRefs[idx] == objectRef {
			found = true
			break
		}
	}
	if found {
		// The given object reference is already present in the binding manager claim. Verify if the view is still up-to-date.
		if err := dryRunPatch(ctx, hubClient, placementPolicy); err != nil {
			return false, errors.Wraps(err, "failed to verify state freshness (the given object reference is already present in the binding manager claim)")
		}
		klog.V(2).InfoS("The given object reference is already present in the binding manager claim; no further action is needed",
			"placementPolicy", klog.KObj(placementPolicy), "controllerName", controllerName, "objectRef", objectRef)
		return true, nil
	}

	bindingManager.ObjectRefs = append(bindingManager.ObjectRefs, objectRef)
	if err := hubClient.Status().Update(ctx, placementPolicy); err != nil {
		// Reset the binding manager to its previous state.
		placementPolicy.GetStatus().BindingManager = bindingManagerCopy
		return false, errors.NewAPIServerError(err, "failed to update placement policy status", false)
	}
	klog.V(2).InfoS("Successfully added the object reference to the binding manager claim",
		"placementPolicy", klog.KObj(placementPolicy), "controllerName", controllerName, "objectRef", objectRef)
	return true, nil
}

// RelinquishRoleFor releases the given object reference (under the management of a controller) from the
// binding manager role for a placement policy.
//
// If the passed-in object reference is the last entry in the binding manager claim, the claim is dropped altogether.
//
// An error will be returned if the current binding manager view is not up-to-date. If the given object
// reference is not currently holding the binding manager role, the function will return with no error.
func RelinquishRoleFor(
	ctx context.Context,
	hubClient client.Client,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	controllerName string,
	objectRef placementv1alpha1.ObjectReference,
) error {
	if placementPolicy == nil || reflect.ValueOf(placementPolicy).IsNil() {
		return errors.NewUnexpectedError(nil, "the placement policy is nil")
	}
	if len(controllerName) == 0 {
		return errors.NewUnexpectedError(nil, "no controller name is provided")
	}
	if len(objectRef.Name) == 0 || len(objectRef.APIVersion) == 0 || len(objectRef.Kind) == 0 {
		return errors.NewUnexpectedError(nil, "the object reference is incomplete")
	}

	bindingManager := placementPolicy.GetStatus().BindingManager
	bindingManagerCopy := bindingManager.DeepCopy()
	if bindingManager == nil || bindingManager.ControllerName != controllerName {
		// The given object (or controller) no longer holds the binding manager role based on the current state.
		// However, the current state might be stale; do a dry-run patch to verify its freshness.
		if err := dryRunPatch(ctx, hubClient, placementPolicy); err != nil {
			return errors.Wraps(err, "failed to verify state freshness (no binding manager role is held by the given controller)")
		}
		// The current state is up to date; no further action is needed.
		klog.V(2).InfoS("No binding manager role is held by the given controller; relinquishing is not needed",
			"placementPolicy", klog.KObj(placementPolicy), "controllerName", controllerName)
		return nil
	}

	found := false
	updatedObjectRefs := make([]placementv1alpha1.ObjectReference, 0, len(bindingManager.ObjectRefs))
	for idx := range bindingManager.ObjectRefs {
		if bindingManager.ObjectRefs[idx] == objectRef {
			found = true
			continue
		}
		updatedObjectRefs = append(updatedObjectRefs, bindingManager.ObjectRefs[idx])
	}
	if !found {
		// The given object (or controller) no longer holds the binding manager role based on the current state. However, the
		// current state might be stale; do a dry-run patch to verify its freshness.
		if err := dryRunPatch(ctx, hubClient, placementPolicy); err != nil {
			return errors.Wraps(err, "failed to verify state freshness (the given object reference is not found in the binding manager claim)")
		}
		// The current state is up to date; no further action is needed.
		klog.V(2).InfoS("The given object reference is not found in the binding manager claim; relinquishing is not needed",
			"placementPolicy", klog.KObj(placementPolicy), "controllerName", controllerName, "objectRef", objectRef)
		return nil
	}

	// Remove the given object reference from the binding manager claim. If the list of object references becomes empty,
	// remove the binding manager claim altogether.
	bindingManager.ObjectRefs = updatedObjectRefs
	if len(bindingManager.ObjectRefs) == 0 {
		placementPolicy.GetStatus().BindingManager = nil
	}

	if err := hubClient.Status().Update(ctx, placementPolicy); err != nil {
		// Reset the binding manager to its previous state.
		placementPolicy.GetStatus().BindingManager = bindingManagerCopy
		return errors.NewAPIServerError(err, "failed to update placement policy status", false)
	}
	klog.V(2).InfoS("Relinquished the binding manager role from the object reference", "placementPolicy", klog.KObj(placementPolicy), "controllerName", controllerName, "objectRef", objectRef)
	return nil
}

// dryRunPatch verifies that the caller's view of the placement policy is still current by issuing a no-op status
// patch that carries the object's resource version; a stale view yields a conflict error.
func dryRunPatch(ctx context.Context, hubClient client.Client, placementPolicy placementv1alpha1.PlacementPolicyAccessor) error {
	placementToPatch := placementPolicy.DeepCopyObject().(placementv1alpha1.PlacementPolicyAccessor)
	if err := hubClient.Status().Patch(
		ctx,
		placementToPatch,
		client.MergeFromWithOptions(placementPolicy, client.MergeFromWithOptimisticLock{}),
		client.DryRunAll,
	); err != nil {
		wrappedErr := errors.NewAPIServerError(err,
			"failed to complete the dry-run: the current state might be stale, or an unexpected API server error has occurred", false)
		return wrappedErr
	}
	return nil
}
