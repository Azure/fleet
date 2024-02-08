/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/controllers/work"
	"go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/utils/condition"
)

// Reconciler reconciles a InternalMemberCluster object in the member cluster.
type Reconciler struct {
	hubClient    client.Client
	memberClient client.Client

	// the join/leave agent maintains the list of controllers in the member cluster
	// so that it can make sure that all the agents on the member cluster have joined/left
	// before updating the internal member cluster CR status
	workController *work.ApplyWorkReconciler

	recorder record.EventRecorder
}

const (
	// EventReasonInternalMemberClusterHealthy is the event type and reason string when the agent is healthy.
	EventReasonInternalMemberClusterHealthy = "InternalMemberClusterHealthy"
	// EventReasonInternalMemberClusterUnhealthy is the event type and reason string when the agent is unhealthy.
	EventReasonInternalMemberClusterUnhealthy = "InternalMemberClusterUnhealthy"
	// EventReasonInternalMemberClusterJoined is the event type and reason string when the agent has joined.
	EventReasonInternalMemberClusterJoined = "InternalMemberClusterJoined"
	// EventReasonInternalMemberClusterFailedToJoin is the event type and reason string when the agent failed to join.
	EventReasonInternalMemberClusterFailedToJoin = "InternalMemberClusterFailedToJoin"
	// EventReasonInternalMemberClusterFailedToLeave is the event type and reason string when the agent failed to leave.
	EventReasonInternalMemberClusterFailedToLeave = "InternalMemberClusterFailedToLeave"
	// EventReasonInternalMemberClusterLeft is the event type and reason string when the agent left.
	EventReasonInternalMemberClusterLeft = "InternalMemberClusterLeft"

	// we add +-5% jitter
	jitterPercent = 10
)

