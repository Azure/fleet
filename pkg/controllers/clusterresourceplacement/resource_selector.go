package clusterresourceplacement

import (
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

// getSelectedResource get all the resources according to the resource selector.
func (r *Reconciler) gatherSelectedResource(placement *fleetv1alpha1.ClusterResourcePlacement) ([]runtime.Object, error) {
	var resources []runtime.Object
	for _, selector := range placement.Spec.ResourceSelectors {
		gvk := schema.GroupVersionKind{
			Group:   selector.Group,
			Version: selector.Version,
			Kind:    selector.Kind,
		}

		if r.DisabledResourceConfig.IsResourceDisabled(gvk) {
			klog.V(3).InfoS("Skip select resource", "group version kind", gvk.String())
			continue
		}
		var objs []runtime.Object
		var err error
		if gvk == utils.NamespaceGVK {
			objs, err = r.fetchNamespaceResources(selector)
		} else {
			objs, err = r.fetchClusterScopedResources(selector)
		}
		if err != nil {
			// TODO: revisit if return partial result makes sense
			return nil, errors.Wrap(err, fmt.Sprintf("selector = %v", selector))
		}
		resources = append(resources, objs...)
	}
	return resources, nil
}

// fetchClusterScopedResources retrieve the objects based on the selector.
func (r *Reconciler) fetchClusterScopedResources(selector fleetv1alpha1.ClusterResourceSelector) ([]runtime.Object, error) {
	klog.V(4).InfoS("start to fetch the resources by the selector", "selector", selector)
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

	objects, err := lister.List(labelSelector)
	if err != nil {
		return nil, errors.Wrap(err, "cannot list all the objets")
	}
	return objects, nil
}

// fetchNamespaceResources retrieve all the objects for a ClusterResourceSelector that is for namespace.
func (r *Reconciler) fetchNamespaceResources(selector fleetv1alpha1.ClusterResourceSelector) ([]runtime.Object, error) {
	klog.V(4).InfoS("start to fetch the namespace resources by the selector", "selector", selector)
	var resources []runtime.Object

	if len(selector.Name) != 0 {
		// just a single namespace
		return r.fetchAllResourcesInOneNamespace(selector.Name)
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
		objs, err := r.fetchAllResourcesInOneNamespace(ns.GetName())
		if err != nil {
			return nil, err
		}
		resources = append(resources, objs...)
	}

	return resources, nil
}

// fetchAllResourcesInOneNamespace retrieve all the objects inside a single namespace.
func (r *Reconciler) fetchAllResourcesInOneNamespace(name string) ([]runtime.Object, error) {
	klog.V(4).InfoS("start to fetch all the resources inside a namespace", "namespace", name)
	var resources []runtime.Object
	trackedResource := r.InformerManager.GetNameSpaceScopedResources()
	for _, gvr := range trackedResource {
		lister := r.InformerManager.Lister(gvr)
		objs, err := lister.ByNamespace(name).List(labels.Everything())
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("cannot list all the objects of type %+v in namespace %s", gvr, name))
		}
		resources = append(resources, objs...)
	}

	return resources, nil
}
