/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"fmt"
<<<<<<< HEAD
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
)

// extractNumOfClustersFromPolicySnapshot extracts the numOfClusters from the policy snapshot.
func extractNumOfClustersFromPolicySnapshot(policy *fleetv1beta1.ClusterPolicySnapshot) (int, error) {
	numOfClustersStr, ok := policy.Annotations[fleetv1beta1.NumOfClustersAnnotation]
	if !ok {
		return 0, controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot find annotation %s", fleetv1beta1.NumOfClustersAnnotation))
	}

	// Cast the annotation to an integer; throw an error if the cast cannot be completed or the value is negative.
	numOfClusters, err := strconv.Atoi(numOfClustersStr)
	if err != nil || numOfClusters < 0 {
		return 0, controller.NewUnexpectedBehaviorError(fmt.Errorf("invalid annotation %s: Atoi(%s) = %v, %v", fleetv1beta1.NumOfClustersAnnotation, numOfClustersStr, numOfClusters, err))
	}

	return numOfClusters, nil
}

// extractOwnerCRPNameFromPolicySnapshot extracts the name of the owner CRP from the policy snapshot.
func extractOwnerCRPNameFromPolicySnapshot(policy *fleetv1beta1.ClusterPolicySnapshot) (string, error) {
	var owner string
	for _, ownerRef := range policy.OwnerReferences {
		if ownerRef.Kind == utils.CRPV1Beta1GVK.Kind {
=======

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1 "go.goms.io/fleet/apis/v1"
	"go.goms.io/fleet/pkg/utils"
)

// extractOwnerCRPNameFromPolicySnapshot extracts the name of the owner CRP from the policy snapshot.
func extractOwnerCRPNameFromPolicySnapshot(policy *fleetv1.ClusterPolicySnapshot) (string, error) {
	var owner string
	for _, ownerRef := range policy.OwnerReferences {
		if ownerRef.Kind == utils.CRPV1GVK.Kind {
>>>>>>> 2223a3a (Added more scheduler framework logic)
			owner = ownerRef.Name
			break
		}
	}
	if len(owner) == 0 {
		return "", fmt.Errorf("cannot find owner reference for policy snapshot %v", policy.Name)
	}
	return owner, nil
}

// classifyBindings categorizes bindings into three groups:
// * active: active bindings, that is, bindings that are not marked for deletion; and
// * deletedWithDispatcherFinalizer: bindings that are marked for deletion, but still has the dispatcher finalizer present; and
// * deletedWithoutDispatcherFinalizer: bindings that are marked for deletion, and the dispatcher finalizer is already removed.
<<<<<<< HEAD
func classifyBindings(bindings []fleetv1beta1.ClusterResourceBinding) (active, deletedWithDispatcherFinalizer, deletedWithoutDispatcherFinalizer []*fleetv1beta1.ClusterResourceBinding) {
	// Pre-allocate arrays.
	active = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))
	deletedWithDispatcherFinalizer = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))
	deletedWithoutDispatcherFinalizer = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))
=======
func classifyBindings(bindings []fleetv1.ClusterResourceBinding) (active, deletedWithDispatcherFinalizer, deletedWithoutDispatcherFinalizer []*fleetv1.ClusterResourceBinding) {
	// Pre-allocate arrays.
	active = make([]*fleetv1.ClusterResourceBinding, 0, len(bindings))
	deletedWithDispatcherFinalizer = make([]*fleetv1.ClusterResourceBinding, 0, len(bindings))
	deletedWithoutDispatcherFinalizer = make([]*fleetv1.ClusterResourceBinding, 0, len(bindings))
>>>>>>> 2223a3a (Added more scheduler framework logic)

	for idx := range bindings {
		binding := bindings[idx]
		if binding.DeletionTimestamp != nil {
			if controllerutil.ContainsFinalizer(&binding, utils.DispatcherFinalizer) {
				deletedWithDispatcherFinalizer = append(deletedWithDispatcherFinalizer, &binding)
			} else {
				deletedWithoutDispatcherFinalizer = append(deletedWithoutDispatcherFinalizer, &binding)
			}
		} else {
			active = append(active, &binding)
		}
	}

	return active, deletedWithDispatcherFinalizer, deletedWithoutDispatcherFinalizer
}
