/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package resourcewatcher

import (
	"context"
	"reflect"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/keys"
)

// make sure that our ChangeDetector implements controller runtime interfaces
var (
	_ manager.Runnable               = &ChangeDetector{}
	_ manager.LeaderElectionRunnable = &ChangeDetector{}
)

// ChangeDetector is a resource watcher which watches all resources and reconcile the events.
type ChangeDetector struct {
	// DiscoveryClientSet is used to do resource discovery.
	DiscoveryClientSet *discovery.DiscoveryClient

	// RESTMapper is used to convert between GVK and GVR
	RESTMapper meta.RESTMapper

	// ClusterResourcePlacementController maintains a rate limited queue which used to store
	// the name of the clusterResourcePlacement and a reconcile function to consume the items in queue.
	ClusterResourcePlacementController controller.Controller

	// ClusterResourcePlacementController maintains a rate limited queue which used to store any resources'
	// cluster wide key and a reconcile function to consume the items in queue.
	ResourceChangeController controller.Controller

	// InformerManager manages all the dynamic informers created by the discovery client
	InformerManager utils.InformerManager

	// DisabledResourceConfig contains all the api resources that we won't select
	DisabledResourceConfig *utils.DisabledResourceConfig

	// AvoidedPropagatingNamespaces contains all the namespaces that we will avoid select
	AvoidedPropagatingNamespaces map[string]bool

	// resourceChangeEventHandler is the event handler for any resource change informer
	resourceChangeEventHandler cache.ResourceEventHandler
}

// Start runs the detector, never stop until stopCh closed.
func (d *ChangeDetector) Start(ctx context.Context) error {
	klog.Infof("Starting resource detector.")

	// watch and enqueue ClusterPropagationPolicy changes.
	clusterResourcePlacementGVR := schema.GroupVersionResource{
		Group:    fleetv1alpha1.GroupVersion.Group,
		Version:  fleetv1alpha1.GroupVersion.Version,
		Resource: fleetv1alpha1.ClusterResourcePlacementResource,
	}
	memberClusterGVR := schema.GroupVersionResource{
		Group:    fleetv1alpha1.GroupVersion.Group,
		Version:  fleetv1alpha1.GroupVersion.Version,
		Resource: fleetv1alpha1.MemberClusterResource,
	}
	clusterPlacementEventHandler := newHandlerOnEvents(d.onClusterResourcePlacementAdd,
		d.onClusterResourcePlacementUpdated, d.onClusterResourcePlacementDeleted)
	d.InformerManager.ForResource(clusterResourcePlacementGVR, clusterPlacementEventHandler)

	// TODO: use a different event handler that list all placement and enqueue them
	d.InformerManager.ForResource(memberClusterGVR, clusterPlacementEventHandler)

	// TODO: add work informer that enqueue the placement name (stored in its label)

	// setup the resourceChangeEventHandler
	d.resourceChangeEventHandler = newFilteringHandlerOnAllEvents(d.resourceFilter,
		d.onResourceAdd, d.onResourceUpdated, d.onResourceDeleted)
	// start the resource type list loop
	go d.discoverResources(ctx, 30*time.Second)

	// We run the two controller in parallel
	errs, cctx := errgroup.WithContext(ctx)
	errs.Go(func() error {
		//TODO: use options passed in from flags for work number
		return d.ClusterResourcePlacementController.Run(cctx, 5)
	})
	errs.Go(func() error {
		//TODO: use options passed in from flags for work number
		return d.ResourceChangeController.Run(cctx, 20)
	})

	return errs.Wait()
}

// discoverResources
func (d *ChangeDetector) discoverResources(ctx context.Context, period time.Duration) {
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		newResources := utils.GetDeletableResources(d.DiscoveryClientSet)
		for r := range newResources {
			if d.isResourceDisabled(r) {
				continue
			}
			klog.Infof("Setup informer for %s", r.String())
			d.InformerManager.ForResource(r, d.resourceChangeEventHandler)
		}
		d.InformerManager.Start()
	}, period)
}

// gvrDisabled returns whether GroupVersionResource is disabled.
func (d *ChangeDetector) isResourceDisabled(gvr schema.GroupVersionResource) bool {
	if d.DisabledResourceConfig == nil {
		return false
	}

	gvks, err := d.RESTMapper.KindsFor(gvr)
	if err != nil {
		klog.Errorf("gvr(%s) transform failed: %v", gvr.String(), err)
		return false
	}

	for _, gvk := range gvks {
		if d.DisabledResourceConfig.IsResourceDisabled(gvk) {
			klog.V(4).InfoS("Skip watch resource", "group version kind", gvk.String())
			return true
		}
	}

	return false
}

