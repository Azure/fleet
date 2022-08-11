/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

// selectResources selects the resources according to the placement resourceSelectors.
// It also generates an array of manifests obj based on the selected resources.
func (r *Reconciler) selectResources(ctx context.Context, placement *fleetv1alpha1.ClusterResourcePlacement) ([]workv1alpha1.Manifest, error) {
	selectedObjects, err := r.gatherSelectedResource(ctx, placement)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to gather all the selected resource")
	}
	if len(selectedObjects) == 0 {
		return nil, fmt.Errorf("failed to select any resources")
	}
	placement.Status.SelectedResources = make([]fleetv1alpha1.ResourceIdentifier, 0)
	manifests := make([]workv1alpha1.Manifest, len(selectedObjects))
	for i, obj := range selectedObjects {
		unstructuredObj := obj.DeepCopyObject().(*unstructured.Unstructured)
		gvk := unstructuredObj.GroupVersionKind()
		res := fleetv1alpha1.ResourceIdentifier{
			Group:     gvk.Group,
			Version:   gvk.Version,
			Kind:      gvk.Kind,
			Name:      unstructuredObj.GetName(),
			Namespace: unstructuredObj.GetNamespace(),
		}
		placement.Status.SelectedResources = append(placement.Status.SelectedResources, res)
		klog.V(4).InfoS("selected one resource ", "placement", placement.Name, "resource", res)
		manifest, err := generateManifest(unstructuredObj)
		if err != nil {
			return nil, err
		}
		manifests[i] = *manifest
	}
	return manifests, nil
}

// getSelectedResource get all the resources according to the resource selector.
func (r *Reconciler) gatherSelectedResource(ctx context.Context, placement *fleetv1alpha1.ClusterResourcePlacement) ([]runtime.Object, error) {
	var resources []runtime.Object
	for _, selector := range placement.Spec.ResourceSelectors {
		gvk := schema.GroupVersionKind{
			Group:   selector.Group,
			Version: selector.Version,
			Kind:    selector.Kind,
		}

		if r.DisabledResourceConfig.IsResourceDisabled(gvk) {
			klog.V(4).InfoS("Skip select resource", "group version kind", gvk.String())
			continue
		}
		var objs []runtime.Object
		var err error
		if gvk == utils.NamespaceGVK {
			objs, err = r.fetchNamespaceResources(ctx, selector, placement.GetName())
		} else {
			objs, err = r.fetchClusterScopedResources(ctx, selector, placement.GetName())
		}
		if err != nil {
			// TODO: revisit if return partial result makes sense
			return nil, errors.Wrapf(err, "selector = %v", selector)
		}
		resources = append(resources, objs...)
	}
	// sort the resources in strict order so that we will get the stable list of manifest so that
	// the generated work object doesn't change between reconcile loops
	sort.Slice(resources, func(i, j int) bool {
		obj1 := resources[i].DeepCopyObject().(*unstructured.Unstructured)
		obj2 := resources[j].DeepCopyObject().(*unstructured.Unstructured)
		// compare group/version;kind
		gvkComp := strings.Compare(obj1.GroupVersionKind().String(), obj2.GroupVersionKind().String())
		if gvkComp > 0 {
			return true
		}
		if gvkComp < 0 {
			return false
		}
		// same gvk, compare namespace/name
		return strings.Compare(fmt.Sprintf("%s/%s", obj1.GetNamespace(), obj1.GetName()),
			fmt.Sprintf("%s/%s", obj2.GetNamespace(), obj2.GetName())) > 0
	})
	return resources, nil
}

