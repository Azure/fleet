/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package memberclusterplacement

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
)

// Reconciler reconciles a MemberCluster object
type Reconciler struct {
	// the informer contains the cache for all the resources we need
	InformerManager informer.Manager

	// PlacementController maintains a rate limited queue which used to store
	// the name of the clusterResourcePlacement and a reconcile function to consume the items in queue.
	PlacementController controller.Controller
}

func (r *Reconciler) Reconcile(_ context.Context, key controller.QueueKey) (ctrl.Result, error) {
	startTime := time.Now()
	memberClusterName, ok := key.(string)
	if !ok {
		err := fmt.Errorf("got a resource key %+v not of type namespaced key", key)
		klog.ErrorS(err, "we have encountered a fatal error that can't be retried")
		return ctrl.Result{}, err
	}

	// add latency log
	defer func() {
		klog.V(2).InfoS("MemberClusterPlacement reconciliation loop ends", "memberCluster", memberClusterName, "latency", time.Since(startTime).Milliseconds())
	}()

	klog.V(2).InfoS("Start to reconcile a member cluster to enqueue placement events", "memberCluster", memberClusterName)
	mObj, err := r.InformerManager.Lister(utils.MCV1Alpha1GVR).Get(memberClusterName)
	if err != nil {
		klog.ErrorS(err, "failed to get the member cluster", "memberCluster", memberClusterName)
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		mObj = nil //guard against unexpected informer lib behavior
	}
	crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementV1Alpha1GVR).List(labels.Everything())
	if err != nil {
		klog.ErrorS(err, "failed to list all the cluster resource placement", "memberCluster", memberClusterName)
		return ctrl.Result{}, err
	}

	for i, crp := range crpList {
		uObj := crp.(*unstructured.Unstructured).DeepCopy()
		var placement fleetv1alpha1.ClusterResourcePlacement
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.Object, &placement)
		if err != nil {
			klog.ErrorS(err, "failed to convert a cluster resource placement", "memberCluster", memberClusterName, "crp", uObj.GetName())
			return ctrl.Result{}, err
		}
		if mObj == nil {
			// This is a corner case that the member cluster is deleted before we handle its status change. We can't use match since we don't have its label.
			klog.V(2).InfoS("enqueue a placement to reconcile for a deleted member cluster", "memberCluster", memberClusterName, "placement", klog.KObj(&placement))
			r.PlacementController.Enqueue(crpList[i])
		} else if matchPlacement(&placement, mObj.(*unstructured.Unstructured).DeepCopy()) {
			klog.V(2).InfoS("enqueue a placement to reconcile", "memberCluster", memberClusterName, "placement", klog.KObj(&placement))
			r.PlacementController.Enqueue(crpList[i])
		}
	}

	return ctrl.Result{}, nil
}

// matchPlacement check if a crp will or already selected a memberCluster
func matchPlacement(placement *fleetv1alpha1.ClusterResourcePlacement, memberCluster *unstructured.Unstructured) bool {
	placementObj := klog.KObj(placement)
	// check if the placement already selected the member cluster
	for _, selectedCluster := range placement.Status.TargetClusters {
		if selectedCluster == memberCluster.GetName() {
			return true
		}
	}
	// no policy set
	if placement.Spec.Policy == nil {
		klog.V(2).InfoS("find a matching placement with no policy",
			"memberCluster", memberCluster.GetName(), "placement", placementObj)
		return true
	}

	// a fix list of clusters set, this takes precedence over the affinity
	if len(placement.Spec.Policy.ClusterNames) != 0 {
		for _, clusterName := range placement.Spec.Policy.ClusterNames {
			if clusterName == memberCluster.GetName() {
				klog.V(2).InfoS("find a matching placement with a list of cluster names",
					"memberCluster", memberCluster.GetName(), "placement", placementObj)
				return true
			}
		}
		return false
	}
	// no cluster affinity set
	if placement.Spec.Policy.Affinity == nil || placement.Spec.Policy.Affinity.ClusterAffinity == nil ||
		len(placement.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms) == 0 {
		klog.V(2).InfoS("find a matching placement with no cluster affinity",
			"memberCluster", memberCluster.GetName(), "placement", placementObj)
		return true
	}
	// check if member cluster match any placement's cluster selectors
	for _, clusterSelector := range placement.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms {
		s, err := metav1.LabelSelectorAsSelector(&clusterSelector.LabelSelector)
		if err != nil {
			// should not happen after we have webhooks
			klog.ErrorS(err, "found a mal-formatted placement", "placement", placementObj, "selector", clusterSelector.LabelSelector)
			continue
		}
		if s.Matches(labels.Set(memberCluster.GetLabels())) {
			klog.V(2).InfoS("find a matching placement with label selector",
				"memberCluster", memberCluster.GetName(), "placement", placementObj, "selector", clusterSelector.LabelSelector)
			return true
		}
	}
	return false
}
