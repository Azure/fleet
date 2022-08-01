/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalmembercluster

import (
	"context"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"go.goms.io/fleet/apis"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/metrics"
)

// Reconciler reconciles a InternalMemberCluster object in the member cluster.
type Reconciler struct {
	hubClient    client.Client
	memberClient client.Client
	recorder     record.EventRecorder
	cancel       context.CancelFunc
}

const (
	eventReasonInternalMemberClusterHBReceived = "InternalMemberClusterHeartbeatReceived"
	eventReasonInternalMemberClusterHealthy    = "InternalMemberClusterHealthy"
	eventReasonInternalMemberClusterUnhealthy  = "InternalMemberClusterUnhealthy"
	eventReasonInternalMemberClusterJoined     = "InternalMemberClusterJoined"
	eventReasonInternalMemberClusterLeft       = "InternalMemberClusterLeft"
)

// NewReconciler creates a new reconciler for the internalMemberCluster CR
func NewReconciler(hubClient client.Client, memberClient client.Client) *Reconciler {
	return &Reconciler{
		hubClient:    hubClient,
		memberClient: memberClient,
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.V(3).InfoS("Reconcile", "InternalMemberCluster", req.NamespacedName)

	var imc fleetv1alpha1.InternalMemberCluster
	if err := r.hubClient.Get(ctx, req.NamespacedName, &imc); err != nil {
		klog.ErrorS(err, "failed to get internal member cluster: %s", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	switch imc.Spec.State {
	case fleetv1alpha1.ClusterStateJoin:
		r.markInternalMemberClusterHeartbeatReceived(&imc)
		updateHealthErr := r.updateHealth(ctx, &imc)
		r.markInternalMemberClusterJoined(&imc)
		if err := r.updateInternalMemberClusterWithRetry(ctx, &imc); err != nil {
			klog.ErrorS(err, "failed to update status for %s", klog.KObj(&imc))
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		if updateHealthErr != nil {
			klog.ErrorS(updateHealthErr, "failed to update health for %s", klog.KObj(&imc))
			return ctrl.Result{}, updateHealthErr
		}
		return ctrl.Result{RequeueAfter: time.Second * time.Duration(imc.Spec.HeartbeatPeriodSeconds)}, nil

	case fleetv1alpha1.ClusterStateLeave:
		r.markInternalMemberClusterLeft(&imc)
		if err := r.updateInternalMemberClusterWithRetry(ctx, &imc); err != nil {
			klog.ErrorS(err, "failed to update status for %s", klog.KObj(&imc))
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		klog.V(3).InfoS("stopping member agent controller by cancelling controller context", klog.KObj(&imc))
		r.cancel()
		return ctrl.Result{}, nil

	default:
		klog.Errorf("encountered a fatal error. unknown state %v in InternalMemberCluster: %s", imc.Spec.State, req.NamespacedName)
		return ctrl.Result{}, nil
	}
}

// updateHealth collects and updates member cluster resource stats and set ConditionTypeInternalMemberClusterHealth.
func (r *Reconciler) updateHealth(ctx context.Context, imc *fleetv1alpha1.InternalMemberCluster) error {
	klog.V(3).InfoS("updateHealth", "InternalMemberCluster", klog.KObj(imc))

	if err := r.updateResourceStats(ctx, imc); err != nil {
		r.markInternalMemberClusterUnhealthy(imc, errors.Wrapf(err, "failed to update resource stats %s", klog.KObj(imc)))
		return err
	}

	r.markInternalMemberClusterHealthy(imc)
	return nil
}

// updateResourceStats collects and updates resource usage stats of the member cluster.
func (r *Reconciler) updateResourceStats(ctx context.Context, imc *fleetv1alpha1.InternalMemberCluster) error {
	klog.V(5).InfoS("updateResourceStats", "InternalMemberCluster", klog.KObj(imc))
	var nodes corev1.NodeList
	if err := r.memberClient.List(ctx, &nodes); err != nil {
		return errors.Wrapf(err, "failed to list nodes for member cluster %s", klog.KObj(imc))
	}

	var capacityCPU, capacityMemory, allocatableCPU, allocatableMemory resource.Quantity

	for _, node := range nodes.Items {
		capacityCPU.Add(*(node.Status.Capacity.Cpu()))
		capacityMemory.Add(*(node.Status.Capacity.Memory()))
		allocatableCPU.Add(*(node.Status.Allocatable.Cpu()))
		allocatableMemory.Add(*(node.Status.Allocatable.Memory()))
	}

	imc.Status.Capacity = corev1.ResourceList{
		corev1.ResourceCPU:    capacityCPU,
		corev1.ResourceMemory: capacityMemory,
	}
	imc.Status.Allocatable = corev1.ResourceList{
		corev1.ResourceCPU:    allocatableCPU,
		corev1.ResourceMemory: allocatableMemory,
	}

	return nil
}

// updateInternalMemberClusterWithRetry updates InternalMemberCluster status.
func (r *Reconciler) updateInternalMemberClusterWithRetry(ctx context.Context, imc *fleetv1alpha1.InternalMemberCluster) error {
	klog.V(5).InfoS("updateInternalMemberClusterWithRetry", "InternalMemberCluster", klog.KObj(imc))
	backOffPeriod := retry.DefaultBackoff
	backOffPeriod.Cap = time.Second * time.Duration(imc.Spec.HeartbeatPeriodSeconds)

	return retry.OnError(backOffPeriod,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err)
		},
		func() error {
			return r.hubClient.Status().Update(ctx, imc)
		})
}

func (r *Reconciler) markInternalMemberClusterHeartbeatReceived(imc apis.ConditionedObj) {
	klog.V(5).InfoS("markInternalMemberClusterHeartbeatReceived", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat,
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterHBReceived,
		ObservedGeneration: imc.GetGeneration(),
	}
	imc.SetConditions(newCondition)

	// Hack: We need to get and set again as SetConditions() will ignore new LastTransitionTime if there is no status
	// change between existing condition and new condition.
	hbc := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat)
	hbc.LastTransitionTime = metav1.Now()
}

func (r *Reconciler) markInternalMemberClusterHealthy(imc apis.ConditionedObj) {
	klog.V(5).InfoS("markInternalMemberClusterHealthy", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHealth,
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterHealthy,
		ObservedGeneration: imc.GetGeneration(),
	}

	// Healthy status changed.
	existingCondition := imc.GetCondition(newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		klog.V(2).InfoS("healthy", "InternalMemberCluster", klog.KObj(imc))
		r.recorder.Event(imc, corev1.EventTypeNormal, eventReasonInternalMemberClusterHealthy, "internal member cluster healthy")
	}

	imc.SetConditions(newCondition)
}

func (r *Reconciler) markInternalMemberClusterUnhealthy(imc apis.ConditionedObj, err error) {
	klog.V(5).InfoS("markInternalMemberClusterUnhealthy", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHealth,
		Status:             metav1.ConditionFalse,
		Reason:             eventReasonInternalMemberClusterUnhealthy,
		Message:            err.Error(),
		ObservedGeneration: imc.GetGeneration(),
	}

	// Healthy status changed.
	existingCondition := imc.GetCondition(newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		klog.V(2).InfoS("unhealthy", "InternalMemberCluster", klog.KObj(imc))
		r.recorder.Event(imc, corev1.EventTypeWarning, eventReasonInternalMemberClusterUnhealthy, "internal member cluster unhealthy")
	}

	imc.SetConditions(newCondition)
}

func (r *Reconciler) markInternalMemberClusterJoined(imc apis.ConditionedObj) {
	klog.V(5).InfoS("markInternalMemberClusterJoined", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterJoined,
		ObservedGeneration: imc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := imc.GetCondition(newCondition.Type)
	if existingCondition == nil || existingCondition.ObservedGeneration != imc.GetGeneration() || existingCondition.Status != newCondition.Status {
		r.recorder.Event(imc, corev1.EventTypeNormal, eventReasonInternalMemberClusterJoined, "internal member cluster joined")
		klog.V(2).InfoS("joined", "InternalMemberCluster", klog.KObj(imc))
		metrics.ReportJoinResultMetric()
	}

	imc.SetConditions(newCondition)
}

func (r *Reconciler) markInternalMemberClusterLeft(imc apis.ConditionedObj) {
	klog.V(5).InfoS("markInternalMemberClusterLeft", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             metav1.ConditionFalse,
		Reason:             eventReasonInternalMemberClusterLeft,
		ObservedGeneration: imc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := imc.GetCondition(newCondition.Type)
	if existingCondition == nil || existingCondition.ObservedGeneration != imc.GetGeneration() || existingCondition.Status != newCondition.Status {
		r.recorder.Event(imc, corev1.EventTypeNormal, eventReasonInternalMemberClusterLeft, "internal member cluster left")
		klog.V(2).InfoS("left", "InternalMemberCluster", klog.KObj(imc))
		metrics.ReportLeaveResultMetric()
	}

	imc.SetConditions(newCondition)
}

// SetupUnmanagedController sets up an unmanaged controller
func (r *Reconciler) SetupUnmanagedController(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("InternalMemberClusterController")
	c, err := controller.NewUnmanaged("member-agent", mgr, controller.Options{Reconciler: r, RecoverPanic: true, MaxConcurrentReconciles: 3})
	if err != nil {
		klog.ErrorS(err, "unable to create member agent controller")
		return err
	}

	updatePredicate := predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
	}}

	if err := c.Watch(&source.Kind{Type: &fleetv1alpha1.InternalMemberCluster{}}, &handler.EnqueueRequestForObject{}, updatePredicate); err != nil {
		klog.ErrorS(err, "unable to watch Internal Member cluster")
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-mgr.Elected()
		if err := c.Start(ctx); err != nil {
			klog.ErrorS(err, "cannot run member agent controller")
		}
	}()
	r.cancel = cancel
	return nil
}
