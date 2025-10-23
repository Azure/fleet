/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/keys"
)

// Reconciler finds the placements that reference to any resource.
type Reconciler struct {
	// the client to write resource updates
	DynamicClient dynamic.Interface

	// RestMapper is used to convert between gvk and gvr
	RestMapper meta.RESTMapper

	// InformerManager holds all the informers that we can use to read from
	InformerManager informer.Manager

	// PlacementControllerV1Beta1 exposes the placement queue for the v1beta1 reconciler to push to.
	PlacementControllerV1Beta1 controller.Controller

	// ResourcePlacementController exposes the ResourcePlacement queue for the reconciler to push to.
	ResourcePlacementController controller.Controller

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

	// The clusterObj is set to be the object that the placement direct selects.
	clusterObj, isClusterScoped, err := r.getUnstructuredObject(clusterWideKey)
	switch {
	case apierrors.IsNotFound(err):
		return r.handleDeletedResource(clusterWideKey, isClusterScoped)
	case err != nil:
		klog.ErrorS(err, "Failed to get unstructured object", "obj", clusterWideKey)
		return ctrl.Result{}, err
	}
	return r.handleUpdatedResource(clusterWideKey, clusterObj, isClusterScoped)
}

// handleDeletedResource handles the deleted resource and triggers the affected placements.
// If the resource is namespace scoped, it will check if CRP selects the namespace and
// RP selects the resource.
func (r *Reconciler) handleDeletedResource(key keys.ClusterWideKey, isClusterScoped bool) (ctrl.Result, error) {
	if isClusterScoped {
		klog.V(2).InfoS("Find clusterResourcePlacement that selects the cluster scoped object", "obj", key)
		// We need to find out which placements have selected this resource.
		return r.triggerAffectedPlacementsForDeletedRes(key, true)
	}

	// For the deleting namespace scoped resource, we find the namespace and treated it as an updated
	// resource inside the namespace.
	klog.V(2).InfoS("Find clusterResourcePlacement that selects the namespace scoped object", "obj", key)
	if err := r.handleUpdatedResourceForClusterResourcePlacement(key, nil, false); err != nil {
		klog.ErrorS(err, "Failed to handle updated resource for cluster resource placement", "obj", key)
		return ctrl.Result{}, err
	}
	klog.V(2).InfoS("Find resourcePlacement that selects the namespace scoped object", "obj", key)
	return r.triggerAffectedPlacementsForDeletedRes(key, false)
}

// handleUpdatedResourceForClusterResourcePlacement handles the updated resource for cluster resource placement.
func (r *Reconciler) handleUpdatedResourceForClusterResourcePlacement(key keys.ClusterWideKey, obj runtime.Object, isClusterScoped bool) error {
	if isClusterScoped {
		klog.V(2).InfoS("Find clusterResourcePlacement that selects the cluster scoped object", "obj", key)
		return r.triggerAffectedPlacementsForUpdatedRes(key, obj.(*unstructured.Unstructured), true)
	}

	klog.V(2).InfoS("Find namespace that contains the namespace scoped object", "obj", key)
	// we will use the parent namespace object to search for the affected placements
	nsObj, err := r.InformerManager.Lister(utils.NamespaceGVR).Get(key.Namespace)
	if err != nil {
		klog.ErrorS(err, "Failed to find the namespace the resource belongs to", "obj", key)
		return client.IgnoreNotFound(err)
	}
	klog.V(2).InfoS("Find clusterResourcePlacement that selects the namespace", "obj", key)
	if err := r.triggerAffectedPlacementsForUpdatedRes(key, nsObj.(*unstructured.Unstructured), true); err != nil {
		klog.ErrorS(err, "Failed to trigger affected placements for updated cluster resource", "obj", key)
		return err
	}
	return nil
}

// handleUpdatedResourceForResourcePlacement handles the updated resource for resource placement.
func (r *Reconciler) handleUpdatedResourceForResourcePlacement(key keys.ClusterWideKey, clusterObj runtime.Object, isClusterScoped bool) error {
	if isClusterScoped {
		return nil
	}

	klog.V(2).InfoS("Find resourcePlacement that selects the namespace scoped object", "obj", key)
	if err := r.triggerAffectedPlacementsForUpdatedRes(key, clusterObj.(*unstructured.Unstructured), false); err != nil {
		klog.ErrorS(err, "Failed to trigger affected placements for updated resource", "obj", key)
		return err
	}
	return nil
}

// handleUpdatedResource handles the updated resource and triggers the affected placements.
func (r *Reconciler) handleUpdatedResource(key keys.ClusterWideKey, clusterObj runtime.Object, isClusterScoped bool) (ctrl.Result, error) {
	if err := r.handleUpdatedResourceForClusterResourcePlacement(key, clusterObj, isClusterScoped); err != nil {
		klog.ErrorS(err, "Failed to handle updated resource for placement", "obj", key)
		return ctrl.Result{}, err
	}
	if err := r.handleUpdatedResourceForResourcePlacement(key, clusterObj, isClusterScoped); err != nil {
		klog.ErrorS(err, "Failed to handle updated resource for resource placement", "obj", key)
		return ctrl.Result{}, err
	}
	klog.V(2).InfoS("Successfully handled updated resource", "obj", key)
	return ctrl.Result{}, nil
}

