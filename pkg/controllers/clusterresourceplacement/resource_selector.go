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

package clusterresourceplacement

import (
	"fmt"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
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

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	fleetv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// selectResources selects the resources according to the placement resourceSelectors.
// It also generates an array of manifests obj based on the selected resources.
func (r *Reconciler) selectResources(placement *fleetv1alpha1.ClusterResourcePlacement) ([]workv1alpha1.Manifest, error) {
	selectedObjects, err := r.gatherSelectedResource(placement.GetName(), convertResourceSelector(placement.Spec.ResourceSelectors))
	if err != nil {
		return nil, err
	}
	placement.Status.SelectedResources = make([]fleetv1alpha1.ResourceIdentifier, 0)
	manifests := make([]workv1alpha1.Manifest, len(selectedObjects))
	for i, unstructuredObj := range selectedObjects {
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

// Note: temporary solution to share the same set of utils between v1alpha1 and v1beta1 APIs so that v1alpha1 implementation
// won't be broken. v1alpha1 implementation should be removed when new API is ready.
// The clusterResourceSelect has no changes between different versions.
func convertResourceSelector(old []fleetv1alpha1.ClusterResourceSelector) []fleetv1beta1.ClusterResourceSelector {
	res := make([]fleetv1beta1.ClusterResourceSelector, len(old))
	for i, item := range old {
		res[i] = fleetv1beta1.ClusterResourceSelector(item)
	}
	return res
}

// gatherSelectedResource gets all the resources according to the resource selector.
func (r *Reconciler) gatherSelectedResource(placement string, selectors []fleetv1beta1.ClusterResourceSelector) ([]*unstructured.Unstructured, error) {
	var resources []*unstructured.Unstructured
	var resourceMap = make(map[fleetv1beta1.ResourceIdentifier]bool)
	for _, selector := range selectors {
		gvk := schema.GroupVersionKind{
			Group:   selector.Group,
			Version: selector.Version,
			Kind:    selector.Kind,
		}

		if r.ResourceConfig.IsResourceDisabled(gvk) {
			klog.V(2).InfoS("Skip select resource", "group version kind", gvk.String())
			continue
		}
		var objs []runtime.Object
		var err error
		if gvk == utils.NamespaceGVK {
			objs, err = r.fetchNamespaceResources(selector, placement)
		} else {
			objs, err = r.fetchClusterScopedResources(selector, placement)
		}
		if err != nil {
			return nil, err
		}
		for _, obj := range objs {
			uObj := obj.(*unstructured.Unstructured)
			ri := fleetv1beta1.ResourceIdentifier{
				Group:     obj.GetObjectKind().GroupVersionKind().Group,
				Version:   obj.GetObjectKind().GroupVersionKind().Version,
				Kind:      obj.GetObjectKind().GroupVersionKind().Kind,
				Name:      uObj.GetName(),
				Namespace: uObj.GetNamespace(),
			}
			if _, exist := resourceMap[ri]; exist {
				err = fmt.Errorf("found duplicate resource %+v", ri)
				klog.ErrorS(err, "user selected one resource more than once", "resource", ri, "placement", placement)
				return nil, controller.NewUserError(err)
			}
			resourceMap[ri] = true
			resources = append(resources, uObj)
		}
	}
	// sort the resources in strict order so that we will get the stable list of manifest so that
	// the generated work object doesn't change between reconcile loops
	sortResources(resources)

	return resources, nil
}

func sortResources(resources []*unstructured.Unstructured) {
	sort.Slice(resources, func(i, j int) bool {
		obj1 := resources[i]
		obj2 := resources[j]
		gvk1 := obj1.GetObjectKind().GroupVersionKind().String()
		gvk2 := obj2.GetObjectKind().GroupVersionKind().String()
		// compare group/version;kind for the rest of type of resources
		gvkComp := strings.Compare(gvk1, gvk2)
		if gvkComp == 0 {
			// same gvk, compare namespace/name, no duplication exists
			return strings.Compare(fmt.Sprintf("%s/%s", obj1.GetNamespace(), obj1.GetName()),
				fmt.Sprintf("%s/%s", obj2.GetNamespace(), obj2.GetName())) > 0
		}
		// sort by the cluster scoped priority resource types first
		if gvk1 == utils.NamespaceMetaGVK.String() || gvk2 == utils.NamespaceMetaGVK.String() {
			return gvk1 == utils.NamespaceMetaGVK.String()
		}
		if gvk1 == utils.CRDMetaGVK.String() || gvk2 == utils.CRDMetaGVK.String() {
			return gvk1 == utils.CRDMetaGVK.String()
		}
		// followed by namespaced priority resource types
		if gvk1 == utils.ConfigMapGVK.String() || gvk2 == utils.ConfigMapGVK.String() {
			return gvk1 == utils.ConfigMapGVK.String()
		}
		if gvk1 == utils.SecretGVK.String() || gvk2 == utils.SecretGVK.String() {
			return gvk1 == utils.SecretGVK.String()
		}
		if gvk1 == utils.PersistentVolumeClaimGVK.String() || gvk2 == utils.PersistentVolumeClaimGVK.String() {
			return gvk1 == utils.PersistentVolumeClaimGVK.String()
		}
		return gvkComp < 0
	})
}

// fetchClusterScopedResources retrieves the objects based on the selector.
func (r *Reconciler) fetchClusterScopedResources(selector fleetv1beta1.ClusterResourceSelector, placeName string) ([]runtime.Object, error) {
	klog.V(2).InfoS("start to fetch the cluster scoped resources by the selector", "selector", selector)
	gk := schema.GroupKind{
		Group: selector.Group,
		Kind:  selector.Kind,
	}
	restMapping, err := r.RestMapper.RESTMapping(gk, selector.Version)
	if err != nil {
		return nil, controller.NewUserError(fmt.Errorf("invalid placement %s, failed to get GVR of the selector: %w", placeName, err))
	}
	gvr := restMapping.Resource
	gvk := schema.GroupVersionKind{
		Group:   selector.Group,
		Version: selector.Version,
		Kind:    selector.Kind,
	}
	if !r.InformerManager.IsClusterScopedResources(gvk) {
		return nil, controller.NewUserError(fmt.Errorf("invalid placement %s: %+v is not a cluster scoped resource", placeName, restMapping.Resource))
	}
	if !r.InformerManager.IsInformerSynced(gvr) {
		return nil, controller.NewExpectedBehaviorError(fmt.Errorf("informer cache for %+v is not synced yet", restMapping.Resource))
	}

	lister := r.InformerManager.Lister(gvr)
	// TODO: validator should enforce the mutual exclusiveness between the `name` and `labelSelector` fields
	if len(selector.Name) != 0 {
		obj, err := lister.Get(selector.Name)
		if err != nil {
			klog.ErrorS(err, "cannot get the resource", "gvr", gvr, "name", selector.Name)
			return nil, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
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
			return nil, controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot convert the label selector to a selector: %w", err))
		}
	}
	var selectedObjs []runtime.Object
	objects, err := lister.List(labelSelector)
	if err != nil {
		return nil, controller.NewAPIServerError(true, fmt.Errorf("cannot list all the objects: %w", err))
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

// fetchNamespaceResources retrieves all the objects for a ClusterResourceSelector that is for namespace.
func (r *Reconciler) fetchNamespaceResources(selector fleetv1beta1.ClusterResourceSelector, placeName string) ([]runtime.Object, error) {
	klog.V(2).InfoS("start to fetch the namespace resources by the selector", "selector", selector)
	var resources []runtime.Object

	if len(selector.Name) != 0 {
		// just a single namespace
		objs, err := r.fetchAllResourcesInOneNamespace(selector.Name, placeName)
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
			return nil, controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot convert the label selector to a selector: %w", err))
		}
	}
	namespaces, err := r.InformerManager.Lister(utils.NamespaceGVR).List(labelSelector)
	if err != nil {
		return nil, controller.NewAPIServerError(true, fmt.Errorf("cannot list all the namespaces given the label selector: %w", err))
	}

	for _, namespace := range namespaces {
		ns, err := meta.Accessor(namespace)
		if err != nil {
			return nil, controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot get the name of a namespace object: %w", err))
		}
		objs, err := r.fetchAllResourcesInOneNamespace(ns.GetName(), placeName)
		if err != nil {
			klog.ErrorS(err, "failed to fetch all the selected resource in a namespace", "namespace", ns.GetName())
			return nil, err
		}
		resources = append(resources, objs...)
	}
	return resources, nil
}

// fetchAllResourcesInOneNamespace retrieves all the objects inside a single namespace which includes the namespace itself.
func (r *Reconciler) fetchAllResourcesInOneNamespace(namespaceName string, placeName string) ([]runtime.Object, error) {
	var resources []runtime.Object

	if !utils.ShouldPropagateNamespace(namespaceName, r.SkippedNamespaces) {
		err := fmt.Errorf("invalid clusterRresourcePlacement %s: namespace %s is not allowed to propagate", placeName, namespaceName)
		return nil, controller.NewUserError(err)
	}

	klog.V(2).InfoS("start to fetch all the resources inside a namespace", "namespace", namespaceName)
	// select the namespace object itself
	obj, err := r.InformerManager.Lister(utils.NamespaceGVR).Get(namespaceName)
	if err != nil {
		klog.ErrorS(err, "cannot get the namespace", "namespace", namespaceName)
		return nil, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
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
			return nil, controller.NewExpectedBehaviorError(fmt.Errorf("informer cache for %+v is not synced yet", gvr))
		}
		lister := r.InformerManager.Lister(gvr)
		objs, err := lister.ByNamespace(namespaceName).List(labels.Everything())
		if err != nil {
			return nil, controller.NewAPIServerError(true, fmt.Errorf("cannot list all the objects of type %+v in namespace %s: %w", gvr, namespaceName, err))
		}
		for _, obj := range objs {
			uObj := obj.DeepCopyObject().(*unstructured.Unstructured)
			shouldInclude, err := utils.ShouldPropagateObj(r.InformerManager, uObj)
			if err != nil {
				klog.ErrorS(err, "cannot determine if we should propagate an object", "object", klog.KObj(uObj))
				return nil, err
			}
			if shouldInclude {
				resources = append(resources, obj)
			}
		}
	}

	return resources, nil
}

// shouldSelectResource returns whether a resource should be selected for propagation.
func (r *Reconciler) shouldSelectResource(gvr schema.GroupVersionResource) bool {
	// By default, all of the APIs are allowed.
	if r.ResourceConfig == nil {
		return true
	}
	gvks, err := r.RestMapper.KindsFor(gvr)
	if err != nil {
		klog.ErrorS(err, "gvr(%s) transform failed: %v", gvr.String(), err)
		return false
	}
	for _, gvk := range gvks {
		if r.ResourceConfig.IsResourceDisabled(gvk) {
			klog.V(2).InfoS("Skip watch resource", "group version kind", gvk.String())
			return false
		}
	}
	return true
}

// generateRawContent strips all the unnecessary fields to prepare the objects for dispatch.
func generateRawContent(object *unstructured.Unstructured) ([]byte, error) {
	// Make a deep copy of the object as we are modifying it.
	object = object.DeepCopy()
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
			return nil, fmt.Errorf("failed to get the ports field in Service object, name =%s: %w", object.GetName(), err)
		}
	} else if object.GetKind() == "Job" && object.GetAPIVersion() == batchv1.SchemeGroupVersion.String() {
		if manualSelector, exist, _ := unstructured.NestedBool(object.Object, "spec", "manualSelector"); !exist || !manualSelector {
			// remove the selector field and labels added by the api-server if the job is not created with manual selector
			// whose value conflict with the ones created by the member cluster api server
			// https://github.com/kubernetes/kubernetes/blob/d4fde1e92a83cb533ae63b3abe9d49f08efb7a2f/pkg/registry/batch/job/strategy.go#L219
			// k8s used to add an old label called "controller-uid" but use a new label called "batch.kubernetes.io/controller-uid" after 1.26
			unstructured.RemoveNestedField(object.Object, "spec", "selector", "matchLabels", "controller-uid")
			unstructured.RemoveNestedField(object.Object, "spec", "selector", "matchLabels", "batch.kubernetes.io/controller-uid")
			unstructured.RemoveNestedField(object.Object, "spec", "template", "metadata", "creationTimestamp")
			unstructured.RemoveNestedField(object.Object, "spec", "template", "metadata", "labels", "controller-uid")
			unstructured.RemoveNestedField(object.Object, "spec", "template", "metadata", "labels", "batch.kubernetes.io/controller-uid")
		}
	}

	rawContent, err := object.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the unstructured object gvk = %s, name =%s: %w", object.GroupVersionKind(), object.GetName(), err)
	}
	return rawContent, nil
}

