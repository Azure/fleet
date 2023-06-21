/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package framework features the scheduler framework, which the scheduler runs to schedule
// a placement to most appropriate clusters.
package framework

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework/parallelizer"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	// eventRecorderNameTemplate is the template used to format event recorder name for a scheduler framework.
	eventRecorderNameTemplate = "scheduler-framework-%s"
)

// Handle is an interface which allows plugins to access some shared structs (e.g., client, manager)
// and set themselves up with the scheduler framework (e.g., sign up for an informer).
type Handle interface {
	// Client returns a cached client.
	Client() client.Client
	// Manager returns a controller manager; this is mostly used for setting up a new informer
	// (indirectly) via a reconciler.
	Manager() ctrl.Manager
	// UncachedReader returns an uncached read-only client, which allows direct (uncached) access to the API server.
	UncachedReader() client.Reader
	// EventRecorder returns an event recorder.
	EventRecorder() record.EventRecorder
}

// Framework is an interface which scheduler framework should implement.
type Framework interface {
	Handle

	// RunSchedulerCycleFor performs scheduling for a policy snapshot.
	RunSchedulingCycleFor(ctx context.Context, crpName string, policy *fleetv1beta1.SchedulingPolicySnapshot, resources *fleetv1beta1.ClusterResourceSnapshot) (result ctrl.Result, err error)
}

// framework implements the Framework interface.
type framework struct {
	// profile is the scheduling profile in use by the scheduler framework; it includes
	// the plugins to run at each extension point.
	profile *Profile

	// client is the (cached) client in use by the scheduler framework for accessing Kubernetes API server.
	client client.Client
	// uncachedReader is the uncached read-only client in use by the scheduler framework for accessing
	// Kubernetes API server; in most cases client should be used instead, unless consistency becomes
	// a serious concern.
	// TO-DO (chenyu1): explore the possbilities of using a mutation cache for better performance.
	uncachedReader client.Reader
	// manager is the controller manager in use by the scheduler framework.
	manager ctrl.Manager
	// eventRecorder is the event recorder in use by the scheduler framework.
	eventRecorder record.EventRecorder

	// parallelizer is a utility which helps run tasks in parallel.
	parallelizer *parallelizer.Parallerlizer
}

var (
	// Verify that framework implements Framework (and consequently, Handle).
	_ Framework = &framework{}
)

// frameworkOptions is the options for a scheduler framework.
type frameworkOptions struct {
	// numOfWorkers is the number of workers the scheduler framework will use to parallelize tasks,
	// e.g., calling plugins.
	numOfWorkers int
}

// Option is the function for configuring a scheduler framework.
type Option func(*frameworkOptions)

// defaultFrameworkOptions is the default options for a scheduler framework.
var defaultFrameworkOptions = frameworkOptions{numOfWorkers: parallelizer.DefaultNumOfWorkers}

// WithNumOfWorkers sets the number of workers to use for a scheduler framework.
func WithNumOfWorkers(numOfWorkers int) Option {
	return func(fo *frameworkOptions) {
		fo.numOfWorkers = numOfWorkers
	}
}

// NewFramework returns a new scheduler framework.
func NewFramework(profile *Profile, manager ctrl.Manager, opts ...Option) Framework {
	options := defaultFrameworkOptions
	for _, opt := range opts {
		opt(&options)
	}

	// In principle, the scheduler needs to set up informers for resources it is interested in,
	// primarily clusters, snapshots, and bindings. In our current architecture, however,
	// some (if not all) of the informers may have already been set up by other controllers
	// sharing the same controller manager, e.g. cluster watcher. Therefore, here no additional
	// informers are explicitly set up.
	//
	// Note that setting up an informer is achieved by setting up an no-op (at this moment)
	// reconciler, as it does not seem to possible to directly manipulate the informers (cache) in
	// use by a controller runtime manager via public API. In the long run, the reconciles might
	// be useful for setting up some common states for the scheduler, e.g., a resource model.
	//
	// Also note that an indexer might need to be set up for improved performance.

	return &framework{
		profile:        profile,
		client:         manager.GetClient(),
		uncachedReader: manager.GetAPIReader(),
		manager:        manager,
		eventRecorder:  manager.GetEventRecorderFor(fmt.Sprintf(eventRecorderNameTemplate, profile.Name())),
		parallelizer:   parallelizer.NewParallelizer(options.numOfWorkers),
	}
}

// Client returns the (cached) client in use by the scheduler framework.
func (f *framework) Client() client.Client {
	return f.client
}

// Manager returns the controller manager in use by the scheduler framework.
func (f *framework) Manager() ctrl.Manager {
	return f.manager
}

// UncachedReader returns the (uncached) read-only client in use by the scheduler framework.
func (f *framework) UncachedReader() client.Reader {
	return f.uncachedReader
}

// EventRecorder returns the event recorder in use by the scheduler framework.
func (f *framework) EventRecorder() record.EventRecorder {
	return f.eventRecorder
}

