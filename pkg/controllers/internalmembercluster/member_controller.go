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
	var memberCluster fleetv1alpha1.InternalMemberCluster
	if err := r.hubClient.Get(ctx, req.NamespacedName, &memberCluster); err != nil {
		return ctrl.Result{}, errors.Wrap(client.IgnoreNotFound(err), "error getting internal member cluster")
	}

	if memberCluster.Spec.State == fleetv1alpha1.ClusterStateJoin {
		return r.updateHeartbeat(ctx, &memberCluster)
	}

	// Cluster state is Leave.
	return r.leave(ctx, &memberCluster)
}

//updateHeartbeat repeatedly performs two below operation. This informs the hub cluster that member cluster is healthy.
//Join flow on internal member cluster controller finishes when the first heartbeat completes.
//1. Gets current cluster usage.
//2. Updates the associated InternalMemberCluster Custom Resource with current cluster usage and marks it as Joined.
func (r *Reconciler) updateHeartbeat(ctx context.Context, memberCluster *fleetv1alpha1.InternalMemberCluster) (ctrl.Result, error) {
	imcLastJoinCond := memberCluster.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
	imcHaveJoined := imcLastJoinCond != nil && imcLastJoinCond.Status == metav1.ConditionTrue

	collectErr := r.collectMemberClusterUsage(ctx, memberCluster)
	if collectErr != nil {
		klog.V(2).ErrorS(collectErr, "failed to collect member cluster usage", "name", memberCluster.Name, "namespace", memberCluster.Namespace)
		r.markInternalMemberClusterUnhealthy(memberCluster, collectErr)
	} else {
		r.markInternalMemberClusterHealthy(memberCluster)
	}

	if updateErr := r.updateInternalMemberClusterWithRetry(ctx, memberCluster); updateErr != nil {
		return ctrl.Result{},
			errors.Wrap(updateErr, "error update heartbeat")
	}

	if !imcHaveJoined {
		metrics.ReportJoinResultMetric()
	}
	return ctrl.Result{RequeueAfter: time.Second * time.Duration(memberCluster.Spec.HeartbeatPeriodSeconds)}, nil
}

func (r *Reconciler) leave(ctx context.Context, memberCluster *fleetv1alpha1.InternalMemberCluster) (ctrl.Result, error) {
	imcLastJoinCond := memberCluster.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
	imcHaveLeft := imcLastJoinCond != nil && imcLastJoinCond.Status == metav1.ConditionFalse

	if imcHaveLeft {
		return ctrl.Result{}, nil
	}

	r.markInternalMemberClusterLeft(memberCluster)
	if err := r.updateInternalMemberClusterWithRetry(ctx, memberCluster); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "internal member cluster leave error")
	}

	metrics.ReportLeaveResultMetric()
	return ctrl.Result{}, nil
}

func (r *Reconciler) updateInternalMemberClusterWithRetry(ctx context.Context, internalMemberCluster *fleetv1alpha1.InternalMemberCluster) error {
	backOffPeriod := retry.DefaultBackoff
	backOffPeriod.Cap = time.Second * time.Duration(internalMemberCluster.Spec.HeartbeatPeriodSeconds)

	return retry.OnError(backOffPeriod,
		func(err error) bool {
			if apierrors.IsNotFound(err) || apierrors.IsInvalid(err) {
				return false
			}
			return true
		},
		func() error {
			err := r.hubClient.Status().Update(ctx, internalMemberCluster)
			if err != nil {
				klog.V(3).ErrorS(err, "error updating internal member cluster status on hub cluster")
			}
			return err
		})
}