// resourceFilter filters out resources that we don't want to watch
func (d *ChangeDetector) resourceFilter(obj interface{}) bool {
	key, err := controller.ClusterWideKeyFunc(obj)
	if err != nil {
		return false
	}

	clusterWideKey, ok := key.(keys.ClusterWideKey)
	if !ok {
		klog.Errorf("Invalid key")
		return false
	}

	// if AvoidedPropagatingNamespaces is set, skip object events in these namespaces.
	if _, ok := d.AvoidedPropagatingNamespaces[clusterWideKey.Namespace]; ok {
		klog.V(5).InfoS("Skip watch resource in namespace", "namespace", clusterWideKey.Namespace)
		return false
	}

	if unstructObj, ok := obj.(*unstructured.Unstructured); ok {
		switch unstructObj.GroupVersionKind() {
		// The secret, with type 'kubernetes.io/service-account-token', is created along with `ServiceAccount` should be
		// prevented from propagating.
		case corev1.SchemeGroupVersion.WithKind("Secret"):
			secretType, found, _ := unstructured.NestedString(unstructObj.Object, "type")
			if found && secretType == string(corev1.SecretTypeServiceAccountToken) {
				return false
			}
		}
	}

	return true
}

// onClusterResourcePlacementAdd handles object add event and push the object to queue.
func (d *ChangeDetector) onClusterResourcePlacementAdd(obj interface{}) {
	klog.V(5).InfoS("ClusterResourcePlacement Added", "obj", obj)
	d.ClusterResourcePlacementController.Enqueue(obj)
}

// onClusterResourcePlacementUpdated handles object update event and push the object to queue.
func (d *ChangeDetector) onClusterResourcePlacementUpdated(oldObj, newObj interface{}) {
	klog.V(5).InfoS("ClusterResourcePlacement Updated", "oldObj", oldObj, "newObj", newObj)
	d.ClusterResourcePlacementController.Enqueue(newObj)
}

// onClusterResourcePlacementDeleted handles object delete event and push the object to queue.
func (d *ChangeDetector) onClusterResourcePlacementDeleted(obj interface{}) {
	klog.V(5).InfoS("ClusterResourcePlacement Deleted", "obj", obj)
	d.ClusterResourcePlacementController.Enqueue(obj)
}

// onResourceAdd handles object add event and push the object to queue.
func (d *ChangeDetector) onResourceAdd(obj interface{}) {
	klog.V(5).InfoS("Resource Added", "obj", obj)
	d.ResourceChangeController.Enqueue(obj)
}

// onResourceUpdated handles object update event and push the object to queue.
func (d *ChangeDetector) onResourceUpdated(oldObj, newObj interface{}) {
	klog.V(5).InfoS("Resource Updated", "oldObj", oldObj, "newObj", newObj)
	if !reflect.DeepEqual(oldObj, newObj) {
		d.ResourceChangeController.Enqueue(newObj)
	}
}

// onResourceDeleted handles object delete event and push the object to queue.
func (d *ChangeDetector) onResourceDeleted(obj interface{}) {
	klog.V(5).InfoS("Resource Deleted", "obj", obj)
	d.ResourceChangeController.Enqueue(obj)
}

// NeedLeaderElection implements LeaderElectionRunnable interface.
// So that the detector could run in the leader election mode.
func (d *ChangeDetector) NeedLeaderElection() bool {
	return true
}

// newHandlerOnEvents builds a ResourceEventHandler.
func newHandlerOnEvents(addFunc func(obj interface{}), updateFunc func(oldObj, newObj interface{}), deleteFunc func(obj interface{})) cache.ResourceEventHandler {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc:    addFunc,
		UpdateFunc: updateFunc,
		DeleteFunc: deleteFunc,
	}
}

// newFilteringHandlerOnAllEvents builds a FilteringResourceEventHandler applies the provided filter to all events
// coming in, ensuring the appropriate nested handler method is invoked.
//
// Note: An object that starts passing the filter after an update is considered an add, and
// an object that stops passing the filter after an update is considered a delete.
// Like the handlers, the filter MUST NOT modify the objects it is given.
func newFilteringHandlerOnAllEvents(filterFunc func(obj interface{}) bool, addFunc func(obj interface{}),
	updateFunc func(oldObj, newObj interface{}), deleteFunc func(obj interface{})) cache.ResourceEventHandler {
	return &cache.FilteringResourceEventHandler{
		FilterFunc: filterFunc,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    addFunc,
			UpdateFunc: updateFunc,
			DeleteFunc: deleteFunc,
		},
	}
}
