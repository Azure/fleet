/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	eventReasonResourceSelected = "ResourceSelected"
)

// Reconciler reconciles a cluster resource placement object
type Reconciler struct {
	// the informer contains the cache for all the resource we need
	InformerManager utils.InformerManager
	RestMapper      meta.RESTMapper

	// DisabledResourceConfig contains all the api resources that we won't select
	DisabledResourceConfig *utils.DisabledResourceConfig

	Recorder record.EventRecorder

	// optimization flag to not have to check the cache sync all the time
	placementInformerSynced     bool
	memberClusterInformerSynced bool
}

func (r *Reconciler) Reconcile(ctx context.Context, key controller.QueueKey) (ctrl.Result, error) {
	name, ok := key.(string)
	if !ok {
		err := fmt.Errorf("get place key %+v not of type string", key)
		klog.ErrorS(err, "we have encountered a fatal error that can't be retried, requeue after a day")
		return ctrl.Result{RequeueAfter: time.Hour * 24}, nil
	}

	placement, err := r.getPlacement(name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "failed to get the cluster resource placement in hub agent", "placement", name)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	//TODO: handle finalizer/GC
	placementCopy := placement.DeepCopy()
	klog.V(2).InfoS("Start to reconcile a ClusterResourcePlacement", "placement", name)
	if err = r.createWorkResources(placementCopy); err != nil {
		klog.ErrorS(err, "failed to create the work resource for this placement", "placement", placement)
		return ctrl.Result{}, err
	}

	_, err = r.selectClusters(placementCopy)
	if err != nil {
		klog.ErrorS(err, "failed to select the clusters ", "placement", placement)
		return ctrl.Result{}, err
	}

	// TODO: place works for each cluster and place them in the cluster scoped namespace

	// Update the status of the placement
	//err = r.InformerManager.GetClient().Resource(utils.ClusterResourcePlacementGVR).Update(ctx, placementCopy, metav1.UpdateOptions{})

	return ctrl.Result{}, err
}

// getPlacement retrieves a ClusterResourcePlacement object by its name, this will hit the informer cache.
func (r *Reconciler) getPlacement(name string) (*fleetv1alpha1.ClusterResourcePlacement, error) {
	if !r.placementInformerSynced && !r.InformerManager.IsInformerSynced(utils.ClusterResourcePlacementGVR) {
		return nil, fmt.Errorf("informer cache for ClusterResourcePlacement is not synced yet")
	}
	r.placementInformerSynced = true
	obj, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).Get(name)
	if err != nil {
		return nil, err
	}
	return obj.(*fleetv1alpha1.ClusterResourcePlacement), nil
}

// createWorkResources selects the resources according to the placement resourceSelectors,
// creates a work obj for the resources and updates the results in its status.
func (r *Reconciler) createWorkResources(placement *fleetv1alpha1.ClusterResourcePlacement) error {
	objects, err := r.gatherSelectedResource(placement)
	if err != nil {
		return err
	}
	klog.V(2).InfoS("Successfully gathered all selected resources", "placement", placement.Name, "number of resources", len(objects))
	placement.Status.SelectedResources = make([]fleetv1alpha1.ResourceIdentifier, 0)
	for _, obj := range objects {
		metaObj, err := meta.Accessor(obj)
		if err != nil {
			// not sure if we can ever get here, just skip this resource
			klog.Warningf("cannot get the name of a runtime object %+v with err %v", obj, err)
			continue
		}
		gvk := obj.GetObjectKind().GroupVersionKind()
		res := fleetv1alpha1.ResourceIdentifier{
			Group:     gvk.Group,
			Version:   gvk.Version,
			Kind:      gvk.Kind,
			Name:      metaObj.GetName(),
			Namespace: metaObj.GetNamespace(),
		}
		placement.Status.SelectedResources = append(placement.Status.SelectedResources, res)
		klog.V(5).InfoS("selected one resource ", "placement", placement.Name, "resource", res)
	}
	r.Recorder.Event(placement, corev1.EventTypeNormal, eventReasonResourceSelected, "successfully gathered all selected resources")

	// TODO: Create works objects

	return nil
}
