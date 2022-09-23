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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
		return nil, err
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
		klog.V(2).InfoS("selected one resource ", "placement", placement.Name, "resource", res)
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
			klog.V(2).InfoS("Skip select resource", "group version kind", gvk.String())
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
	klog.V(2).InfoS("start to fetch the cluster scoped resources by the selector", "selector", selector)
	gk := schema.GroupKind{
		Group: selector.Group,
		Kind:  selector.Kind,
	}
	restMapping, err := r.RestMapper.RESTMapping(gk, selector.Version)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get GVR of the selector")
	}
	gvr := restMapping.Resource
	gvk := schema.GroupVersionKind{
		Group:   selector.Group,
		Version: selector.Version,
		Kind:    selector.Kind,
	}
	if !r.InformerManager.IsClusterScopedResources(gvk) {
		return nil, errors.New(fmt.Sprintf("%+v is not a cluster scoped resource", restMapping.Resource))
	}
	if !r.InformerManager.IsInformerSynced(gvr) {
		return nil, errors.New(fmt.Sprintf("informer cache for %+v is not synced yet", restMapping.Resource))
	}

	lister := r.InformerManager.Lister(gvr)
	// TODO: validator should enforce the mutual exclusiveness between the `name` and `labelSelector` fields
	if len(selector.Name) != 0 {
		obj, err := lister.Get(selector.Name)
		if err != nil {
			klog.ErrorS(err, "cannot get the resource", "gvr", gvr, "name", selector.Name)
			return nil, client.IgnoreNotFound(err)
		}
		uObj := obj.DeepCopyObject().(*unstructured.Unstructured)
		if uObj.GetDeletionTimestamp() != nil {
			// skip a to be deleted namespace
			klog.V(2).InfoS("skip the deleting cluster scoped resources by the selector",
				"selector", selector, "placeName", placeName, "resource name", uObj.GetName())
			return []runtime.Object{}, nil
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
			klog.V(2).InfoS("skip the deleting cluster scoped resources by the selector",
				"selector", selector, "placeName", placeName, "resource name", uObj.GetName())
			continue
		}
		selectedObjs = append(selectedObjs, objects[i])
	}

	return selectedObjs, nil
}

// fetchNamespaceResources retrieve all the objects for a ClusterResourceSelector that is for namespace.
func (r *Reconciler) fetchNamespaceResources(ctx context.Context, selector fleetv1alpha1.ClusterResourceSelector, placeName string) ([]runtime.Object, error) {
	klog.V(2).InfoS("start to fetch the namespace resources by the selector", "selector", selector)
	var resources []runtime.Object

	if len(selector.Name) != 0 {
		// just a single namespace
		objs, err := r.fetchAllResourcesInOneNamespace(ctx, selector.Name, placeName)
		if err != nil {
			klog.ErrorS(err, "failed to fetch all the selected resource in a namespace", "namespace", selector.Name)
			return nil, err
		}
		return objs, err
	}

	// go through each namespace
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
	namespaces, err := r.InformerManager.Lister(utils.NamespaceGVR).List(labelSelector)
	if err != nil {
		return nil, errors.Wrap(err, "cannot list all the namespaces given the label selector")
	}

	for _, namespace := range namespaces {
		ns, err := meta.Accessor(namespace)
		if err != nil {
			return nil, errors.Wrap(err, "cannot get the name of a namespace object")
		}
		objs, err := r.fetchAllResourcesInOneNamespace(ctx, ns.GetName(), placeName)
		if err != nil {
			klog.ErrorS(err, "failed to fetch all the selected resource in a namespace", "namespace", ns.GetName())
			return nil, err
		}
		resources = append(resources, objs...)
	}
	return resources, nil
}

// fetchAllResourcesInOneNamespace retrieve all the objects inside a single namespace which includes the namespace itself.
func (r *Reconciler) fetchAllResourcesInOneNamespace(ctx context.Context, namespaceName string, placeName string) ([]runtime.Object, error) {
	var resources []runtime.Object

	if !utils.ShouldPropagateNamespace(namespaceName, r.SkippedNamespaces) {
		return nil, errors.New(fmt.Sprintf("namespace %s is not allowed to propagate", namespaceName))
	}

	klog.V(2).InfoS("start to fetch all the resources inside a namespace", "namespace", namespaceName)
	// select the namespace object itself
	obj, err := r.InformerManager.Lister(utils.NamespaceGVR).Get(namespaceName)
	if err != nil {
		klog.ErrorS(err, "cannot get the namespace", "namespace", namespaceName)
		return nil, client.IgnoreNotFound(err)
	}
	nameSpaceObj := obj.DeepCopyObject().(*unstructured.Unstructured)
	if nameSpaceObj.GetDeletionTimestamp() != nil {
		// skip a to be deleted namespace
		klog.V(2).InfoS("skip the deleting namespace resources by the selector",
			"placeName", placeName, "namespace", namespaceName)
		return resources, nil
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
			klog.V(2).InfoS("Skip watch resource", "group version kind", gvk.String())
			return false
		}
	}
	return true
}

// generateManifest creates a manifest from the unstructured obj,
// it stripped all the unnecessary fields to prepare the objects for dispatch.
func generateManifest(object *unstructured.Unstructured) (*workv1alpha1.Manifest, error) {
	// we keep the annotation/label/finalizer/owner references/delete grace period
	object.SetResourceVersion("")
	object.SetGeneration(0)
	object.SetUID("")
	object.SetSelfLink("")
	object.SetDeletionTimestamp(nil)
	object.SetManagedFields(nil)
	// remove kubectl last applied annotation if exist
	annots := object.GetAnnotations()
	if annots != nil {
		delete(annots, corev1.LastAppliedConfigAnnotation)
		if len(annots) == 0 {
			object.SetAnnotations(nil)
		} else {
			object.SetAnnotations(annots)
		}
	}
	// Remove all the owner references as the UID in the owner reference can't be transferred to
	// the member clusters
	// TODO: Establish a way to keep the ownership relation through work-api
	object.SetOwnerReferences(nil)
	unstructured.RemoveNestedField(object.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(object.Object, "status")

	// TODO: see if there are other cases that we may have some extra fields
	if object.GetKind() == "Service" && object.GetAPIVersion() == "v1" {
		if clusterIP, exist, _ := unstructured.NestedString(object.Object, "spec", "clusterIP"); exist && clusterIP != corev1.ClusterIPNone {
			unstructured.RemoveNestedField(object.Object, "spec", "clusterIP")
			unstructured.RemoveNestedField(object.Object, "spec", "clusterIPs")
		}
		// We should remove all node ports that are assigned by hubcluster if any.
		unstructured.RemoveNestedField(object.Object, "spec", "healthCheckNodePort")

		vals, found, err := unstructured.NestedFieldNoCopy(object.Object, "spec", "ports")
		if found && err == nil {
			if ports, ok := vals.([]interface{}); ok {
				for i := range ports {
					if each, ok := ports[i].(map[string]interface{}); ok {
						delete(each, "nodePort")
					}
				}
			}
		}
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get the ports field in Serivce object, name =%s", object.GetName())
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
