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
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework/parallelizer"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	// eventRecorderNameTemplate is the template used to format event recorder name for a scheduler framework.
	eventRecorderNameTemplate = "scheduler-framework-%s"

	// maxClusterDecisionCount controls the maximum number of decisions added to the policy snapshot status.
	//
	// Note that all picked clusters will have their associated decisions on the status, even if the number
	// exceeds this limit. The limit is mostly for filling up the status with reasons why a cluster is
	// **not** picked by the scheduler, when there are enough number of clusters to choose from, and not
	// enough picked clusters.
	maxClusterDecisionCount = 20

	// The reasons and messages for scheduled conditions.
	fullyScheduledReason     = "SchedulingCompleted"
	fullyScheduledMessage    = "all required number of bindings have been created"
	notFullyScheduledReason  = "Pendingscheduling"
	notFullyScheduledMessage = "might not have enough bindings created"
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
	RunSchedulingCycleFor(ctx context.Context, policy *fleetv1beta1.ClusterPolicySnapshot, resources *fleetv1beta1.ClusterResourceSnapshot) (result ctrl.Result, err error)
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

// APIReader returns the (uncached) read-only client in use by the scheduler framework.
func (f *framework) UncachedReader() client.Reader {
	return f.uncachedReader
}

// EventRecorder returns the event recorder in use by the scheduler framework.
func (f *framework) EventRecorder() record.EventRecorder {
	return f.eventRecorder
}

