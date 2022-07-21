package informerfactory

import (
	"context"
	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type MemberClusterController struct{}

func (m MemberClusterController) Add(mgr manager.Manager, delivererHandler *PlacementQueue) error {
	reconciler := &ReconcileMemberCluster{
		client:           mgr.GetClient(),
		recorder:         mgr.GetEventRecorderFor("member-cluster-reconciler"),
		scheme:           mgr.GetScheme(),
		generationMap:    make(map[string]int64),
		delivererHandler: delivererHandler,
	}
	c, err := controller.New("member-cluster-controller", mgr, controller.Options{Reconciler: reconciler})
	if err != nil {
		return err
	}
	klog.Infof("start member cluster controller ...")

	err = c.Watch(&source.Kind{Type: &v1alpha1.MemberCluster{}}, &handler.EnqueueRequestForObject{})
	return err
}

type ReconcileMemberCluster struct {
	client   client.Client
	recorder record.EventRecorder
	scheme   *runtime.Scheme

	generationLock sync.Mutex
	generationMap  map[string]int64

	delivererHandler *PlacementQueue
}

func (m *ReconcileMemberCluster) Reconcile(context context.Context, request reconcile.Request) (_ reconcile.Result, err error) {
	cluster := &v1alpha1.MemberCluster{}

	err = m.client.Get(context, request.NamespacedName, cluster)
	if err != nil {
		if errors.IsNotFound(err) {
			// if not found, do i need to clean up work cr?
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	m.generationLock.Lock()
	if cluster.GetGeneration() == m.generationMap[request.NamespacedName.String()] {
		// ignore reconcile if generation not changed
		return reconcile.Result{}, nil
	}
	// generation changed update
	m.generationMap[request.NamespacedName.String()] = cluster.GetGeneration()
	m.generationLock.Unlock()

	crpList := &v1alpha1.ClusterResourcePlacementList{}
	err = m.client.List(context, crpList)
	if err != nil {
		return reconcile.Result{}, err
	}
	for _, crp := range crpList.Items {
		m.delivererHandler.Deliver(&v1alpha1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: crp.Namespace,
				Name:      crp.Name,
			},
		}, cluster, schema.GroupVersionKind{
			Group:   utils.ClusterResourcePlacementGVR.Group,
			Version: utils.ClusterResourcePlacementGVR.Version,
			Kind:    v1alpha1.MemberClusterKind,
		})
	}
	return reconcile.Result{}, nil
}
