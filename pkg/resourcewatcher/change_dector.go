/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package resourcewatcher

import (
	"context"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
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
	// DiscoveryClient is used to do resource discovery.
	DiscoveryClient *discovery.DiscoveryClient

	// RESTMapper is used to convert between GVK and GVR
	RESTMapper meta.RESTMapper

	// ClusterResourcePlacementController maintains a rate limited queue which is used to store
	// the name of the changed clusterResourcePlacement and a reconcile function to consume the items in queue.
	ClusterResourcePlacementController controller.Controller

	// ClusterResourcePlacementController maintains a rate limited queue which is used to store any resources'
	// cluster wide key and a reconcile function to consume the items in queue.
	ResourceChangeController controller.Controller

	// MemberClusterPlacementController maintains a rate limited queue which is used to store
	// the name of the changed memberCluster and a reconcile function to consume the items in queue.
	MemberClusterPlacementController controller.Controller

	// InformerManager manages all the dynamic informers created by the discovery client
	InformerManager utils.InformerManager

	// DisabledResourceConfig contains all the api resources that we won't select
	DisabledResourceConfig *utils.DisabledResourceConfig

	// SkippedNamespaces contains all the namespaces that we won't select
	SkippedNamespaces map[string]bool

	// ConcurrentClusterPlacementWorker is the number of cluster `placement` reconcilers that are
	// allowed to sync concurrently.
	ConcurrentClusterPlacementWorker int

	// ConcurrentResourceChangeWorker is the number of resource change work that are
	// allowed to sync concurrently.
	ConcurrentResourceChangeWorker int
}

// Start runs the detector, never stop until stopCh closed. This is called by the controller manager.
func (d *ChangeDetector) Start(ctx context.Context) error {
	klog.Infof("Starting the api resource change detector")

	// Ensure all informers are closed when the context closes
	defer klog.Infof("The api resource change detector is stopped")

	// create the placement informer that handles placement events and enqueues to the placement queue.
	clusterPlacementEventHandler := newHandlerOnEvents(d.onClusterResourcePlacementAdded,
		d.onClusterResourcePlacementUpdated, d.onClusterResourcePlacementDeleted)
	d.InformerManager.AddStaticResource(
		utils.APIResourceMeta{
			GroupVersionResource: utils.ClusterResourcePlacementGVR,
			IsClusterScoped:      true,
		}, clusterPlacementEventHandler)

	// create the work informer that handles work event and enqueues the placement name (stored in its label) to
	// the placement queue. We don't need to handle the add event as they are placed by the placement controller.
	workEventHandler := newHandlerOnEvents(nil, d.onWorkUpdated, d.onWorkDeleted)
	d.InformerManager.AddStaticResource(
		utils.APIResourceMeta{
			GroupVersionResource: utils.WorkGVR,
			IsClusterScoped:      false,
		}, workEventHandler)

	// create the member cluster informer that handles memberCluster add and update. We don't need to handle the
	// delete event as the work resources in this cluster will all get deleted which will trigger placement reconcile.
	memberClusterEventHandler := newHandlerOnEvents(nil, d.onMemberClusterUpdated, nil)
	d.InformerManager.AddStaticResource(
		utils.APIResourceMeta{
			GroupVersionResource: utils.MemberClusterGVR,
			IsClusterScoped:      true,
		}, memberClusterEventHandler)

	// set up the dynamicResourceChangeEventHandler that enqueue an event to the resource change controller's queue.
	dynamicResourceChangeEventHandler := newFilteringHandlerOnAllEvents(d.dynamicResourceFilter,
		d.onResourceAdded, d.onResourceUpdated, d.onResourceDeleted)
	// run the resource type list once to start informers for the existing resources
	d.discoverResources(dynamicResourceChangeEventHandler)
	defer d.InformerManager.Stop()

	// wait for all the existing informer cache to sync before we proceed to add new ones
	// so all the controllers don't need to check cache sync for any static resources.
	// TODO: Controllers also don't need to check any k8s built-in resource but there is no easy way to know
	//      if any gvr is a built-in or a custom resource. We could use a pre-built built-in resources map.
	d.InformerManager.WaitForCacheSync()

	// continue the resource type list loop in the background to discovery resources change.
	go d.discoverAPIResourcesLoop(ctx, 30*time.Second, dynamicResourceChangeEventHandler)

	// We run the three controllers in parallel.
	errs, cctx := errgroup.WithContext(ctx)
	errs.Go(func() error {
		return d.ClusterResourcePlacementController.Run(cctx, d.ConcurrentClusterPlacementWorker)
	})
	errs.Go(func() error {
		return d.ResourceChangeController.Run(cctx, d.ConcurrentResourceChangeWorker)
	})
	errs.Go(func() error {
		return d.MemberClusterPlacementController.Run(cctx, 1)
	})

	return errs.Wait()
}