// triggerAffectedPlacementsForDeletedRes find the affected placements for a given deleted resources.
func (r *Reconciler) triggerAffectedPlacementsForDeletedRes(key keys.ClusterWideKey, isClusterScope bool) (ctrl.Result, error) {
	if isClusterScope {
		if r.PlacementControllerV1Beta1 != nil {
			crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).List(labels.Everything())
			if err != nil {
				klog.ErrorS(err, "Failed to list all the v1beta1 cluster placement", "obj", key)
				return ctrl.Result{}, err
			}

			matchedCRPs := findPlacementsSelectedDeletedResV1Beta1(key, convertToClusterResourcePlacements(crpList))
			if len(matchedCRPs) == 0 {
				klog.V(2).InfoS("Deleted object does not affect any v1beta1 cluster resource placement", "obj", key)
				return ctrl.Result{}, nil
			}
			for _, crp := range matchedCRPs {
				klog.V(2).InfoS("Deleted object triggered v1beta1 cluster resource placement reconcile", "obj", key, "crp", crp)
				r.PlacementControllerV1Beta1.Enqueue(crp)
			}
		}
		return ctrl.Result{}, nil
	}
	if r.ResourcePlacementController != nil {
		rpList, err := r.InformerManager.Lister(utils.ResourcePlacementGVR).ByNamespace(key.Namespace).List(labels.Everything())
		if err != nil {
			klog.ErrorS(err, "Failed to list all the resource placement in namespace", "obj", key)
			return ctrl.Result{}, err
		}

		matchedRPs := findPlacementsSelectedDeletedResV1Beta1(key, convertToResourcePlacements(rpList))
		if len(matchedRPs) == 0 {
			klog.V(2).InfoS("Deleted object does not affect any resource placement", "obj", key)
			return ctrl.Result{}, nil
		}

		for _, rp := range matchedRPs {
			klog.V(2).InfoS("Deleted object triggered resource placement reconcile", "obj", key, "rp", rp)
			r.ResourcePlacementController.Enqueue(controller.GetObjectKeyFromNamespaceName(key.Namespace, rp))
		}
	}
	return ctrl.Result{}, nil
}

// findPlacementsSelectedDeletedResV1Beta1 finds placement name which has selected this resource before it's deleted.
func findPlacementsSelectedDeletedResV1Beta1(res keys.ClusterWideKey, placementList []placementv1beta1.PlacementObj) []string {
	matchedPlacements := make([]string, 0)
	for _, placement := range placementList {
		for _, selectedRes := range placement.GetPlacementStatus().SelectedResources {
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
				matchedPlacements = append(matchedPlacements, placement.GetName())
				break
			}
		}
	}
	return matchedPlacements
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

// triggerAffectedPlacementsForUpdatedRes find the affected placements for a given updated cluster scoped or namespace scoped resources.
// If the key is namespace scoped, res will be the namespace object for the clusterResourcePlacement.
// If triggerCRP is true, it will trigger the cluster resource placement controller, otherwise it will trigger the resource placement controller.
func (r *Reconciler) triggerAffectedPlacementsForUpdatedRes(key keys.ClusterWideKey, res *unstructured.Unstructured, triggerCRP bool) error {
	if triggerCRP {
		if r.PlacementControllerV1Beta1 != nil {
			// List all the CRPs.
			crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).List(labels.Everything())
			if err != nil {
				klog.ErrorS(err, "Failed to list all the v1beta1 cluster placements", "obj", key)
				return fmt.Errorf("failed to list all the v1beta1 cluster placements: %w", err)
			}

			// Find all matching CRPs.
			matchedCRPs := collectAllAffectedPlacementsV1Beta1(key, res, convertToClusterResourcePlacements(crpList))
			if len(matchedCRPs) == 0 {
				klog.V(2).InfoS("Change in object does not affect any v1beta1 cluster resource placement", "obj", key)
				return nil
			}

			// Enqueue the CRPs for reconciliation.
			for crp := range matchedCRPs {
				klog.V(2).InfoS("Change in object triggered v1beta1 cluster resource placement reconcile", "obj", key, "crp", crp)
				r.PlacementControllerV1Beta1.Enqueue(crp)
			}
		}
		return nil
	}

	if r.ResourcePlacementController != nil {
		// List all the ResourcePlacements.
		rpList, err := r.InformerManager.Lister(utils.ResourcePlacementGVR).ByNamespace(key.Namespace).List(labels.Everything())
		if err != nil {
			klog.ErrorS(err, "Failed to list all the resource placements", "obj", key)
			return fmt.Errorf("failed to list all the resource placements in namespace %s: %w", key.Namespace, err)
		}

		// Find all matching ResourcePlacements.
		matchedRPs := collectAllAffectedPlacementsV1Beta1(key, res, convertToResourcePlacements(rpList))
		if len(matchedRPs) == 0 {
			klog.V(2).InfoS("Change in object does not affect any resource placement", "obj", key)
			return nil
		}

		// Enqueue the ResourcePlacements for reconciliation.
		for rp := range matchedRPs {
			klog.V(2).InfoS("Change in object triggered resource placement reconcile", "obj", key, "rp", rp)
			r.ResourcePlacementController.Enqueue(controller.GetObjectKeyFromNamespaceName(key.Namespace, rp))
		}
	}
	return nil
}

