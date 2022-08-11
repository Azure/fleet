/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package resourcechange

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/keys"
	"go.goms.io/fleet/pkg/utils/validator"
)

// Reconciler finds the placements that reference to any resource.
type Reconciler struct {
	// the client to write resource updates
	DynamicClient dynamic.Interface

	// RestMapper is used to convert between gvk and gvr
	RestMapper meta.RESTMapper

	// InformerManager holds all the informers that we can use to read from
	InformerManager utils.InformerManager

	// PlacementController exposes the placement queue for the reconciler to push to
	PlacementController controller.Controller

	// Event recorder to indicate the which placement picks up this object
	Recorder record.EventRecorder
}

func (r *Reconciler) Reconcile(ctx context.Context, key controller.QueueKey) (ctrl.Result, error) {
	clusterWideKey, ok := key.(keys.ClusterWideKey)
	if !ok {
		err := fmt.Errorf("got a resource key %+v not of type cluster wide key", key)
		klog.ErrorS(err, "we have encountered a fatal error that can't be retried")
		return ctrl.Result{}, err
	}
	klog.V(3).InfoS("Reconciling object", "obj", clusterWideKey)

	// the clusterObj is set to be the object that the placement direct selects,
	// in the case of a deleted namespace scoped object, the clusterObj is set to be its parent namespace object.
	var clusterObj runtime.Object
	object, isClusterScoped, err := r.getUnstructuredObject(clusterWideKey)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to get unstructured object", "obj", clusterWideKey)
			return ctrl.Result{}, err
		}
		// we have put a finalizer on all selected cluster scope resources, so it's okay to not handle this
		if isClusterScoped {
			klog.V(5).InfoS("Cluster scoped object is deleted", "obj", clusterWideKey)
			return ctrl.Result{}, nil
		}
		nameSpaceObj, err := r.InformerManager.Lister(utils.NamespaceGVR).Get(clusterWideKey.Namespace)
		if err != nil {
			klog.ErrorS(err, "Failed to find the namespace the resource belongs to", "obj", clusterWideKey)
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		klog.V(3).InfoS("A namespace scoped object is deleted, find placement that select its namespace", "obj", clusterWideKey)
		clusterObj = nameSpaceObj
	} else if isClusterScoped {
		clusterObj = object
	} else {
		clusterObj, err = r.InformerManager.Lister(utils.NamespaceGVR).Get(clusterWideKey.Namespace)
		if err != nil {
			klog.ErrorS(err, "Failed to find the namespace the resource belongs to", "obj", clusterWideKey)
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		klog.V(3).InfoS("Find placement that select the namespace that contains a namespace scoped object", "obj", clusterWideKey)
	}
	matchedCrps, isDeleted, err := r.findAffectedPlacements(ctx, clusterObj.DeepCopyObject().(*unstructured.Unstructured), clusterWideKey)
	if isDeleted {
		return ctrl.Result{}, err
	}

	if len(matchedCrps) == 0 {
		klog.V(4).InfoS("change in object does not affect any placement", "obj", clusterWideKey)
		return ctrl.Result{}, nil
	}

	r.Recorder.Event(object, corev1.EventTypeNormal, "try to place this object", fmt.Sprintf("find %d matching placement", len(matchedCrps)))
	// enqueue each CRP object into the CRP controller queue to get reconciled
	for crp := range matchedCrps {
		klog.V(3).InfoS("Change in object triggered placement reconcile", "obj", clusterWideKey, "crp", crp)
		r.PlacementController.Enqueue(crp)
	}

	return ctrl.Result{}, nil
}

// getUnstructuredObject retrieves an unstructured object by its gvknn key, this will hit the informer cache
func (r *Reconciler) getUnstructuredObject(objectKey keys.ClusterWideKey) (runtime.Object, bool, error) {
	restMapping, err := r.RestMapper.RESTMapping(objectKey.GroupKind(), objectKey.Version)
	if err != nil {
		return nil, false, errors.Wrap(err, "Failed to get GVR of object")
	}
	gvr := restMapping.Resource
	isClusterScoped := r.InformerManager.IsClusterScopedResources(gvr)
	if !r.InformerManager.IsInformerSynced(gvr) {
		return nil, isClusterScoped, fmt.Errorf("informer cache for %+v is not synced yet", restMapping.Resource)
	}
	var obj runtime.Object
	if isClusterScoped {
		obj, err = r.InformerManager.Lister(gvr).Get(objectKey.Name)
	} else {
		obj, err = r.InformerManager.Lister(gvr).ByNamespace(objectKey.Namespace).Get(objectKey.Name)
	}
	if err != nil {
		return nil, isClusterScoped, errors.Wrap(err, "failed to get the object")
	}

	return obj, isClusterScoped, nil
}

