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

package placement

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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

var (
	// resourceSortOrder is the order in which resources are sorted when KubeFleet
	// organizes the resources in a resource snapshot.
	//
	// Note (chenyu1): the sort order here does not affect the order in which resources
	// are applied on a selected member cluster (the work applier will handle the resources
	// in batch with its own grouping logic). KubeFleet sorts resources here solely
	// for consistency (deterministic processing) reasons (i.e., if the set of the
	// resources remain the same, no new snapshots are generated).
	//
	// Important (chenyu1): changing the sort order here may induce side effects in
	// existing KubeFleet deployments, as a new snapshot might be prepared and rolled out.
	// Do not update the sort order unless absolutely necessary.
	resourceSortOrder = map[string]int{
		"PriorityClass":                  0,
		"Namespace":                      1,
		"NetworkPolicy":                  2,
		"ResourceQuota":                  3,
		"LimitRange":                     4,
		"PodDisruptionBudget":            5,
		"ServiceAccount":                 6,
		"Secret":                         7,
		"ConfigMap":                      8,
		"StorageClass":                   9,
		"PersistentVolume":               10,
		"PersistentVolumeClaim":          11,
		"CustomResourceDefinition":       12,
		"ClusterRole":                    13,
		"ClusterRoleBinding":             14,
		"Role":                           15,
		"RoleBinding":                    16,
		"Service":                        17,
		"DaemonSet":                      18,
		"Pod":                            19,
		"ReplicationController":          20,
		"ReplicaSet":                     21,
		"Deployment":                     22,
		"HorizontalPodAutoscaler":        23,
		"StatefulSet":                    24,
		"Job":                            25,
		"CronJob":                        26,
		"IngressClass":                   27,
		"Ingress":                        28,
		"APIService":                     29,
		"MutatingWebhookConfiguration":   30,
		"ValidatingWebhookConfiguration": 31,
	}
)

// gatherSelectedResource gets all the resources according to the resource selector.
func (r *Reconciler) gatherSelectedResource(placementKey types.NamespacedName, selectors []fleetv1beta1.ResourceSelectorTerm) ([]*unstructured.Unstructured, error) {
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
		if gvk == utils.NamespaceGVK && placementKey.Namespace == "" && selector.SelectionScope != fleetv1beta1.NamespaceOnly {
			objs, err = r.fetchNamespaceResources(selector, placementKey.Name)
		} else {
			objs, err = r.fetchResources(selector, placementKey)
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
				klog.ErrorS(err, "User selected one resource more than once", "resource", ri, "placement", placementKey)
				return nil, controller.NewUserError(err)
			}
			resourceMap[ri] = true
			resources = append(resources, uObj)
		}
	}
	// sort the resources in strict order so that we will get the stable list of manifest so that
	// the generated work object doesn't change between reconcile loops.
	sortResources(resources)

	return resources, nil
}

func sortResources(resources []*unstructured.Unstructured) {
	sort.Slice(resources, func(i, j int) bool {
		obj1 := resources[i]
		obj2 := resources[j]
		k1 := obj1.GetObjectKind().GroupVersionKind().Kind
		k2 := obj2.GetObjectKind().GroupVersionKind().Kind

		first, aok := resourceSortOrder[k1]
		second, bok := resourceSortOrder[k2]
		switch {
		// if both kinds are unknown.
		case !aok && !bok:
			return lessByGVK(obj1, obj2, false)
		// unknown kind should be last.
		case !aok:
			return false
		case !bok:
			return true
		// same kind.
		case first == second:
			return lessByGVK(obj1, obj2, true)
		}
		// different known kinds, sort based on order index.
		return first < second
	})
}

func lessByGVK(obj1, obj2 *unstructured.Unstructured, ignoreKind bool) bool {
	var gvk1, gvk2 string
	if ignoreKind {
		gvk1 = obj1.GetObjectKind().GroupVersionKind().GroupVersion().String()
		gvk2 = obj2.GetObjectKind().GroupVersionKind().GroupVersion().String()
	} else {
		gvk1 = obj1.GetObjectKind().GroupVersionKind().String()
		gvk2 = obj2.GetObjectKind().GroupVersionKind().String()
	}
	comp := strings.Compare(gvk1, gvk2)
	if comp == 0 {
		return strings.Compare(fmt.Sprintf("%s/%s", obj1.GetNamespace(), obj1.GetName()),
			fmt.Sprintf("%s/%s", obj2.GetNamespace(), obj2.GetName())) < 0
	}
	return comp < 0
}

