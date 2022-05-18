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
	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/controllers/common"
)

// Reconcile event reasons.
const (
	reasonMembershipJoined      = "MembershipJoined"
	reasonMembershipJoinUnknown = "MembershipJoinUnknown"
)

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

// Reconcile reconciles membership Custom Resource on member cluster.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var clusterMembership fleetv1alpha1.Membership

	if err := r.Client.Get(ctx, req.NamespacedName, &clusterMembership); err != nil {
		if !apierr.IsNotFound(err) {
			return ctrl.Result{}, errors.Wrap(err, "error getting membership CR")
		}
	}

	if clusterMembership.Spec.State == fleetv1alpha1.ClusterStateJoin {
		r.membershipChan <- fleetv1alpha1.ClusterStateJoin
		internalMemberClusterState := r.getInternalMemberClusterState()
		if internalMemberClusterState == fleetv1alpha1.ClusterStateJoin {
			r.markMembershipJoinSucceed(&clusterMembership)
			err := r.Client.Update(ctx, &clusterMembership)
			return ctrl.Result{}, errors.Wrap(err, "error marking membership as joined")
		}
		// the state can be leave or unknown
		r.markMembershipJoinUnknown(&clusterMembership, nil)
		err := r.Client.Update(ctx, &clusterMembership)
		return ctrl.Result{RequeueAfter: time.Minute}, errors.Wrap(err, "error marking membership as unknown")
	}
	// This is when the state is leave
	r.membershipChan <- fleetv1alpha1.ClusterStateLeave
	return ctrl.Result{}, nil
}

func (r *Reconciler) getInternalMemberClusterState() fleetv1alpha1.ClusterState {
	r.clusterStateLock.RLock()
	defer r.clusterStateLock.RUnlock()
	return r.internalMemberClusterState
}

func (r *Reconciler) markMembershipJoinSucceed(membership apis.ConditionedObj) {
	klog.InfoS("mark membership joined",
		"namespace", membership.GetNamespace(), "membership", membership.GetName())
	r.recorder.Event(membership, corev1.EventTypeNormal, reasonMembershipJoined, "membership joined")
	joinedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMembershipJoin,
		Status:             metav1.ConditionTrue,
		Reason:             reasonMembershipJoined,
		ObservedGeneration: membership.GetGeneration(),
	}
	membership.SetConditions(joinedCondition, common.ReconcileSuccessCondition())
}

// TODO: separate out the error case from the real "joining" condition
func (r *Reconciler) markMembershipJoinUnknown(membership apis.ConditionedObj, err error) {
	klog.V(5).InfoS("mark membership join unknown",
		"namespace", membership.GetNamespace(), "membership", membership.GetName())
	r.recorder.Event(membership, corev1.EventTypeNormal, reasonMembershipJoinUnknown, "membership join unknown")

	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}

	joinUnknownCondition := metav1.Condition{
		Type:    fleetv1alpha1.ConditionTypeMembershipJoin,
		Status:  metav1.ConditionUnknown,
		Reason:  reasonMembershipJoinUnknown,
		Message: errMsg,
	}
	membership.SetConditions(joinUnknownCondition, common.ReconcileErrorCondition(err))
}

func (r *Reconciler) watchInternalMemberClusterChan() {
	for internalMemberClusterState := range r.internalMemberClusterChan {
		klog.InfoS("internal memberCluster state has changed", "internalMemberCluster", internalMemberClusterState)
		r.clusterStateLock.Lock()
		r.internalMemberClusterState = internalMemberClusterState
		r.clusterStateLock.Unlock()
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("membership")
	go r.watchInternalMemberClusterChan()
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.Membership{}).
		Complete(r)
}
