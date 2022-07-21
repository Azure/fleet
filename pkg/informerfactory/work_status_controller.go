package informerfactory

import (
	"context"
	v1alpha12 "go.goms.io/fleet/apis/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/work-api/pkg/apis/v1alpha1"
	"strings"
	"sync"
)

type WorkStatusController struct{}

func (w WorkStatusController) Add(mgr manager.Manager) error {
	statusSyncer := &SyncWorkStatus{
		client:        mgr.GetClient(),
		recorder:      mgr.GetEventRecorderFor("work-status-reconciler"),
		scheme:        mgr.GetScheme(),
		generationMap: make(map[string]int64),
	}
	c, err := controller.New("work-status-controller", mgr, controller.Options{Reconciler: statusSyncer})
	if err != nil {
		return err
	}
	klog.Infof("start work status controller ...")
	err = c.Watch(&source.Kind{Type: &v1alpha1.Work{}}, &handler.EnqueueRequestForObject{})
	return err
}

type SyncWorkStatus struct {
	client         client.Client
	recorder       record.EventRecorder
	scheme         *runtime.Scheme
	generationLock sync.Mutex
	generationMap  map[string]int64
}

func (r *SyncWorkStatus) Reconcile(context context.Context, request reconcile.Request) (_ reconcile.Result, err error) {
	// todo: remove this lock once work has observedgeneration
	r.generationLock.Lock()
	defer r.generationLock.Unlock()

	work := &v1alpha1.Work{}
	err = r.client.Get(context, request.NamespacedName, work)
	if err != nil {
		if errors.IsNotFound(err) {
			// if not found, do i need to clean up work cr?
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if work.GetGeneration() == r.generationMap[request.NamespacedName.String()] {
		// ignore reconcile if generation not changed
		return reconcile.Result{}, nil
	}
	// generation changed update
	r.generationMap[request.NamespacedName.String()] = work.GetGeneration()
	// now sync back work crd status to crp status

	crp := &v1alpha12.ClusterResourcePlacement{}
	err = r.client.Get(context, types.NamespacedName{Name: work.Name}, crp)
	if err != nil {
		if errors.IsNotFound(err) {
			// if not found, do i need to clean up work cr?
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	// 1. update status condition
	clusterName := work.Namespace
	strings.Replace(clusterName, "fleet-", "", 0)

	crp.Status.Conditions = append(crp.Status.Conditions, work.Status.Conditions...)
	failed := make([]v1alpha12.FailedResourcePlacement, 0)
	for _, cond := range work.Status.ManifestConditions {
		degradedCond := getConditionByType("Degraded", cond.Conditions)
		if degradedCond != nil {
			failed = append(failed, v1alpha12.FailedResourcePlacement{
				ResourceIdentifier: v1alpha12.ResourceIdentifier{
					Group:     cond.Identifier.Group,
					Version:   cond.Identifier.Version,
					Kind:      cond.Identifier.Kind,
					Name:      cond.Identifier.Name,
					Namespace: cond.Identifier.Namespace,
				},
				Condition:   *degradedCond,
				ClusterName: clusterName,
			})
		}
	}
	// 2. only replace this clusterName's failed, and not touch other clusters
	allFailed := make([]v1alpha12.FailedResourcePlacement, 0)
	for _, failedCond := range crp.Status.FailedResourcePlacements {
		if failedCond.ClusterName != clusterName {
			allFailed = append(allFailed, failedCond)
		}
	}
	allFailed = append(allFailed, failed...)
	crp.Status.FailedResourcePlacements = allFailed
	err = r.client.Status().Update(context, crp)
	// todo: add retry on 409 for status update
	return reconcile.Result{}, err
}

func getConditionByType(conditionType string, conditions []metav1.Condition) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