// fetchResources retrieves the objects based on the selector.
func (r *Reconciler) fetchResources(selector fleetv1beta1.ResourceSelectorTerm, placementKey types.NamespacedName) ([]runtime.Object, error) {
	klog.V(2).InfoS("Start to fetch resources by the selector", "selector", selector, "placement", placementKey)
	gk := schema.GroupKind{
		Group: selector.Group,
		Kind:  selector.Kind,
	}
	restMapping, err := r.RestMapper.RESTMapping(gk, selector.Version)
	if err != nil {
		return nil, controller.NewUserError(fmt.Errorf("invalid placement %s, failed to get GVR of the selector: %w", placementKey, err))
	}
	gvr := restMapping.Resource
	gvk := schema.GroupVersionKind{
		Group:   selector.Group,
		Version: selector.Version,
		Kind:    selector.Kind,
	}

	isNamespacedResource := !r.InformerManager.IsClusterScopedResources(gvk)
	if isNamespacedResource && placementKey.Namespace == "" {
		// If it's a namespace-scoped resource but placement has no namespace, return error.
		err := fmt.Errorf("invalid placement %s: cannot select namespace-scoped resource %v in a clusterResourcePlacement", placementKey, gvr)
		klog.ErrorS(err, "Invalid resource selector", "selector", selector)
		return nil, controller.NewUserError(err)
	} else if !isNamespacedResource && placementKey.Namespace != "" {
		// If it's a cluster-scoped resource but placement has a namespace, return error.
		err := fmt.Errorf("invalid placement %s: cannot select cluster-scoped resource %v in a resourcePlacement", placementKey, gvr)
		klog.ErrorS(err, "Invalid resource selector", "selector", selector)
		return nil, controller.NewUserError(err)
	}

	if !r.InformerManager.IsInformerSynced(gvr) {
		err := fmt.Errorf("informer cache for %+v is not synced yet", restMapping.Resource)
		klog.ErrorS(err, "Informer cache is not synced", "gvr", gvr, "placement", placementKey)
		return nil, controller.NewExpectedBehaviorError(err)
	}

	lister := r.InformerManager.Lister(gvr)

	// TODO: validator should enforce the mutual exclusiveness between the `name` and `labelSelector` fields
	if len(selector.Name) != 0 {
		var obj runtime.Object
		var err error

		if isNamespacedResource {
			obj, err = lister.ByNamespace(placementKey.Namespace).Get(selector.Name)
		} else {
			obj, err = lister.Get(selector.Name)
		}

		if err != nil {
			klog.ErrorS(err, "Cannot get the resource", "gvr", gvr, "name", selector.Name, "namespace", placementKey.Namespace)
			return nil, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
		}

		shouldInclude, err := r.shouldPropagateObj(placementKey.Namespace, placementKey.Name, obj)
		if err != nil {
			return nil, err
		}
		if shouldInclude {
			return []runtime.Object{obj}, nil
		}
		return []runtime.Object{}, nil
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
	var objects []runtime.Object

	if isNamespacedResource {
		objects, err = lister.ByNamespace(placementKey.Namespace).List(labelSelector)
	} else {
		objects, err = lister.List(labelSelector)
	}
	if err != nil {
		klog.ErrorS(err, "Cannot list all the objects", "gvr", gvr, "labelSelector", labelSelector, "placement", placementKey)
		return nil, controller.NewAPIServerError(true, err)
	}

	// go ahead and claim all objects by adding a finalizer and insert the placement in its annotation
	for i := 0; i < len(objects); i++ {
		shouldInclude, err := r.shouldPropagateObj(placementKey.Namespace, placementKey.Name, objects[i])
		if err != nil {
			return nil, err
		}
		if shouldInclude {
			selectedObjs = append(selectedObjs, objects[i])
		}
	}

	return selectedObjs, nil
}

func (r *Reconciler) shouldPropagateObj(namespace, placementName string, obj runtime.Object) (bool, error) {
	uObj := obj.DeepCopyObject().(*unstructured.Unstructured)
	uObjKObj := klog.KObj(uObj)
	if uObj.GetDeletionTimestamp() != nil {
		// skip a to be deleted resource
		klog.V(2).InfoS("Skip the deleting resource by the selector", "namespace", namespace, "placement", placementName, "object", uObjKObj)
		return false, nil
	}

	shouldInclude, err := utils.ShouldPropagateObj(r.InformerManager, uObj)
	if err != nil {
		klog.ErrorS(err, "Cannot determine if we should propagate an object", "namespace", namespace, "placement", placementName, "object", uObjKObj)
		return false, err
	}
	if !shouldInclude {
		klog.V(2).InfoS("Skip the resource by the selector which is forbidden", "namespace", namespace, "placement", placementName, "object", uObjKObj)
		return false, nil
	}
	return true, nil
}

// fetchNamespaceResources retrieves all the objects for a ResourceSelectorTerm that is for namespace.
func (r *Reconciler) fetchNamespaceResources(selector fleetv1beta1.ResourceSelectorTerm, placementName string) ([]runtime.Object, error) {
	klog.V(2).InfoS("start to fetch the namespace resources by the selector", "selector", selector)
	var resources []runtime.Object

	if len(selector.Name) != 0 {
		// just a single namespace
		objs, err := r.fetchAllResourcesInOneNamespace(selector.Name, placementName)
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
		klog.ErrorS(err, "Cannot list all the namespaces by the label selector", "labelSelector", labelSelector, "placement", placementName)
		return nil, controller.NewAPIServerError(true, err)
	}

	for _, namespace := range namespaces {
		ns, err := meta.Accessor(namespace)
		if err != nil {
			return nil, controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot get the name of a namespace object: %w", err))
		}
		objs, err := r.fetchAllResourcesInOneNamespace(ns.GetName(), placementName)
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
			klog.ErrorS(err, "Cannot list all the objects in namespace", "gvr", gvr, "namespace", namespaceName)
			return nil, controller.NewAPIServerError(true, err)
		}
		for _, obj := range objs {
			shouldInclude, err := r.shouldPropagateObj(namespaceName, placeName, obj)
			if err != nil {
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
func (r *Reconciler) selectResourcesForPlacement(placementObj fleetv1beta1.PlacementObj) (int, []fleetv1beta1.ResourceContent, []fleetv1beta1.ResourceIdentifier, error) {
	envelopeObjCount := 0
	selectedObjects, err := r.gatherSelectedResource(types.NamespacedName{
		Name:      placementObj.GetName(),
		Namespace: placementObj.GetNamespace(),
	}, placementObj.GetPlacementSpec().ResourceSelectors)
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
		uGVK := unstructuredObj.GetObjectKind().GroupVersionKind().GroupKind()
		switch uGVK {
		case utils.ClusterResourceEnvelopeGK:
			envelopeObjCount++
		case utils.ResourceEnvelopeGK:
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