// RunSchedulingCycleFor performs scheduling for a policy snapshot.
func (f *framework) RunSchedulingCycleFor(ctx context.Context, crpName string, policy *fleetv1beta1.SchedulingPolicySnapshot, resources *fleetv1beta1.ClusterResourceSnapshot) (result ctrl.Result, err error) { //nolint:revive
	startTime := time.Now()
	schedulingPolicySnapshotRef := klog.KObj(policy)
	klog.V(2).InfoS("Scheduling cycle starts", "schedulingPolicySnapshot", schedulingPolicySnapshotRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Scheduling cycle ends", "schedulingPolicySnapshot", schedulingPolicySnapshotRef, "latency", latency)
	}()

	errorMessage := "Failed to run scheduling cycle"

	klog.V(2).InfoS("Retrieving clusters and bindings", "schedulingPolicySnapshot", schedulingPolicySnapshotRef)

	// Retrieve the desired number of clusters from the policy.
	//
	// TO-DO (chenyu1): assign variable(s) when more logic is added.
	_, err = utils.ExtractNumOfClustersFromPolicySnapshot(policy)
	if err != nil {
		klog.ErrorS(err, errorMessage, "schedulingPolicySnapshot", schedulingPolicySnapshotRef)
		return ctrl.Result{}, err
	}

	// Collect all clusters.
	//
	// Note that clusters here are listed from the cached client for improved performance. This is
	// safe in consistency as it is guaranteed that the scheduler will receive all events for cluster
	// changes eventually.
	//
	// TO-DO (chenyu1): assign variable(s) when more logic is added.
	_, err = f.collectClusters(ctx)
	if err != nil {
		klog.ErrorS(err, errorMessage, schedulingPolicySnapshotRef)
		return ctrl.Result{}, err
	}

	// Collect all bindings.
	//
	// Note that for consistency reasons, bindings are listed directly from the API server; this helps
	// avoid a classic read-after-write consistency issue, which, though should only happen when there
	// are connectivity issues and/or API server is overloaded, can lead to over-scheduling in adverse
	// scenarios. It is true that even when bindings are over-scheduled, the scheduler can still correct
	// the situation in the next cycle; however, considering that placing resources to clusters, unlike
	// pods to nodes, is more expensive, it is better to avoid over-scheduling in the first place.
	//
	// This, of course, has additional performance overhead (and may further exacerbate API server
	// overloading). In the long run we might still want to resort to a cached situtation.
	//
	// TO-DO (chenyu1): explore the possbilities of using a mutation cache for better performance.
	bindings, err := f.collectBindings(ctx, crpName)
	if err != nil {
		klog.ErrorS(err, errorMessage, schedulingPolicySnapshotRef)
		return ctrl.Result{}, err
	}

	// Parse the bindings, find out
	// * active bindings, i.e., bindings that should be picked up by the dispatcher; and
	// * creating bindings with associated target clusters; and
	// * creating bindings without associated target clusters; and
	// * bindings that are already marked for deletion
	//
	// Note that the scheduler does not perform deletions at all, and all the bindings it creates
	// are initially not ready for fulfillment, i.e., the dispatcher will not place resources
	// to the target cluster. As a result it is safe for the scheduler to disregard deleted clusters
	// completely in a scheduling cycle (aside for finalizer cleanup).
	//
	// A similar patterns applies to obsolete bindings as well.
	klog.V(2).InfoS("Classifying bindings", "schedulingPolicySnapshot", schedulingPolicySnapshotRef)
	_, _, _, deleted, err := classifyBindings(bindings)

	// If a binding has been marked for deletion and no longer has the dispatcher finalizer, the scheduler
	// removes its own finalizer from it, to clear it for eventual deletion.
	if err := f.removeSchedulerFinalizerFromBindings(ctx, deleted); err != nil {
		klog.ErrorS(err, errorMessage, schedulingPolicySnapshotRef)
		return ctrl.Result{}, err
	}

	// Not yet fully implemented.
	return ctrl.Result{}, nil
}

// collectClusters lists all clusters in the cache.
func (f *framework) collectClusters(ctx context.Context) ([]fleetv1beta1.MemberCluster, error) {
	errorFormat := "failed to collect clusters: %w"

	clusterList := &fleetv1beta1.MemberClusterList{}
	if err := f.client.List(ctx, clusterList, &client.ListOptions{}); err != nil {
		return nil, controller.NewAPIServerError(fmt.Errorf(errorFormat, err))
	}
	return clusterList.Items, nil
}

// collectBindings lists all bindings associated with a CRP **using the uncached client**.
func (f *framework) collectBindings(ctx context.Context, crpName string) ([]fleetv1beta1.ClusterResourceBinding, error) {
	errorFormat := "failed to collect bindings: %w"

	bindingList := &fleetv1beta1.ClusterResourceBindingList{}
	labelSelector := labels.SelectorFromSet(labels.Set{fleetv1beta1.CRPTrackingLabel: crpName})
	// List bindings directly from the API server.
	if err := f.uncachedReader.List(ctx, bindingList, &client.ListOptions{LabelSelector: labelSelector}); err != nil {
		return nil, controller.NewAPIServerError(fmt.Errorf(errorFormat, err))
	}
	return bindingList.Items, nil
}

// removeSchedulerFinalizerFromBindings removes the scheduler finalizer from a list of bindings.
func (f *framework) removeSchedulerFinalizerFromBindings(ctx context.Context, bindings []*fleetv1beta1.ClusterResourceBinding) error {
	errorFormat := "failed to remove scheduler finalizer from binding %s: %w"

	for _, binding := range bindings {
		if controllerutil.ContainsFinalizer(binding, fleetv1beta1.SchedulerFinalizer) {
			controllerutil.RemoveFinalizer(binding, fleetv1beta1.SchedulerFinalizer)
			if err := f.client.Update(ctx, binding, &client.UpdateOptions{}); err != nil {
				return controller.NewAPIServerError(fmt.Errorf(errorFormat, binding.Name, err))
			}
		}
	}
	return nil
}
