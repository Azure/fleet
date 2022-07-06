/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"go.goms.io/fleet/pkg/utils/controller/metrics"
	"go.goms.io/fleet/pkg/utils/keys"
)

const (
	labelError        = "error"
	labelRequeueAfter = "requeue_after"
	labelRequeue      = "requeue"
	labelSuccess      = "success"
)

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
	if err != nil || key == nil {
		klog.ErrorS(fmt.Errorf("failed to generate key for obj %+v", obj), "failed to enqueue a resource", "controller", w.name)
		return
	}

	w.queue.Add(key)
}

// Run can only be run once as we will
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

// MetaNamespaceKeyFunc generates a namespaced key for object.
func MetaNamespaceKeyFunc(obj interface{}) (QueueKey, error) {
	return cache.MetaNamespaceKeyFunc(obj)
}

// ClusterWideKeyFunc generates a ClusterWideKey for object.
func ClusterWideKeyFunc(obj interface{}) (QueueKey, error) {
	return keys.GetClusterWideKeyForObject(obj)
}
