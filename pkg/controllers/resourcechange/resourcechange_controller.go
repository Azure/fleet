/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package resourcechange

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
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
	"go.goms.io/fleet/pkg/utils/informer"
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
	InformerManager informer.Manager

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
	klog.V(2).InfoS("Reconciling object", "obj", clusterWideKey)

	// the clusterObj is set to be the object that the placement direct selects,
	// in the case of a deleted namespace scoped object, the clusterObj is set to be its parent namespace object.
	clusterObj, isClusterScoped, err := r.getUnstructuredObject(clusterWideKey)
	switch {
	case apierrors.IsNotFound(err):
		if isClusterScoped {
			// We only care about cluster scoped resources here, and we need to find out which placements have selected this resource.
			// For namespaces resources, we just need to find its parent namespace just like a normal resource.
			return r.triggerAffectedPlacementsForDeletedClusterRes(clusterWideKey)
		}
	case err != nil:
		klog.ErrorS(err, "Failed to get unstructured object", "obj", clusterWideKey)
		return ctrl.Result{}, err
	}
	// we will use the parent namespace object to search for the affected placements
	if !isClusterScoped {
		clusterObj, err = r.InformerManager.Lister(utils.NamespaceGVR).Get(clusterWideKey.Namespace)
		if err != nil {
			klog.ErrorS(err, "Failed to find the namespace the resource belongs to", "obj", clusterWideKey)
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		klog.V(2).InfoS("Find placement that select the namespace that contains a namespace scoped object", "obj", clusterWideKey)
	}
	matchedCrps, err := r.findAffectedPlacements(clusterObj.DeepCopyObject().(*unstructured.Unstructured))
	if err != nil {
		klog.ErrorS(err, "Failed to find the affected placement the resource change triggers", "obj", clusterWideKey)
		return ctrl.Result{}, err
	}
	if len(matchedCrps) == 0 {
		klog.V(2).InfoS("change in object does not affect any placement", "obj", clusterWideKey)
		return ctrl.Result{}, nil
	}
	// enqueue each CRP object into the CRP controller queue to get reconciled
	for crp := range matchedCrps {
		klog.V(2).InfoS("Change in object triggered placement reconcile", "obj", clusterWideKey, "crp", crp)
		r.PlacementController.Enqueue(crp)
	}

	return ctrl.Result{}, nil
}

// triggerAffectedPlacementsForDeletedClusterRes find the affected placements for a given deleted cluster scoped resources
func (r *Reconciler) triggerAffectedPlacementsForDeletedClusterRes(res keys.ClusterWideKey) (ctrl.Result, error) {
	crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).List(labels.Everything())
	if err != nil {
		klog.ErrorS(err, "failed to list all the cluster placement", "obj", res)
		return ctrl.Result{}, err
	}
	return r.findPlacementsSelectedDeletedRes(res, crpList)
}

// findPlacementsSelectedDeletedRes finds the placements which has selected this resource before it's deleted
func (r *Reconciler) findPlacementsSelectedDeletedRes(res keys.ClusterWideKey, crpList []runtime.Object) (ctrl.Result, error) {
	matchedCrps := make([]string, 0)
	for _, crp := range crpList {
		var placement fleetv1alpha1.ClusterResourcePlacement
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(crp.(*unstructured.Unstructured).Object, &placement)
		for _, selectedRes := range placement.Status.SelectedResources {
			if selectedRes == res.ResourceIdentifier {
				matchedCrps = append(matchedCrps, placement.Name)
				break
			}
		}
	}
	if len(matchedCrps) == 0 {
		klog.V(2).InfoS("change in deleted object does not affect any placement", "obj", res)
		return ctrl.Result{}, nil
	}
	for _, crp := range matchedCrps {
		klog.V(2).InfoS("change in deleted object triggered placement reconcile", "obj", res, "crp", crp)
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
	isClusterScoped := r.InformerManager.IsClusterScopedResources(objectKey.GroupVersionKind())
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
func (r *Reconciler) findAffectedPlacements(res *unstructured.Unstructured) (map[string]bool, error) {
	// we have to list all the CRPs
	crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).List(labels.Everything())
	if err != nil {
		return nil, errors.Wrap(err, "failed to list all the cluster placement")
	}
	return collectAllAffectedPlacements(res, crpList), nil
}

// collectAllAffectedPlacements goes through all the placements and collect the ones whose resource selector matches the object given its gvk
func collectAllAffectedPlacements(res *unstructured.Unstructured, crpList []runtime.Object) map[string]bool {
	placements := make(map[string]bool)
	for _, crp := range crpList {
		match := false
		var placement fleetv1alpha1.ClusterResourcePlacement
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(crp.DeepCopyObject().(*unstructured.Unstructured).Object, &placement)
		// TODO: remove after we add validation webhooks
		if err := validator.ValidateClusterResourcePlacement(&placement); err != nil {
			klog.ErrorS(err, "find one invalid cluster resource placement", "placement", klog.KObj(&placement))
			continue
		}
		// find the placements selected this resource (before this change)
		for _, selectedRes := range placement.Status.SelectedResources {
			if selectedRes.Group == res.GroupVersionKind().Group && selectedRes.Version == res.GroupVersionKind().Version &&
				selectedRes.Kind == res.GroupVersionKind().Kind && selectedRes.Name == res.GetName() {
				placements[placement.Name] = true
				match = true
				break
			}
		}
		if match {
			continue
		}
		// check if object match any placement's resource selectors
		for _, selector := range placement.Spec.ResourceSelectors {
			if !matchSelectorGVK(res.GetObjectKind().GroupVersionKind(), selector) {
				continue
			}
			// if there is 1 selector match, it is a placement match, add only once
			if selector.Name == res.GetName() {
				placements[placement.Name] = true
				break
			}
			if matchSelectorLabelSelector(res.GetLabels(), selector) {
				placements[placement.Name] = true
				break
			}
		}
	}
	return placements
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
