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

package stagedupdaterun

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/bindingmanager"
)

const (
	stagedUpdateRunCleanupFinalizer = "rollout.kubefleet.dev/staged-update-run-cleanup"
)

func (r *Reconciler) cleanup(ctx context.Context, stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor) error {
	// Note that approval requests are created with owner references; once the staged update run is deleted,
	// the approval requests will be automatically garbage collected.

	// Give up the binding manager role for the linked placement policy, if currently held.
	placementPolicyAccessor, err := r.retrieveLinkedPlacementPolicy(ctx, stagedUpdateRun)
	switch {
	case apierrors.IsNotFound(err):
		// The linked placement policy no longer exists; there is no binding manager role to relinquish, so take no action.
	case err != nil:
		return errors.Wraps(err, "failed to retrieve the linked placement policy")
	default:
		// Successfully retrieved the linked placement policy; relinquish the binding manager role if currently held.
		stagedUpdateRunObjRef := stagedUpdateRunObjRef(stagedUpdateRun)
		if err := bindingmanager.RelinquishRoleFor(ctx, r.hubClient, placementPolicyAccessor, controllerName, stagedUpdateRunObjRef); err != nil {
			return errors.Wraps(err, "failed to relinquish the binding manager role")
		}
	}

	// Drop the cleanup finalizer so that the staged update run object can be deleted.
	if controllerutil.ContainsFinalizer(stagedUpdateRun, stagedUpdateRunCleanupFinalizer) {
		controllerutil.RemoveFinalizer(stagedUpdateRun, stagedUpdateRunCleanupFinalizer)
		if err := r.hubClient.Update(ctx, stagedUpdateRun); err != nil {
			return errors.NewAPIServerError(err, "failed to update staged update run object", false)
		}
	}

	return nil
}

func (r *Reconciler) ensureCleanupFinalizer(ctx context.Context, stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor) error {
	if controllerutil.ContainsFinalizer(stagedUpdateRun, stagedUpdateRunCleanupFinalizer) {
		return nil
	}

	controllerutil.AddFinalizer(stagedUpdateRun, stagedUpdateRunCleanupFinalizer)
	if err := r.hubClient.Update(ctx, stagedUpdateRun); err != nil {
		return errors.NewAPIServerError(err, "failed to update staged update run object", false)
	}

	klog.V(2).InfoS("Added cleanup finalizer to staged update run object", "stagedUpdateRun", klog.KObj(stagedUpdateRun))
	return nil
}
