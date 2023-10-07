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
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/clustereligibilitychecker"
	"go.goms.io/fleet/pkg/scheduler/framework/parallelizer"
	"go.goms.io/fleet/pkg/utils/annotations"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	// eventRecorderNameTemplate is the template used to format event recorder name for a scheduler framework.
	eventRecorderNameTemplate = "scheduler-framework-%s"

	// The reasons to use for scheduling decisions.
	pickedByPolicyReason                  = "picked by scheduling policy"
	pickFixedInvalidClusterReasonTemplate = "cluster is not eligible for resource placement yet: %s"
	pickFixedNotFoundClusterReason        = "specified cluster is not found"
	notPickedByScoreReason                = "cluster does not score high enough"

	// The reasons and messages for scheduled conditions.
	fullyScheduledReason     = "SchedulingPolicyFulfilled"
	notFullyScheduledReason  = "SchedulingPolicyUnfulfilled"
	fullyScheduledMessage    = "found all the clusters needed as specified by the scheduling policy"
	notFullyScheduledMessage = "could not find all the clusters needed as specified by the scheduling policy"

	// The array length limit of the cluster decision array in the scheduling policy snapshot
	// status API.
	clustersDecisionArrayLengthLimitInAPI = 1000
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
	// ClusterEligibilityChecker returns the cluster eligibility checker associated with the scheduler.
	ClusterEligibilityChecker() *clustereligibilitychecker.ClusterEligibilityChecker
}