// fetchClusterScopedResources retrieve the objects based on the selector.
func (r *Reconciler) fetchClusterScopedResources(ctx context.Context, selector fleetv1alpha1.ClusterResourceSelector, placeName string) ([]runtime.Object, error) {
	klog.V(4).InfoS("start to fetch the cluster scoped resources by the selector", "selector", selector)
	gk := schema.GroupKind{
		Group: selector.Group,
		Kind:  selector.Kind,
	}
	restMapping, err := r.RestMapper.RESTMapping(gk, selector.Version)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get GVR of the selector")
	}
	gvr := restMapping.Resource
	if !r.InformerManager.IsInformerSynced(gvr) {
		return nil, fmt.Errorf("informer cache for %+v is not synced yet", restMapping.Resource)
	}

	lister := r.InformerManager.Lister(gvr)
	// TODO: validator should enforce the mutual exclusiveness between the `name` and `labelSelector` fields
	if len(selector.Name) != 0 {
		obj, err := lister.Get(selector.Name)
		if err != nil {
			return nil, client.IgnoreNotFound(errors.Wrap(err, "cannot get the objets"))
		}
		uObj := obj.DeepCopyObject().(*unstructured.Unstructured)
		if uObj.GetDeletionTimestamp() != nil {
			// skip a to be deleted namespace
			klog.V(4).InfoS("skip the deleting cluster scoped resources by the selector",
				"selector", selector, "placeName", placeName, "resource name", uObj.GetName())
			return []runtime.Object{}, nil
		}
		utils.AddPlacement(uObj, placeName)
		// TODO: use a get/update retry loop to solve conflict error
		if _, err = r.InformerManager.GetClient().Resource(gvr).Update(ctx, uObj, metav1.UpdateOptions{}); err != nil {
			return nil, errors.Wrapf(err, "cannot claim the cluster scoped object %+v as selected by the placement", selector)
		}
		return []runtime.Object{obj}, nil
	}

	var labelSelector labels.Selector
	if selector.LabelSelector == nil {
		labelSelector = labels.Everything()
	} else {
		// TODO: validator should enforce the validity of the labelSelector
		labelSelector, err = metav1.LabelSelectorAsSelector(selector.LabelSelector)
		if err != nil {
			return nil, errors.Wrap(err, "cannot convert the label selector to a selector")
		}
	}
	var selectedObjs []runtime.Object
	objects, err := lister.List(labelSelector)
	if err != nil {
		return nil, errors.Wrap(err, "cannot list all the objets")
	}
	// go ahead and claim all objects by adding a finalizer and insert the placement in its annotation
	for i := 0; i < len(objects); i++ {
		uObj := objects[i].DeepCopyObject().(*unstructured.Unstructured)
		if uObj.GetDeletionTimestamp() != nil {
			// skip a to be deleted namespace
			klog.V(4).InfoS("skip the deleting cluster scoped resources by the selector",
				"selector", selector, "placeName", placeName, "resource name", uObj.GetName())
			continue
		}
		selectedObjs = append(selectedObjs, objects[i])
		utils.AddPlacement(uObj, placeName)
		// TODO: use a get/update retry loop to solve conflict error
		if _, err = r.InformerManager.GetClient().Resource(gvr).Update(ctx, uObj, metav1.UpdateOptions{}); err != nil {
			return nil, errors.Wrapf(err, "cannot claim the cluster scoped object %+v as selected by the placement", selector)
		}
	}

	return selectedObjs, nil
}

// fetchNamespaceResources retrieve all the objects for a ClusterResourceSelector that is for namespace.
func (r *Reconciler) fetchNamespaceResources(ctx context.Context, selector fleetv1alpha1.ClusterResourceSelector, placeName string) ([]runtime.Object, error) {
	klog.V(4).InfoS("start to fetch the namespace resources by the selector", "selector", selector)
	var resources []runtime.Object

	if len(selector.Name) != 0 {
		// just a single namespace
		return r.fetchAllResourcesInOneNamespace(ctx, selector.Name, placeName)
	}
	// go through each namespace
	lister := r.InformerManager.Lister(utils.NamespaceGVR)

	var labelSelector labels.Selector
	var err error
	if selector.LabelSelector == nil {
		labelSelector = labels.Everything()
	} else {
		labelSelector, err = metav1.LabelSelectorAsSelector(selector.LabelSelector)
		if err != nil {
			return nil, errors.Wrap(err, "cannot convert the label selector to a selector")
		}
	}
	namespaces, err := lister.List(labelSelector)
	if err != nil {
		return nil, errors.Wrap(err, "cannot list all the namespaces")
	}

	for _, namespace := range namespaces {
		ns, err := meta.Accessor(namespace)
		if err != nil {
			return nil, errors.Wrap(err, "cannot get the name of a namespace object")
		}
		objs, err := r.fetchAllResourcesInOneNamespace(ctx, ns.GetName(), placeName)
		if err != nil {
			return nil, err
		}
		resources = append(resources, objs...)
	}

	return resources, nil
}