func isSelectNamespaceOnly(selector placementv1beta1.ResourceSelectorTerm) bool {
	return selector.Group == "" && selector.Version == "v1" && selector.Kind == "Namespace" && selector.SelectionScope == placementv1beta1.NamespaceOnly
}

// collectAllAffectedPlacementsV1Beta1 goes through all v1beta1 placements and collect the ones whose resource selector matches the object given its gvk.
// If the key is namespace scoped, res will be the namespace object for the clusterResourcePlacement.
func collectAllAffectedPlacementsV1Beta1(key keys.ClusterWideKey, res *unstructured.Unstructured, placementList []placementv1beta1.PlacementObj) map[string]bool {
	placements := make(map[string]bool)
	for _, placement := range placementList {
		match := false
		// find the placements selected this resource (before this change)
		// If the namespaced scope resource is in the clusterResourcePlacement status and placement is namespaceOnly,
		// the placement should be triggered to create a new resourceSnapshot.
		for _, selectedRes := range placement.GetPlacementStatus().SelectedResources {
			if selectedRes.Group == key.GroupVersionKind().Group && selectedRes.Version == key.GroupVersionKind().Version &&
				selectedRes.Kind == key.GroupVersionKind().Kind && selectedRes.Name == key.Name && selectedRes.Namespace == key.Namespace {
				placements[placement.GetName()] = true
				match = true
				break
			}
		}
		if match {
			continue
		}
		// check if object match any placement's resource selectors
		// For the resource placement, we do not compare the namespace in the selector.
		// We assume the namespace is the same as the resource placement's namespace and webhook/CEL
		// will validate the resource placement's namespace matches the resource's namespace.
		for _, selector := range placement.GetPlacementSpec().ResourceSelectors {
			// For the clusterResourcePlacement, we skip the namespace scoped resources if the placement is cluster scoped.
			if key.Namespace != "" && isSelectNamespaceOnly(selector) && placement.GetNamespace() == "" {
				// If the selector is namespace only, we skip the namespace scoped resources.
				klog.V(2).InfoS("Skipping namespace scoped resource for namespace only selector", "key", key, "obj", klog.KRef(res.GetNamespace(), res.GetName()), "selector", selector, "placement", klog.KObj(placement))
				continue
			}

			if !matchSelectorGVKV1Beta1(res.GetObjectKind().GroupVersionKind(), selector) {
				continue
			}
			// if there is 1 selector match, it is a placement match, add only once
			if selector.Name != "" {
				if selector.Name == res.GetName() {
					placements[placement.GetName()] = true
					break
				}
			} else if matchSelectorLabelSelectorV1Beta1(res.GetLabels(), selector) {
				placements[placement.GetName()] = true
				break
			}
		}
	}
	return placements
}

// convertToClusterResourcePlacements converts a list of runtime.Object to ClusterResourcePlacement objects
func convertToClusterResourcePlacements(objects []runtime.Object) []placementv1beta1.PlacementObj {
	placements := make([]placementv1beta1.PlacementObj, 0, len(objects))
	for _, obj := range objects {
		if unstructuredObj, ok := obj.(*unstructured.Unstructured); ok {
			var placement placementv1beta1.ClusterResourcePlacement
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &placement); err == nil {
				placements = append(placements, &placement)
			}
		}
	}
	return placements
}

// convertToResourcePlacements converts a list of runtime.Object to ResourcePlacement objects
func convertToResourcePlacements(objects []runtime.Object) []placementv1beta1.PlacementObj {
	placements := make([]placementv1beta1.PlacementObj, 0, len(objects))
	for _, obj := range objects {
		if unstructuredObj, ok := obj.(*unstructured.Unstructured); ok {
			var placement placementv1beta1.ResourcePlacement
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &placement); err == nil {
				placements = append(placements, &placement)
			}
		}
	}
	return placements
}

func matchSelectorGVKV1Beta1(targetGVK schema.GroupVersionKind, selector placementv1beta1.ResourceSelectorTerm) bool {
	return selector.Group == targetGVK.Group && selector.Version == targetGVK.Version &&
		selector.Kind == targetGVK.Kind
}

func matchSelectorLabelSelectorV1Beta1(targetLabels map[string]string, selector placementv1beta1.ResourceSelectorTerm) bool {
	if selector.LabelSelector == nil {
		// if the labelselector not set, it means select all
		return true
	}
	// we have validated earlier
	s, _ := metav1.LabelSelectorAsSelector(selector.LabelSelector)
	return s.Matches(labels.Set(targetLabels))
}
