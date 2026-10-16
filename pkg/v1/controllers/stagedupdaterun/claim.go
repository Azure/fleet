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

	"k8s.io/klog/v2"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/bindingmanager"
)

func stagedUpdateRunObjRef(stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor) placementv1alpha1.ObjectReference {
	var kind string
	if stagedUpdateRun.GetNamespace() == "" {
		kind = rolloutv1alpha1.ClusterStagedUpdateRunKind
	} else {
		kind = rolloutv1alpha1.StagedUpdateRunKind
	}
	return placementv1alpha1.ObjectReference{
		APIGroup:   rolloutv1alpha1.GroupVersion.Group,
		APIVersion: rolloutv1alpha1.GroupVersion.Version,
		Kind:       kind,
		Namespace:  stagedUpdateRun.GetNamespace(),
		Name:       stagedUpdateRun.GetName(),
	}
}

func (r *Reconciler) claimAsBindingManagerFor(
	ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
) (claimed bool, err error) {
	stagedUpdateRunObjRef := stagedUpdateRunObjRef(stagedUpdateRun)
	claimed, err = bindingmanager.ClaimRoleAs(ctx, r.hubClient, placementPolicy, controllerName, stagedUpdateRunObjRef)
	if err != nil {
		wrappedErr := errors.Wraps(err, "",
			"stagedUpdateRun", klog.KObj(stagedUpdateRun),
			"placementPolicyName", stagedUpdateRun.GetSpec().PlacementPolicyName,
			"controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to claim binding manager role", errors.Args(wrappedErr)...)
		return false, wrappedErr
	}
	if !claimed {
		klog.V(2).InfoS("Cannot claim the binding manager role for now; will retry later",
			"stagedUpdateRun", klog.KObj(stagedUpdateRun),
			"placementPolicyName", stagedUpdateRun.GetSpec().PlacementPolicyName,
			"controller", controllerName)
		return false, nil
	}
	klog.V(2).InfoS("Successfully claimed the binding manager role for the staged update run",
		"stagedUpdateRun", klog.KObj(stagedUpdateRun),
		"placementPolicyName", stagedUpdateRun.GetSpec().PlacementPolicyName,
		"controller", controllerName)
	return true, nil
}
