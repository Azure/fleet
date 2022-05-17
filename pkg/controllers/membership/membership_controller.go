/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membership

import (
	"context"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/controllers/common"

	"github.com/pkg/errors"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
)

// Reconciler reconciles a Membership object
type Reconciler struct {
	client.Client
	Scheme                    *runtime.Scheme
	recorder                  record.EventRecorder
	InternalMemberClusterChan <-chan fleetv1alpha1.ClusterState
	MembershipChan            chan<- fleetv1alpha1.ClusterState
}

// Reconcile event reasons.
const (
	reasonMembershipJoined      = "MembershipJoined"
	reasonMembershipJoinUnknown = "MembershipJoinUnknown"
)

var internalMemberClusterStateThreadSafe struct {
	mu    sync.Mutex
	state fleetv1alpha1.ClusterState
}

// checkStateJoining checks if the state of the membership controller is Joining.
// This indicates that the join flow has finished on the InternalMemberCluster side.
func checkStateJoining() bool {
	internalMemberClusterStateThreadSafe.mu.Lock()
	defer internalMemberClusterStateThreadSafe.mu.Unlock()
	return internalMemberClusterStateThreadSafe.state == fleetv1alpha1.ClusterStateJoin
}

func markMembershipJoinSucceed(recorder record.EventRecorder, membership apis.ConditionedObj) {
	klog.InfoS("mark membership joined",
		"namespace", membership.GetNamespace(), "membership", membership.GetName())
	recorder.Event(membership, corev1.EventTypeNormal, reasonMembershipJoined, "mark membership joined")
	joinedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMembershipJoin,
		Status:             metav1.ConditionTrue,
		Reason:             reasonMembershipJoined,
		ObservedGeneration: membership.GetGeneration(),
	}
	membership.SetConditions(joinedCondition, common.ReconcileSuccessCondition())
}

func markMembershipJoinUnknown(recorder record.EventRecorder, membership apis.ConditionedObj, err error) {
	klog.InfoS("mark membership join unknown",
		"namespace", membership.GetNamespace(), "membership", membership.GetName())
	recorder.Event(membership, corev1.EventTypeNormal, reasonMembershipJoinUnknown, "mark membership join unknown")

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

func watchInternalMemberClusterChan(imcState <-chan fleetv1alpha1.ClusterState) {
	for range imcState {
		internalMemberClusterStateSignal, more := <-imcState
		if !more {
			return
		}
		klog.InfoS("internal member cluster state",
			"internalMemberCluster", internalMemberClusterStateSignal)

		internalMemberClusterStateThreadSafe.mu.Lock()
		internalMemberClusterStateThreadSafe.state = internalMemberClusterStateSignal
		internalMemberClusterStateThreadSafe.mu.Unlock()
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
		if !errors2.IsNotFound(err) {
			return ctrl.Result{}, errors.Wrap(err, "error getting membership")
		}
	}

	if clusterMembership.Spec.MemberClusterName == clusterMembership.Name {
		if clusterMembership.Spec.State == fleetv1alpha1.ClusterStateJoin {
			markMembershipJoinUnknown(r.recorder, &clusterMembership, nil)
			r.MembershipChan <- fleetv1alpha1.ClusterStateJoin

			if err := r.Client.Update(ctx, &clusterMembership); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "error marking membership as joined")
			}

			if checkStateJoining() {
				markMembershipJoinSucceed(r.recorder, &clusterMembership)
				if err := r.Client.Update(ctx, &clusterMembership); err != nil {
					return ctrl.Result{}, errors.Wrap(err, "error marking membership as joined")
				}
			}

			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("membership")
	go watchInternalMemberClusterChan(r.InternalMemberClusterChan)
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.Membership{}).
		Complete(r)
}
