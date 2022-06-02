/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package memberinternalmembercluster

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"go.goms.io/fleet/apis"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/controllers/common"
)

// TODO (mng): move `util` pkg to `controller/util`

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
	errInternalMemberClusterJoin = "error joining internal member cluster"
)

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

func checkHeartbeatReceived(nodes corev1.NodeList, clusterHeartbeat time.Duration) bool {
	for _, node := range nodes.Items {
		// check if node is master node
		if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
			if len(node.Status.Conditions) > 0 &&
				node.Status.Conditions[0].LastHeartbeatTime.Add(clusterHeartbeat).After(time.Now()) {
				return true
			}
		}
	}
	return false
}

func (r *Reconciler) updateInternalMemberClusterWithRetry(ctx context.Context, internalMemberCluster fleetv1alpha1.InternalMemberCluster) error {
	backOffPeriod := wait.Backoff{
		Duration: internalMemberCluster.GetHeartbeatAsDurations(),
	}

	return retry.OnError(backOffPeriod,
		func(err error) bool {
			return true
		},
		func() error {
			err := r.hubClient.Update(ctx, &internalMemberCluster)
			if err != nil {
				klog.Error(err.Error())
				klog.InfoS("retry update internal member cluster on hub cluster")
			}
			return err
		})
}

//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/finalizers,verbs=update

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	var memberCluster fleetv1alpha1.InternalMemberCluster
	if err := r.hubClient.Get(ctx, req.NamespacedName, &memberCluster); err != nil {
		return ctrl.Result{}, errors.Wrap(client.IgnoreNotFound(err), "error getting internal member cluster")
	}

	if memberCluster.Spec.State == fleetv1alpha1.ClusterStateJoin {
		return r.join(ctx, memberCluster)
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) getMembershipClusterState() fleetv1alpha1.ClusterState {
	r.membershipStateLock.RLock()
	defer r.membershipStateLock.RUnlock()
	return r.membershipState
}

//join carries two operations:
//1. Gets current cluster usage.
//2. Updates the associated InternalMemberCluster Custom Resource with current cluster usage and marks it as Joined.
func (r *Reconciler) join(ctx context.Context, memberCluster fleetv1alpha1.InternalMemberCluster) (ctrl.Result, error) {
	membershipState := r.getMembershipClusterState()

	if membershipState == fleetv1alpha1.ClusterStateJoin {
		kubeConfig := ctrl.GetConfigOrDie()
		clientSet, err := kubernetes.NewForConfig(kubeConfig)
		if err != nil {
			klog.ErrorS(err, errInternalMemberClusterJoin, "name", memberCluster.Name, "namespace", memberCluster.Namespace)
			return ctrl.Result{}, errors.Wrap(err, errInternalMemberClusterJoin)
		}

		nodes, err := clientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			klog.ErrorS(err, errInternalMemberClusterJoin, "name", memberCluster.Name, "namespace", memberCluster.Namespace)
			return ctrl.Result{}, errors.Wrap(err, errInternalMemberClusterJoin)
		}

		heartbeatReceived := checkHeartbeatReceived(*(nodes), memberCluster.GetHeartbeatAsDurations())
		if heartbeatReceived {
			memberCluster.Status.Capacity.Cpu().Set(0)
			memberCluster.Status.Capacity.Memory().Set(0)
			memberCluster.Status.Allocatable.Cpu().Set(0)
			memberCluster.Status.Allocatable.Memory().Set(0)

			for _, node := range nodes.Items {
				memberCluster.Status.Capacity.Cpu().Add(*(node.Status.Capacity.Cpu()))
				memberCluster.Status.Capacity.Memory().Add(*(node.Status.Capacity.Memory()))
				memberCluster.Status.Allocatable.Cpu().Add(*(node.Status.Allocatable.Cpu()))
				memberCluster.Status.Allocatable.Memory().Add(*(node.Status.Allocatable.Memory()))
			}

			r.markInternalMemberClusterJoined(&memberCluster)
			r.markInternalMemberClusterHeartbeatReceived(&memberCluster)

			if err := r.updateInternalMemberClusterWithRetry(ctx, memberCluster); err != nil {
				klog.ErrorS(err, errInternalMemberClusterJoin, "name", memberCluster.Name, "namespace", memberCluster.Namespace)
				return ctrl.Result{}, errors.Wrap(err, errInternalMemberClusterJoin)
			}
			r.internalMemberClusterChan <- fleetv1alpha1.ClusterStateJoin
			return ctrl.Result{}, nil
		}
	}

	r.markInternalMemberClusterUnknown(&memberCluster)
	r.markInternalMemberClusterHeartbeatUnknown(&memberCluster)
	err := r.updateInternalMemberClusterWithRetry(ctx, memberCluster)
	return ctrl.Result{RequeueAfter: memberCluster.GetHeartbeatAsDurations()}, errors.Wrap(err, "error marking internal member cluster as unknown")
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
	internalMemberCluster.SetConditions(hearbeatReceivedCondition, common.ReconcileSuccessCondition())
}

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
	internalMemberCluster.SetConditions(heartbeatUnknownCondition, common.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterJoined(internalMemberCluster apis.ConditionedObj) {
	klog.InfoS("mark internal member cluster joined",
		"namespace", internalMemberCluster.GetNamespace(), "internal member cluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterJoined, "internal member cluster joined")
	joinSucceedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonInternalMemberClusterJoined,
		ObservedGeneration: internalMemberCluster.GetGeneration(),
	}
	internalMemberCluster.SetConditions(joinSucceedCondition, common.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterLeft(internalMemberCluster apis.ConditionedObj) {
	klog.InfoS("mark internal member cluster left",
		"namespace", internalMemberCluster.GetNamespace(), "internal member cluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterLeft, "internal member cluster left")
	joinSucceedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             metav1.ConditionFalse,
		Reason:             eventReasonInternalMemberClusterLeft,
		ObservedGeneration: internalMemberCluster.GetGeneration(),
	}
	internalMemberCluster.SetConditions(joinSucceedCondition, common.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterUnknown(internalMemberCluster apis.ConditionedObj) {
	klog.InfoS("mark internal member cluster unknown",
		"namespace", internalMemberCluster.GetNamespace(), "internal member cluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterUnknown, "internal member cluster unknown")
	joinUnknownCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             metav1.ConditionUnknown,
		Reason:             eventReasonInternalMemberClusterUnknown,
		ObservedGeneration: internalMemberCluster.GetGeneration(),
	}
	internalMemberCluster.SetConditions(joinUnknownCondition, common.ReconcileSuccessCondition())
}

func (r *Reconciler) watchMembershipChan() {
	for membershipState := range r.membershipChan {
		klog.InfoS("membership state has changed", "membershipState", membershipState)
		r.membershipStateLock.Lock()
		r.membershipState = membershipState
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
