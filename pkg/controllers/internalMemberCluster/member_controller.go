/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalMemberCluster

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

// Reconciler reconciles a InternalMemberCluster object in the member cluster.
type Reconciler struct {
	hubClient                 client.Client
	memberClient              client.Client
	restMapper                meta.RESTMapper
	recorder                  record.EventRecorder
	internalMemberClusterChan chan<- fleetv1alpha1.ClusterState
	membershipChan            <-chan fleetv1alpha1.ClusterState
	membershipState           fleetv1alpha1.ClusterState
	membershipStateLock       sync.RWMutex
}

const (
	eventReasonInternalMemberClusterHBReceived = "InternalMemberClusterHeartbeatReceived"
	eventReasonInternalMemberClusterHBUnknown  = "InternalMemberClusterHeartbeatUnknown"
	eventReasonInternalMemberClusterJoined     = "InternalMemberClusterJoined"
	eventReasonInternalMemberClusterLeft       = "InternalMemberClusterLeft"
	eventReasonInternalMemberClusterUnknown    = "InternalMemberClusterUnknown"
)

// NewReconciler creates a new reconciler for the internal membership CR
func NewReconciler(hubClient client.Client, memberClient client.Client, restMapper meta.RESTMapper,
	internalMemberClusterChan chan<- fleetv1alpha1.ClusterState,
	membershipChan <-chan fleetv1alpha1.ClusterState) *Reconciler {
	return &Reconciler{
		hubClient:                 hubClient,
		memberClient:              memberClient,
		restMapper:                restMapper,
		internalMemberClusterChan: internalMemberClusterChan,
		membershipChan:            membershipChan,
	}
}

//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/finalizers,verbs=update

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var memberCluster *fleetv1alpha1.InternalMemberCluster
	if err := r.hubClient.Get(ctx, req.NamespacedName, memberCluster); err != nil {
		return ctrl.Result{}, errors.Wrap(client.IgnoreNotFound(err), "error getting internal member cluster")
	}

	if memberCluster.Spec.State == fleetv1alpha1.ClusterStateJoin {
		return r.join(ctx, memberCluster)
	}

	//TODO: make sure the state is leave, alert otherwise
	return r.leave(ctx, memberCluster)
}

//join carries two operations:
//1. Gets current cluster usage.
//2. Updates the associated InternalMemberCluster Custom Resource with current cluster usage and marks it as Joined.
func (r *Reconciler) join(ctx context.Context, memberCluster *fleetv1alpha1.InternalMemberCluster) (ctrl.Result, error) {
	membershipState := r.getMembershipClusterState()

	if membershipState != fleetv1alpha1.ClusterStateJoin {
		r.markInternalMemberClusterUnknown(memberCluster)
		err := r.updateInternalMemberClusterWithRetry(ctx, memberCluster)
		return ctrl.Result{RequeueAfter: time.Second * time.Duration(memberCluster.Spec.HeartbeatPeriodSeconds)},
			errors.Wrap(err, "error marking internal member cluster as unknown")
	}

	collectErr := r.collectMemberClusterUsage(ctx, memberCluster)
	if collectErr != nil {
		klog.ErrorS(collectErr, "failed to collect member cluster usage", "name", memberCluster.Name, "namespace", memberCluster.Namespace)
		memberCluster.SetConditions(utils.ReconcileErrorCondition(collectErr))
	}

	updateErr := r.updateInternalMemberClusterWithRetry(ctx, memberCluster)
	if collectErr == nil && updateErr == nil {
		r.internalMemberClusterChan <- fleetv1alpha1.ClusterStateJoin
	}

	return ctrl.Result{RequeueAfter: time.Second * time.Duration(memberCluster.Spec.HeartbeatPeriodSeconds)}, nil
}