// generateManifest creates a manifest from the unstructured obj.
func generateManifest(object *unstructured.Unstructured) (*workv1alpha1.Manifest, error) {
	rawContent, err := generateRawContent(object)
	if err != nil {
		return nil, err
	}
	return &workv1alpha1.Manifest{
		RawExtension: runtime.RawExtension{Raw: rawContent},
	}, nil
}

// generateResourceContent creates a resource content from the unstructured obj.
func generateResourceContent(object *unstructured.Unstructured) (*fleetv1beta1.ResourceContent, error) {
	rawContent, err := generateRawContent(object)
	if err != nil {
		return nil, controller.NewUnexpectedBehaviorError(err)
	}
	return &fleetv1beta1.ResourceContent{
		RawExtension: runtime.RawExtension{Raw: rawContent},
	}, nil
}

// selectResourcesForPlacement selects the resources according to the placement resourceSelectors.
// It also generates an array of resource content and resource identifier based on the selected resources.
// It also returns the number of envelope configmaps so the CRP controller can have the right expectation of the number of work objects.
func (r *Reconciler) selectResourcesForPlacement(placement *fleetv1beta1.ClusterResourcePlacement) (int, []fleetv1beta1.ResourceContent, []fleetv1beta1.ResourceIdentifier, error) {
	envelopeObjCount := 0
	selectedObjects, err := r.gatherSelectedResource(placement.GetName(), placement.Spec.ResourceSelectors)
	if err != nil {
		return 0, nil, nil, err
	}

	resources := make([]fleetv1beta1.ResourceContent, len(selectedObjects))
	resourcesIDs := make([]fleetv1beta1.ResourceIdentifier, len(selectedObjects))
	for i, unstructuredObj := range selectedObjects {
		rc, err := generateResourceContent(unstructuredObj)
		if err != nil {
			return 0, nil, nil, err
		}
		if unstructuredObj.GetObjectKind().GroupVersionKind() == utils.ConfigMapGVK &&
			len(unstructuredObj.GetAnnotations()[fleetv1beta1.EnvelopeConfigMapAnnotation]) != 0 {
			envelopeObjCount++
		}
		resources[i] = *rc
		ri := fleetv1beta1.ResourceIdentifier{
			Group:     unstructuredObj.GroupVersionKind().Group,
			Version:   unstructuredObj.GroupVersionKind().Version,
			Kind:      unstructuredObj.GroupVersionKind().Kind,
			Name:      unstructuredObj.GetName(),
			Namespace: unstructuredObj.GetNamespace(),
		}
		resourcesIDs[i] = ri
	}
	return envelopeObjCount, resources, resourcesIDs, nil
}
