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

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework/parallelizer"
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

	// RunSchedulerCycleFor performs scheduling for a cluster resource placement, specifically
	// its associated latest scheduling policy snapshot.
	RunSchedulingCycleFor(ctx context.Context, crpName string, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) (result ctrl.Result, err error)
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

	// maxClusterDecisionCount controls the maximum number of decisions added to the policy snapshot status.
	//
	// Note that all picked clusters will have their associated decisions written to the status, regardless
	// of this limit. When the number of picked clusters is below this limit, the scheduler will use the
	// remaining slots to fill up the status with explanations on why a specific cluster is not picked
	// by scheduler (if applicable), so that user are better informed on how the scheduler functions.
	maxClusterDecisionCount int
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

	// maxClusterDecisionCount controls the maximum number of decisions added to the policy snapshot status.
	maxClusterDecisionCount int
}

// Option is the function for configuring a scheduler framework.
type Option func(*frameworkOptions)

// defaultFrameworkOptions is the default options for a scheduler framework.
var defaultFrameworkOptions = frameworkOptions{
	numOfWorkers:            parallelizer.DefaultNumOfWorkers,
	maxClusterDecisionCount: 20,
}

// WithNumOfWorkers sets the number of workers to use for a scheduler framework.
func WithNumOfWorkers(numOfWorkers int) Option {
	return func(fo *frameworkOptions) {
		fo.numOfWorkers = numOfWorkers
	}
}