// fetchAllResourcesInOneNamespace retrieve all the objects inside a single namespace which includes the namespace itself.
func (r *Reconciler) fetchAllResourcesInOneNamespace(ctx context.Context, namespaceName string, placeName string) ([]runtime.Object, error) {
	klog.V(4).InfoS("start to fetch all the resources inside a namespace", "namespace", namespaceName)
	var resources []runtime.Object

	// select the namespace object itself
	obj, err := r.InformerManager.Lister(utils.NamespaceGVR).Get(namespaceName)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot get the namespace %s object", namespaceName)
	}
	nameSpaceObj := obj.DeepCopyObject().(*unstructured.Unstructured)
	if nameSpaceObj.GetDeletionTimestamp() != nil {
		// skip a to be deleted namespace
		klog.V(4).InfoS("skip the deleting namespace resources by the selector",
			"placeName", placeName, "namespace", namespaceName)
		return resources, nil
	}
	utils.AddPlacement(nameSpaceObj, placeName)
	// TODO: use a get/update retry loop to solve conflict error
	if _, err = r.InformerManager.GetClient().Resource(utils.NamespaceGVR).Update(ctx, nameSpaceObj, metav1.UpdateOptions{}); err != nil {
		return nil, errors.Wrapf(err, "cannot claim the namespace %s as selected by the placement", namespaceName)
	}
	resources = append(resources, obj)

	trackedResource := r.InformerManager.GetNameSpaceScopedResources()
	for _, gvr := range trackedResource {
		if !r.shouldSelectResource(gvr) {
			continue
		}
		if !r.InformerManager.IsInformerSynced(gvr) {
			return nil, fmt.Errorf("informer cache for %+v is not synced yet", gvr)
		}
		lister := r.InformerManager.Lister(gvr)
		objs, err := lister.ByNamespace(namespaceName).List(labels.Everything())
		if err != nil {
			return nil, errors.Wrapf(err, "cannot list all the objects of type %+v in namespace %s", gvr, namespaceName)
		}
		for _, obj := range objs {
			uObj := obj.DeepCopyObject().(*unstructured.Unstructured)
			shouldInclude, err := utils.ShouldPropagateObj(r.InformerManager, uObj)
			if err != nil {
				return nil, errors.Wrap(err, "cannot determine if we should propagate an object")
			}
			if shouldInclude {
				resources = append(resources, obj)
			}
		}
	}

	return resources, nil
}

// shouldSelectResource returns whether a resource should be propagated
func (r *Reconciler) shouldSelectResource(gvr schema.GroupVersionResource) bool {
	if r.DisabledResourceConfig == nil {
		return true
	}
	gvks, err := r.RestMapper.KindsFor(gvr)
	if err != nil {
		klog.ErrorS(err, "gvr(%s) transform failed: %v", gvr.String(), err)
		return false
	}
	for _, gvk := range gvks {
		if r.DisabledResourceConfig.IsResourceDisabled(gvk) {
			klog.V(4).InfoS("Skip watch resource", "group version kind", gvk.String())
			return false
		}
	}
	return true
}

