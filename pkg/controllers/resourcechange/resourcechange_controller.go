/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package resourcechange

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/keys"
)

// Reconciler finds the placements that reference to any resource.
type Reconciler struct {
	// Client is used to retrieve objects, it is often more convenient than lister.
	Client client.Client

	Recorder record.EventRecorder

	// DynamicClient used to fetch arbitrary resources.
	DynamicClient dynamic.Interface

	RestMapper meta.RESTMapper

	InformerManager utils.InformerManager

	// the handler of the placement controller
	PlacementController controller.Controller
}

func (r *Reconciler) Reconcile(ctx context.Context, key controller.QueueKey) (ctrl.Result, error) {
	clusterWideKey, ok := key.(keys.ClusterWideKey)
	if !ok {
		err := fmt.Errorf("got a resource key %+v not of type cluster wide key", key)
		klog.ErrorS(err, "we have encountered a fatal error that can't be retried")
		return ctrl.Result{}, err
	}
	klog.V(2).InfoS("Reconciling object", "key", clusterWideKey)

	object, err := r.getUnstructuredObject(ctx, clusterWideKey)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to get unstructured object", "key", clusterWideKey)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	_, err = r.findMatchedPolicies(object)
	return ctrl.Result{}, err
}

// getUnstructuredObject retrieves an unstructured object by its gvknn key, this will hit the informer cache
func (r *Reconciler) getUnstructuredObject(ctx context.Context, objectKey keys.ClusterWideKey) (*unstructured.Unstructured, error) {
	restMapping, err := r.RestMapper.RESTMapping(objectKey.GroupKind(), objectKey.Version)
	if err != nil {
		klog.Errorf("Failed to get GVR of object: %s, error: %v", objectKey, err)
		return nil, err
	}
	gvr := restMapping.Resource
	if !r.InformerManager.IsInformerSynced(gvr) {
		return nil, fmt.Errorf("informer cache for %+v is not synced yet", restMapping.Resource)
	}

	lister := r.InformerManager.Lister(gvr)
	obj, err := lister.ByNamespace(objectKey.Namespace).Get(objectKey.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, err
		}
		// print logs only for real error.
		klog.ErrorS(err, "Failed to get obj", "object", objectKey)
		return nil, err
	}
	return obj.(*unstructured.Unstructured), nil
}

// findMatchedPolicies find all the polices that this unstructured object is referenced
func (r *Reconciler) findMatchedPolicies(object *unstructured.Unstructured) ([]*fleetv1alpha1.ClusterResourcePlacement, error) {
	return nil, nil
}
