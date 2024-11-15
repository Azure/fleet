/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller/metrics"
	"go.goms.io/fleet/pkg/utils/keys"
	"go.goms.io/fleet/pkg/utils/labels"
)

const (
	// ClusterManagerName is the name of KubeFleet cluster manager.
	ClusterManagerName = "KubeFleet"
)

const (
	labelError        = "error"
	labelRequeueAfter = "requeue_after"
	labelRequeue      = "requeue"
	labelSuccess      = "success"
)

var (
	// ErrUnexpectedBehavior indicates the current situation is not expected.
	// There should be something wrong with the system and cannot be recovered by itself.
	ErrUnexpectedBehavior = errors.New("unexpected behavior which cannot be handled by the controller")

	// ErrExpectedBehavior indicates the current situation is expected, which can be recovered by itself after retries.
	ErrExpectedBehavior = errors.New("expected behavior which can be recovered by itself")

	// ErrAPIServerError indicates the error is returned by the API server.
	ErrAPIServerError = errors.New("error returned by the API server")

	// ErrUserError indicates the error is caused by the user and customer needs to take the action.
	ErrUserError = errors.New("failed to process the request due to a client error")
)

// NewUnexpectedBehaviorError returns ErrUnexpectedBehavior type error when err is not nil.
func NewUnexpectedBehaviorError(err error) error {
	if err != nil {
		klog.ErrorS(err, "Unexpected behavior identified by the controller", "stackTrace", debug.Stack())
		return fmt.Errorf("%w: %v", ErrUnexpectedBehavior, err.Error())
	}
	return nil
}

// NewExpectedBehaviorError returns ErrExpectedBehavior type error when err is not nil.
func NewExpectedBehaviorError(err error) error {
	if err != nil {
		klog.ErrorS(err, "Expected behavior which can be recovered by itself")
		return fmt.Errorf("%w: %v", ErrExpectedBehavior, err.Error())
	}
	return nil
}

// NewAPIServerError returns error types when accessing data from cache or API server.
func NewAPIServerError(fromCache bool, err error) error {
	if err != nil {
		if fromCache && isUnexpectedCacheError(err) {
			return NewUnexpectedBehaviorError(err)
		}
		klog.ErrorS(err, "Error returned by the API server", "fromCache", fromCache, "reason", apierrors.ReasonForError(err))
		return fmt.Errorf("%w: %v", ErrAPIServerError, err.Error())
	}
	return nil
}

func isUnexpectedCacheError(err error) bool {
	// may need to add more error code based on the production
	// Cache will return notFound for GET.
	return !apierrors.IsNotFound(err)
}

// NewUserError returns ErrUserError type error when err is not nil.
func NewUserError(err error) error {
	if err != nil {
		klog.ErrorS(err, "Failed to process the request due to a client error")
		return fmt.Errorf("%w: %v", ErrUserError, err.Error())
	}
	return nil
}

// NewCreateIgnoreAlreadyExistError returns ErrExpectedBehavior type error if the error is already exist.
// Otherwise, returns ErrAPIServerError type error.
func NewCreateIgnoreAlreadyExistError(err error) error {
	if !apierrors.IsAlreadyExists(err) {
		return NewAPIServerError(false, err)
	}
	return NewExpectedBehaviorError(err)
}

// NewUpdateIgnoreConflictError returns ErrExpectedBehavior type error if the error is conflict.
// Otherwise, returns ErrAPIServerError type error.
func NewUpdateIgnoreConflictError(err error) error {
	if !apierrors.IsConflict(err) {
		return NewAPIServerError(false, err)
	}
	return NewExpectedBehaviorError(err)
}

// NewDeleteIgnoreNotFoundError returns nil if the error is not found.
// Otherwise, returns ErrAPIServerError type error if err is not nil
func NewDeleteIgnoreNotFoundError(err error) error {
	if !apierrors.IsNotFound(err) {
		return NewAPIServerError(false, err)
	}
	return nil
}

// Controller maintains a rate limiting queue and the items in the queue will be reconciled by a "ReconcileFunc".
// The item will be re-queued if "ReconcileFunc" returns an error, maximum re-queue times defined by "maxRetries" above,
// after that the item will be discarded from the queue.
type Controller interface {
	// Enqueue generates the key of 'obj' according to a 'KeyFunc' then adds the 'item' to queue immediately.
	Enqueue(obj interface{})

	// Run starts a certain number of concurrent workers to reconcile the items and will never stop until
	// the context is closed or canceled
	Run(ctx context.Context, workerNumber int) error
}

// QueueKey is the type of the item key that stores in queue.
// The key could be arbitrary types.
//
// The most common full-qualified key is of type '<namespace>/<name>' which doesn't carry the `GVK`
// info of the resource the key points to.
// We need to support a key type that includes GVK(Group Version Kind) so that we can reconcile on any type of resources.
type QueueKey interface{}

