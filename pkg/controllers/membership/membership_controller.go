/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membership

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.goms.io/fleet/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

// Reconcile event reasons.
const (
	eventReasonMembershipJoined  = "MembershipJoined"
	eventReasonMembershipUnknown = "MembershipUnknown"
	eventReasonMembershipLeft    = "MembershipLeft"
)

//TODO: Will be fixed in #103
//var (
//	joinSucceedCounter = promauto.NewCounter(prometheus.CounterOpts{
//		Name: "member_agent_join_succeed_cnt",
//		Help: "counts the number of successful Join operations for hub agent",
//	})
//	joinFailCounter = promauto.NewCounter(prometheus.CounterOpts{
//		Name: "member_agent_join_fail_cnt",
//		Help: "counts the number of failed Join operations for hub agent",
//	})
//	leaveSucceedCounter = promauto.NewCounter(prometheus.CounterOpts{
//		Name: "member_agent_join_succeed_cnt",
//		Help: "counts the number of successful Leave operations for hub agent",
//	})
//	leaveFailCounter = promauto.NewCounter(prometheus.CounterOpts{
//		Name: "member_agent_join_fail_cnt",
//		Help: "counts the number of failed Leave operations for hub agent",
//	})
//)

// Reconciler reconciles a Membership object
type Reconciler struct {
	client.Client
	recorder                   record.EventRecorder
	internalMemberClusterChan  <-chan fleetv1alpha1.ClusterState
	membershipChan             chan<- fleetv1alpha1.ClusterState
	internalMemberClusterState fleetv1alpha1.ClusterState
	clusterStateLock           sync.RWMutex
}

// NewReconciler creates a new Reconciler for membership
func NewReconciler(hubClient client.Client, internalMemberClusterChan <-chan fleetv1alpha1.ClusterState,
	membershipChan chan<- fleetv1alpha1.ClusterState) *Reconciler {
	return &Reconciler{
		Client:                    hubClient,
		internalMemberClusterChan: internalMemberClusterChan,
		membershipChan:            membershipChan,
	}
}

//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberships,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberships/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberships/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile reconciles membership Custom Resource on member cluster.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var clusterMembership fleetv1alpha1.Membership

	if err := r.Client.Get(ctx, req.NamespacedName, &clusterMembership); err != nil {
		if !apierr.IsNotFound(err) {
			return ctrl.Result{}, errors.Wrap(err, "error getting membership CR")
		}
	}

	if clusterMembership.Spec.State == fleetv1alpha1.ClusterStateJoin {
		return r.join(ctx, &clusterMembership)
	}

	// This is when the state is leave.
	return r.leave(ctx, &clusterMembership)
}

func (r *Reconciler) join(ctx context.Context, clusterMembership *fleetv1alpha1.Membership) (ctrl.Result, error) {
	r.membershipChan <- fleetv1alpha1.ClusterStateJoin
	internalMemberClusterState := r.getInternalMemberClusterState()
	if internalMemberClusterState == fleetv1alpha1.ClusterStateJoin {
		r.markMembershipJoined(clusterMembership)
		err := r.Client.Status().Update(ctx, clusterMembership)
		//if err != nil {
		//	joinSucceedCounter.Add(1)
		//} else {
		//	joinFailCounter.Add(1)
		//}
		if err == nil {
			klog.InfoS("reporting leave", "cnt", 1)
			metrics.ReportJoinResultMetric()
		}
		return ctrl.Result{}, errors.Wrap(err, "error marking membership as joined")
	}
	// the state can be leave or unknown.
	r.markMembershipUnknown(clusterMembership)
	err := r.Client.Status().Update(ctx, clusterMembership)
	return ctrl.Result{RequeueAfter: time.Minute}, errors.Wrap(err, "error marking membership as unknown")
}