// Framework is an interface which scheduler framework should implement.
type Framework interface {
	Handle

	// RunSchedulingCycleFor performs scheduling for a cluster resource placement, specifically
	// its associated latest scheduling policy snapshot.
	RunSchedulingCycleFor(ctx context.Context, crpName string, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (result ctrl.Result, err error)
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

	// eligibilityChecker is a utility which helps determine if a cluster is eligible for resource placement.
	clusterEligibilityChecker *clustereligibilitychecker.ClusterEligibilityChecker

	// maxUnselectedClusterDecisionCount controls the maximum number of decisions for unselected clusters
	// added to the policy snapshot status.
	//
	// Note that all picked clusters will always have their associated decisions written to the status.
	maxUnselectedClusterDecisionCount int
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

	// maxUnselectedClusterDecisionCount controls the maximum number of decisions for
	// unselected clusters added to the policy snapshot status.
	maxUnselectedClusterDecisionCount int

	// checker is the cluster eligibility checker the scheduler framework will use to check
	// if a cluster is eligibile for resource placement.
	clusterEligibilityChecker *clustereligibilitychecker.ClusterEligibilityChecker
}

// Option is the function for configuring a scheduler framework.
type Option func(*frameworkOptions)

// defaultFrameworkOptions is the default options for a scheduler framework.
var defaultFrameworkOptions = frameworkOptions{
	numOfWorkers:                      parallelizer.DefaultNumOfWorkers,
	maxUnselectedClusterDecisionCount: 20,
	clusterEligibilityChecker:         clustereligibilitychecker.New(),
}

// WithNumOfWorkers sets the number of workers to use for a scheduler framework.
func WithNumOfWorkers(numOfWorkers int) Option {
	return func(fo *frameworkOptions) {
		fo.numOfWorkers = numOfWorkers
	}
}

// WithMaxClusterDecisionCount sets the maximum number of decisions added to the policy snapshot status.
func WithMaxClusterDecisionCount(maxUnselectedClusterDecisionCount int) Option {
	return func(fo *frameworkOptions) {
		fo.maxUnselectedClusterDecisionCount = maxUnselectedClusterDecisionCount
	}
}

// WithClusterEligibilityChecker sets the cluster eligibility checker for a scheduler framework.
func WithClusterEligibilityChecker(checker *clustereligibilitychecker.ClusterEligibilityChecker) Option {
	return func(fo *frameworkOptions) {
		fo.clusterEligibilityChecker = checker
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

	f := &framework{
		profile:                           profile,
		client:                            manager.GetClient(),
		uncachedReader:                    manager.GetAPIReader(),
		manager:                           manager,
		eventRecorder:                     manager.GetEventRecorderFor(fmt.Sprintf(eventRecorderNameTemplate, profile.Name())),
		parallelizer:                      parallelizer.NewParallelizer(options.numOfWorkers),
		maxUnselectedClusterDecisionCount: options.maxUnselectedClusterDecisionCount,
		clusterEligibilityChecker:         options.clusterEligibilityChecker,
	}
	// initialize all the plugins
	for _, plugin := range f.profile.registeredPlugins {
		plugin.SetUpWithFramework(f)
	}
	return f
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

// ClusterEligibilityChecker returns the cluster eligibility checker in use by the scheduler framework.
func (f *framework) ClusterEligibilityChecker() *clustereligibilitychecker.ClusterEligibilityChecker {
	return f.clusterEligibilityChecker
}

// RunSchedulingCycleFor performs scheduling for a cluster resource placement
// (more specifically, its associated scheduling policy snapshot).
func (f *framework) RunSchedulingCycleFor(ctx context.Context, crpName string, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (result ctrl.Result, err error) {
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
	// * unscheduled bindings, i.e., bindings that are marked as unscheduled in the previous round
	//   of scheduling activity; it can either produced by the same or different policy snapshot; and
	// * dangling bindings, i.e., bindings that are associated with a cluster that is no longer
	//   in a normally operating state (the cluster has left the fleet, or is in the state of leaving),
	//   yet has not been marked as unscheduled by the scheduler; and
	//
	// Any deleted binding is also ignored.
	// Note that bindings marked as unscheduled are ignored by the scheduler, as they
	// are irrelevant to the scheduling cycle. However, we will reconcile them with the latest scheduling
	// result so that we won't have a ever increasing chain of flip flop bindings.
	bound, scheduled, obsolete, unscheduled, dangling := classifyBindings(policy, bindings, clusters)

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
	state := NewCycleState(clusters, obsolete, bound, scheduled)

	switch {
	case policy.Spec.Policy == nil:
		// The placement policy is not set; in such cases the policy is considered to be of
		// the PickAll placement type.
		return f.runSchedulingCycleForPickAllPlacementType(ctx, state, crpName, policy, clusters, bound, scheduled, unscheduled, obsolete)
	case policy.Spec.Policy.PlacementType == placementv1beta1.PickFixedPlacementType:
		// The placement policy features a fixed set of clusters to select; in such cases, the
		// scheduler will bind to these clusters directly.
		return f.runSchedulingCycleForPickFixedPlacementType(ctx, crpName, policy, clusters, bound, scheduled, unscheduled, obsolete)
	case policy.Spec.Policy.PlacementType == placementv1beta1.PickAllPlacementType:
		// Run the scheduling cycle for policy of the PickAll placement type.
		return f.runSchedulingCycleForPickAllPlacementType(ctx, state, crpName, policy, clusters, bound, scheduled, unscheduled, obsolete)
	case policy.Spec.Policy.PlacementType == placementv1beta1.PickNPlacementType:
		// Run the scheduling cycle for policy of the PickN placement type.
		return f.runSchedulingCycleForPickNPlacementType(ctx, state, crpName, policy, clusters, bound, scheduled, unscheduled, obsolete)
	default:
		// This normally should never occur.
		klog.ErrorS(err, fmt.Sprintf("The placement type %s is unknown", policy.Spec.Policy.PlacementType), "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
	}
}

// collectClusters lists all clusters in the cache.
func (f *framework) collectClusters(ctx context.Context) ([]clusterv1beta1.MemberCluster, error) {
	clusterList := &clusterv1beta1.MemberClusterList{}
	if err := f.client.List(ctx, clusterList, &client.ListOptions{}); err != nil {
		return nil, controller.NewAPIServerError(true, err)
	}
	return clusterList.Items, nil
}

// collectBindings lists all bindings associated with a CRP **using the uncached client**.
func (f *framework) collectBindings(ctx context.Context, crpName string) ([]placementv1beta1.ClusterResourceBinding, error) {
	bindingList := &placementv1beta1.ClusterResourceBindingList{}
	labelSelector := labels.SelectorFromSet(labels.Set{placementv1beta1.CRPTrackingLabel: crpName})
	// List bindings directly from the API server.
	if err := f.uncachedReader.List(ctx, bindingList, &client.ListOptions{LabelSelector: labelSelector}); err != nil {
		return nil, controller.NewAPIServerError(false, err)
	}
	return bindingList.Items, nil
}

// markAsUnscheduledFor marks a list of bindings as unscheduled.
func (f *framework) markAsUnscheduledFor(ctx context.Context, bindings []*placementv1beta1.ClusterResourceBinding) error {
	// issue all the update requests in parallel
	errs, cctx := errgroup.WithContext(ctx)
	for _, binding := range bindings {
		unscheduledBinding := binding
		errs.Go(func() error {
			return retry.OnError(retry.DefaultBackoff,
				func(err error) bool {
					return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsConflict(err)
				},
				func() error {
					// Remember the previous unscheduledBinding state so that we might be able to revert this change if this
					// cluster is being selected again before the resources are removed from it. Need to do a get and set if
					// we add more annotations to the binding.
					unscheduledBinding.SetAnnotations(map[string]string{placementv1beta1.PreviousBindingStateAnnotation: string(unscheduledBinding.Spec.State)})
					// Mark the unscheduledBinding as unscheduled which can conflict with the rollout controller which also changes the state of a
					// unscheduledBinding from "scheduled" to "bound".
					unscheduledBinding.Spec.State = placementv1beta1.BindingStateUnscheduled
					err := f.client.Update(cctx, unscheduledBinding, &client.UpdateOptions{})
					klog.V(2).InfoS("Marking binding as unscheduled", "clusterResourceBinding", klog.KObj(unscheduledBinding), "error", err)
					// We will just retry for conflict errors since the scheduler holds the truth here.
					if apierrors.IsConflict(err) {
						// get the binding again to make sure we have the latest version to update again.
						return f.client.Get(cctx, client.ObjectKeyFromObject(unscheduledBinding), unscheduledBinding)
					}
					return err
				})
		})
	}
	return errs.Wait()
}

// runSchedulingCycleForPickAllPlacementType runs a scheduling cycle for a scheduling policy of the
// PickAll placement type.
func (f *framework) runSchedulingCycleForPickAllPlacementType(
	ctx context.Context,
	state *CycleState,
	crpName string,
	policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
	clusters []clusterv1beta1.MemberCluster,
	bound, scheduled, unscheduled, obsolete []*placementv1beta1.ClusterResourceBinding,
) (result ctrl.Result, err error) {
	policyRef := klog.KObj(policy)

	// The scheduler always needs to take action when processing scheduling policies of the PickAll
	// placement type; enter the actual scheduling stages right away.
	klog.V(2).InfoS("Scheduling is always needed for CRPs of the PickAll placement type; entering scheduling stages", "clusterSchedulingPolicySnapshot", policyRef)

	// Run all plugins needed.
	//
	// Note that it is up to some plugin (by default the same placement anti-affinity plugin)
	// to identify clusters that already have placements, in accordance with the latest
	// scheduling policy, on them. Such clusters will not be scored; it will not be included
	// as a filtered out cluster, either.
	scored, filtered, err := f.runAllPluginsForPickAllPlacementType(ctx, state, policy, clusters)
	if err != nil {
		klog.ErrorS(err, "Failed to run all plugins (pickAll placement type)", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// Sort all the scored clusters.
	//
	// Since the Score stage is not run at all for policies of the PickAll placement type,
	// the clusters at this point all have the same zero scores; they are in actuality sorted by
	// their names to achieve deterministic behaviors.
	sort.Sort(scored)

	// Cross-reference the newly picked clusters with obsolete bindings; find out
	//
	// * bindings that should be created, i.e., create a binding for every cluster that is newly picked
	//   and does not have a binding associated with;
	// * bindings that should be patched, i.e., associate a binding whose target cluster is picked again
	//   in the current run with the latest score and the latest scheduling policy snapshot;
	// * bindings that should be deleted, i.e., mark a binding as unschedulable if its target cluster is no
	//   longer picked in the current run.
	//
	// Fields in the returned bindings are fulfilled and/or refreshed as applicable.
	klog.V(2).InfoS("Cross-referencing bindings with picked clusters", "clusterSchedulingPolicySnapshot", policyRef)
	toCreate, toDelete, toPatch, err := crossReferencePickedClustersAndDeDupBindings(crpName, policy, scored, unscheduled, obsolete)
	if err != nil {
		klog.ErrorS(err, "Failed to cross-reference bindings with picked clusters", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// Manipulate bindings accordingly.
	klog.V(2).InfoS("Manipulating bindings", "clusterSchedulingPolicySnapshot", policyRef)
	if err := f.manipulateBindings(ctx, policy, toCreate, toDelete, toPatch); err != nil {
		klog.ErrorS(err, "Failed to manipulate bindings", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// Extract the patched bindings.
	patched := make([]*placementv1beta1.ClusterResourceBinding, 0, len(toPatch))
	for _, p := range toPatch {
		patched = append(patched, p.updated)
	}

	// Update policy snapshot status with the latest scheduling decisions and condition.
	klog.V(2).InfoS("Updating policy snapshot status", "clusterSchedulingPolicySnapshot", policyRef)

	// With the PickAll placement type, the desired number of clusters to select always matches
	// with the count of scheduled + bound bindings.
	numOfClusters := len(toCreate) + len(patched) + len(scheduled) + len(bound)
	if err := f.updatePolicySnapshotStatusFromBindings(ctx, policy, numOfClusters, nil, filtered, toCreate, patched, scheduled, bound); err != nil {
		klog.ErrorS(err, "Failed to update latest scheduling decisions and condition", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// The scheduling cycle has completed.
	//
	// Note that for CRPs of the PickAll type, no requeue check is needed.
	return ctrl.Result{}, nil
}

// runAllPluginsForPickAllPlacementType runs all plugins in each stage of the scheduling cycle for a
// scheduling policy of the PickAll placement type.
//
// Note that for policies of the PickAll placement type, only the following stages are needed:
// * PreFilter
// * Filter
func (f *framework) runAllPluginsForPickAllPlacementType(
	ctx context.Context,
	state *CycleState,
	policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
	clusters []clusterv1beta1.MemberCluster,
) (scored ScoredClusters, filtered []*filteredClusterWithStatus, err error) {
	policyRef := klog.KObj(policy)

	// Run pre-filter plugins.
	//
	// Each plugin can:
	// * set up some common state for future calls (on different extensions points) in the scheduling cycle; and/or
	// * check if it needs to run the Filter stage.
	//   Any plugin that would like to be skipped is listed in the cycle state for future reference.
	//
	// Note that any failure would lead to the cancellation of the scheduling cycle.
	if status := f.runPreFilterPlugins(ctx, state, policy); status.IsInteralError() {
		klog.ErrorS(status.AsError(), "Failed to run pre filter plugins", "clusterSchedulingPolicySnapshot", policyRef)
		return nil, nil, controller.NewUnexpectedBehaviorError(status.AsError())
	}

	// Run filter plugins.
	//
	// The scheduler checks each cluster candidate by calling the chain of filter plugins; if any plugin suggests
	// that the cluster should not be bound, the cluster is ignored for the rest of the cycle. Note that clusters
	// are inspected in parallel.
	//
	// Note that any failure would lead to the cancellation of the scheduling cycle.
	passed, filtered, err := f.runFilterPlugins(ctx, state, policy, clusters)
	if err != nil {
		klog.ErrorS(err, "Failed to run filter plugins", "clusterSchedulingPolicySnapshot", policyRef)
		return nil, nil, controller.NewUnexpectedBehaviorError(err)
	}

	// Wrap all clusters that have passed the Filter stage as scored clusters.
	scored = make(ScoredClusters, 0, len(passed))
	for _, cluster := range passed {
		scored = append(scored, &ScoredCluster{
			Cluster: cluster,
			Score:   &ClusterScore{},
		})
	}
	return scored, filtered, nil
}

// runPreFilterPlugins runs all pre filter plugins sequentially.
func (f *framework) runPreFilterPlugins(ctx context.Context, state *CycleState, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) *Status {
	for _, pl := range f.profile.preFilterPlugins {
		status := pl.PreFilter(ctx, state, policy)
		switch {
		case status.IsSuccess(): // Do nothing.
		case status.IsInteralError():
			return status
		case status.IsSkip():
			state.skippedFilterPlugins.Insert(pl.Name())
		default:
			// Any status that is not Success, InternalError, or Skip is considered an error.
			return FromError(fmt.Errorf("prefilter plugin returned an unknown status %s", status), pl.Name())
		}
	}

	return nil
}

// runFilterPluginsFor runs filter plugins for a single cluster.
func (f *framework) runFilterPluginsFor(ctx context.Context, state *CycleState, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) *Status {
	for _, pl := range f.profile.filterPlugins {
		// Skip the plugin if it is not needed.
		if state.skippedFilterPlugins.Has(pl.Name()) {
			continue
		}
		status := pl.Filter(ctx, state, policy, cluster)
		switch {
		case status.IsSuccess(): // Do nothing.
		case status.IsInteralError():
			return status
		case status.IsClusterUnschedulable():
			return status
		case status.IsClusterAlreadySelected():
			return status
		default:
			// Any status that is not Success, InternalError, or ClusterUnschedulable is considered an error.
			return FromError(fmt.Errorf("filter plugin returned an unknown status %s", status), pl.Name())
		}
	}

	return nil
}

// filteredClusterWithStatus is struct that documents clusters filtered out at the Filter stage,
// along with a plugin status, which documents why a cluster is filtered out.
//
// This struct is used for the purpose of keeping reasons for returning scheduling decision to
// the user.
type filteredClusterWithStatus struct {
	cluster *clusterv1beta1.MemberCluster
	status  *Status
}

// runFilterPlugins runs filter plugins on clusters in parallel.
func (f *framework) runFilterPlugins(ctx context.Context, state *CycleState, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, clusters []clusterv1beta1.MemberCluster) (passed []*clusterv1beta1.MemberCluster, filtered []*filteredClusterWithStatus, err error) {
	// Create a child context.
	childCtx, cancel := context.WithCancel(ctx)

	// Pre-allocate slices to avoid races.
	passed = make([]*clusterv1beta1.MemberCluster, len(clusters))
	var passedIdx int32 = -1
	filtered = make([]*filteredClusterWithStatus, len(clusters))
	var filteredIdx int32 = -1

	errFlag := parallelizer.NewErrorFlag()

	doWork := func(pieces int) {
		cluster := clusters[pieces]
		status := f.runFilterPluginsFor(childCtx, state, policy, &cluster)
		switch {
		case status.IsSuccess():
			// Use atomic add to avoid races with minimum overhead.
			newPassedIdx := atomic.AddInt32(&passedIdx, 1)
			passed[newPassedIdx] = &cluster
		case status.IsClusterUnschedulable():
			// Use atomic add to avoid races with minimum overhead.
			newFilteredIdx := atomic.AddInt32(&filteredIdx, 1)
			filtered[newFilteredIdx] = &filteredClusterWithStatus{
				cluster: &cluster,
				status:  status,
			}
		case status.IsClusterAlreadySelected():
			// Simply ignore the cluster if it is already selected; no further stages need
			// to run for this cluster, and it should not be considered as a filtered out one
			// either.
		default: // An error has occurred.
			errFlag.Raise(status.AsError())
			// Cancel the child context, which will lead the parallelizer to stop running tasks.
			cancel()
		}
	}

	// Run inspection in parallel.
	//
	// Note that the parallel run will be stopped immediately upon encounter of the first error.
	f.parallelizer.ParallelizeUntil(childCtx, len(clusters), doWork, "runFilterPlugins")
	// Retrieve the first error from the error flag.
	if err := errFlag.Lower(); err != nil {
		return nil, nil, err
	}

	// Trim the slices to the actual size.
	passed = passed[:passedIdx+1]
	filtered = filtered[:filteredIdx+1]

	return passed, filtered, nil
}

// manipulateBindings creates, patches, and deletes bindings.
func (f *framework) manipulateBindings(
	ctx context.Context,
	policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
	toCreate, toDelete []*placementv1beta1.ClusterResourceBinding,
	toPatch []*bindingWithPatch,
) error {
	policyRef := klog.KObj(policy)

	// Create new bindings; these bindings will be of the Scheduled state.
	if err := f.createBindings(ctx, toCreate); err != nil {
		klog.ErrorS(err, "Failed to create new bindings", "clusterSchedulingPolicySnapshot", policyRef)
		return err
	}

	// Patch existing bindings.
	//
	// A race condition may arise here, when a rollout controller attempts to update bindings
	// at the same time with the scheduler, e.g., marking a binding as bound (from the scheduled
	// state). To avoid such races, the method performs a JSON patch rather than a regular update.
	if err := f.patchBindings(ctx, toPatch); err != nil {
		klog.ErrorS(err, "Failed to update old bindings", "clusterSchedulingPolicySnapshot", policyRef)
		return err
	}

	// Mark bindings as unschedulable.
	//
	// Note that a race condition may arise here, when a rollout controller attempts to update bindings
	// at the same time with the scheduler. An error induced requeue will happen in this case.
	//
	// This is set to happen after new bindings are created and old bindings are updated, to
	// avoid interruptions (deselected then reselected) in a best effort manner.
	if err := f.markAsUnscheduledFor(ctx, toDelete); err != nil {
		klog.ErrorS(err, "Failed to mark bindings as unschedulable", "clusterSchedulingPolicySnapshot", policyRef)
		return err
	}

	return nil
}

// createBindings creates a list of new bindings.
func (f *framework) createBindings(ctx context.Context, toCreate []*placementv1beta1.ClusterResourceBinding) error {
	for _, binding := range toCreate {
		// TO-DO (chenyu1): Add some jitters here to avoid swarming the API when there is a large number of
		// bindings to create.
		if err := f.client.Create(ctx, binding); err != nil {
			return controller.NewCreateIgnoreAlreadyExistError(fmt.Errorf("failed to create binding %s: %w", binding.Name, err))
		}
	}
	return nil
}

// patchBindings patches a list of existing bindings using JSON patch.
func (f *framework) patchBindings(ctx context.Context, toPatch []*bindingWithPatch) error {
	// TODO (rzhang): issue those patches in parallel, retry if there is conflict
	for _, bp := range toPatch {
		// Use JSON patch to avoid races.
		if err := f.client.Patch(ctx, bp.updated, bp.patch); err != nil {
			return controller.NewUpdateIgnoreConflictError(fmt.Errorf("failed to patch binding %s: %w", bp.updated.Name, err))
		}
	}
	return nil
}

// updatePolicySnapshotStatusFromBindings updates the policy snapshot status, in accordance with the list of
// clusters filtered out by the scheduler, and the list of bindings provisioned by the scheduler.
func (f *framework) updatePolicySnapshotStatusFromBindings(
	ctx context.Context,
	policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
	numOfClusters int,
	notPicked ScoredClusters,
	filtered []*filteredClusterWithStatus,
	existing ...[]*placementv1beta1.ClusterResourceBinding,
) error {
	policyRef := klog.KObj(policy)

	// Prepare new scheduling decisions.
	newDecisions := newSchedulingDecisionsFromBindings(f.maxUnselectedClusterDecisionCount, notPicked, filtered, existing...)
	// Prepare new scheduling condition.
	newCondition := newScheduledConditionFromBindings(policy, numOfClusters, existing...)

	// Compare the new decisions + condition with the old ones.
	currentDecisions := policy.Status.ClusterDecisions
	currentCondition := meta.FindStatusCondition(policy.Status.Conditions, string(placementv1beta1.PolicySnapshotScheduled))
	if equalDecisions(currentDecisions, newDecisions) && condition.EqualCondition(currentCondition, &newCondition) {
		// Skip if there is no change in decisions and conditions.
		return nil
	}

	// Retrieve the corresponding CRP generation.
	observedCRPGeneration, err := annotations.ExtractObservedCRPGenerationFromPolicySnapshot(policy)
	if err != nil {
		klog.ErrorS(err, "Failed to retrieve CRP generation from annoation", "clusterSchedulingPolicySnapshot", policyRef)
		return controller.NewUnexpectedBehaviorError(err)
	}

	// Update the status.
	policy.Status.ClusterDecisions = newDecisions
	policy.Status.ObservedCRPGeneration = observedCRPGeneration
	meta.SetStatusCondition(&policy.Status.Conditions, newCondition)
	if err := f.client.Status().Update(ctx, policy, &client.SubResourceUpdateOptions{}); err != nil {
		klog.ErrorS(err, "Failed to update policy snapshot status", "clusterSchedulingPolicySnapshot", policyRef)
		return controller.NewAPIServerError(false, err)
	}
	return nil
}

// runSchedulingCycleForPickNPlacementType runs the scheduling cycle for a scheduling policy of the PickN
// placement type.
func (f *framework) runSchedulingCycleForPickNPlacementType(
	ctx context.Context,
	state *CycleState,
	crpName string,
	policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
	clusters []clusterv1beta1.MemberCluster,
	bound, scheduled, unscheduled, obsolete []*placementv1beta1.ClusterResourceBinding,
) (result ctrl.Result, err error) {
	policyRef := klog.KObj(policy)

	// Retrieve the desired number of clusters from the policy.
	//
	// Note that for scheduling policies of the PickN type, this annotation is expected to be present.
	numOfClusters, err := annotations.ExtractNumOfClustersFromPolicySnapshot(policy)
	if err != nil {
		klog.ErrorS(err, "Failed to extract number of clusters required from policy snapshot", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
	}

	// Check if the scheduler should downscale, i.e., mark some scheduled/bound bindings as unscheduled and/or
	// clean up all obsolete bindings right away.
	//
	// Normally obsolete bindings are kept for cross-referencing at the end of the scheduling cycle to minimize
	// interruptions caused by scheduling policy change; however, in the case of downscaling, they can be removed
	// right away.
	//
	// To summarize, the scheduler will only downscale when
	//
	// * the scheduling policy is of the PickN type; and
	// * currently there are too many selected clusters, or more specifically too many scheduled/bound bindings
	//   in the system; or there are exactly the right number of selected clusters, but some obsolete bindings still linger
	//   in the system.
	if act, downscaleCount := shouldDownscale(policy, numOfClusters, len(scheduled)+len(bound), len(obsolete)); act {
		// Downscale if needed.
		//
		// To minimize interruptions, the scheduler picks scheduled bindings first, and then
		// bound bindings; when processing bound bindings, the logic prioritizes bindings that
		//
		//
		// This step will also mark all obsolete bindings (if any) as unscheduled right away.
		klog.V(2).InfoS("Downscaling is needed", "clusterSchedulingPolicySnapshot", policyRef, "downscaleCount", downscaleCount)

		// Mark all obsolete bindings as unscheduled first.
		if err := f.markAsUnscheduledFor(ctx, obsolete); err != nil {
			klog.ErrorS(err, "Failed to mark obsolete bindings as unscheduled", "clusterSchedulingPolicySnapshot", policyRef)
			return ctrl.Result{}, err
		}

		// Perform actual downscaling; this will be skipped if the downscale count is zero.
		scheduled, bound, err = f.downscale(ctx, scheduled, bound, downscaleCount)
		if err != nil {
			klog.ErrorS(err, "failed to downscale", "clusterSchedulingPolicySnapshot", policyRef)
			return ctrl.Result{}, err
		}

		// Update the policy snapshot status with the latest scheduling decisions and condition.
		//
		// Note that since there is no reliable way to determine the validity of old decisions added
		// to the policy snapshot status, we will only update the status with the known facts, i.e.,
		// the clusters that are currently selected.
		if err := f.updatePolicySnapshotStatusFromBindings(ctx, policy, numOfClusters, nil, nil, scheduled, bound); err != nil {
			klog.ErrorS(err, "Failed to update latest scheduling decisions and condition when downscaling", "clusterSchedulingPolicySnapshot", policyRef)
			return ctrl.Result{}, err
		}

		// Return immediately as there are no more bindings for the scheduler to scheduler at this moment.
		return ctrl.Result{}, nil
	}

	// Check if the scheduler needs to take action; a scheduling cycle is only needed if
	// currently there are not enough number of bindings.
	if !shouldSchedule(numOfClusters, len(bound)+len(scheduled)) {
		// No action is needed; however, a status refresh might be warranted.
		//
		// This is needed as a number of situations (e.g., POST/PUT failures) may lead to inconsistencies between
		// the decisions added to the policy snapshot status and the actual list of bindings.
		klog.V(2).InfoS("No scheduling is needed", "clusterSchedulingPolicySnapshot", policyRef)
		// Note that since there is no reliable way to determine the validity of old decisions added
		// to the policy snapshot status, we will only update the status with the known facts, i.e.,
		// the clusters that are currently selected.
		if err := f.updatePolicySnapshotStatusFromBindings(ctx, policy, numOfClusters, nil, nil, bound, scheduled); err != nil {
			klog.ErrorS(err, "Failed to update latest scheduling decisions and condition when no scheduling run is needed", "clusterSchedulingPolicySnapshot", policyRef)
			return ctrl.Result{}, err
		}

		// Return immediate as there no more bindings for the scheduler to schedule at this moment.
		return ctrl.Result{}, nil
	}

	// The scheduler needs to take action; enter the actual scheduling stages.
	klog.V(2).InfoS("Scheduling is needed; entering scheduling stages", "clusterSchedulingPolicySnapshot", policyRef)

	// Run all the plugins.
	//
	// Note that it is up to some plugin (by default the same placement anti-affinity plugin)
	// to identify clusters that already have placements, in accordance with the latest
	// scheduling policy, on them. Such clusters will not be scored; it will not be included
	// as a filtered out cluster, either.
	scored, filtered, err := f.runAllPluginsForPickNPlacementType(ctx, state, policy, numOfClusters, len(bound)+len(scheduled), clusters)
	if err != nil {
		klog.ErrorS(err, "Failed to run all plugins", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// Pick the top scored clusters.
	klog.V(2).InfoS("Picking clusters", "clusterSchedulingPolicySnapshot", policyRef)

	// Calculate the number of clusters to pick.
	numOfClustersToPick := calcNumOfClustersToSelect(state.desiredBatchSize, state.batchSizeLimit, len(scored))

	// Do a sanity check; normally this branch will never run, as earlier check
	// guarantees that the number of clusters to pick is always no greater than number of
	// scored clusters.
	if numOfClustersToPick > len(scored) {
		err := fmt.Errorf("number of clusters to pick is greater than number of scored clusters: %d > %d", numOfClustersToPick, len(scored))
		klog.ErrorS(err, "Failed to calculate number of clusters to pick", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
	}

	// Pick the clusters.
	//
	// Note that at this point of the scheduling cycle, any cluster associated with a currently
	// bound or scheduled binding should be filtered out already.
	picked, notPicked := pickTopNScoredClusters(scored, numOfClustersToPick)

	// Cross-reference the newly picked clusters with obsolete bindings; find out
	//
	// * bindings that should be created, i.e., create a binding for every cluster that is newly picked
	//   and does not have a binding associated with;
	// * bindings that should be patched, i.e., associate a binding whose target cluster is picked again
	//   in the current run with the latest score and the latest scheduling policy snapshot;
	// * bindings that should be deleted, i.e., mark a binding as unschedulable if its target cluster is no
	//   longer picked in the current run.
	//
	// Fields in the returned bindings are fulfilled and/or refreshed as applicable.
	klog.V(2).InfoS("Cross-referencing bindings with picked clusters", "clusterSchedulingPolicySnapshot", policyRef)
	toCreate, toDelete, toPatch, err := crossReferencePickedClustersAndDeDupBindings(crpName, policy, picked, unscheduled, obsolete)
	if err != nil {
		klog.ErrorS(err, "Failed to cross-reference bindings with picked clusters", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// Manipulate bindings accordingly.
	klog.V(2).InfoS("Manipulating bindings", "clusterSchedulingPolicySnapshot", policyRef)
	if err := f.manipulateBindings(ctx, policy, toCreate, toDelete, toPatch); err != nil {
		klog.ErrorS(err, "Failed to manipulate bindings", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// Requeue if needed.
	//
	// The scheduler will requeue to pick more clusters for the current policy snapshot if and only if
	// * one or more plugins have imposed a valid batch size limit; and
	// * the scheduler has found enough clusters for the current policy snapshot per this batch size limit.
	//
	// Note that the scheduler workflow at this point guarantees that the desired batch size is no less
	// than the batch size limit, and that the number of picked clusters is no more than the batch size
	// limit.
	//
	// Also note that if a requeue is needed, the scheduling decisions and condition are updated only
	// when there are no more clusters to pick.
	if shouldRequeue(state.desiredBatchSize, state.batchSizeLimit, len(toCreate)+len(toPatch)) {
		return ctrl.Result{Requeue: true}, nil
	}

	// Extract the patched bindings.
	patched := make([]*placementv1beta1.ClusterResourceBinding, 0, len(toPatch))
	for _, p := range toPatch {
		patched = append(patched, p.updated)
	}

	// Update policy snapshot status with the latest scheduling decisions and condition.
	klog.V(2).InfoS("Updating policy snapshot status", "clusterSchedulingPolicySnapshot", policyRef)
	if err := f.updatePolicySnapshotStatusFromBindings(ctx, policy, numOfClusters, notPicked, filtered, toCreate, patched, scheduled, bound); err != nil {
		klog.ErrorS(err, "Failed to update latest scheduling decisions and condition", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// The scheduling cycle has completed.
	return ctrl.Result{}, nil
}

// downscale performs downscaling on scheduled and bound bindings, i.e., marks some of them as unscheduled.
//
// To minimize interruptions, the scheduler picks scheduled bindings first (in any order); if there
// are still more bindings to trim, the scheduler will move onto bound bindings, and it prefers
// ones with a lower cluster score and a smaller name (in alphabetical order) .
func (f *framework) downscale(ctx context.Context, scheduled, bound []*placementv1beta1.ClusterResourceBinding, count int) (updatedScheduled, updatedBound []*placementv1beta1.ClusterResourceBinding, err error) {
	if count == 0 {
		// Skip if the downscale count is zero.
		return scheduled, bound, nil
	}

	// A sanity check is added here to avoid index errors; normally the downscale count is guaranteed
	// to be no greater than the sum of the number of scheduled and bound bindings.
	if count > len(scheduled)+len(bound) {
		err := fmt.Errorf("received an invalid downscale count %d (scheduled count: %d, bound count: %d)", count, len(scheduled), len(bound))
		return scheduled, bound, controller.NewUnexpectedBehaviorError(err)
	}

	switch {
	case count < len(scheduled):
		// Trim part of scheduled bindings should suffice.

		// Sort the scheduled bindings by their cluster scores (and secondly, their names).
		//
		// The scheduler will attempt to trim first bindings that less fitting to the scheduling
		// policy; for any two clusters with the same score, prefer the one with a smaller name
		// (in alphabetical order).
		//
		// Note that this is at best an approximation, as the cluster score assigned earlier might
		// no longer apply, due to the ever-changing state in the fleet.
		sortedScheduled := sortByClusterScoreAndName(scheduled)

		// Trim scheduled bindings.
		bindingsToDelete := make([]*placementv1beta1.ClusterResourceBinding, 0, count)
		for i := 0; i < len(sortedScheduled) && i < count; i++ {
			bindingsToDelete = append(bindingsToDelete, sortedScheduled[i])
		}

		return sortedScheduled[count:], bound, f.markAsUnscheduledFor(ctx, bindingsToDelete)
	case count == len(scheduled):
		// Trim all scheduled bindings.
		return nil, bound, f.markAsUnscheduledFor(ctx, scheduled)
	case count < len(scheduled)+len(bound):
		// Trim all scheduled bindings and part of bound bindings.
		bindingsToDelete := make([]*placementv1beta1.ClusterResourceBinding, 0, count)
		bindingsToDelete = append(bindingsToDelete, scheduled...)

		left := count - len(bindingsToDelete)

		// Sort the scheduled bindings by their cluster scores (and secondly, their names).
		//
		// The scheduler will attempt to trim first bindings that less fitting to the scheduling
		// policy; for any two clusters with the same score, prefer the one with a smaller name
		// (in alphabetical order).
		//
		// Note that this is at best an approximation, as the cluster score assigned earlier might
		// no longer apply, due to the ever-changing state in the fleet.
		sortedBound := sortByClusterScoreAndName(bound)
		for i := 0; i < left && i < len(sortedBound); i++ {
			bindingsToDelete = append(bindingsToDelete, sortedBound[i])
		}

		return nil, sortedBound[left:], f.markAsUnscheduledFor(ctx, bindingsToDelete)
	case count == len(scheduled)+len(bound):
		// Trim all scheduled and bound bindings.
		bindingsToDelete := make([]*placementv1beta1.ClusterResourceBinding, 0, count)
		bindingsToDelete = append(bindingsToDelete, scheduled...)
		bindingsToDelete = append(bindingsToDelete, bound...)
		return nil, nil, f.markAsUnscheduledFor(ctx, bindingsToDelete)
	default:
		// Normally this branch will never run, as an earlier check has guaranteed that
		// count <= len(scheduled) + len(bound).
		return nil, nil, controller.NewUnexpectedBehaviorError(fmt.Errorf("received an invalid downscale count %d (scheduled count: %d, bound count: %d)", count, len(scheduled), len(bound)))
	}
}

// runAllPluginsForPickNPlacementType runs all plugins for a scheduling policy of the PickN placement type.
//
// Note that all stages are required to run for this placement type.
func (f *framework) runAllPluginsForPickNPlacementType(
	ctx context.Context,
	state *CycleState,
	policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
	numOfClusters int,
	numOfBoundOrScheduledBindings int,
	clusters []clusterv1beta1.MemberCluster,
) (scored ScoredClusters, filtered []*filteredClusterWithStatus, err error) {
	policyRef := klog.KObj(policy)

	// Calculate the batch size.
	//
	// Note that obsolete bindings are not counted.
	state.desiredBatchSize = numOfClusters - numOfBoundOrScheduledBindings

	// An earlier check guarantees that the desired batch size is always positive; however, the scheduler still
	// performs a sanity check here; normally this branch will never run.
	if state.desiredBatchSize <= 0 {
		err := fmt.Errorf("desired batch size is below zero: %d", state.desiredBatchSize)
		klog.ErrorS(err, "Failed to calculate desired batch size", "clusterSchedulingPolicySnapshot", policyRef)
		return nil, nil, controller.NewUnexpectedBehaviorError(err)
	}

	// Run pre-batch plugins.
	//
	// These plugins each yields a batch size limit; the minimum of these limits is used as the actual batch size for
	// this scheduling cycle.
	//
	// Note that any failure would lead to the cancellation of the scheduling cycle.
	batchSizeLimit, status := f.runPostBatchPlugins(ctx, state, policy)
	if status.IsInteralError() {
		klog.ErrorS(status.AsError(), "Failed to run post batch plugins", "clusterSchedulingPolicySnapshot", policyRef)
		return nil, nil, controller.NewUnexpectedBehaviorError(status.AsError())
	}

	// A sanity check; normally this branch will never run, as runPostBatchPlugins guarantees that
	// the batch size limit is never greater than the desired batch size.
	if batchSizeLimit > state.desiredBatchSize || batchSizeLimit < 0 {
		err := fmt.Errorf("batch size limit is not valid: %d, desired batch size: %d", batchSizeLimit, state.desiredBatchSize)
		klog.ErrorS(err, "Failed to set batch size limit", "clusterSchedulingPolicySnapshot", policyRef)
		return nil, nil, controller.NewUnexpectedBehaviorError(err)
	}
	state.batchSizeLimit = batchSizeLimit

	// Run pre-filter plugins.
	//
	// Each plugin can:
	// * set up some common state for future calls (on different extensions points) in the scheduling cycle; and/or
	// * check if it needs to run the the Filter stage.
	//   Any plugin that would like to be skipped is listed in the cycle state for future reference.
	//
	// Note that any failure would lead to the cancellation of the scheduling cycle.
	if status := f.runPreFilterPlugins(ctx, state, policy); status.IsInteralError() {
		klog.ErrorS(status.AsError(), "Failed to run pre filter plugins", "clusterSchedulingPolicySnapshot", policyRef)
		return nil, nil, controller.NewUnexpectedBehaviorError(status.AsError())
	}

	// Run filter plugins.
	//
	// The scheduler checks each cluster candidate by calling the chain of filter plugins; if any plugin suggests
	// that the cluster should not be bound, the cluster is ignored for the rest of the cycle. Note that clusters
	// are inspected in parallel.
	//
	// Note that any failure would lead to the cancellation of the scheduling cycle.
	passed, filtered, err := f.runFilterPlugins(ctx, state, policy, clusters)
	if err != nil {
		klog.ErrorS(err, "Failed to run filter plugins", "clusterSchedulingPolicySnapshot", policyRef)
		return nil, nil, controller.NewUnexpectedBehaviorError(err)
	}

	// Run pre-score plugins.
	if status := f.runPreScorePlugins(ctx, state, policy); status.IsInteralError() {
		klog.ErrorS(status.AsError(), "Failed ro run pre-score plugins", "clusterSchedulingPolicySnapshot", policyRef)
		return nil, nil, controller.NewUnexpectedBehaviorError(status.AsError())
	}

	// Run score plugins.
	//
	// The scheduler checks each cluster candidate by calling the chain of score plugins; all scores are added together
	// as the final score for a specific cluster.
	//
	// Note that at this moment, since no normalization is needed, the addition is performed directly at this step;
	// when need for normalization materializes, this step should return a list of scores per cluster per plugin instead.
	scored, err = f.runScorePlugins(ctx, state, policy, passed)
	if err != nil {
		klog.ErrorS(err, "Failed to run score plugins", "clusterSchedulingPolicySnapshot", policyRef)
		return nil, nil, controller.NewUnexpectedBehaviorError(err)
	}

	return scored, filtered, nil
}

// runPostBatchPlugins runs all post batch plugins sequentially.
func (f *framework) runPostBatchPlugins(ctx context.Context, state *CycleState, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (int, *Status) {
	minBatchSizeLimit := state.desiredBatchSize
	for _, pl := range f.profile.postBatchPlugins {
		batchSizeLimit, status := pl.PostBatch(ctx, state, policy)
		switch {
		case status.IsSuccess():
			if batchSizeLimit < minBatchSizeLimit && batchSizeLimit >= 0 {
				minBatchSizeLimit = batchSizeLimit
			}
		case status.IsInteralError():
			return 0, status
		case status.IsSkip(): // Do nothing.
		default:
			// Any status that is not Success, InternalError, or Skip is considered an error.
			return 0, FromError(fmt.Errorf("postbatch plugin returned an unsupported status: %s", status), pl.Name())
		}
	}

	return minBatchSizeLimit, nil
}

// runPreScorePlugins runs all pre score plugins sequentially.
func (f *framework) runPreScorePlugins(ctx context.Context, state *CycleState, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) *Status {
	for _, pl := range f.profile.preScorePlugins {
		status := pl.PreScore(ctx, state, policy)
		switch {
		case status.IsSuccess(): // Do nothing.
		case status.IsInteralError():
			return status
		case status.IsSkip():
			state.skippedScorePlugins.Insert(pl.Name())
		default:
			// Any status that is not Success, InternalError, or Skip is considered an error.
			return FromError(fmt.Errorf("prescore plugin returned an unknown status %s", status), pl.Name())
		}
	}

	return nil
}

// runScorePluginsFor runs score plugins for a single cluster.
func (f *framework) runScorePluginsFor(ctx context.Context, state *CycleState, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (scoreList map[string]*ClusterScore, status *Status) {
	// Pre-allocate score list to avoid races.
	scoreList = make(map[string]*ClusterScore, len(f.profile.scorePlugins))

	for _, pl := range f.profile.scorePlugins {
		// Skip the plugin if it is not needed.
		if state.skippedScorePlugins.Has(pl.Name()) {
			continue
		}
		score, status := pl.Score(ctx, state, policy, cluster)
		switch {
		case status.IsSuccess():
			scoreList[pl.Name()] = score
		case status.IsInteralError():
			return nil, status
		default:
			// Any status that is not Success or InternalError is considered an error.
			return nil, FromError(fmt.Errorf("score plugin returned an unknown status %s", status), pl.Name())
		}
	}

	return scoreList, nil
}

// runScorePlugins runs score plugins on clusters in parallel.
func (f *framework) runScorePlugins(ctx context.Context, state *CycleState, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, clusters []*clusterv1beta1.MemberCluster) (ScoredClusters, error) {
	// Pre-allocate slices to avoid races.
	scoredClusters := make(ScoredClusters, len(clusters))

	// As a shortcut, return immediately if there is no available cluster.
	if len(clusters) == 0 {
		return scoredClusters, nil
	}

	// Create a child context.
	childCtx, cancel := context.WithCancel(ctx)

	var scoredClustersIdx int32 = -1

	errFlag := parallelizer.NewErrorFlag()

	doWork := func(pieces int) {
		cluster := clusters[pieces]
		scoreList, status := f.runScorePluginsFor(childCtx, state, policy, cluster)
		switch {
		case status.IsSuccess():
			totalScore := &ClusterScore{}
			for _, score := range scoreList {
				totalScore.Add(score)
			}
			// Use atomic add to avoid races with minimum overhead.
			newScoredClustersIdx := atomic.AddInt32(&scoredClustersIdx, 1)
			scoredClusters[newScoredClustersIdx] = &ScoredCluster{
				Cluster: cluster,
				Score:   totalScore,
			}
		default: // An error has occurred.
			errFlag.Raise(status.AsError())
			// Cancel the child context, which will lead the parallelizer to stop running tasks.
			cancel()
		}
	}

	// Run inspection in parallel.
	//
	// Note that the parallel run will be stopped immediately upon encounter of the first error.
	f.parallelizer.ParallelizeUntil(childCtx, len(clusters), doWork, "runScorePlugins")
	if err := errFlag.Lower(); err != nil {
		return nil, err
	}

	// Trim the slice to its actual size.
	scoredClusters = scoredClusters[:scoredClustersIdx+1]

	return scoredClusters, nil
}

// invalidClusterWithReason is struct that documents a cluster that is, though present in
// the list of current clusters, not valid for resource placement (e.g., it is experiencing
// a network partition)
// along with a plugin status, which documents why a cluster is filtered out.
//
// This struct is used for the purpose of keeping reasons for returning scheduling decision to
// the user.
type invalidClusterWithReason struct {
	cluster *clusterv1beta1.MemberCluster
	reason  string
}

// crossReferenceClustersWithTargetNames cross-references the current list of clusters in the fleet
// and the list of target clusters user specifies in the placement policy.
func (f *framework) crossReferenceClustersWithTargetNames(current []clusterv1beta1.MemberCluster, target []string) (valid []*clusterv1beta1.MemberCluster, invalid []*invalidClusterWithReason, notFound []string) {
	// Pre-allocate with a reasonable capacity.
	valid = make([]*clusterv1beta1.MemberCluster, 0, len(target))
	invalid = make([]*invalidClusterWithReason, 0, len(target))
	notFound = make([]string, 0, len(target))

	// Build a map of current clusters for quick lookup.
	currentMap := make(map[string]clusterv1beta1.MemberCluster)
	for idx := range current {
		cluster := current[idx]
		currentMap[cluster.Name] = cluster
	}

	for idx := range target {
		targetName := target[idx]
		cluster, ok := currentMap[targetName]
		if !ok {
			// The target cluster is not found in the list of current clusters.
			notFound = append(notFound, targetName)
			continue
		}

		eligible, reason := f.clusterEligibilityChecker.IsEligible(&cluster)
		if !eligible {
			// The target cluster is found, but it is not a valid target (ineligible for resource placement).
			invalid = append(invalid, &invalidClusterWithReason{
				cluster: &cluster,
				reason:  reason,
			})
			continue
		}

		// The target cluster is found, and it is a valid target (eligible for resource placement).
		valid = append(valid, &cluster)
	}

	return valid, invalid, notFound
}

// updatePolicySnapshotStatusForPickFixedPlacementType updates the policy snapshot status with
// the latest scheduling decisions and condition when there is a fixed set of clusters to
// select (PickFixed placement type).
//
// Note that due to the nature of scheduling to a fixed set of clusters, in the function
// the scheduler prepares the scheduling related status based on the different types of
// target clusters other than the final outcome (i.e., the actual list of bindings created).
// The correctness is still guaranteed as the outcome of the scheduling cycle is deterministic
// when given a set of fixed clusters to schedule resources to, as long as
//   - there is no manipulation of the scheduling result (e.g., binding directly created by
//     the user) without acknowledge from the scheduler;and
//   - the status is only added after the actual binding manipulation has been completed without
//     an error.
func (f *framework) updatePolicySnapshotStatusForPickFixedPlacementType(
	ctx context.Context,
	policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
	valid []*clusterv1beta1.MemberCluster,
	invalid []*invalidClusterWithReason,
	notFound []string,
) error {
	policyRef := klog.KObj(policy)

	// Prepare new scheduling decisions.
	newDecisions := newSchedulingDecisionsForPickFixedPlacementType(valid, invalid, notFound)
	// Prepare new scheduling condition.
	var newCondition metav1.Condition
	if len(invalid)+len(notFound) == 0 {
		// The scheduler has selected all the clusters, as the scheduling policy dictates.
		newCondition = newScheduledCondition(policy, metav1.ConditionTrue, fullyScheduledReason, fullyScheduledMessage)
	} else {
		// Some of the targets cannot be selected.
		newCondition = newScheduledCondition(policy, metav1.ConditionFalse, notFullyScheduledReason, notFullyScheduledMessage)
	}

	// Compare new decisions + condition with the old ones.
	currentDecisions := policy.Status.ClusterDecisions
	currentCondition := meta.FindStatusCondition(policy.Status.Conditions, string(placementv1beta1.PolicySnapshotScheduled))
	if equalDecisions(currentDecisions, newDecisions) && condition.EqualCondition(currentCondition, &newCondition) {
		// Skip if there is no change in decisions and conditions.
		return nil
	}

	// Retrieve the corresponding CRP generation.
	observedCRPGeneration, err := annotations.ExtractObservedCRPGenerationFromPolicySnapshot(policy)
	if err != nil {
		klog.ErrorS(err, "Failed to retrieve CRP generation from annotation", "clusterSchedulingPolicySnapshot", policyRef)
		return controller.NewUnexpectedBehaviorError(err)
	}

	// Update the status.
	policy.Status.ClusterDecisions = newDecisions
	policy.Status.ObservedCRPGeneration = observedCRPGeneration
	meta.SetStatusCondition(&policy.Status.Conditions, newCondition)
	if err := f.client.Status().Update(ctx, policy, &client.SubResourceUpdateOptions{}); err != nil {
		klog.ErrorS(err, "Failed to update policy snapshot status", "clusterSchedulingPolicySnapshot", policyRef)
		return controller.NewAPIServerError(false, err)
	}

	return nil
}

// runSchedulingCycleForPickFixedPlacementType runs the scheduling cycle when there is a fixed
// set of clusters to select in the placement policy.
func (f *framework) runSchedulingCycleForPickFixedPlacementType(
	ctx context.Context,
	crpName string,
	policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
	clusters []clusterv1beta1.MemberCluster,
	bound, scheduled, unscheduled, obsolete []*placementv1beta1.ClusterResourceBinding,
) (ctrl.Result, error) {
	policyRef := klog.KObj(policy)

	targetClusterNames := policy.Spec.Policy.ClusterNames
	if len(targetClusterNames) == 0 {
		// Skip the cycle if the list of target clusters is empty; normally this should not
		// occur.
		klog.V(2).InfoS("No scheduling is needed: list of target clusters is empty", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, nil
	}

	// Cross-reference the current list of clusters with the list of target cluster names to
	// find out:
	// * valid targets, i.e., cluster that is both present in the list of current clusters in
	//   the fleet and the list of target clusters, and is eligible for resource placement;
	// * invalid targets, i.e., cluster that is present in the list of current clusters in the
	//   fleet and the list of target clusters, but is not eligible for resource placement;
	// * not found targets, i.e., cluster that is present in the list of target clusters, but
	//   is not present in the list of current clusters in the fleet.
	valid, invalid, notFound := f.crossReferenceClustersWithTargetNames(clusters, targetClusterNames)

	// Cross-reference the valid target clusters with obsolete bindings; find out
	//
	// * bindings that should be created, i.e., create a binding for every cluster that is a valid target
	//   and does not have a binding associated with;
	// * bindings that should be patched, i.e., associate a binding whose target cluster is a valid target
	//   in the current run with the latest score and the latest scheduling policy snapshot;
	// * bindings that should be deleted, i.e., mark a binding as unschedulable if its target cluster is no
	//   longer picked in the current run.
	//
	// Fields in the returned bindings are fulfilled and/or refreshed as applicable.
	klog.V(2).InfoS("Cross-referencing bindings with valid target clusters", "clusterSchedulingPolicySnapshot", policyRef)
	toCreate, toDelete, toPatch, err := crossReferenceValidTargetsWithBindings(crpName, policy, valid, bound, scheduled, unscheduled, obsolete)
	if err != nil {
		klog.ErrorS(err, "Failed to cross-reference bindings with valid targets", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// Manipulate bindings accordingly.
	klog.V(2).InfoS("Manipulating bindings", "clusterSchedulingPolicySnapshot", policyRef)
	if err := f.manipulateBindings(ctx, policy, toCreate, toDelete, toPatch); err != nil {
		klog.ErrorS(err, "Failed to manipulate bindings", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// Update policy snapshot status with the latest scheduling decisions and condition.
	if err := f.updatePolicySnapshotStatusForPickFixedPlacementType(ctx, policy, valid, invalid, notFound); err != nil {
		klog.ErrorS(err, "Failed to update latest scheduling decisions and condition", "clusterSchedulingPolicySnapshot", policyRef)
		return ctrl.Result{}, err
	}

	// The scheduling cycle is completed.
	return ctrl.Result{}, nil
}