// RunSchedulingCycleFor performs scheduling for a policy snapshot.
func (f *framework) RunSchedulingCycleFor(ctx context.Context, policy *fleetv1beta1.ClusterPolicySnapshot, resources *fleetv1beta1.ClusterResourceSnapshot) (result ctrl.Result, err error) { //nolint:revive
	startTime := time.Now()
	clusterPolicySnapshotRef := klog.KObj(policy)
	klog.V(2).InfoS("Scheduling cycle starts", "clusterPolicySnapshot", clusterPolicySnapshotRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Scheduling cycle ends", "clusterPolicySnapshot", clusterPolicySnapshotRef, "latency", latency)
	}()

	errorMessage := "failed to run scheduling cycle"

	klog.V(2).InfoS("Retrieving clusters and bindings", "clusterPolicySnapshot", clusterPolicySnapshotRef)

	// Retrieve the desired number of clusters from the policy.
	numOfClusters, err := extractNumOfClustersFromPolicySnapshot(policy)
	if err != nil {
		klog.ErrorS(err, errorMessage, klog.KObj(policy))
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
		klog.ErrorS(err, errorMessage, klog.KObj(policy))
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
	bindings, err := f.collectBindings(ctx, policy)
	if err != nil {
		klog.ErrorS(err, errorMessage, klog.KObj(policy))
		return ctrl.Result{}, err
	}

	// Parse the bindings, find out
	// * active bindings, i.e., bindings that are not marked for deletion; and
	// * bindings that are already marked for deletion, but still have the dispatcher finalizer
	//   present;
	// * bindings that are already marked for deletion, and no longer have the dispatcher finalizer
	//
	// Note that the scheduler only considers a binding to be deleted if it is marked for deletion
	// and it no longer has the dispatcher finalizer. This helps avoid a rare racing condition
	// where the scheduler binds a cluster to a resource placement, even though the dispatcher has
	// not started, or is still in the process of, removing the same set of resources from
	// the cluster, triggered by a recently deleted binding.
	klog.V(2).InfoS("Classifying bindings", "clusterPolicySnapshot", clusterPolicySnapshotRef)
	active, deletedWithDispatcherFinalizer, deletedWithoutDispatcherFinalizer := classifyBindings(bindings)

	// If a binding has been marked for deletion and no longer has the dispatcher finalizer, the scheduler
	// removes its own finalizer from it, to clear it for eventual deletion.
	if err := f.removeSchedulerFinalizerFromBindings(ctx, deletedWithoutDispatcherFinalizer); err != nil {
		klog.ErrorS(err, errorMessage, klog.KObj(policy))
		return ctrl.Result{}, err
	}

	// Check if the scheduler should downscale, i.e., remove some bindings.
	// Note that the scheduler will only down-scale if
	// * the policy is of the PickN type; and
	// * the desired number of bindings is less the number of active bindings.
	//
	// Once again, note that the scheduler only considers a binding to be deleted if it is marked for deletion
	// and does not have the dispatcher finalizer. Scheduler will be triggered again for such bindings,
	// and a up-scaling may happen then, if appropriate.
	act, downscaleCount := shouldDownscale(policy, numOfClusters, active)

	// Downscale if necessary; older (ranked by CreationTimestamp) bindings will be picked first.
	//
	// This assumes a monotonic clock.
	if act {
		klog.V(2).InfoS("Downscaling", "clusterPolicySnapshot", clusterPolicySnapshotRef, "count", downscaleCount)
		remaining, err := f.downscale(ctx, active, downscaleCount)
		if err != nil {
			klog.ErrorS(err, errorMessage, klog.KObj(policy))
			return ctrl.Result{}, err
		}

		// Prepare new scheduling decisions.
		newSchedulingDecisions := prepareNewSchedulingDecisions(policy, remaining, deletedWithDispatcherFinalizer)
		// Prepare new scheduling condition.
		//
		// In the case of downscaling, the scheduler considers the policy to be fully scheduled.
		newSchedulingCondition := fullyScheduledCondition(policy)

		// Update the policy snapshot status; since a downscaling has occurred, this update is always requied, hence
		// no sameness (no change) checks are necessary.
		//
		// Note that the op would fail if the policy snapshot is not the latest, so that consistency is
		// preserved.
		klog.V(2).InfoS("Refreshing scheduling decision", "clusterPolicySnapshot", clusterPolicySnapshotRef)
		if err := f.updatePolicySnapshotStatus(ctx, policy, newSchedulingDecisions, newSchedulingCondition); err != nil {
			klog.ErrorS(err, errorMessage, klog.KObj(policy))
			return ctrl.Result{}, err
		}

		// Return immediately as there are no more bindings for the scheduler to scheduler at this moment.
		return ctrl.Result{}, nil
	}

	// If no downscaling is needed, update the policy snapshot status any way.
	//
	// This is needed as a number of situations (e.g., POST/PUT failures) may lead to inconsistencies between
	// the decisions added to the policy snapshot status and the actual list of bindings.

	// Collect current decisions and conditions for sameness (no change) checks.
	currentSchedulingDecisions := policy.Status.ClusterDecisions
	currentSchedulingCondition := meta.FindStatusCondition(policy.Status.Conditions, string(fleetv1beta1.PolicySnapshotScheduled))

	// Check if the scheduler needs to take action; a scheduling cycle is only needed if
	// * the policy is of the PickAll type; or
	// * the policy is of the PickN type, and currently there are not enough number of bindings.
	if !shouldSchedule(policy, numOfClusters, len(active)+len(deletedWithDispatcherFinalizer)) {
		// No action is needed; however, a status refresh might be warranted.
		klog.V(2).InfoS("No scheduling action is needed", "clusterPolicySnapshot", clusterPolicySnapshotRef)

		// Prepare new scheduling decisions.
		newSchedulingDecisions := prepareNewSchedulingDecisions(policy, active, deletedWithDispatcherFinalizer)
		// Prepare new scheduling condition.
		//
		// In this case, since no action is needed, the scheduler considers the policy to be fully scheduled.
		newSchedulingCondition := fullyScheduledCondition(policy)

		// Check if a refresh is warranted; the scheduler only update the status when there is a change in
		// scheduling decisions and/or the scheduling condition.
		if !equalDecisions(currentSchedulingDecisions, newSchedulingDecisions) || condition.EqualCondition(currentSchedulingCondition, &newSchedulingCondition) {
			// Update the policy snapshot status.
			//
			// Note that the op would fail if the policy snapshot is not the latest, so that consistency is
			// preserved.
			klog.V(2).InfoS("Refreshing scheduling decision", "clusterPolicySnapshot", clusterPolicySnapshotRef)
			if err := f.updatePolicySnapshotStatus(ctx, policy, newSchedulingDecisions, newSchedulingCondition); err != nil {
				klog.ErrorS(err, errorMessage, klog.KObj(policy))
				return ctrl.Result{}, err
			}
		}

		// Return immediate as there no more bindings for the scheduler to schedule at this moment.
		return ctrl.Result{}, nil
	}

	// The scheduler needs to take action; refresh the status first.
	klog.V(2).InfoS("Scheduling action is needed", "clusterPolicySnapshot", clusterPolicySnapshotRef)

	// Prepare new scheduling decisions.
	newSchedulingDecisions := prepareNewSchedulingDecisions(policy, active, deletedWithDispatcherFinalizer)
	// Prepare new scheduling condition.
	//
	// In this case, since action is needed, the scheduler marks the policy as not fully scheduled.
	newSchedulingCondition := notFullyScheduledCondition(policy, numOfClusters)
	// Check if a refresh is warranted; the scheduler only update the status when there is a change in
	// scheduling decisions and/or the scheduling condition.
	if !equalDecisions(currentSchedulingDecisions, newSchedulingDecisions) || condition.EqualCondition(currentSchedulingCondition, &newSchedulingCondition) {
		// Update the policy snapshot status.
		//
		// Note that the op would fail if the policy snapshot is not the latest, so that consistency is
		// preserved.
		klog.V(2).InfoS("Refreshing scheduling decision", "clusterPolicySnapshot", clusterPolicySnapshotRef)
		if err := f.updatePolicySnapshotStatus(ctx, policy, newSchedulingDecisions, newSchedulingCondition); err != nil {
			klog.ErrorS(err, errorMessage, klog.KObj(policy))
			return ctrl.Result{}, err
		}
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
func (f *framework) collectBindings(ctx context.Context, policy *fleetv1beta1.ClusterPolicySnapshot) ([]fleetv1beta1.ClusterResourceBinding, error) {
	errorFormat := "failed to collect bindings: %w"

	bindingOwner, err := extractOwnerCRPNameFromPolicySnapshot(policy)
	if err != nil {
		// This branch should never run in most cases, as the a policy snapshot is expected to be
		// owned by a CRP.
		return nil, controller.NewUnexpectedBehaviorError(fmt.Errorf(errorFormat, err))
	}

	bindingList := &fleetv1beta1.ClusterResourceBindingList{}
	labelSelector := labels.SelectorFromSet(labels.Set{fleetv1beta1.CRPTrackingLabel: bindingOwner})
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
		controllerutil.RemoveFinalizer(binding, utils.SchedulerFinalizer)
		if err := f.client.Update(ctx, binding, &client.UpdateOptions{}); err != nil {
			return controller.NewAPIServerError(fmt.Errorf(errorFormat, binding.Name, err))
		}
	}
	return nil
}

// sortByCreationTimestampBindings is a wrapper which implements Sort.Interface, which allows easy
// sorting of bindings by their CreationTimestamps.
type sortByCreationTimestampBindings []*fleetv1beta1.ClusterResourceBinding

// Len() is for implementing Sort.Interface.
func (s sortByCreationTimestampBindings) Len() int {
	return len(s)
}

// Swap() is for implementing Sort.Interface.
func (s sortByCreationTimestampBindings) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// Less() is for implementing Sort.Interface.
func (s sortByCreationTimestampBindings) Less(i, j int) bool {
	return s[i].CreationTimestamp.Before(&(s[j].CreationTimestamp))
}

// downscale performs downscaling, removing some number of bindings. It picks the oldest (by CreationTimestamp)
// bindings first.
func (f *framework) downscale(ctx context.Context, active []*fleetv1beta1.ClusterResourceBinding, count int) ([]*fleetv1beta1.ClusterResourceBinding, error) {
	errorFormat := "failed to delete binding %s: %w"

	// Sort the bindings by their CreationTimestamps.
	sorted := sortByCreationTimestampBindings(active)
	sort.Sort(sorted)

	// Delete the first count number of bindings.
	bindingsToDelete := sorted[:count]
	for _, binding := range bindingsToDelete {
		if err := f.client.Delete(ctx, binding, &client.DeleteOptions{}); err != nil {
			return nil, controller.NewAPIServerError(fmt.Errorf(errorFormat, binding.Name, err))
		}
	}

	// Return the remaining bindings.
	return sorted[count:], nil
}

// updatePolicySnapshotStatus updates the status of a policy snapshot, setting new scheduling decisions
// and condition on the object.
func (f *framework) updatePolicySnapshotStatus(ctx context.Context, policy *fleetv1beta1.ClusterPolicySnapshot, decisions []fleetv1beta1.ClusterDecision, condition metav1.Condition) error {
	errorFormat := "failed to update policy snapshot status: %w"

	policy.Status.ClusterDecisions = decisions
	meta.SetStatusCondition(&policy.Status.Conditions, condition)
	if err := f.client.Status().Update(ctx, policy, &client.UpdateOptions{}); err != nil {
		return controller.NewAPIServerError(fmt.Errorf(errorFormat, err))
	}
	return nil
}
