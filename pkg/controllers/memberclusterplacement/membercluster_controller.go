/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package memberclusterplacement

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
)

// Reconciler reconciles a MemberCluster object
type Reconciler struct {
	// the informer contains the cache for all the resources we need
	InformerManager utils.InformerManager

	// PlacementController maintains a rate limited queue which used to store
	// the name of the clusterResourcePlacement and a reconcile function to consume the items in queue.
	PlacementController controller.Controller
}

func (r *Reconciler) Reconcile(ctx context.Context, key controller.QueueKey) (ctrl.Result, error) {
	memberClusterName, ok := key.(string)
	if !ok {
		err := fmt.Errorf("got a resource key %+v not of type cluster wide key", key)
		klog.ErrorS(err, "we have encountered a fatal error that can't be retried")
		return ctrl.Result{}, err
	}

	klog.V(2).InfoS("Start to reconcile a memberCluster to enqueue placement events", "memberCluster", memberClusterName)
	_, err := r.InformerManager.Lister(utils.MemberClusterGVR).Get(memberClusterName)
	if err != nil {
		klog.ErrorS(err, "failed to get the member cluster", "memberCluster", memberClusterName)
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	// we just blindly list all the placements for now
	// TODO: gather all the placements that scheduled resources to this cluster before the change
	crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).List(labels.Everything())
	if err != nil {
		klog.ErrorS(err, "failed to list all the cluster resource placement", "memberCluster", memberClusterName)
		return ctrl.Result{}, err
	}

	for i, crp := range crpList {
		placement := crp.(*unstructured.Unstructured)
		klog.V(4).InfoS("enqueue a placement to reconcile", "memberCluster", memberClusterName, "placement", klog.KObj(placement))
		r.PlacementController.Enqueue(crpList[i])
	}

	return ctrl.Result{}, nil
}
