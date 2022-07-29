/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalmembercluster

import (
	"context"
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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"go.goms.io/fleet/apis"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/utils"
)

// Reconciler reconciles a InternalMemberCluster object in the member cluster.
type Reconciler struct {
	hubClient    client.Client
	memberClient client.Client
	recorder     record.EventRecorder
}

const (
	eventReasonInternalMemberClusterHBReceived = "InternalMemberClusterHeartbeatReceived"
	eventReasonInternalMemberClusterHealthy    = "InternalMemberClusterHealthy"
	eventReasonInternalMemberClusterUnhealthy  = "InternalMemberClusterUnhealthy"
	eventReasonInternalMemberClusterJoined     = "InternalMemberClusterJoined"
	eventReasonInternalMemberClusterLeft       = "InternalMemberClusterLeft"
	eventReasonInternalMemberClusterUnknown    = "InternalMemberClusterUnknown"
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
		return logIfError(errors.Wrapf(client.IgnoreNotFound(err), "failed to get internal member cluster: %s", req.NamespacedName))
	}

	switch imc.Spec.State {
	case fleetv1alpha1.ClusterStateJoin:
		r.updateHeartbeat(&imc)
		updateHealthErr := r.updateHealth(ctx, &imc)
		r.join(&imc)
		if err := r.updateInternalMemberClusterWithRetry(ctx, &imc); err != nil {
			return logIfError(errors.Wrapf(err, "failed to update status for %s", klog.KObj(&imc)))
		}
		if updateHealthErr != nil {
			return logIfError(errors.Wrapf(updateHealthErr, "failed to update health for %s", klog.KObj(&imc)))
		}
		return ctrl.Result{RequeueAfter: time.Second * time.Duration(imc.Spec.HeartbeatPeriodSeconds)}, nil

	case fleetv1alpha1.ClusterStateLeave:
		r.leave(ctx, &imc)
		if err := r.updateInternalMemberClusterWithRetry(ctx, &imc); err != nil {
			return logIfError(errors.Wrapf(err, "failed to set internal member cluster heartbeat for %s", klog.KObj(&imc)))
		}
		return ctrl.Result{}, nil

	default:
		klog.Errorf("unknown state %v in InternalMemberCluster: %s", imc.Spec.State, req.NamespacedName)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func logIfError(err error) (ctrl.Result, error) {
	if err != nil {
		klog.ErrorS(err, "reconcile failed")
	}
	return ctrl.Result{}, err
}

// updateHeartbeat updates ConditionTypeInternalMemberClusterHeartbeat to true and set last transition time
// to now.
func (r *Reconciler) updateHeartbeat(imc *fleetv1alpha1.InternalMemberCluster) {
	r.markInternalMemberClusterHeartbeatReceived(imc)
	hbc := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat)
	hbc.LastTransitionTime = metav1.Now()
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

// join updates ConditionTypeInternalMemberClusterJoin to True if not joined yet.
func (r *Reconciler) join(imc *fleetv1alpha1.InternalMemberCluster) {
	klog.V(3).InfoS("join", "InternalMemberCluster", klog.KObj(imc))

	joined := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)

	// Already joined for the current spec.
	if joined != nil && joined.ObservedGeneration == imc.Generation && joined.Status == metav1.ConditionTrue {
		klog.V(3).InfoS("already joined", "InternalMemberCluster", klog.KObj(imc))
		return
	}

	// Join.
	klog.V(2).InfoS("join", "InternalMemberCluster", klog.KObj(imc))
	r.markInternalMemberClusterJoined(imc)
	metrics.ReportJoinResultMetric()
}

