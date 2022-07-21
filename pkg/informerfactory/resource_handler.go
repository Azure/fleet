package informerfactory

import (
	"context"
	"fmt"
	"go.goms.io/fleet/apis/v1alpha1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

var ignoredNamespaces = []string{"kube-system", "gatekeeper-system", "fleet-system"}

type ResourceHandler interface {
	ResourceEventHandler
	Run()
}

type resourceHandler struct {
	gvk                     schema.GroupVersionKind
	apiResource             metav1.APIResource
	delivererHandler        *PlacementQueue
	informer                ResourceInformer
	controllerManagerClient client.Client
	wq                      workqueue.RateLimitingInterface
	stopCh                  <-chan struct{}
}

func NewResourceHandler(apiResource metav1.APIResource, informer ResourceInformer,
	client client.Client, delivererHandler *PlacementQueue, stopCh <-chan struct{}) ResourceHandler {
	gvk := informer.TargetGVK()
	return &resourceHandler{
		gvk:                     gvk,
		informer:                informer,
		apiResource:             apiResource,
		stopCh:                  stopCh,
		controllerManagerClient: client,
		delivererHandler:        delivererHandler,
		wq:                      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "dynamic-resource-queue"),
	}
}

func (r *resourceHandler) Name() string { return r.gvk.String() }

func (r *resourceHandler) OnAdd(cur interface{}) {
	obj, ok := cur.(runtime.Object)
	if !ok {
		klog.Warningf("cur %+v not runtime object, ignore", cur)
		return
	}
	accessor, _ := meta.Accessor(cur)
	if shouldIgnore(accessor.GetNamespace()) {
		return
	}
	klog.Infof("onAdd %s %s/%s, gv: %v", r.gvk.String(), accessor.GetNamespace(), accessor.GetName(), accessor.GetGeneration())
	r.wq.Add(obj)
}

func (r *resourceHandler) OnUpdate(old, cur interface{}) {
	obj, ok := cur.(runtime.Object)
	if !ok {
		klog.Warningf("cur %+v not runtime object, ignore", cur)
		return
	}
	_, ok = old.(runtime.Object)
	if !ok {
		klog.Warningf("old %+v not runtime object, ignore", old)
		return
	}
	accessorNew, _ := meta.Accessor(cur)
	accessorOld, _ := meta.Accessor(old)

	if accessorNew.GetGeneration() != 0 && accessorNew.GetGeneration() <= accessorOld.GetGeneration() {
		// configmaps does not have generation, any change needs to pass down
		return
	}
	if shouldIgnore(accessorNew.GetNamespace()) {
		return
	}
	klog.Infof("onUpdate %s %s/%s, gv: %v", r.gvk.String(), accessorNew.GetNamespace(), accessorNew.GetName(), accessorNew.GetGeneration())
	r.wq.Add(obj)
}

func (r *resourceHandler) OnDelete(old interface{}) {
	oldObj, ok := old.(runtime.Object)
	if !ok {
		klog.Warningf("old %+v not runtime object, ignore", old)
		return
	}
	accessor, _ := meta.Accessor(old)
	if shouldIgnore(accessor.GetNamespace()) {
		return
	}
	klog.Infof("onDelete %s %s/%s, gv: %v", r.gvk.String(), accessor.GetNamespace(), accessor.GetName(), accessor.GetGeneration())
	r.wq.Add(oldObj)
}

func shouldIgnore(namespace string) bool {
	for _, ignoredNS := range ignoredNamespaces {
		if ignoredNS == namespace {
			return true
		}
	}
	return strings.HasSuffix(namespace, "fleet-")
}

func (r *resourceHandler) Run() {
	go wait.Until(func() {
		//klog.Infof("Starting processing placement task queue...")
		for r.processTask() {
		}
	}, 0, r.stopCh)

	// ensure task queue is shutdown when stopch closes
	go func() {
		<-r.stopCh
		r.wq.ShutDown()
	}()
}

