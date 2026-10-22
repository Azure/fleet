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

package autorollout

import (
	"context"

	"k8s.io/klog/v2"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/bindingmanager"
)

func holdsBindingManagerRole(placementPolicy placementv1alpha1.PlacementPolicyAccessor) bool {
	bindingManager := placementPolicy.GetStatus().BindingManager
	return bindingManager != nil && bindingManager.ControllerName == controllerName
}

// placementPolicyObjRef returns an object reference to the placement policy, which is the object that
// the auto-rollout controller manages bindings on behalf of.
func placementPolicyObjRef(placementPolicy placementv1alpha1.PlacementPolicyAccessor) placementv1alpha1.ObjectReference {
	var kind string
	if placementPolicy.GetNamespace() == "" {
		kind = placementv1alpha1.ClusterPlacementPolicyKind
	} else {
		kind = placementv1alpha1.PlacementPolicyKind
	}
	return placementv1alpha1.ObjectReference{
		APIGroup:   placementv1alpha1.GroupVersion.Group,
		APIVersion: placementv1alpha1.GroupVersion.Version,
		Kind:       kind,
		Namespace:  placementPolicy.GetNamespace(),
		Name:       placementPolicy.GetName(),
	}
}

// claimAsBindingManagerFor claims the binding manager role for the placement policy on behalf of the
// auto-rollout controller.
//
// This method returns (true, nil) if the role has been claimed; (false, nil) if another source currently holds the
// role and the claim should be retried later; and (false, err) if an error occurs during the attempt.
func (r *Reconciler) claimAsBindingManagerFor(ctx context.Context, placementPolicy placementv1alpha1.PlacementPolicyAccessor) (bool, error) {
	claimed, err := bindingmanager.ClaimRoleAs(ctx, r.hubClient, placementPolicy, controllerName, placementPolicyObjRef(placementPolicy))
	if err != nil {
		wrappedErr := errors.Wraps(err, "", "placementPolicy", klog.KObj(placementPolicy), "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to claim binding manager role", errors.Args(wrappedErr)...)
		return false, wrappedErr
	}
	if !claimed {
		klog.V(2).InfoS("Cannot claim the binding manager role for now; will retry later",
			"placementPolicy", klog.KObj(placementPolicy), "controller", controllerName)
		return false, nil
	}
	klog.V(2).InfoS("Successfully claimed the binding manager role for the placement policy",
		"placementPolicy", klog.KObj(placementPolicy), "controller", controllerName)
	return true, nil
}

// relinquishBindingManagerRole gives up the binding manager role for the placement policy on behalf of the
// auto-rollout controller, if the role is currently held.
func (r *Reconciler) relinquishBindingManagerRole(ctx context.Context, placementPolicy placementv1alpha1.PlacementPolicyAccessor) error {
	if err := bindingmanager.RelinquishRoleFor(ctx, r.hubClient, placementPolicy, controllerName, placementPolicyObjRef(placementPolicy)); err != nil {
		wrappedErr := errors.Wraps(err, "", "placementPolicy", klog.KObj(placementPolicy), "controller", controllerName)
		klog.ErrorS(wrappedErr, "Failed to relinquish binding manager role", errors.Args(wrappedErr)...)
		return wrappedErr
	}
	return nil
}