// leave updates ConditionTypeInternalMemberClusterJoin to false if not left yet.
func (r *Reconciler) leave(ctx context.Context, imc *fleetv1alpha1.InternalMemberCluster) {
	klog.V(3).InfoS("leave", "InternalMemberCluster", klog.KObj(imc))

	joined := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)

	// Already left for the current spec.
	if joined != nil && joined.ObservedGeneration == imc.Generation && joined.Status == metav1.ConditionFalse {
		klog.V(3).InfoS("already left", "InternalMemberCluster", klog.KObj(imc))
		return
	}

	// Leave.
	klog.V(2).InfoS("leave", "InternalMemberCluster", klog.KObj(imc))
	r.markInternalMemberClusterLeft(imc)
	metrics.ReportLeaveResultMetric()
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
	r.recorder.Event(imc, corev1.EventTypeNormal, eventReasonInternalMemberClusterHBReceived, "internal member cluster heartbeat received")
	hearbeatReceivedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat,
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterHBReceived,
		ObservedGeneration: imc.GetGeneration(),
	}
	imc.SetConditions(hearbeatReceivedCondition, utils.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterHealthy(imc apis.ConditionedObj) {
	klog.V(5).InfoS("markInternalMemberClusterHealthy", "InternalMemberCluster", klog.KObj(imc))
	r.recorder.Event(imc, corev1.EventTypeNormal, eventReasonInternalMemberClusterHealthy, "internal member cluster healthy")
	internalMemberClusterHealthyCond := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHealth,
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterHealthy,
		ObservedGeneration: imc.GetGeneration(),
	}
	imc.SetConditions(internalMemberClusterHealthyCond, utils.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterUnhealthy(imc apis.ConditionedObj, err error) {
	klog.V(5).InfoS("markInternalMemberClusterUnhealthy", "InternalMemberCluster", klog.KObj(imc))
	r.recorder.Event(imc, corev1.EventTypeWarning, eventReasonInternalMemberClusterUnhealthy, "internal member cluster unhealthy")
	internalMemberClusterUnhealthyCond := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHealth,
		Status:             metav1.ConditionFalse,
		Reason:             eventReasonInternalMemberClusterUnhealthy,
		Message:            err.Error(),
		ObservedGeneration: imc.GetGeneration(),
	}
	imc.SetConditions(internalMemberClusterUnhealthyCond, utils.ReconcileErrorCondition(err))
}

func (r *Reconciler) markInternalMemberClusterJoined(imc apis.ConditionedObj) {
	klog.V(5).InfoS("markInternalMemberClusterJoined", "InternalMemberCluster", klog.KObj(imc))
	r.recorder.Event(imc, corev1.EventTypeNormal, eventReasonInternalMemberClusterJoined, "internal member cluster has joined")
	joinSucceedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterJoined,
		ObservedGeneration: imc.GetGeneration(),
	}
	imc.SetConditions(joinSucceedCondition, utils.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterLeft(imc apis.ConditionedObj) {
	klog.V(5).InfoS("markInternalMemberClusterLeft", "InternalMemberCluster", klog.KObj(imc))
	r.recorder.Event(imc, corev1.EventTypeNormal, eventReasonInternalMemberClusterLeft, "internal member cluster has left")
	joinSucceedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             metav1.ConditionFalse,
		Reason:             eventReasonInternalMemberClusterLeft,
		ObservedGeneration: imc.GetGeneration(),
	}
	imc.SetConditions(joinSucceedCondition, utils.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterUnknown(imc apis.ConditionedObj) {
	klog.V(5).InfoS("markInternalMemberClusterUnknown", "InternalMemberCluster", klog.KObj(imc))
	r.recorder.Event(imc, corev1.EventTypeNormal, eventReasonInternalMemberClusterUnknown, "internal member cluster join state unknown")
	joinUnknownCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             metav1.ConditionUnknown,
		Reason:             eventReasonInternalMemberClusterUnknown,
		ObservedGeneration: imc.GetGeneration(),
	}
	imc.SetConditions(joinUnknownCondition, utils.ReconcileSuccessCondition())
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("InternalMemberClusterController")
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.InternalMemberCluster{}).
		WithEventFilter(predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		}}).
		Complete(r)
}
