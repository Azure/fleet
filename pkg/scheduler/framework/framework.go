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

	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework/parallelizer"
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
	RunSchedulingCycleFor(ctx context.Context, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, resources *fleetv1beta1.ClusterResourceSnapshot) (result ctrl.Result, err error)
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
func (f *framework) RunSchedulingCycleFor(ctx context.Context, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, resources *fleetv1beta1.ClusterResourceSnapshot) (result ctrl.Result, err error) { //nolint:revive
	// Not yet implemented.
	return ctrl.Result{}, nil
}
