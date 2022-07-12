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

	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/keys"
)

// make sure that our ChangeDetector implements controller runtime interfaces
var (
	_ manager.Runnable               = &ChangeDetector{}
	_ manager.LeaderElectionRunnable = &ChangeDetector{}
)

// ChangeDetector is a resource watcher which watches all types of resources in the cluster and reconcile the events.
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

	// SkippedNamespaces contains all the namespaces that we won't select
	SkippedNamespaces map[string]bool

	// dynamicResourceChangeEventHandler is the event handler for any resource change informer
	dynamicResourceChangeEventHandler cache.ResourceEventHandler
}

// Start runs the detector, never stop until stopCh closed.
func (d *ChangeDetector) Start(ctx context.Context) error {
	klog.Infof("Starting the api resource change detector")

	// Ensure all informers are closed when the context closes
	defer klog.Infof("The api resource change detector is stopped")
	defer d.InformerManager.Stop()

	clusterPlacementEventHandler := newHandlerOnEvents(d.onClusterResourcePlacementAdd,
		d.onClusterResourcePlacementUpdated, d.onClusterResourcePlacementDeleted)
	d.InformerManager.AddStaticResource(
		utils.APIResourceMeta{
			GroupVersionResource: utils.ClusterResourcePlacementGVR,
			IsClusterScoped:      true,
		}, clusterPlacementEventHandler)

	// TODO: use a different event handler that list all placements and enqueue them
	d.InformerManager.AddStaticResource(
		utils.APIResourceMeta{
			GroupVersionResource: utils.MemberClusterGVR,
			IsClusterScoped:      true,
		}, clusterPlacementEventHandler)

	// TODO: add work informer that enqueue the placement name (stored in its label)

	// setup the dynamicResourceChangeEventHandler that enqueue an event to the resource change controller's queue
	d.dynamicResourceChangeEventHandler = newFilteringHandlerOnAllEvents(d.dynamicResourceFilter,
		d.onResourceAdd, d.onResourceUpdated, d.onResourceDeleted)
	// start the resource type list loop
	go d.discoverAPIResources(ctx, 30*time.Second)

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

// discoverAPIResources goes through all the api resources in the cluster and create informers on selected types
func (d *ChangeDetector) discoverAPIResources(ctx context.Context, period time.Duration) {
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		newResources, err := utils.GetWatchableResources(d.DiscoveryClientSet)
		var dynamicResources []utils.APIResourceMeta
		if err != nil {
			klog.Warningf("Failed to get all the api resources from the cluster, err = %v", err)
		}
		for _, res := range newResources {
			// all the static resources are disabled by default
			if !d.isResourceDisabled(res.GroupVersionResource) {
				dynamicResources = append(dynamicResources, res)
			}
		}
		d.InformerManager.AddDynamicResources(dynamicResources, d.dynamicResourceChangeEventHandler, err == nil)
		// this will start the newly added informers if there is any
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

// dynamicResourceFilter filters out resources that we don't want to watch
func (d *ChangeDetector) dynamicResourceFilter(obj interface{}) bool {
	key, err := controller.ClusterWideKeyFunc(obj)
	if err != nil {
		return false
	}

	clusterWideKey, ok := key.(keys.ClusterWideKey)
	if !ok {
		klog.Errorf("Invalid key")
		return false
	}

	// if SkippedNamespaces is set, skip any events related to the object in these namespaces.
	if _, ok := d.SkippedNamespaces[clusterWideKey.Namespace]; ok {
		klog.V(5).InfoS("Skip watch resource in namespace", "namespace", clusterWideKey.Namespace)
		return false
	}

	if unstructuredObj, ok := obj.(*unstructured.Unstructured); ok {
		// TODO:  add more special handling here
		switch unstructuredObj.GroupVersionKind() {
		// The secret, with type 'kubernetes.io/service-account-token', is created along with `ServiceAccount` should be
		// prevented from propagating.
		case corev1.SchemeGroupVersion.WithKind("Secret"):
			secretType, found, _ := unstructured.NestedString(unstructuredObj.Object, "type")
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
	// TODO: see if we can check the generation
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
