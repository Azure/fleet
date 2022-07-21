package informerfactory

import (
	"context"
	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sync"
)

type PlacementController struct{}

func (p PlacementController) Add(mgr manager.Manager, delivererHandler *PlacementQueue) error {
	reconciler := &ReconcilePlacement{
		client:           mgr.GetClient(),
		recorder:         mgr.GetEventRecorderFor("placement-reconciler"),
		scheme:           mgr.GetScheme(),
		generationMap:    make(map[string]int64),
		delivererHandler: delivererHandler,
	}
	c, err := controller.New("placement-controller", mgr, controller.Options{Reconciler: reconciler})
	if err != nil {
		return err
	}
	klog.Infof("start placement controller ...")
	err = c.Watch(&source.Kind{Type: &v1alpha1.ClusterResourcePlacement{}}, &handler.EnqueueRequestForObject{})
	return err
}

type ReconcilePlacement struct {
	client   client.Client
	recorder record.EventRecorder
	scheme   *runtime.Scheme

	generationLock sync.Mutex
	generationMap  map[string]int64

	delivererHandler *PlacementQueue
}

func (r *ReconcilePlacement) Reconcile(context context.Context, request reconcile.Request) (_ reconcile.Result, err error) {
	r.generationLock.Lock()
	defer r.generationLock.Unlock()

	crp := &v1alpha1.ClusterResourcePlacement{}

	err = r.client.Get(context, request.NamespacedName, crp)
	if err != nil {
		if errors.IsNotFound(err) {
			// if not found, do i need to clean up work cr?
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if crp.GetGeneration() == r.generationMap[request.NamespacedName.String()] {
		// ignore reconcile if generation not changed
		return reconcile.Result{}, nil
	}
	// generation changed update
	r.generationMap[request.NamespacedName.String()] = crp.GetGeneration()

	r.delivererHandler.Deliver(crp, crp, schema.GroupVersionKind{
		Group:   utils.ClusterResourcePlacementGVR.Group,
		Version: utils.ClusterResourcePlacementGVR.Version,
		Kind:    v1alpha1.ClusterResourcePlacementKind,
	})

	return reconcile.Result{}, nil
}