func (r *Reconciler) collectMemberClusterUsage(ctx context.Context, mc *fleetv1alpha1.InternalMemberCluster) error {
	var nodes corev1.NodeList
	if err := r.memberClient.List(ctx, &nodes); err != nil {
		klog.V(3).ErrorS(err, "failed to get member cluster node list", "name", mc.Name, "namespace", mc.Namespace)
		return err
	}

	var capacityCPU, capacityMemory, allocatableCPU, allocatableMemory resource.Quantity

	for _, node := range nodes.Items {
		capacityCPU.Add(*(node.Status.Capacity.Cpu()))
		capacityMemory.Add(*(node.Status.Capacity.Memory()))
		allocatableCPU.Add(*(node.Status.Allocatable.Cpu()))
		allocatableMemory.Add(*(node.Status.Allocatable.Memory()))
	}

	mc.Status.Capacity = corev1.ResourceList{
		corev1.ResourceCPU:    capacityCPU,
		corev1.ResourceMemory: capacityMemory,
	}
	mc.Status.Allocatable = corev1.ResourceList{
		corev1.ResourceCPU:    allocatableCPU,
		corev1.ResourceMemory: allocatableMemory,
	}

	r.markInternalMemberClusterHeartbeatReceived(mc)
	r.markInternalMemberClusterJoined(mc)
	return nil
}

func (r *Reconciler) markInternalMemberClusterHeartbeatReceived(internalMemberCluster apis.ConditionedObj) {
	klog.V(3).InfoS("mark internal member cluster heartbeat received",
		"namespace", internalMemberCluster.GetNamespace(), "internalMemberCluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterHBReceived, "internal member cluster heartbeat received")
	hearbeatReceivedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat,
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterHBReceived,
		ObservedGeneration: internalMemberCluster.GetGeneration(),
	}
	internalMemberCluster.SetConditions(hearbeatReceivedCondition, utils.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterHealthy(internalMemberCluster apis.ConditionedObj) {
	klog.V(3).InfoS("mark internal member cluster healthy",
		"namespace", internalMemberCluster.GetNamespace(), "internalMemberCluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterHealthy, "internal member cluster healthy")
	internalMemberClusterHealthyCond := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHealth,
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterHealthy,
		ObservedGeneration: internalMemberCluster.GetGeneration(),
	}
	internalMemberCluster.SetConditions(internalMemberClusterHealthyCond, utils.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterUnhealthy(internalMemberCluster apis.ConditionedObj, err error) {
	klog.V(3).InfoS("mark internal member cluster unhealthy",
		"namespace", internalMemberCluster.GetNamespace(), "internalMemberCluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, corev1.EventTypeWarning, eventReasonInternalMemberClusterUnhealthy, "internal member cluster unhealthy")
	internalMemberClusterUnhealthyCond := metav1.Condition{
		Type:    fleetv1alpha1.ConditionTypeInternalMemberClusterHealth,
		Status:  metav1.ConditionFalse,
		Reason:  eventReasonInternalMemberClusterUnhealthy,
		Message: err.Error(),
	}
	internalMemberCluster.SetConditions(internalMemberClusterUnhealthyCond, utils.ReconcileErrorCondition(err))
}

func (r *Reconciler) markInternalMemberClusterJoined(internalMemberCluster apis.ConditionedObj) {
	klog.V(3).InfoS("mark internal member cluster as joined",
		"namespace", internalMemberCluster.GetNamespace(), "internal member cluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterJoined, "internal member cluster has joined")
	joinSucceedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterJoined,
		ObservedGeneration: internalMemberCluster.GetGeneration(),
	}
	internalMemberCluster.SetConditions(joinSucceedCondition, utils.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterLeft(internalMemberCluster apis.ConditionedObj) {
	klog.V(3).InfoS("mark internal member cluster as left",
		"namespace", internalMemberCluster.GetNamespace(), "internal member cluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterLeft, "internal member cluster has left")
	joinSucceedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             metav1.ConditionFalse,
		Reason:             eventReasonInternalMemberClusterLeft,
		ObservedGeneration: internalMemberCluster.GetGeneration(),
	}
	internalMemberCluster.SetConditions(joinSucceedCondition, utils.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterUnknown(internalMemberCluster apis.ConditionedObj) {
	klog.V(3).InfoS("mark internal member cluster join state unknown",
		"namespace", internalMemberCluster.GetNamespace(), "internal member cluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterUnknown, "internal member cluster join state unknown")
	joinUnknownCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             metav1.ConditionUnknown,
		Reason:             eventReasonInternalMemberClusterUnknown,
		ObservedGeneration: internalMemberCluster.GetGeneration(),
	}
	internalMemberCluster.SetConditions(joinUnknownCondition, utils.ReconcileSuccessCondition())
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("InternalMemberClusterController")
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.InternalMemberCluster{}).
		Complete(r)
}