// TODO (mng): double-check RequeueAfter field in all returning Ctrl.Result{}
func (r *Reconciler) leave(ctx context.Context, memberCluster *fleetv1alpha1.InternalMemberCluster) (ctrl.Result, error) {
	membershipState := r.getMembershipClusterState()
	if membershipState != fleetv1alpha1.ClusterStateLeave {
		r.markInternalMemberClusterUnknown(memberCluster)
		err := r.updateInternalMemberClusterWithRetry(ctx, memberCluster)
		return ctrl.Result{RequeueAfter: time.Minute},
			errors.Wrap(err, "error marking internal member cluster as unknown")
	}

	r.markInternalMemberClusterLeft(memberCluster)
	err := r.updateInternalMemberClusterWithRetry(ctx, memberCluster)
	if err != nil {
		r.internalMemberClusterChan <- fleetv1alpha1.ClusterStateLeave
	}
	return ctrl.Result{}, errors.Wrap(err, "internal member cluster leave error")
}

func (r *Reconciler) getMembershipClusterState() fleetv1alpha1.ClusterState {
	r.membershipStateLock.RLock()
	defer r.membershipStateLock.RUnlock()
	return r.membershipState
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
				klog.ErrorS(err, "error updating internal member cluster status on hub cluster")
			}
			return err
		})
}

func (r *Reconciler) collectMemberClusterUsage(ctx context.Context, mc *fleetv1alpha1.InternalMemberCluster) error {
	var nodes corev1.NodeList
	if err := r.memberClient.List(ctx, &nodes); err != nil {
		klog.ErrorS(err, "failed to get member cluster node list", "name", mc.Name, "namespace", mc.Namespace)
		return err
	}

	mc.Status.Capacity.Cpu().Set(0)
	mc.Status.Capacity.Memory().Set(0)
	mc.Status.Allocatable.Cpu().Set(0)
	mc.Status.Allocatable.Memory().Set(0)

	for _, node := range nodes.Items {
		mc.Status.Capacity.Cpu().Add(*(node.Status.Capacity.Cpu()))
		mc.Status.Capacity.Memory().Add(*(node.Status.Capacity.Memory()))
		mc.Status.Allocatable.Cpu().Add(*(node.Status.Allocatable.Cpu()))
		mc.Status.Allocatable.Memory().Add(*(node.Status.Allocatable.Memory()))
	}

	r.markInternalMemberClusterHeartbeatReceived(mc)
	r.markInternalMemberClusterJoined(mc)
	return nil
}

func (r *Reconciler) markInternalMemberClusterHeartbeatReceived(internalMemberCluster apis.ConditionedObj) {
	klog.InfoS("mark internal member cluster heartbeat received",
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

//TODO: replace this with a markInternalMemberClusterUnhealthy
func (r *Reconciler) markInternalMemberClusterHeartbeatUnknown(internalMemberCluster apis.ConditionedObj) {
	klog.InfoS("mark internal member cluster heartbeat unknown",
		"namespace", internalMemberCluster.GetNamespace(), "internalMemberCluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterHBUnknown, "internal member cluster heartbeat unknown")
	heartbeatUnknownCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat,
		Status:             metav1.ConditionUnknown,
		Reason:             eventReasonInternalMemberClusterHBUnknown,
		ObservedGeneration: internalMemberCluster.GetGeneration(),
	}
	internalMemberCluster.SetConditions(heartbeatUnknownCondition, utils.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterJoined(internalMemberCluster apis.ConditionedObj) {
	klog.InfoS("mark internal member cluster as joined",
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
	klog.InfoS("mark internal member cluster as left",
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
	klog.InfoS("mark internal member cluster join state unknown",
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

func (r *Reconciler) watchMembershipChan() {
	for membershipState := range r.membershipChan {
		r.membershipStateLock.Lock()
		if r.membershipState != membershipState {
			klog.InfoS("membership state has changed", "membershipState", membershipState)
			r.membershipState = membershipState
		}
		r.membershipStateLock.Unlock()
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("MemberShipController")
	go r.watchMembershipChan()
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.InternalMemberCluster{}).
		Complete(r)
}