func (r *Reconciler) leave(ctx context.Context, clusterMembership *fleetv1alpha1.Membership) (ctrl.Result, error) {
	r.membershipChan <- fleetv1alpha1.ClusterStateLeave
	internalMemberClusterState := r.getInternalMemberClusterState()
	if internalMemberClusterState == fleetv1alpha1.ClusterStateLeave {
		r.markMembershipLeft(clusterMembership)
		err := r.Client.Status().Update(ctx, clusterMembership)
		//if err != nil {
		//	leaveSucceedCounter.Add(1)
		//} else {
		//	leaveFailCounter.Add(1)
		//}
		if err == nil {
			klog.InfoS("reporting leave", "cnt", 1)
			metrics.ReportLeaveResultMetric()
		}
		return ctrl.Result{}, errors.Wrap(err, "error marking membership as left")
	}
	// internalMemberClusterState state can be joined or unknown.
	r.markMembershipUnknown(clusterMembership)
	// TODO: use the same retry pattern as internal member cluster
	err := r.Client.Status().Update(ctx, clusterMembership)
	return ctrl.Result{RequeueAfter: time.Minute}, errors.Wrap(err, "error marking membership as unknown")
}

func (r *Reconciler) getInternalMemberClusterState() fleetv1alpha1.ClusterState {
	r.clusterStateLock.RLock()
	defer r.clusterStateLock.RUnlock()
	return r.internalMemberClusterState
}

func (r *Reconciler) watchInternalMemberClusterChan() {
	for internalMemberClusterState := range r.internalMemberClusterChan {
		r.clusterStateLock.Lock()
		if r.internalMemberClusterState != internalMemberClusterState {
			klog.InfoS("internal memberCluster state has changed", "internalMemberCluster", internalMemberClusterState)
			r.internalMemberClusterState = internalMemberClusterState
		}
		r.clusterStateLock.Unlock()
	}
}

func (r *Reconciler) markMembershipJoined(membership apis.ConditionedObj) {
	klog.InfoS("mark membership joined",
		"namespace", membership.GetNamespace(), "membership", membership.GetName())
	r.recorder.Event(membership, corev1.EventTypeNormal, eventReasonMembershipJoined, "membership joined")
	joinedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMembershipJoin,
		Status:             metav1.ConditionTrue,
		Reason:             eventReasonMembershipJoined,
		ObservedGeneration: membership.GetGeneration(),
	}
	membership.SetConditions(joinedCondition, utils.ReconcileSuccessCondition())
}

// TODO (mng) we will have a systematic way to define logging level. See #33 for context
func (r *Reconciler) markMembershipUnknown(membership apis.ConditionedObj) {
	klog.V(5).InfoS("mark membership unknown",
		"namespace", membership.GetNamespace(), "membership", membership.GetName())
	r.recorder.Event(membership, corev1.EventTypeNormal, eventReasonMembershipUnknown, "membership unknown")

	unknownCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMembershipJoin,
		Status:             metav1.ConditionUnknown,
		Reason:             eventReasonMembershipUnknown,
		ObservedGeneration: membership.GetGeneration(),
	}

	membership.SetConditions(unknownCondition, utils.ReconcileSuccessCondition())
}

func (r *Reconciler) markMembershipLeft(membership apis.ConditionedObj) {
	klog.InfoS("mark membership left",
		"namespace", membership.GetNamespace(), "membership", membership.GetName())
	r.recorder.Event(membership, corev1.EventTypeNormal, eventReasonMembershipLeft, "membership left")
	joinedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMembershipJoin,
		Status:             metav1.ConditionFalse,
		Reason:             eventReasonMembershipLeft,
		ObservedGeneration: membership.GetGeneration(),
	}
	membership.SetConditions(joinedCondition, utils.ReconcileSuccessCondition())
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("membership")
	go r.watchInternalMemberClusterChan()
	//metrics.Registry.MustRegister(joinSucceedCounter, joinFailCounter, leaveSucceedCounter, leaveFailCounter)
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.Membership{}).
		Complete(r)
}