// KeyFunc knows how to make a key from an object. Implementations should be deterministic.
type KeyFunc func(obj interface{}) (QueueKey, error)

// ReconcileFunc knows how to consume items(key) from the queue.
type ReconcileFunc func(ctx context.Context, key QueueKey) (reconcile.Result, error)

var _ Controller = &controller{}

// controller implements Controller interface
type controller struct {
	// the name of the controller
	name string

	// keyFunc is the function that make keys for API objects.
	keyFunc KeyFunc

	// reconcileFunc is the function that process keys from the queue.
	reconcileFunc ReconcileFunc

	// queue allowing parallel processing of resources.
	queue workqueue.RateLimitingInterface
}

// NewController returns a controller which can process resource periodically. We create the queue during the creation
// of the controller which means it can only be run once. We can move that to the run if we need to run it multiple times
func NewController(Name string, KeyFunc KeyFunc, ReconcileFunc ReconcileFunc, rateLimiter workqueue.RateLimiter) Controller {
	return &controller{
		name:          Name,
		keyFunc:       KeyFunc,
		reconcileFunc: ReconcileFunc,
		queue:         workqueue.NewNamedRateLimitingQueue(rateLimiter, Name),
	}
}

func (w *controller) Enqueue(obj interface{}) {
	key, err := w.keyFunc(obj)
	if err != nil {
		klog.ErrorS(err, "failed to enqueue a resource", "controller", w.name)
		return
	}

	w.queue.Add(key)
}

// Run can only be run once as we will shut down the queue on stop.
func (w *controller) Run(ctx context.Context, workerNumber int) error {
	// we shut down the queue after each run, therefore we can't start again.
	if w.queue.ShuttingDown() {
		return fmt.Errorf("controller %s was started more than once", w.name)
	}

	klog.InfoS("Starting controller", "controller", w.name)

	w.initMetrics(workerNumber)

	// Ensure all goroutines are cleaned up when the context closes
	go func() {
		<-ctx.Done()
		w.queue.ShutDown()
	}()

	wg := &sync.WaitGroup{}
	wg.Add(workerNumber)
	for i := 0; i < workerNumber; i++ {
		go func() {
			defer wg.Done()
			defer utilruntime.HandleCrash()
			// Run a worker thread that just dequeues items, processes them, and marks them done.
			//revive:disable:empty-block
			for w.processNextWorkItem(ctx) {
			}
		}()
	}

	<-ctx.Done()
	klog.InfoS("Shutdown signal received, waiting for all workers to finish", "controller", w.name)
	wg.Wait()
	klog.InfoS("All workers finished, shutting down controller", "controller", w.name)
	return nil
}

func (w *controller) processNextWorkItem(ctx context.Context) bool {
	key, shutdown := w.queue.Get()
	if shutdown {
		// Stop working
		return false
	}

	// Done marks item as done processing, and if it has been marked as dirty again
	// while it was being processed, it will be re-added to the queue for
	// re-processing.
	defer w.queue.Done(key)

	metrics.FleetActiveWorkers.WithLabelValues(w.name).Add(1)
	defer metrics.FleetActiveWorkers.WithLabelValues(w.name).Add(-1)

	w.reconcileHandler(ctx, key)
	return true
}

func (w *controller) reconcileHandler(ctx context.Context, key interface{}) {
	// Update metrics after processing each item
	reconcileStartTS := time.Now()
	defer func() {
		metrics.FleetReconcileTime.WithLabelValues(w.name).Observe(time.Since(reconcileStartTS).Seconds())
	}()

	// RunInformersAndControllers the syncHandler, passing it the Namespace/Name string of the
	// resource to be synced.
	result, err := w.reconcileFunc(ctx, key)
	switch {
	case err != nil:
		w.queue.AddRateLimited(key)
		metrics.FleetReconcileErrors.WithLabelValues(w.name).Inc()
		metrics.FleetReconcileTotal.WithLabelValues(w.name, labelError).Inc()
		klog.ErrorS(err, "Reconciler error")
	case result.RequeueAfter > 0:
		// The result.RequeueAfter request will be lost, if it is returned
		// along with a non-nil error. But this is intended as
		// We need to drive to stable reconcile loops before queuing due
		// to result.RequestAfter
		w.queue.Forget(key)
		w.queue.AddAfter(key, result.RequeueAfter)
		metrics.FleetReconcileTotal.WithLabelValues(w.name, labelRequeueAfter).Inc()
	case result.Requeue:
		w.queue.AddRateLimited(key)
		metrics.FleetReconcileTotal.WithLabelValues(w.name, labelRequeue).Inc()
	default:
		// Forget indicates that an item is finished being retried.  Doesn't matter whether it's for perm failing
		// or for success, we'll stop the rate limiter from tracking it.  This only clears the `rateLimiter`, you
		// still have to call `Done` on the queue.
		w.queue.Forget(key)
		metrics.FleetReconcileTotal.WithLabelValues(w.name, labelSuccess).Inc()
	}
}

