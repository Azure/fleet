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

package resourcewatcher

import (
	"context"
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

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/keys"
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

	// ClusterResourcePlacementControllerV1Beta1 maintains a rate limited queue which is used to store
	// the name of the changed v1beta1 clusterResourcePlacement and a reconcile function to consume the items in queue.
	//
	// Note that the v1beta1 controller, different from the v1alpha1 controller, features its own set of
	// watchers and does not rely on this struct to detect changes.
	ClusterResourcePlacementControllerV1Beta1 controller.Controller

	// ResourceChangeController maintains a rate limited queue which is used to store
	// the name of the changed resourcePlacement and a reconcile function to consume the items in queue.
	//
	// ResourcePlacementController is only enabled as the v1beta1 controller.
	ResourcePlacementController controller.Controller

	// ClusterResourcePlacementController maintains a rate limited queue which is used to store any resources'
	// cluster wide key and a reconcile function to consume the items in queue.
	// This controller will be used by both v1alpha1 & v1beta1 ClusterResourcePlacementController.
	ResourceChangeController controller.Controller

	// InformerManager manages all the dynamic informers created by the discovery client
	InformerManager informer.Manager

	// ResourceConfig contains all the API resources that we won't select based on the allowed or skipped propagating APIs option.
	ResourceConfig *utils.ResourceConfig

	// SkippedNamespaces contains all the namespaces that we won't select
	SkippedNamespaces map[string]bool

	// ConcurrentPlacementWorker is the number of `placement` reconcilers that are
	// allowed to sync concurrently.
	ConcurrentPlacementWorker int

	// ConcurrentResourceChangeWorker is the number of resource change work that are
	// allowed to sync concurrently.
	ConcurrentResourceChangeWorker int
}

// Start runs the detector, never stop until stopCh closed. This is called by the controller manager.
func (d *ChangeDetector) Start(ctx context.Context) error {
	klog.Infof("Starting the api resource change detector")

	// Ensure all informers are closed when the context closes
	defer klog.Infof("The api resource change detector is stopped")

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

	// Run the following controllers (if applicable) in parallel.
	errs, cctx := errgroup.WithContext(ctx)
	if d.ClusterResourcePlacementControllerV1Beta1 != nil {
		errs.Go(func() error {
			return d.ClusterResourcePlacementControllerV1Beta1.Run(cctx, d.ConcurrentPlacementWorker)
		})
	}
	if d.ResourcePlacementController != nil {
		errs.Go(func() error {
			return d.ResourcePlacementController.Run(cctx, d.ConcurrentPlacementWorker)
		})
	}
	errs.Go(func() error {
		return d.ResourceChangeController.Run(cctx, d.ConcurrentResourceChangeWorker)
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
	var dynamicResources []informer.APIResourceMeta
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
	// By default, all of the APIs are allowed.
	if d.ResourceConfig == nil {
		return true
	}

	gvks, err := d.RESTMapper.KindsFor(gvr)
	if err != nil {
		klog.ErrorS(err, "gvr transform failed", "gvr", gvr.String())
		return false
	}
	for _, gvk := range gvks {
		if d.ResourceConfig.IsResourceDisabled(gvk) {
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
	if !utils.ShouldPropagateNamespace(cwKey.Namespace, d.SkippedNamespaces) {
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
