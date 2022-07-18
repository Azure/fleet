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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"

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
		klog.ErrorS(err, "Failed to get unstructured object", "key", clusterWideKey)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if object != nil {
		matchedRS, err := r.findMatchedPolicies(object)
		if err != nil {
			klog.Errorf("failed to find matched policies for dynamic resource %s: err %+v", clusterWideKey, err)
			return ctrl.Result{}, err
		}
		// enqueue
		for _, crp := range matchedRS {
			klog.Infof("enqueue crp %s to placementcontroller queue", crp.Name)
			r.PlacementController.Enqueue(crp.Name)
		}
	}
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
			klog.Infof("getUnstructuredObject %+v not found, ignore", objectKey)
			return nil, nil
		}
		// print logs only for real error.
		klog.ErrorS(err, "Failed to get obj", "object", objectKey)
		return nil, err
	}
	objUnstructured, ok := obj.(*unstructured.Unstructured)
	if !ok || objUnstructured == nil {
		klog.Warningf("getUnstructuredObject obj %+v is not an unstructured or nil, ignore", obj)
		return nil, nil
	}
	return objUnstructured, nil
}

// findMatchedPolicies find all the polices that this unstructured object is referenced
func (r *Reconciler) findMatchedPolicies(object *unstructured.Unstructured) ([]*fleetv1alpha1.ClusterResourcePlacement, error) {
	rs := make([]*fleetv1alpha1.ClusterResourcePlacement, 0)
	requestObject := types.NamespacedName{
		Name:      object.GetName(),
		Namespace: object.GetNamespace(),
	}
	requestObjectGVK := object.GetObjectKind().GroupVersionKind()
	// now get all crp list
	crpList, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).List(labels.Everything())

	if err != nil {
		klog.Errorf("findMatchedPolicies failed to get ClusterResourcePlacement list: %+v", err)
		return rs, err
	}

	for i := range crpList {
		crp, ok := crpList[i].(*fleetv1alpha1.ClusterResourcePlacement)
		if !ok {
			klog.Warningf("findMatchedPolicies failed to convert from runtime.object %+v, ignore: %+v", crpList[i], err)
			continue
		}
		// check if object match any crp's resource selectors
		for _, selector := range crp.Spec.ResourceSelectors {
			selectorMatch, err := r.matchClusterResourceSelector(selector, requestObject, requestObjectGVK, object.GetLabels())
			if err != nil {
				klog.Warningf("findMatchedPolicies find invalid policy %s, ignore: err %+v", crp.Name, err)
				continue
			}
			if selectorMatch {
				// if there is 1 selector match, it is a crp match, add only once
				rs = append(rs, crp)
				break
			}
		}
	}
	return rs, nil
}

func (r *Reconciler) matchClusterResourceSelector(selector fleetv1alpha1.ClusterResourceSelector,
	requestObject types.NamespacedName, requestObjectGVK schema.GroupVersionKind,
	requestObjectLabels map[string]string) (bool, error) {
	allNamespaceScopedResources := r.InformerManager.GetNameSpaceScopedResources()

	// only when gvk is a match, continue check on name or labelselector
	// namespace scoped
	if isNamespaceScoped(requestObjectGVK, allNamespaceScopedResources) {
		if selector.Kind == "Namespace" {
			if selector.Name == requestObject.Namespace {
				// if requestObject's namespace name matches with selector's name
				return true, nil
			}
			// requestObject's namespace labelSelector matches with selector's labelSelector
			return matchSelectorLabelSelector(requestObjectLabels, selector)
		}
		return false, nil
	}
	// cluster scoped
	if !matchSelectorGVK(requestObjectGVK, selector) {
		// gvk not even match, return
		return false, nil
	}
	if selector.Name == requestObject.Name {
		return true, nil
	}
	return matchSelectorLabelSelector(requestObjectLabels, selector)
}

func isNamespaceScoped(targetGVK schema.GroupVersionKind, namespaceScopedResources []schema.GroupVersionResource) bool {
	for _, r := range namespaceScopedResources {
		if r.Group == targetGVK.Group && r.Version == targetGVK.Version &&
			r.Resource == PluralName(targetGVK.Kind) {
			return true
		}
	}
	return false
}

func matchSelectorGVK(targetGVK schema.GroupVersionKind, selector fleetv1alpha1.ClusterResourceSelector) bool {
	return selector.Group == targetGVK.Group && selector.Version == targetGVK.Version &&
		selector.Kind == targetGVK.Kind
}

func matchSelectorLabelSelector(targetLabels map[string]string, selector fleetv1alpha1.ClusterResourceSelector) (bool, error) {
	if selector.LabelSelector == nil {
		// if labelselector not set, not match
		return false, nil
	}
	s, err := metav1.LabelSelectorAsSelector(selector.LabelSelector)
	if err != nil {
		return false, fmt.Errorf("user input invalid label selector %+v, err %w", selector, err)
	}
	if s.Matches(labels.Set(targetLabels)) {
		return true, nil
	}
	return false, nil
}

// PluralName computes the plural name from the kind by
// lowercasing and suffixing with 's' or `es`.
func PluralName(kind string) string {
	lowerKind := strings.ToLower(kind)
	if strings.HasSuffix(lowerKind, "s") || strings.HasSuffix(lowerKind, "x") ||
		strings.HasSuffix(lowerKind, "ch") || strings.HasSuffix(lowerKind, "sh") ||
		strings.HasSuffix(lowerKind, "z") || strings.HasSuffix(lowerKind, "o") {
		return fmt.Sprintf("%ses", lowerKind)
	}
	if strings.HasSuffix(lowerKind, "y") {
		lowerKind = strings.TrimSuffix(lowerKind, "y")
		return fmt.Sprintf("%sies", lowerKind)
	}
	return fmt.Sprintf("%ss", lowerKind)
}
