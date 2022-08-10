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
)

// Reconciler reconciles a InternalMemberCluster object in the member cluster.
type Reconciler struct {
	hubClient          client.Client
	memberClient       client.Client
	recorder           record.EventRecorder
	workAPIReconcilers []apis.Joinable
}

const (
	eventReasonInternalMemberClusterHealthy   = "InternalMemberClusterHealthy"
	eventReasonInternalMemberClusterUnhealthy = "InternalMemberClusterUnhealthy"
	eventReasonInternalMemberClusterJoined    = "InternalMemberClusterJoined"
	eventReasonInternalMemberClusterLeft      = "InternalMemberClusterLeft"
)

// NewReconciler creates a new reconciler for the internalMemberCluster CR
func NewReconciler(hubClient client.Client, memberClient client.Client, workAPIReconcilers []apis.Joinable) *Reconciler {
	return &Reconciler{
		hubClient:          hubClient,
		memberClient:       memberClient,
		workAPIReconcilers: workAPIReconcilers,
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
		updateMemberAgentHeartBeat(&imc)
		updateHealthErr := r.updateHealth(ctx, &imc)
		r.markInternalMemberClusterJoined(&imc)
		if err := r.updateInternalMemberClusterWithRetry(ctx, &imc); err != nil {
			klog.ErrorS(err, "failed to update status for %s", klog.KObj(&imc))
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		r.startWorkAPIControllers()
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

	imc.Status.ResourceUsage.Capacity = corev1.ResourceList{
		corev1.ResourceCPU:    capacityCPU,
		corev1.ResourceMemory: capacityMemory,
	}
	imc.Status.ResourceUsage.Allocatable = corev1.ResourceList{
		corev1.ResourceCPU:    allocatableCPU,
		corev1.ResourceMemory: allocatableMemory,
	}
	imc.Status.ResourceUsage.ObservationTime = metav1.Now()

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

func (r *Reconciler) startWorkAPIControllers() {
	for _, reconciler := range r.workAPIReconcilers {
		reconciler.Join()
	}
}

// updateMemberAgentHeartBeat is used to update member agent heart beat for Internal member cluster.
func updateMemberAgentHeartBeat(imc *fleetv1alpha1.InternalMemberCluster) {
	klog.V(5).InfoS("update Internal member cluster heartbeat", "InternalMemberCluster", klog.KObj(imc))
	desiredAgentStatus := imc.GetAgentStatus(fleetv1alpha1.MemberAgent)
	if desiredAgentStatus != nil {
		desiredAgentStatus.LastReceivedHeartbeat = metav1.Now()
	}
}

func (r *Reconciler) markInternalMemberClusterHealthy(imc apis.ConditionedAgentObj) {
	klog.V(5).InfoS("markInternalMemberClusterHealthy", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               string(fleetv1alpha1.AgentHealthy),
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterHealthy,
		ObservedGeneration: imc.GetGeneration(),
	}

	// Healthy status changed.
	existingCondition := imc.GetConditionWithType(fleetv1alpha1.MemberAgent, newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		klog.V(2).InfoS("healthy", "InternalMemberCluster", klog.KObj(imc))
		r.recorder.Event(imc, corev1.EventTypeNormal, eventReasonInternalMemberClusterHealthy, "internal member cluster healthy")
	}

	imc.SetConditionsWithType(fleetv1alpha1.MemberAgent, newCondition)
}

func (r *Reconciler) markInternalMemberClusterUnhealthy(imc apis.ConditionedAgentObj, err error) {
	klog.V(5).InfoS("markInternalMemberClusterUnhealthy", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               string(fleetv1alpha1.AgentHealthy),
		Status:             metav1.ConditionFalse,
		Reason:             eventReasonInternalMemberClusterUnhealthy,
		Message:            err.Error(),
		ObservedGeneration: imc.GetGeneration(),
	}

	// Healthy status changed.
	existingCondition := imc.GetConditionWithType(fleetv1alpha1.MemberAgent, newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		klog.V(2).InfoS("unhealthy", "InternalMemberCluster", klog.KObj(imc))
		r.recorder.Event(imc, corev1.EventTypeWarning, eventReasonInternalMemberClusterUnhealthy, "internal member cluster unhealthy")
	}

	imc.SetConditionsWithType(fleetv1alpha1.MemberAgent, newCondition)
}

func (r *Reconciler) markInternalMemberClusterJoined(imc apis.ConditionedAgentObj) {
	klog.V(5).InfoS("markInternalMemberClusterJoined", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               string(fleetv1alpha1.AgentJoined),
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterJoined,
		ObservedGeneration: imc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := imc.GetConditionWithType(fleetv1alpha1.MemberAgent, newCondition.Type)
	if existingCondition == nil || existingCondition.ObservedGeneration != imc.GetGeneration() || existingCondition.Status != newCondition.Status {
		r.recorder.Event(imc, corev1.EventTypeNormal, eventReasonInternalMemberClusterJoined, "internal member cluster joined")
		klog.V(2).InfoS("joined", "InternalMemberCluster", klog.KObj(imc))
		metrics.ReportJoinResultMetric()
	}

	imc.SetConditionsWithType(fleetv1alpha1.MemberAgent, newCondition)
}

func (r *Reconciler) markInternalMemberClusterLeft(imc apis.ConditionedAgentObj) {
	klog.V(5).InfoS("markInternalMemberClusterLeft", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               string(fleetv1alpha1.AgentJoined),
		Status:             metav1.ConditionFalse,
		Reason:             eventReasonInternalMemberClusterLeft,
		ObservedGeneration: imc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := imc.GetConditionWithType(fleetv1alpha1.MemberAgent, newCondition.Type)
	if existingCondition == nil || existingCondition.ObservedGeneration != imc.GetGeneration() || existingCondition.Status != newCondition.Status {
		r.recorder.Event(imc, corev1.EventTypeNormal, eventReasonInternalMemberClusterLeft, "internal member cluster left")
		klog.V(2).InfoS("left", "InternalMemberCluster", klog.KObj(imc))
		metrics.ReportLeaveResultMetric()
	}

	imc.SetConditionsWithType(fleetv1alpha1.MemberAgent, newCondition)
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