// findAffectedPlacements find all the placement by which that this object change may affect which includes
// 1. The placements selected this resource (before this change)
// 2. The placements whose selectors will select this resource (after this change)
func (r *Reconciler) findAffectedPlacements(ctx context.Context, clusterObj *unstructured.Unstructured, clusterWideKey keys.ClusterWideKey) (map[string]bool, bool, error) {
	existingPlacements, isDeleted := utils.FindSelectedPlacements(clusterObj)
	restMapping, _ := r.RestMapper.RESTMapping(clusterObj.GroupVersionKind().GroupKind(), clusterObj.GroupVersionKind().Version)
	if isDeleted {
		// enqueue placement first before really delete it
		for _, placement := range existingPlacements {
			klog.V(3).InfoS("deleting object triggered placement reconcile",
				"obj", clusterWideKey, "clusterObj", klog.KObj(clusterObj), "placement", placement)
			r.PlacementController.Enqueue(placement)
		}
		// update the cluster object to remove the finalizer and the placement annotation
		utils.RemoveAllPlacement(clusterObj)
		if _, err := r.DynamicClient.Resource(restMapping.Resource).Update(ctx, clusterObj, metav1.UpdateOptions{}); err != nil {
			klog.ErrorS(err, "Failed to remove the placement finalizer from a cluster scoped resource", "obj", klog.KObj(clusterObj))
			return nil, true, client.IgnoreNotFound(err)
		}
		return nil, true, nil
	}

	matchedCrps, err := r.findMatchingPlacements(clusterObj)
	if err != nil {
		klog.ErrorS(err, "Failed to find the matching cluster resource placement object", "obj", clusterWideKey)
		return nil, false, err
	}
	for _, existingPlacement := range existingPlacements {
		_, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).Get(existingPlacement)
		if apierrors.IsNotFound(err) {
			// only handle not found error as a safety check that the crp in the placement annotation exists
			// in case placement GC miss some resources on race conditions
			utils.RemovePlacement(clusterObj, existingPlacement)
			// update the cluster object to remove the deleted placement from the placement annotation
			// TODO: use a get/update retry loop to solve conflict error
			if _, err := r.DynamicClient.Resource(restMapping.Resource).Update(ctx, clusterObj, metav1.UpdateOptions{}); err != nil {
				klog.ErrorS(err, "Failed to remove the placement from a cluster scoped resource", "obj", klog.KObj(clusterObj))
				return nil, true, client.IgnoreNotFound(err)
			}
			continue
		}
		matchedCrps[existingPlacement] = true
	}
	return matchedCrps, false, nil
}

// findMatchingPlacements finds the placements which will select this resource
func (r *Reconciler) findMatchingPlacements(uObj *unstructured.Unstructured) (map[string]bool, error) {
	placements := make(map[string]bool, 0)
	// we have to list all the CRPs
	crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).List(labels.Everything())
	if err != nil {
		return nil, errors.Wrap(err, "failed to list all the cluster placement")
	}
	for _, crp := range crpList {
		var placement fleetv1alpha1.ClusterResourcePlacement
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(crp.DeepCopyObject().(*unstructured.Unstructured).Object, &placement)
		// TODO: remove after we have webhooks
		if err := validator.ValidateClusterResourcePlacement(&placement); err != nil {
			klog.ErrorS(err, "find one invalid cluster resource placement", "placement", klog.KObj(&placement))
			continue
		}
		// check if object match any placement's resource selectors
		for _, selector := range placement.Spec.ResourceSelectors {
			if matchClusterResourceSelector(selector, uObj) {
				// if there is 1 selector match, it is a placement match, add only once
				placements[placement.Name] = true
				break
			}
		}
	}
	return placements, nil
}

// matchClusterResourceSelector indicates if the resource selector matches the object given its gvk
func matchClusterResourceSelector(selector fleetv1alpha1.ClusterResourceSelector, uObj *unstructured.Unstructured) bool {
	if !matchSelectorGVK(uObj.GetObjectKind().GroupVersionKind(), selector) {
		return false
	}
	if selector.Name == uObj.GetName() {
		return true
	}
	return matchSelectorLabelSelector(uObj.GetLabels(), selector)
}

func matchSelectorGVK(targetGVK schema.GroupVersionKind, selector fleetv1alpha1.ClusterResourceSelector) bool {
	return selector.Group == targetGVK.Group && selector.Version == targetGVK.Version &&
		selector.Kind == targetGVK.Kind
}

func matchSelectorLabelSelector(targetLabels map[string]string, selector fleetv1alpha1.ClusterResourceSelector) bool {
	if selector.LabelSelector == nil {
		// if the labelselector not set, it means select all
		return true
	}
	// we have validated earlier
	s, _ := metav1.LabelSelectorAsSelector(selector.LabelSelector)
	return s.Matches(labels.Set(targetLabels))
}
