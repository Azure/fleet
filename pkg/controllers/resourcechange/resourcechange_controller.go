/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package resourcechange

import (
	"context"
	"fmt"
	"time"

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

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
	"go.goms.io/fleet/pkg/utils/keys"
)

// Reconciler finds the placements that reference to any resource.
type Reconciler struct {
	// the client to write resource updates
	DynamicClient dynamic.Interface

	// RestMapper is used to convert between gvk and gvr
	RestMapper meta.RESTMapper

	// InformerManager holds all the informers that we can use to read from
	InformerManager informer.Manager

	// PlacementControllerV1Alpha1 exposes the placement queue for the v1alpha1 reconciler to push to
	PlacementControllerV1Alpha1 controller.Controller

	// PlacementControllerV1Beta1 exposes the placement queue for the v1beta1 reconciler to push to.
	PlacementControllerV1Beta1 controller.Controller

	// Event recorder to indicate the which placement picks up this object
	Recorder record.EventRecorder
}

func (r *Reconciler) Reconcile(_ context.Context, key controller.QueueKey) (ctrl.Result, error) {
	startTime := time.Now()
	clusterWideKey, ok := key.(keys.ClusterWideKey)
	if !ok {
		err := fmt.Errorf("got a resource key %+v not of type cluster wide key", key)
		klog.ErrorS(err, "we have encountered a fatal error that can't be retried")
		return ctrl.Result{}, err
	}
	klog.V(2).InfoS("Reconciling object", "obj", clusterWideKey)

	// add latency log
	defer func() {
		klog.V(2).InfoS("ResourceChange reconciliation loop ends", "obj", clusterWideKey, "latency", time.Since(startTime).Milliseconds())
	}()

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

	return r.triggerAffectedPlacementsForUpdatedClusterRes(clusterWideKey, clusterObj.(*unstructured.Unstructured))
}

// triggerAffectedPlacementsForDeletedClusterRes find the affected placements for a given deleted cluster scoped resources
func (r *Reconciler) triggerAffectedPlacementsForDeletedClusterRes(res keys.ClusterWideKey) (ctrl.Result, error) {
	if r.PlacementControllerV1Alpha1 != nil {
		crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementV1Alpha1GVR).List(labels.Everything())
		if err != nil {
			klog.ErrorS(err, "failed to list all the v1alpha1 cluster placement", "obj", res)
			return ctrl.Result{}, err
		}

		r.findPlacementsSelectedDeletedResV1Alpha1(res, crpList)
	}

	if r.PlacementControllerV1Beta1 != nil {
		crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).List(labels.Everything())
		if err != nil {
			klog.ErrorS(err, "failed to list all the v1beta1 cluster placement", "obj", res)
			return ctrl.Result{}, err
		}

		r.findPlacementsSelectedDeletedResV1Beta1(res, crpList)
	}

	return ctrl.Result{}, nil
}

// findPlacementsSelectedDeletedResV1Alpha1 finds v1alpha1 placements which has selected this resource before it's deleted
func (r *Reconciler) findPlacementsSelectedDeletedResV1Alpha1(res keys.ClusterWideKey, crpList []runtime.Object) {
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
		klog.V(2).InfoS("change in deleted object does not affect any v1alpha1 placement", "obj", res)
		return
	}

	for _, crp := range matchedCrps {
		klog.V(2).InfoS("change in deleted object triggered v1alpha1 placement reconcile", "obj", res, "crp", crp)
		r.PlacementControllerV1Alpha1.Enqueue(crp)
	}
}

// findPlacementsSelectedDeletedResV1Beta1 finds v1beta1 placements which has selected this resource before it's deleted
func (r *Reconciler) findPlacementsSelectedDeletedResV1Beta1(res keys.ClusterWideKey, crpList []runtime.Object) {
	matchedCrps := make([]string, 0)
	for _, crp := range crpList {
		var placement placementv1beta1.ClusterResourcePlacement
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(crp.(*unstructured.Unstructured).Object, &placement)
		for _, selectedRes := range placement.Status.SelectedResources {
			// Perform an expedient conversion as the cluster-wide key is currently bound
			// to v1alpha1 APIs.
			//
			// TO-DO: decouple the key struct from specific API versions.
			expectedRes := placementv1beta1.ResourceIdentifier{
				Group:     res.Group,
				Version:   res.Version,
				Kind:      res.Kind,
				Name:      res.Name,
				Namespace: res.Namespace,
			}
			if selectedRes == expectedRes {
				matchedCrps = append(matchedCrps, placement.Name)
				break
			}
		}
	}

	if len(matchedCrps) == 0 {
		klog.V(2).InfoS("change in deleted object does not affect any v1beta1 placement", "obj", res)
		return
	}

	for _, crp := range matchedCrps {
		klog.V(2).InfoS("change in deleted object triggered v1beta1 placement reconcile", "obj", res, "crp", crp)
		r.PlacementControllerV1Beta1.Enqueue(crp)
	}
}

// getUnstructuredObject retrieves an unstructured object by its gvknn key, this will hit the informer cache
func (r *Reconciler) getUnstructuredObject(objectKey keys.ClusterWideKey) (runtime.Object, bool, error) {
	restMapping, err := r.RestMapper.RESTMapping(objectKey.GroupKind(), objectKey.Version)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get GVR of object: %w", err)
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
		return nil, isClusterScoped, fmt.Errorf("failed to get the object: %w", err)
	}

	return obj, isClusterScoped, nil
}