// removeResourcesClaims finds all the resources that we no longer place and removes this placement from the resource's placement annotation
func (r *Reconciler) removeResourcesClaims(ctx context.Context, placement *fleetv1alpha1.ClusterResourcePlacement,
	existingResources, newResources []fleetv1alpha1.ResourceIdentifier) (int, error) {
	var allErr []error
	resourceMap := make(map[fleetv1alpha1.ResourceIdentifier]bool, 0)
	for _, res := range newResources {
		resourceMap[res] = true
	}
	released := 0
	for _, oldResource := range existingResources {
		if !resourceMap[oldResource] {
			klog.V(4).InfoS("find a no longer selected object", "resource", oldResource, "placement", klog.KObj(placement))
			gk := schema.GroupKind{
				Group: oldResource.Group,
				Kind:  oldResource.Kind,
			}
			restMapping, err := r.RestMapper.RESTMapping(gk, oldResource.Version)
			if err != nil {
				allErr = append(allErr, errors.Wrapf(err, "failed to get the gvk of no longer selected obj %+v from placement %s", oldResource, placement.Name))
				continue
			}
			// we only care about the cluster scoped resources
			gvr := restMapping.Resource
			if r.InformerManager.IsClusterScopedResources(gvr) {
				klog.V(5).InfoS("find a no longer selected cluster scoped object", "resource", oldResource, "placement", klog.KObj(placement))
				obj, err := r.InformerManager.Lister(gvr).Get(oldResource.Name)
				if err != nil {
					if !apierrors.IsNotFound(err) {
						allErr = append(allErr, errors.Wrapf(err, "failed to get a no longer selected cluster scoped obj %+v from placement %s", oldResource, placement.Name))
					}
					continue
				}
				uObj := obj.DeepCopyObject().(*unstructured.Unstructured)
				utils.RemovePlacement(uObj, placement.Name)
				// TODO: use a get/update retry loop to solve conflict error
				if _, updateErr := r.InformerManager.GetClient().Resource(gvr).Update(ctx, uObj, metav1.UpdateOptions{}); updateErr != nil {
					allErr = append(allErr, errors.Wrapf(updateErr, "failed to release a no longer selected cluster scoped obj %+v from placement %s", oldResource, placement.Name))
					continue
				}
				released++
				klog.V(3).InfoS("release a no longer selected cluster scoped obj", "resource", oldResource, "placement", klog.KObj(placement))
			}
		}
	}
	return released, utilerrors.NewAggregate(allErr)
}

// generateManifest creates a manifest from the unstructured obj,
// it stripped all the unnecessary fields to prepare the objects for dispatch.
func generateManifest(object *unstructured.Unstructured) (*workv1alpha1.Manifest, error) {
	object.SetResourceVersion("")
	object.SetGeneration(0)
	object.SetOwnerReferences(nil)
	object.SetDeletionTimestamp(nil)
	object.SetManagedFields(nil)
	object.SetUID("")
	object.SetFinalizers(nil)
	object.SetDeletionGracePeriodSeconds(nil)
	unstructured.RemoveNestedField(object.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(object.Object, "status")
	// remove the placement annotation we added
	a := object.GetAnnotations()
	delete(a, utils.AnnotationPlacementList)
	object.SetAnnotations(a)
	// TODO: see if there are other cases that we may have some extra fields
	if object.GetKind() == "Service" && object.GetAPIVersion() == "v1" {
		if clusterIP, exist, _ := unstructured.NestedString(object.Object, "spec", "clusterIP"); exist && clusterIP != corev1.ClusterIPNone {
			unstructured.RemoveNestedField(object.Object, "spec", "clusterIP")
			unstructured.RemoveNestedField(object.Object, "spec", "clusterIPs")
		}
	}

	rawContent, err := object.MarshalJSON()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal the unstructured object gvk = %s, name =%s", object.GroupVersionKind(), object.GetName())
	}
	return &workv1alpha1.Manifest{
		RawExtension: runtime.RawExtension{Raw: rawContent},
	}, nil
}