func (r *resourceHandler) processTask() bool {
	work, quit := r.wq.Get()
	if quit {
		//klog.Infof("dynamic resource wq shutting down...")
		return false
	}
	defer r.wq.Done(work)

	runtimeObj, ok := work.(runtime.Object)
	if !ok {
		return true
	}

	matchedPolicies, err := r.findMatchedPolicies(runtimeObj)
	if err != nil {
		klog.Errorf("failed to find match policies for %+v: err %+v", runtimeObj, err)
		r.wq.AddRateLimited(work)
	}

	for _, policy := range matchedPolicies {
		r.delivererHandler.Deliver(&v1alpha1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      policy.Name,
				Namespace: policy.Namespace,
			},
		}, runtimeObj, r.gvk)
	}
	return true
}

func (r *resourceHandler) findMatchedPolicies(obj runtime.Object) ([]*v1alpha1.ClusterResourcePlacement, error) {
	rs := make([]*v1alpha1.ClusterResourcePlacement, 0)
	objAccessor, err := meta.Accessor(obj)
	if err != nil {
		return rs, err
	}
	objNamespaceName := types.NamespacedName{
		Namespace: objAccessor.GetNamespace(),
		Name:      objAccessor.GetName(),
	}
	namespaceObj := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: objAccessor.GetNamespace(),
		},
	}
	namespaceObjLabels := make(map[string]string)
	err = r.controllerManagerClient.Get(context.TODO(), types.NamespacedName{Name: objAccessor.GetNamespace()}, namespaceObj)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return rs, err
		}
		// namespace not found, the runtime.object could be a cluster-scoped object that does not have a namespace
	} else {
		namespaceObjLabels = namespaceObj.GetLabels()
	}
	crpList := &v1alpha1.ClusterResourcePlacementList{}
	err = r.controllerManagerClient.List(context.TODO(), crpList)
	if err != nil {
		return rs, err
	}
	for _, crp := range crpList.Items {
		// check if object match any crp's resource selectors
		for _, selector := range crp.Spec.ResourceSelectors {
			selectorMatch, err := r.matchClusterResourceSelector(selector, objNamespaceName, r.gvk, objAccessor.GetLabels(), namespaceObjLabels)
			if err != nil {
				klog.Warningf("findMatchedPolicies find invalid policy %s, ignore: err %+v", crp.Name, err)
				continue
			}
			if selectorMatch {
				// if there is 1 selector match, it is a crp match, add only once
				rs = append(rs, &v1alpha1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      crp.Name,
						Namespace: crp.Namespace,
					},
				})
				break
			}
		}
	}
	return rs, nil
}

func (r *resourceHandler) matchClusterResourceSelector(selector v1alpha1.ClusterResourceSelector,
	requestObject types.NamespacedName, requestObjectGVK schema.GroupVersionKind,
	requestObjectLabels map[string]string,
	requestObjectNamespaceLabels map[string]string) (bool, error) {
	return matchClusterResourceSelector(selector, r.apiResource.Namespaced, requestObject,
		requestObjectGVK, requestObjectLabels, requestObjectNamespaceLabels)
}

func matchSelectorGVK(targetGVK schema.GroupVersionKind, selector v1alpha1.ClusterResourceSelector) bool {
	return selector.Group == targetGVK.Group && selector.Version == targetGVK.Version &&
		selector.Kind == targetGVK.Kind
}

func matchSelectorLabelSelector(targetLabels map[string]string, selector v1alpha1.ClusterResourceSelector) (bool, error) {
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

func matchClusterResourceSelector(
	selector v1alpha1.ClusterResourceSelector,
	requestObjectIsNamespaced bool,
	requestObject types.NamespacedName,
	requestObjectGVK schema.GroupVersionKind,
	requestObjectLabels map[string]string,
	requestObjectNamespaceLabels map[string]string) (bool, error) {

	// namespace scoped
	if requestObjectIsNamespaced {
		if selector.Kind == "Namespace" {
			if selector.Name == requestObject.Namespace {
				// if requestObject's namespace name matches with selector's name
				return true, nil
			}
			// requestObject's namespace labelSelector matches with selector's labelSelector
			return matchSelectorLabelSelector(requestObjectNamespaceLabels, selector)
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
