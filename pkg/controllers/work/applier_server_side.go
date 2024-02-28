/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package work

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
)

// ServerSideApplier applies the manifest to the cluster using server side apply.
type ServerSideApplier struct {
	HubClient          client.Client
	WorkNamespace      string
	SpokeDynamicClient dynamic.Interface
}

// ApplyUnstructured applies the manifest to the cluster using server side apply according to the given apply strategy.
func (applier *ServerSideApplier) ApplyUnstructured(ctx context.Context, applyStrategy *fleetv1beta1.ApplyStrategy, gvr schema.GroupVersionResource, manifestObj *unstructured.Unstructured) (*unstructured.Unstructured, ApplyAction, error) {
	force := applyStrategy.ServerSideApplyConfig.ForceConflicts

	manifestRef := klog.KObj(manifestObj)
	// support resources with generated name
	if manifestObj.GetName() == "" && manifestObj.GetGenerateName() != "" {
		klog.V(2).InfoS("Create the resource with generated name regardless", "gvr", gvr, "manifest", manifestRef)
		return serverSideApply(ctx, applier.SpokeDynamicClient, force, gvr, manifestObj)
	}

	curObj, err := applier.SpokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).Get(ctx, manifestObj.GetName(), metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		return serverSideApply(ctx, applier.SpokeDynamicClient, force, gvr, manifestObj)
	case err != nil:
		return nil, errorApplyAction, controller.NewAPIServerError(false, err)
	}

	conflictedWork, err := findConflictedWork(ctx, applier.HubClient, applier.WorkNamespace, applyStrategy, curObj.GetOwnerReferences())
	if err != nil {
		return nil, errorApplyAction, err
	}
	if conflictedWork != nil {
		placement := conflictedWork.Labels[fleetv1beta1.CRPTrackingLabel]
		err := fmt.Errorf(conflictBetweenPlacementsErrorFormat, placement)
		klog.ErrorS(err, "Skip applying a manifest managed by another placement but with different apply strategy",
			"gvr", gvr, "manifest", manifestRef, "applyStrategy", applyStrategy,
			"conflictedWork", conflictedWork.Name, "conflictedPlacement", placement, "conflictedWorkApplyStrategy", conflictedWork.Spec.ApplyStrategy)
		return nil, applyConflictBetweenPlacements, controller.NewUserError(err)
	}
	return serverSideApply(ctx, applier.SpokeDynamicClient, force, gvr, manifestObj)
}