// NewReconciler creates a new reconciler for the internalMemberCluster CR
func NewReconciler(hubClient client.Client, memberClient client.Client, workController *work.ApplyWorkReconciler) *Reconciler {
	return &Reconciler{
		hubClient:      hubClient,
		memberClient:   memberClient,
		workController: workController,
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("InternalMemberCluster reconciliation starts", "internalMemberCluster", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("InternalMemberCluster reconciliation ends", "internalMemberCluster", req.NamespacedName, "latency", latency)
	}()

	var imc clusterv1beta1.InternalMemberCluster
	if err := r.hubClient.Get(ctx, req.NamespacedName, &imc); err != nil {
		klog.ErrorS(err, "failed to get internal member cluster: %s", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	switch imc.Spec.State {
	case clusterv1beta1.ClusterStateJoin:
		if err := r.startAgents(ctx, &imc); err != nil {
			return ctrl.Result{}, err
		}
		updateMemberAgentHeartBeat(&imc)
		updateHealthErr := r.updateHealth(ctx, &imc)
		r.markInternalMemberClusterJoined(&imc)
		if err := r.updateInternalMemberClusterWithRetry(ctx, &imc); err != nil {
			if apierrors.IsConflict(err) {
				klog.V(2).InfoS("failed to update status due to conflicts", "imc", klog.KObj(&imc))
			} else {
				klog.ErrorS(err, "failed to update status", "imc", klog.KObj(&imc))
			}
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		if updateHealthErr != nil {
			klog.ErrorS(updateHealthErr, "failed to update health", "imc", klog.KObj(&imc))
			return ctrl.Result{}, updateHealthErr
		}
		// add jitter to the heart beat to mitigate the herding of multiple agents
		hbinterval := 1000 * imc.Spec.HeartbeatPeriodSeconds
		jitterRange := int64(hbinterval*jitterPercent) / 100
		return ctrl.Result{RequeueAfter: time.Millisecond *
			(time.Duration(hbinterval) + time.Duration(utilrand.Int63nRange(0, jitterRange)-jitterRange/2))}, nil

	case clusterv1beta1.ClusterStateLeave:
		if err := r.stopAgents(ctx, &imc); err != nil {
			return ctrl.Result{}, err
		}
		r.markInternalMemberClusterLeft(&imc)
		if err := r.updateInternalMemberClusterWithRetry(ctx, &imc); err != nil {
			if apierrors.IsConflict(err) {
				klog.V(2).InfoS("failed to update status due to conflicts", "imc", klog.KObj(&imc))
			} else {
				klog.ErrorS(err, "failed to update status", "imc", klog.KObj(&imc))
			}
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		return ctrl.Result{}, nil

	default:
		klog.Errorf("encountered a fatal error. unknown state %v in InternalMemberCluster: %s", imc.Spec.State, req.NamespacedName)
		return ctrl.Result{}, nil
	}
}

// startAgents start all the member agents running on the member cluster
func (r *Reconciler) startAgents(ctx context.Context, imc *clusterv1beta1.InternalMemberCluster) error {
	// TODO: handle all the controllers uniformly if we have more
	if err := r.workController.Join(ctx); err != nil {
		r.markInternalMemberClusterJoinFailed(imc, err)
		// ignore the update error since we will return an error anyway
		_ = r.updateInternalMemberClusterWithRetry(ctx, imc)
		return err
	}
	return nil
}

// stopAgents stops all the member agents running on the member cluster
func (r *Reconciler) stopAgents(ctx context.Context, imc *clusterv1beta1.InternalMemberCluster) error {
	// TODO: handle all the controllers uniformly if we have more
	if err := r.workController.Leave(ctx); err != nil {
		r.markInternalMemberClusterLeaveFailed(imc, err)
		// ignore the update error since we will return an error anyway
		_ = r.updateInternalMemberClusterWithRetry(ctx, imc)
		return err
	}
	return nil
}

// updateHealth collects and updates member cluster resource stats and set ConditionTypeInternalMemberClusterHealth.
func (r *Reconciler) updateHealth(ctx context.Context, imc *clusterv1beta1.InternalMemberCluster) error {
	klog.V(2).InfoS("updateHealth", "InternalMemberCluster", klog.KObj(imc))

	if err := r.updateResourceStats(ctx, imc); err != nil {
		r.markInternalMemberClusterUnhealthy(imc, fmt.Errorf("failed to update resource stats %s: %w", klog.KObj(imc), err))
		return err
	}

	r.markInternalMemberClusterHealthy(imc)
	return nil
}

// updateResourceStats collects and updates resource usage stats of the member cluster.
func (r *Reconciler) updateResourceStats(ctx context.Context, imc *clusterv1beta1.InternalMemberCluster) error {
	klog.V(2).InfoS("updateResourceStats", "InternalMemberCluster", klog.KObj(imc))
	var nodes corev1.NodeList
	if err := r.memberClient.List(ctx, &nodes); err != nil {
		return fmt.Errorf("failed to list nodes for member cluster %s: %w", klog.KObj(imc), err)
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
func (r *Reconciler) updateInternalMemberClusterWithRetry(ctx context.Context, imc *clusterv1beta1.InternalMemberCluster) error {
	klog.V(2).InfoS("updateInternalMemberClusterWithRetry", "InternalMemberCluster", klog.KObj(imc))
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

// updateMemberAgentHeartBeat is used to update member agent heart beat for Internal member cluster.
func updateMemberAgentHeartBeat(imc *clusterv1beta1.InternalMemberCluster) {
	klog.V(2).InfoS("update Internal member cluster heartbeat", "InternalMemberCluster", klog.KObj(imc))
	desiredAgentStatus := imc.GetAgentStatus(clusterv1beta1.MemberAgent)
	if desiredAgentStatus != nil {
		desiredAgentStatus.LastReceivedHeartbeat = metav1.Now()
	}
}

func (r *Reconciler) markInternalMemberClusterHealthy(imc clusterv1beta1.ConditionedAgentObj) {
	klog.V(2).InfoS("markInternalMemberClusterHealthy", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               string(clusterv1beta1.AgentHealthy),
		Status:             metav1.ConditionTrue,
		Reason:             EventReasonInternalMemberClusterHealthy,
		ObservedGeneration: imc.GetGeneration(),
	}

	// Healthy status changed.
	existingCondition := imc.GetConditionWithType(clusterv1beta1.MemberAgent, newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		klog.V(2).InfoS("healthy", "InternalMemberCluster", klog.KObj(imc))
		r.recorder.Event(imc, corev1.EventTypeNormal, EventReasonInternalMemberClusterHealthy, "internal member cluster healthy")
	}

	imc.SetConditionsWithType(clusterv1beta1.MemberAgent, newCondition)
}

func (r *Reconciler) markInternalMemberClusterUnhealthy(imc clusterv1beta1.ConditionedAgentObj, err error) {
	klog.V(2).InfoS("markInternalMemberClusterUnhealthy", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               string(clusterv1beta1.AgentHealthy),
		Status:             metav1.ConditionFalse,
		Reason:             EventReasonInternalMemberClusterUnhealthy,
		Message:            err.Error(),
		ObservedGeneration: imc.GetGeneration(),
	}

	// Healthy status changed.
	existingCondition := imc.GetConditionWithType(clusterv1beta1.MemberAgent, newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		klog.V(2).InfoS("unhealthy", "InternalMemberCluster", klog.KObj(imc))
		r.recorder.Event(imc, corev1.EventTypeWarning, EventReasonInternalMemberClusterUnhealthy, "internal member cluster unhealthy")
	}

	imc.SetConditionsWithType(clusterv1beta1.MemberAgent, newCondition)
}

func (r *Reconciler) markInternalMemberClusterJoined(imc clusterv1beta1.ConditionedAgentObj) {
	klog.V(2).InfoS("markInternalMemberClusterJoined", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               string(clusterv1beta1.AgentJoined),
		Status:             metav1.ConditionTrue,
		Reason:             EventReasonInternalMemberClusterJoined,
		ObservedGeneration: imc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := imc.GetConditionWithType(clusterv1beta1.MemberAgent, newCondition.Type)
	if existingCondition == nil || existingCondition.ObservedGeneration != imc.GetGeneration() || existingCondition.Status != newCondition.Status {
		r.recorder.Event(imc, corev1.EventTypeNormal, EventReasonInternalMemberClusterJoined, "internal member cluster joined")
		klog.V(2).InfoS("joined", "InternalMemberCluster", klog.KObj(imc))
		metrics.ReportJoinResultMetric()
	}

	imc.SetConditionsWithType(clusterv1beta1.MemberAgent, newCondition)
}

func (r *Reconciler) markInternalMemberClusterJoinFailed(imc clusterv1beta1.ConditionedAgentObj, err error) {
	klog.V(2).InfoS("markInternalMemberCluster join failed", "error", err, "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               string(clusterv1beta1.AgentJoined),
		Status:             metav1.ConditionUnknown,
		Reason:             EventReasonInternalMemberClusterFailedToJoin,
		Message:            err.Error(),
		ObservedGeneration: imc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := imc.GetConditionWithType(clusterv1beta1.MemberAgent, newCondition.Type)
	if existingCondition == nil || existingCondition.ObservedGeneration != imc.GetGeneration() || existingCondition.Status != newCondition.Status {
		r.recorder.Event(imc, corev1.EventTypeNormal, EventReasonInternalMemberClusterFailedToJoin, "internal member cluster failed to join")
		klog.ErrorS(err, "agent join failed", "InternalMemberCluster", klog.KObj(imc))
	}

	imc.SetConditionsWithType(clusterv1beta1.MemberAgent, newCondition)
}

func (r *Reconciler) markInternalMemberClusterLeft(imc clusterv1beta1.ConditionedAgentObj) {
	klog.V(2).InfoS("markInternalMemberClusterLeft", "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               string(clusterv1beta1.AgentJoined),
		Status:             metav1.ConditionFalse,
		Reason:             EventReasonInternalMemberClusterLeft,
		ObservedGeneration: imc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := imc.GetConditionWithType(clusterv1beta1.MemberAgent, newCondition.Type)
	if existingCondition == nil || existingCondition.ObservedGeneration != imc.GetGeneration() || existingCondition.Status != newCondition.Status {
		r.recorder.Event(imc, corev1.EventTypeNormal, EventReasonInternalMemberClusterLeft, "internal member cluster left")
		klog.V(2).InfoS("left", "InternalMemberCluster", klog.KObj(imc))
		metrics.ReportLeaveResultMetric()
	}

	imc.SetConditionsWithType(clusterv1beta1.MemberAgent, newCondition)
}

func (r *Reconciler) markInternalMemberClusterLeaveFailed(imc clusterv1beta1.ConditionedAgentObj, err error) {
	klog.V(2).InfoS("markInternalMemberCluster leave failed", "error", err, "InternalMemberCluster", klog.KObj(imc))
	newCondition := metav1.Condition{
		Type:               string(clusterv1beta1.AgentJoined),
		Status:             metav1.ConditionUnknown,
		Reason:             EventReasonInternalMemberClusterFailedToLeave,
		Message:            err.Error(),
		ObservedGeneration: imc.GetGeneration(),
	}

	// Joined status changed.
	if !condition.IsConditionStatusTrue(imc.GetConditionWithType(clusterv1beta1.MemberAgent, newCondition.Type), imc.GetGeneration()) {
		r.recorder.Event(imc, corev1.EventTypeNormal, EventReasonInternalMemberClusterFailedToLeave, "internal member cluster failed to leave")
		klog.ErrorS(err, "agent leave failed", "InternalMemberCluster", klog.KObj(imc))
	}

	imc.SetConditionsWithType(clusterv1beta1.MemberAgent, newCondition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("v1beta1InternalMemberClusterController")
	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1beta1.InternalMemberCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