// discoverAPIResourcesLoop runs discoverResources periodically
func (d *ChangeDetector) discoverAPIResourcesLoop(ctx context.Context, period time.Duration, dynamicResourceEventHandler cache.ResourceEventHandler) {
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		d.discoverResources(dynamicResourceEventHandler)
	}, period)
}

// discoverResources goes through all the api resources in the cluster and create informers on selected types
func (d *ChangeDetector) discoverResources(dynamicResourceEventHandler cache.ResourceEventHandler) {
	newResources, err := d.getWatchableResources()
	var dynamicResources []utils.APIResourceMeta
	if err != nil {
		klog.ErrorS(err, "Failed to get all the api resources from the cluster")
	}
	for _, res := range newResources {
		// all the static resources are disabled by default
		if d.shouldWatchResource(res.GroupVersionResource) {
			dynamicResources = append(dynamicResources, res)
		}
	}
	d.InformerManager.AddDynamicResources(dynamicResources, dynamicResourceEventHandler, err == nil)
	// this will start the newly added informers if there is any
	d.InformerManager.Start()
}

// gvrDisabled returns whether GroupVersionResource is disabled.
func (d *ChangeDetector) shouldWatchResource(gvr schema.GroupVersionResource) bool {
	if d.DisabledResourceConfig == nil {
		return true
	}

	gvks, err := d.RESTMapper.KindsFor(gvr)
	if err != nil {
		klog.ErrorS(err, "gvr transform failed", "gvr", gvr.String())
		return false
	}
	for _, gvk := range gvks {
		if d.DisabledResourceConfig.IsResourceDisabled(gvk) {
			klog.V(4).InfoS("Skip watch resource", "group version kind", gvk.String())
			return false
		}
	}
	return true
}

// dynamicResourceFilter filters out resources that we don't want to watch
// TODO: add UTs for this
func (d *ChangeDetector) dynamicResourceFilter(obj interface{}) bool {
	key, err := controller.ClusterWideKeyFunc(obj)
	if err != nil {
		return false
	}

	cwKey, _ := key.(keys.ClusterWideKey)
	// special case for cluster namespace
	if strings.HasPrefix(cwKey.Namespace, utils.ClusterNamespacePrefix) {
		klog.V(5).InfoS("Skip watching resource in namespace", "namespace", cwKey.Namespace,
			"group", cwKey.Group, "version", cwKey.Version, "kind", cwKey.Kind, "object", cwKey.Name)
		return false
	}

	// if SkippedNamespaces is set, skip any events related to the object in these namespaces.
	if _, ok := d.SkippedNamespaces[cwKey.Namespace]; ok {
		klog.V(5).InfoS("Skip watching resource in namespace", "namespace", cwKey.Namespace,
			"group", cwKey.Group, "version", cwKey.Version, "kind", cwKey.Kind, "object", cwKey.Name)
		return false
	}

	if unstructuredObj, ok := obj.(*unstructured.Unstructured); ok {
		shouldPropagate, err := utils.ShouldPropagateObj(d.InformerManager, unstructuredObj.DeepCopy())
		if err != nil || !shouldPropagate {
			klog.V(5).InfoS("Skip watching resource in namespace", "namespace", cwKey.Namespace,
				"group", cwKey.Group, "version", cwKey.Version, "kind", cwKey.Kind, "object", cwKey.Name)
			return false
		}
	}

	return true
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