// triggerAffectedPlacementsForUpdatedClusterRes find the affected placements for a given updated cluster scoped resources.
func (r *Reconciler) triggerAffectedPlacementsForUpdatedClusterRes(key keys.ClusterWideKey, res *unstructured.Unstructured) (ctrl.Result, error) {
	if r.PlacementControllerV1Alpha1 != nil {
		// List all the CRPs.
		crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementV1Alpha1GVR).List(labels.Everything())
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to list all the v1alpha1 cluster placements: %w", err)
		}

		// Find all matching CRPs.
		matchedCRPs := collectAllAffectedPlacementsV1Alpha1(res, crpList)
		if len(matchedCRPs) == 0 {
			klog.V(2).InfoS("change in object does not affect any v1alpha1 placement", "obj", key)
			return ctrl.Result{}, nil
		}

		// Enqueue the CRPs for reconciliation.
		for crp := range matchedCRPs {
			klog.V(2).InfoS("Change in object triggered v1alpha1 placement reconcile", "obj", key, "crp", crp)
			r.PlacementControllerV1Alpha1.Enqueue(crp)
		}
	}

	if r.PlacementControllerV1Beta1 != nil {
		// List all the CRPs.
		crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).List(labels.Everything())
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to list all the v1beta1 cluster placements: %w", err)
		}

		// Find all matching CRPs.
		matchedCRPs := collectAllAffectedPlacementsV1Beta1(res, crpList)
		if len(matchedCRPs) == 0 {
			klog.V(2).InfoS("change in object does not affect any v1beta1 placement", "obj", key)
			return ctrl.Result{}, nil
		}

		// Enqueue the CRPs for reconciliation.
		for crp := range matchedCRPs {
			klog.V(2).InfoS("Change in object triggered v1beta1 placement reconcile", "obj", key, "crp", crp)
			r.PlacementControllerV1Beta1.Enqueue(crp)
		}
	}

	return ctrl.Result{}, nil
}

// collectAllAffectedPlacementsV1Alpha1 goes through all v1alpha1 placements and collect the ones whose resource selector matches the object given its gvk
func collectAllAffectedPlacementsV1Alpha1(res *unstructured.Unstructured, crpList []runtime.Object) map[string]bool {
	placements := make(map[string]bool)
	for _, crp := range crpList {
		match := false
		var placement fleetv1alpha1.ClusterResourcePlacement
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(crp.DeepCopyObject().(*unstructured.Unstructured).Object, &placement)

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
			if !matchSelectorGVKV1Alpha1(res.GetObjectKind().GroupVersionKind(), selector) {
				continue
			}
			// if there is 1 selector match, it is a placement match, add only once
			if selector.Name != "" {
				if selector.Name == res.GetName() {
					placements[placement.Name] = true
					break
				}
			} else if matchSelectorLabelSelectorV1Alpha1(res.GetLabels(), selector) {
				placements[placement.Name] = true
				break
			}
		}
	}
	return placements
}

// collectAllAffectedPlacementsV1Beta1 goes through all v1beta1 placements and collect the ones whose resource selector matches the object given its gvk
func collectAllAffectedPlacementsV1Beta1(res *unstructured.Unstructured, crpList []runtime.Object) map[string]bool {
	placements := make(map[string]bool)
	for _, crp := range crpList {
		match := false
		var placement placementv1beta1.ClusterResourcePlacement
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(crp.DeepCopyObject().(*unstructured.Unstructured).Object, &placement)

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
			if !matchSelectorGVKV1Beta1(res.GetObjectKind().GroupVersionKind(), selector) {
				continue
			}
			// if there is 1 selector match, it is a placement match, add only once
			if selector.Name != "" {
				if selector.Name == res.GetName() {
					placements[placement.Name] = true
					break
				}
			} else if matchSelectorLabelSelectorV1Beta1(res.GetLabels(), selector) {
				placements[placement.Name] = true
				break
			}
		}
	}
	return placements
}

func matchSelectorGVKV1Alpha1(targetGVK schema.GroupVersionKind, selector fleetv1alpha1.ClusterResourceSelector) bool {
	return selector.Group == targetGVK.Group && selector.Version == targetGVK.Version &&
		selector.Kind == targetGVK.Kind
}

func matchSelectorGVKV1Beta1(targetGVK schema.GroupVersionKind, selector placementv1beta1.ClusterResourceSelector) bool {
	return selector.Group == targetGVK.Group && selector.Version == targetGVK.Version &&
		selector.Kind == targetGVK.Kind
}

func matchSelectorLabelSelectorV1Alpha1(targetLabels map[string]string, selector fleetv1alpha1.ClusterResourceSelector) bool {
	if selector.LabelSelector == nil {
		// if the labelselector not set, it means select all
		return true
	}
	// we have validated earlier
	s, _ := metav1.LabelSelectorAsSelector(selector.LabelSelector)
	return s.Matches(labels.Set(targetLabels))
}

func matchSelectorLabelSelectorV1Beta1(targetLabels map[string]string, selector placementv1beta1.ClusterResourceSelector) bool {
	if selector.LabelSelector == nil {
		// if the labelselector not set, it means select all
		return true
	}
	// we have validated earlier
	s, _ := metav1.LabelSelectorAsSelector(selector.LabelSelector)
	return s.Matches(labels.Set(targetLabels))
}