// WithMaxClusterDecisionCount sets the maximum number of decisions added to the policy snapshot status.
func WithMaxClusterDecisionCount(maxClusterDecisionCount int) Option {
	return func(fo *frameworkOptions) {
		fo.maxClusterDecisionCount = maxClusterDecisionCount
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
		profile:                 profile,
		client:                  manager.GetClient(),
		uncachedReader:          manager.GetAPIReader(),
		manager:                 manager,
		eventRecorder:           manager.GetEventRecorderFor(fmt.Sprintf(eventRecorderNameTemplate, profile.Name())),
		parallelizer:            parallelizer.NewParallelizer(options.numOfWorkers),
		maxClusterDecisionCount: options.maxClusterDecisionCount,
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

// RunSchedulingCycleFor performs scheduling for a cluster resource placement
// (more specifically, its associated scheduling policy snapshot).
func (f *framework) RunSchedulingCycleFor(ctx context.Context, crpName string, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) (result ctrl.Result, err error) {
	startTime := time.Now()
	policyRef := klog.KObj(policy)
	klog.V(2).InfoS("Scheduling cycle starts", "clusterSchedulingPolicySnapshot", policyRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Scheduling cycle ends", "clusterSchedulingPolicySnapshot", policyRef, "latency", latency)
	}()

	// TO-DO (chenyu1): add metrics.

	// Collect all clusters.
	//
	// Note that clusters here are listed from the cached client for improved performance. This is
	// safe in consistency as it is guaranteed that the scheduler will receive all events for cluster
	// changes eventually.
	clusters, err := f.collectClusters(ctx)
	if err != nil {
		klog.ErrorS(err, "Failed to collect clusters", "clusterSchedulingPolicySnapshot", policyRef)
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
		klog.ErrorS(err, "Failed to collect bindings", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// Parse the bindings, find out
	//
	// * bound bindings, i.e., bindings that are associated with a normally operating cluster and
	//   have been cleared for processing by the dispatcher; and
	// * scheduled bindings, i.e., bindings that have been associated with a normally operating cluster,
	//   but have not yet been cleared for processing by the dispatcher; and
	// * obsolete bindings, i.e., bindings that are scheduled in accordance with an out-of-date
	//   (i.e., no longer active) scheduling policy snapshot; it may or may have been cleared for
	//   processing by the dispatcher; and
	// * dangling bindings, i.e., bindings that are associated with a cluster that is no longer
	//   in a normally operating state (the cluster has left the fleet, or is in the state of leaving),
	//   yet has not been marked as unscheduled by the scheduler; and
	//
	// Note that bindings marked as unscheduled are ignored by the scheduler, as they
	// are irrelevant to the scheduling cycle. Any deleted binding is also ignored.
	bound, scheduled, obsolete, dangling := classifyBindings(policy, bindings, clusters)

	// Mark all dangling bindings as unscheduled.
	if err := f.markAsUnscheduledFor(ctx, dangling); err != nil {
		klog.ErrorS(err, "Failed to mark dangling bindings as unscheduled", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// Prepare the cycle state for this run.
	//
	// Note that this state is shared between all plugins and the scheduler framework itself (though some fields are reserved by
	// the framework). These resevered fields are never accessed concurrently, as each scheduling run has its own cycle and a run
	// is always executed in one single goroutine; plugin access to the state is guarded by sync.Map.
	state := NewCycleState()

	switch policy.Spec.Policy.PlacementType {
	case fleetv1beta1.PickAllPlacementType:
		// Run the scheduling cycle for policy of the PickAll placement type.
		return f.runSchedulingCycleForPickAllPlacementType(ctx, state, crpName, policy, clusters, bound, scheduled, obsolete)
	case fleetv1beta1.PickNPlacementType:
		// Run the scheduling cycle for policy of the PickN placement type.
		return f.runSchedulingCycleForPickNPlacementType(ctx, state, crpName, policy, clusters, bound, scheduled, obsolete)
	default:
		// This normally should never occur.
		klog.ErrorS(err, fmt.Sprintf("The placement type %s is unknown", policy.Spec.Policy.PlacementType), "schedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
	}
}

// collectClusters lists all clusters in the cache.
func (f *framework) collectClusters(ctx context.Context) ([]fleetv1beta1.MemberCluster, error) {
	clusterList := &fleetv1beta1.MemberClusterList{}
	if err := f.client.List(ctx, clusterList, &client.ListOptions{}); err != nil {
		return nil, controller.NewAPIServerError(true, err)
	}
	return clusterList.Items, nil
}

// collectBindings lists all bindings associated with a CRP **using the uncached client**.
func (f *framework) collectBindings(ctx context.Context, crpName string) ([]fleetv1beta1.ClusterResourceBinding, error) {
	bindingList := &fleetv1beta1.ClusterResourceBindingList{}
	labelSelector := labels.SelectorFromSet(labels.Set{fleetv1beta1.CRPTrackingLabel: crpName})
	// List bindings directly from the API server.
	if err := f.uncachedReader.List(ctx, bindingList, &client.ListOptions{LabelSelector: labelSelector}); err != nil {
		return nil, controller.NewAPIServerError(false, err)
	}
	return bindingList.Items, nil
}

// markAsUnscheduledFor marks a list of bindings as unscheduled.
func (f *framework) markAsUnscheduledFor(ctx context.Context, bindings []*fleetv1beta1.ClusterResourceBinding) error {
	for _, binding := range bindings {
		// Note that Unscheduled is a terminal state for a binding; since there is no point of return,
		// and the scheduler has acknowledged its fate at this moment, any binding marked as
		// deletion will be disregarded by the scheduler from this point onwards.
		binding.Spec.State = fleetv1beta1.BindingStateUnscheduled
		if err := f.client.Update(ctx, binding, &client.UpdateOptions{}); err != nil {
			return controller.NewAPIServerError(false, fmt.Errorf("failed to mark binding %s as unscheduled: %w", binding.Name, err))
		}
	}
	return nil
}

// runSchedulingCycleForPickAllPlacementType runs a scheduling cycle for a scheduling policy of the
// PickAll placement type.
//
// TO-DO (chenyu1): remove the nolint directives once the function is implemented.
func (f *framework) runSchedulingCycleForPickAllPlacementType(
	ctx context.Context, //nolint: revive
	state *CycleState, //nolint: revive
	crpName string, //nolint: revive
	policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, //nolint: revive
	clusters []fleetv1beta1.MemberCluster, //nolint: revive
	bound, scheduled, obsolete []*fleetv1beta1.ClusterResourceBinding, //nolint: revive
) (result ctrl.Result, err error) {
	// Not yet implemented.
	return ctrl.Result{}, nil
}

// runSchedulingCycleForPickNPlacementType runs the scheduling cycle for a scheduling policy of the PickN
// placement type.
//
// TO-DO (chenyu1): remove the nolint directives once the function is implemented.
func (f *framework) runSchedulingCycleForPickNPlacementType(
	ctx context.Context, //nolint: revive
	state *CycleState, //nolint: revive
	crpName string, //nolint: revive
	policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, //nolint: revive
	clusters []fleetv1beta1.MemberCluster, //nolint: revive
	bound, scheduled, obsolete []*fleetv1beta1.ClusterResourceBinding, //nolint: revive
) (result ctrl.Result, err error) {
	// Not yet implemented.
	return ctrl.Result{}, nil
}