func (w *controller) initMetrics(workerNumber int) {
	metrics.FleetActiveWorkers.WithLabelValues(w.name).Set(0)
	metrics.FleetReconcileErrors.WithLabelValues(w.name).Add(0)
	metrics.FleetReconcileTotal.WithLabelValues(w.name, labelError).Add(0)
	metrics.FleetReconcileTotal.WithLabelValues(w.name, labelRequeueAfter).Add(0)
	metrics.FleetReconcileTotal.WithLabelValues(w.name, labelRequeue).Add(0)
	metrics.FleetReconcileTotal.WithLabelValues(w.name, labelSuccess).Add(0)
	metrics.FleetWorkerCount.WithLabelValues(w.name).Set(float64(workerNumber))
}

// NamespaceKeyFunc generates a namespaced key for any objects.
func NamespaceKeyFunc(obj interface{}) (QueueKey, error) {
	return keys.GetNamespaceKeyForObject(obj)
}

// ClusterWideKeyFunc generates a ClusterWideKey for object.
func ClusterWideKeyFunc(obj interface{}) (QueueKey, error) {
	return keys.GetClusterWideKeyForObject(obj)
}

var (
	errResourceNotFullyCreated = errors.New("not all resource snapshot in the same index group are created")
)

// FetchAllClusterResourceSnapshots fetches the group of clusterResourceSnapshots using master clusterResourceSnapshot.
func FetchAllClusterResourceSnapshots(ctx context.Context, k8Client client.Client, crp string, masterResourceSnapshot *fleetv1beta1.ClusterResourceSnapshot) (map[string]*fleetv1beta1.ClusterResourceSnapshot, error) {
	resourceSnapshots := make(map[string]*fleetv1beta1.ClusterResourceSnapshot)
	resourceSnapshots[masterResourceSnapshot.Name] = masterResourceSnapshot

	// check if there are more snapshot in the same index group
	countAnnotation := masterResourceSnapshot.Annotations[fleetv1beta1.NumberOfResourceSnapshotsAnnotation]
	snapshotCount, err := strconv.Atoi(countAnnotation)
	if err != nil || snapshotCount < 1 {
		return nil, NewUnexpectedBehaviorError(fmt.Errorf(
			"master resource snapshot %s has an invalid snapshot count %d or err %w", masterResourceSnapshot.Name, snapshotCount, err))
	}

	if snapshotCount > 1 {
		// fetch all the resource snapshot in the same index group
		index, err := labels.ExtractResourceIndexFromClusterResourceSnapshot(masterResourceSnapshot)
		if err != nil {
			klog.ErrorS(err, "Master resource snapshot has invalid resource index", "clusterResourceSnapshot", klog.KObj(masterResourceSnapshot))
			return nil, NewUnexpectedBehaviorError(err)
		}
		resourceIndexLabelMatcher := client.MatchingLabels{
			fleetv1beta1.ResourceIndexLabel: strconv.Itoa(index),
			fleetv1beta1.CRPTrackingLabel:   crp,
		}
		resourceSnapshotList := &fleetv1beta1.ClusterResourceSnapshotList{}
		if err := k8Client.List(ctx, resourceSnapshotList, resourceIndexLabelMatcher); err != nil {
			klog.ErrorS(err, "Failed to list all the resource snapshot", "clusterResourcePlacement", crp)
			return nil, NewAPIServerError(true, err)
		}
		//insert all the resource snapshot into the map
		for i := 0; i < len(resourceSnapshotList.Items); i++ {
			resourceSnapshots[resourceSnapshotList.Items[i].Name] = &resourceSnapshotList.Items[i]
		}
	}

	// check if all the resource snapshots are created since that may take a while but the rollout controller may update the resource binding on master snapshot creation
	if len(resourceSnapshots) != snapshotCount {
		misMatchErr := fmt.Errorf("%w: resource snapshots are still being created for the masterResourceSnapshot %s, total snapshot in the index group = %d, num Of existing snapshot in the group= %d",
			errResourceNotFullyCreated, masterResourceSnapshot.Name, snapshotCount, len(resourceSnapshots))
		klog.ErrorS(misMatchErr, "Resource snapshot are not ready", "clusterResourcePlacement", crp)
		return nil, NewExpectedBehaviorError(misMatchErr)
	}
	return resourceSnapshots, nil
}

// MemberController configures how to join or leave the fleet as a member.
type MemberController interface {
	// Join describes the process of joining the fleet as a member.
	Join(ctx context.Context) error

	// Leaves describes the process of leaving the fleet as a member.
	// For example, delete all the resources created by the member controller.
	Leave(ctx context.Context) error
}
