/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"fmt"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
)

// classifyBindings categorizes bindings into the following groups:
//   - active: active bindings, that is, bindings that should be (though may not necessarily have been) picked up by the dispatcher;
//   - creating: bindings that are being created, that is, bindings that should not be picked up by the dispatcher yet,
//     but have a target cluster associated;
//   - creatingWithoutTargetCluster: bindings that are being created, that is bindings that should not picked up by the dispatcher yet,
//     and do not have a target cluster assigned.
//   - deleted: bindings that are marked for deletion.
func classifyBindings(bindings []fleetv1beta1.ClusterResourceBinding) (active, creating, creatingWithoutTargetCluster, deleted []*fleetv1beta1.ClusterResourceBinding, err error) {
	// Pre-allocate arrays.
	active = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))
	creating = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))
	creatingWithoutTargetCluster = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))
	deleted = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))

	for idx := range bindings {
		binding := bindings[idx]

		_, activeLabelExists := binding.Labels[fleetv1beta1.ActiveBindingLabel]
		_, creatingLabelExists := binding.Labels[fleetv1beta1.CreatingBindingLabel]
		_, obsoleteLabelExists := binding.Labels[fleetv1beta1.ObsoleteBindingLabel]
		_, noTargetClusterLabelExists := binding.Labels[fleetv1beta1.NoTargetClusterBindingLabel]

		// Note that this utility assumes that label mutual-exclusivity is always enforced.
		switch {
		case binding.DeletionTimestamp != nil:
			deleted = append(deleted, &binding)
		case activeLabelExists:
			active = append(active, &binding)
		case creatingLabelExists && !noTargetClusterLabelExists:
			creating = append(creating, &binding)
		case creatingLabelExists && noTargetClusterLabelExists:
			creatingWithoutTargetCluster = append(creatingWithoutTargetCluster, &binding)
		case obsoleteLabelExists:
			// Do nothing.
			//
			// The scheduler cares not for obsolete bindings.
		default:
			// Normally this branch is never run, as a binding that has not been marked for deletion is
			// always of one of the following states:
			// active, creating, and obsolete
			return nil, nil, nil, nil, controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to classify bindings: an binding %s has no desired labels", binding.Name))
		}
	}

	return active, creating, creatingWithoutTargetCluster, deleted, nil
}
