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

//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/finalizers,verbs=update

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.V(3).InfoS("Reconcile", "InternalMemberCluster", req.NamespacedName)

	var imc fleetv1alpha1.InternalMemberCluster
	if err := r.hubClient.Get(ctx, req.NamespacedName, &imc); err != nil {
		return logIfError(ctrl.Result{}, errors.Wrapf(client.IgnoreNotFound(err), "failed to get internal member cluster: %s", req.NamespacedName))
	}

	switch imc.Spec.State {
	case fleetv1alpha1.ClusterStateJoin:
		return logIfError(r.updateHeartbeat(ctx, &imc))
	case fleetv1alpha1.ClusterStateLeave:
		return logIfError(r.leave(ctx, &imc))
	default:
		klog.Errorf("unknown state %v in InternalMemberCluster: %s", imc.Spec.State, req.NamespacedName)
		return ctrl.Result{}, nil
	}
}

func logIfError(result ctrl.Result, err error) (ctrl.Result, error) {
	if err != nil {
		klog.ErrorS(err, "reconcile failed")
	}
	return result, err
}

// updateHeartbeat repeatedly performs two below operation. This informs the hub cluster that member cluster is healthy.
// Join flow on internal member cluster controller finishes when the first heartbeat completes.
// 1. Gets current cluster usage.
// 2. Updates the associated InternalMemberCluster Custom Resource with current cluster usage and marks it as Joined.
func (r *Reconciler) updateHeartbeat(ctx context.Context, imc *fleetv1alpha1.InternalMemberCluster) (ctrl.Result, error) {
	klog.V(3).InfoS("updateHeartbeat", "InternalMemberCluster", klog.KObj(imc))

	imcLastJoinCond := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
	imcHaveJoined := imcLastJoinCond != nil && imcLastJoinCond.Status == metav1.ConditionTrue
	if !imcHaveJoined {
		klog.V(2).InfoS("join", "InternalMemberCluster", klog.KObj(imc))
	}

	if err := r.collectMemberClusterUsage(ctx, imc); err != nil {
		r.markInternalMemberClusterUnhealthy(imc, errors.Wrapf(err, "failed to collect member cluster usage for %s", klog.KObj(imc)))
	} else {
		r.markInternalMemberClusterHealthy(imc)
		hbc := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat)
		hbc.LastTransitionTime = metav1.Now()
	}

	if err := r.updateInternalMemberClusterWithRetry(ctx, imc); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to set internal member cluster heartbeat for %s", klog.KObj(imc))
	}

	if !imcHaveJoined {
		klog.V(2).InfoS("join succeeded", "InternalMemberCluster", klog.KObj(imc))
		metrics.ReportJoinResultMetric()
	}

	klog.V(3).InfoS("updateHeartbeat succeeded", "InternalMemberCluster", klog.KObj(imc))
	return ctrl.Result{RequeueAfter: time.Second * time.Duration(imc.Spec.HeartbeatPeriodSeconds)}, nil
}

func (r *Reconciler) leave(ctx context.Context, imc *fleetv1alpha1.InternalMemberCluster) (ctrl.Result, error) {
	imcLastJoinCond := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
	imcHaveLeft := imcLastJoinCond != nil && imcLastJoinCond.Status == metav1.ConditionFalse

	if imcHaveLeft {
		klog.V(3).InfoS("leave: already left", "InternalMemberCluster", klog.KObj(imc))
		return ctrl.Result{}, nil
	}

	// if !imcHaveLeft.
	klog.V(2).InfoS("leave", "InternalMemberCluster", klog.KObj(imc))

	r.markInternalMemberClusterLeft(imc)
	if err := r.updateInternalMemberClusterWithRetry(ctx, imc); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to set internal member cluster member to left")
	}

	metrics.ReportLeaveResultMetric()
	klog.V(2).InfoS("leave succeeded", "InternalMemberCluster", klog.KObj(imc))
	return ctrl.Result{}, nil
}

func (r *Reconciler) updateInternalMemberClusterWithRetry(ctx context.Context, imc *fleetv1alpha1.InternalMemberCluster) error {
	klog.V(5).InfoS("updateInternalMemberClusterWithRetry", "InternalMemberCluster", klog.KObj(imc))
	backOffPeriod := retry.DefaultBackoff
	backOffPeriod.Cap = time.Second * time.Duration(imc.Spec.HeartbeatPeriodSeconds)

	return retry.OnError(backOffPeriod,
		func(err error) bool {
			if apierrors.IsNotFound(err) || apierrors.IsInvalid(err) {
				return false
			}
			if err != nil {
				klog.ErrorS(err, "failed to update internal member cluster status", "InternalMemberCluster", klog.KObj(imc))
			}
			return true
		},
		func() error {
			err := r.hubClient.Status().Update(ctx, imc)
			return err
		})
}

func (r *Reconciler) collectMemberClusterUsage(ctx context.Context, imc *fleetv1alpha1.InternalMemberCluster) error {
	klog.V(5).InfoS("collectMemberClusterUsage", "InternalMemberCluster", klog.KObj(imc))
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

	r.markInternalMemberClusterHeartbeatReceived(imc)
	r.markInternalMemberClusterJoined(imc)
	return nil
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
