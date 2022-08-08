/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/utils"
)

const (
	eventReasonNamespaceCreated    = "NamespaceCreated"
	eventReasonRoleCreated         = "RoleCreated"
	eventReasonRoleUpdated         = "RoleUpdated"
	eventReasonRoleBindingCreated  = "RoleBindingCreated"
	eventReasonRoleBindingUpdated  = "RoleBindingUpdated"
	eventReasonIMCCreated          = "InternalMemberClusterCreated"
	eventReasonIMCSpecUpdated      = "InternalMemberClusterSpecUpdated"
	reasonMemberClusterReadyToJoin = "MemberClusterReadyToJoin"
	reasonMemberClusterJoined      = "MemberClusterJoined"
	reasonMemberClusterLeft        = "MemberClusterLeft"
)

var (
	numberOfAgents = 1
)

// Reconciler reconciles a MemberCluster object
type Reconciler struct {
	client.Client
	recorder                record.EventRecorder
	NetworkingAgentsEnabled bool // if networking agents are enabled, need to handle unjoin before leave
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.V(3).InfoS("Reconcile", "memberCluster", req.NamespacedName)
	var mc fleetv1alpha1.MemberCluster
	if err := r.Client.Get(ctx, req.NamespacedName, &mc); err != nil {
		klog.ErrorS(err, "failed to get member cluster", "memberCluster", req.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Get current internal member cluster.
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	imcNamespacedName := types.NamespacedName{Namespace: namespaceName, Name: mc.Name}
	var imc fleetv1alpha1.InternalMemberCluster
	currentImc := &imc
	if err := r.Client.Get(ctx, imcNamespacedName, &imc); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "failed to get internal member cluster", "internalMemberCluster", imcNamespacedName)
			return ctrl.Result{}, err
		}
		// Not found.
		currentImc = nil
	}

	switch mc.Spec.State {
	case fleetv1alpha1.ClusterStateJoin:
		if err := r.join(ctx, &mc, currentImc); err != nil {
			klog.ErrorS(err, "failed to join", "memberCluster", klog.KObj(&mc))
			return ctrl.Result{}, err
		}

	case fleetv1alpha1.ClusterStateLeave:
		if err := r.leave(ctx, &mc, currentImc); err != nil {
			klog.ErrorS(err, "failed to leave", "memberCluster", klog.KObj(&mc))
			return ctrl.Result{}, err
		}

	default:
		klog.Errorf("encountered a fatal error. unknown state %v in MemberCluster: %s", mc.Spec.State, klog.KObj(&mc))
		return ctrl.Result{}, nil
	}

	// Copy status from InternalMemberCluster to MemberCluster.
	r.copyInternalMemberClusterStatus(currentImc, &mc)
	if err := r.updateMemberClusterStatus(ctx, &mc); err != nil {
		klog.ErrorS(err, "failed to update status for %s", klog.KObj(&mc))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return ctrl.Result{}, nil
}

// join takes the actions to make hub cluster ready for member cluster to join, including:
// - Create namespace for member cluster
// - Create role & role bindings for member cluster to access hub cluster
// - Create InternalMemberCluster with state=Join for member cluster
// - Set ReadyToJoin to true
//
// Condition ReadyToJoin == true means all the above actions have been done successfully at least once.
// It will never turn false after true.
func (r *Reconciler) join(ctx context.Context, mc *fleetv1alpha1.MemberCluster, imc *fleetv1alpha1.InternalMemberCluster) error {
	klog.V(3).InfoS("join", "memberCluster", klog.KObj(mc))

	namespaceName, err := r.syncNamespace(ctx, mc)
	if err != nil {
		return errors.Wrapf(err, "failed to sync namespace")
	}

	roleName, err := r.syncRole(ctx, mc, namespaceName)
	if err != nil {
		return errors.Wrapf(err, "failed to sync role")
	}

	err = r.syncRoleBinding(ctx, mc, namespaceName, roleName)
	if err != nil {
		return errors.Wrapf(err, "failed to sync role binding")
	}

	if _, err := r.syncInternalMemberCluster(ctx, mc, namespaceName, imc); err != nil {
		return errors.Wrapf(err, "failed to sync internal member cluster spec")
	}

	markMemberClusterReadyToJoin(r.recorder, mc)
	return nil
}

// leave notifies member cluster to leave by setting InternalMemberCluster's state to Leave.
//
// Note that leave doesn't delete any of the resources created by join(). Instead, deleting MemberCluster will delete them.
func (r *Reconciler) leave(ctx context.Context, mc *fleetv1alpha1.MemberCluster, imc *fleetv1alpha1.InternalMemberCluster) error {
	klog.V(3).InfoS("leave", "memberCluster", klog.KObj(mc))
	// Never joined successfully before.
	if imc == nil {
		return nil
	}

	// Copy spec from member cluster to internal member cluster.
	// TODO: mark readToJoin as metav1.ConditionFalse
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	if _, err := r.syncInternalMemberCluster(ctx, mc, namespaceName, imc); err != nil {
		return errors.Wrapf(err, "failed to sync internal member cluster spec")
	}

	return nil
}

// syncNamespace creates or updates the namespace for member cluster.
func (r *Reconciler) syncNamespace(ctx context.Context, mc *fleetv1alpha1.MemberCluster) (string, error) {
	klog.V(5).InfoS("syncNamespace", "memberCluster", klog.KObj(mc))
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	expected := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:            namespaceName,
			OwnerReferences: []metav1.OwnerReference{*toOwnerReference(mc)},
		},
	}

	// Creates namespace if not found.
	var current corev1.Namespace
	if err := r.Client.Get(ctx, types.NamespacedName{Name: namespaceName}, &current); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", errors.Wrapf(err, "failed to get namespace %s", namespaceName)
		}
		klog.V(4).InfoS("creating namespace", "memberCluster", klog.KObj(mc), "namespace", namespaceName)
		// Make sure the entire namespace is removed if the member cluster is deleted.
		if err = r.Client.Create(ctx, &expected, client.FieldOwner(mc.GetUID())); err != nil {
			return "", errors.Wrapf(err, "failed to create namespace %s", namespaceName)
		}
		r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonNamespaceCreated, "Namespace was created")
		klog.V(2).InfoS("created namespace", "memberCluster", klog.KObj(mc), "namespace", namespaceName)
		return namespaceName, nil
	}

	// Update namespace if current != expected.
	// Nothing to update.

	return namespaceName, nil
}

// syncRole creates or updates the role for member cluster to access its namespace in hub cluster.
func (r *Reconciler) syncRole(ctx context.Context, mc *fleetv1alpha1.MemberCluster, namespaceName string) (string, error) {
	klog.V(5).InfoS("syncRole", "memberCluster", klog.KObj(mc))
	// Role name is created using member cluster name.
	roleName := fmt.Sprintf(utils.RoleNameFormat, mc.Name)
	expected := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            roleName,
			Namespace:       namespaceName,
			OwnerReferences: []metav1.OwnerReference{*toOwnerReference(mc)},
		},
		Rules: []rbacv1.PolicyRule{utils.FleetRule, utils.EventRule, utils.FleetNetworkRule, utils.LeaseRule, utils.WorkRule},
	}

	// Creates role if not found.
	var current rbacv1.Role
	if err := r.Client.Get(ctx, types.NamespacedName{Name: roleName, Namespace: namespaceName}, &current); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", errors.Wrapf(err, "failed to get role %s", roleName)
		}
		klog.V(4).InfoS("creating role", "memberCluster", klog.KObj(mc), "role", roleName)
		if err = r.Client.Create(ctx, &expected, client.FieldOwner(mc.GetUID())); err != nil {
			return "", errors.Wrapf(err, "failed to create role %s with rules %+v", roleName, expected.Rules)
		}
		r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonRoleCreated, "role was created")
		klog.V(2).InfoS("created role", "memberCluster", klog.KObj(mc), "role", roleName)
		return roleName, nil
	}

	// Updates role if current != expected.
	if cmp.Equal(current.Rules, expected.Rules) {
		return roleName, nil
	}
	current.Rules = expected.Rules
	klog.V(4).InfoS("updating role", "memberCluster", klog.KObj(mc), "role", roleName)
	if err := r.Client.Update(ctx, &current, client.FieldOwner(mc.GetUID())); err != nil {
		return "", errors.Wrapf(err, "failed to update role %s with rules %+v", roleName, current.Rules)
	}
	r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonRoleUpdated, "role was updated")
	klog.V(2).InfoS("updated role", "memberCluster", klog.KObj(mc), "role", roleName)
	return roleName, nil
}

// syncRoleBinding creates or updates the role binding for member cluster to access its namespace in hub cluster.
func (r *Reconciler) syncRoleBinding(ctx context.Context, mc *fleetv1alpha1.MemberCluster, namespaceName string, roleName string) error {
	klog.V(5).InfoS("syncRoleBinding", "memberCluster", klog.KObj(mc))
	// Role binding name is created using member cluster name
	roleBindingName := fmt.Sprintf(utils.RoleBindingNameFormat, mc.Name)
	expected := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            roleBindingName,
			Namespace:       namespaceName,
			OwnerReferences: []metav1.OwnerReference{*toOwnerReference(mc)},
		},
		Subjects: []rbacv1.Subject{mc.Spec.Identity},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     roleName,
		},
	}

	// Creates role binding if not found.
	var current rbacv1.RoleBinding
	if err := r.Client.Get(ctx, types.NamespacedName{Name: roleBindingName, Namespace: namespaceName}, &current); err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrapf(err, "failed to get role binding %s", roleBindingName)
		}
		klog.V(4).InfoS("creating role binding", "memberCluster", klog.KObj(mc), "roleBinding", roleBindingName)
		if err = r.Client.Create(ctx, &expected, client.FieldOwner(mc.GetUID())); err != nil {
			return errors.Wrapf(err, "failed to create role binding %s", roleBindingName)
		}
		r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonRoleBindingCreated, "role binding was created")
		klog.V(2).InfoS("created role binding", "memberCluster", klog.KObj(mc), "roleBinding", roleBindingName)
		return nil
	}

	// Updates role binding if current != expected.
	if cmp.Equal(current.Subjects, expected.Subjects) && cmp.Equal(current.RoleRef, expected.RoleRef) {
		return nil
	}
	current.Subjects = expected.Subjects
	current.RoleRef = expected.RoleRef
	klog.V(4).InfoS("updating role binding", "memberCluster", klog.KObj(mc), "roleBinding", roleBindingName)
	if err := r.Client.Update(ctx, &expected, client.FieldOwner(mc.GetUID())); err != nil {
		return errors.Wrapf(err, "failed to update role binding %s", roleBindingName)
	}
	r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonRoleBindingUpdated, "role binding was updated")
	klog.V(2).InfoS("updated role binding", "memberCluster", klog.KObj(mc), "roleBinding", roleBindingName)
	return nil
}

// syncInternalMemberCluster is used to sync spec from MemberCluster to InternalMemberCluster.
func (r *Reconciler) syncInternalMemberCluster(ctx context.Context, mc *fleetv1alpha1.MemberCluster, namespaceName string, current *fleetv1alpha1.InternalMemberCluster) (*fleetv1alpha1.InternalMemberCluster, error) {
	klog.V(5).InfoS("syncInternalMemberCluster", "memberCluster", klog.KObj(mc))
	expected := fleetv1alpha1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:            mc.Name,
			Namespace:       namespaceName,
			OwnerReferences: []metav1.OwnerReference{*toOwnerReference(mc)},
		},
		Spec: fleetv1alpha1.InternalMemberClusterSpec{
			State:                  mc.Spec.State,
			HeartbeatPeriodSeconds: mc.Spec.HeartbeatPeriodSeconds,
		},
	}

	// Creates internal member cluster if not found.
	if current == nil {
		klog.V(4).InfoS("creating internal member cluster", "InternalMemberCluster", klog.KObj(&expected), "spec", expected.Spec)
		if err := r.Client.Create(ctx, &expected, client.FieldOwner(mc.GetUID())); err != nil {
			return nil, errors.Wrapf(err, "failed to create internal member cluster %s with spec %+v", klog.KObj(&expected), expected.Spec)
		}
		r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonIMCCreated, "Internal member cluster was created")
		klog.V(2).InfoS("created internal member cluster", "InternalMemberCluster", klog.KObj(&expected), "spec", expected.Spec)
		return &expected, nil
	}

	// Updates internal member cluster if current != expected.
	if cmp.Equal(current.Spec, expected.Spec) {
		return current, nil
	}
	current.Spec = expected.Spec
	klog.V(4).InfoS("updating internal member cluster", "InternalMemberCluster", klog.KObj(current), "spec", current.Spec)
	if err := r.Client.Update(ctx, current, client.FieldOwner(mc.GetUID())); err != nil {
		return nil, errors.Wrapf(err, "failed to update internal member cluster %s with spec %+v", klog.KObj(current), current.Spec)
	}
	r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonIMCSpecUpdated, "internal member cluster spec updated")
	klog.V(2).InfoS("updated internal member cluster", "InternalMemberCluster", klog.KObj(current), "spec", current.Spec)
	return current, nil
}

func toOwnerReference(memberCluster *fleetv1alpha1.MemberCluster) *metav1.OwnerReference {
	return &metav1.OwnerReference{APIVersion: fleetv1alpha1.GroupVersion.String(), Kind: fleetv1alpha1.MemberClusterKind,
		Name: memberCluster.Name, UID: memberCluster.UID, Controller: pointer.Bool(true)}
}

// copyInternalMemberClusterStatus is used to sync status from InternalMemberCluster to MemberCluster.
func (r *Reconciler) copyInternalMemberClusterStatus(imc *fleetv1alpha1.InternalMemberCluster, mc *fleetv1alpha1.MemberCluster) {
	klog.V(5).InfoS("syncInternalMemberClusterStatus", "memberCluster", klog.KObj(mc))
	if imc == nil {
		return
	}

	// Copy Agent status.
	mc.Status.AgentStatus = imc.Status.AgentStatus
	r.aggregateJoinedCondition(mc)
	// TODO: We didn't handle condition type: fleetv1alpha1.ConditionTypeMemberClusterHealth.
	// TODO: We didn't handle condition type: fleetv1alpha1.ConditionTypeMemberClusterHeartbeat as this condition type is not defined at all.

	// Copy resource usages.
	mc.Status.ResourceUsage = imc.Status.ResourceUsage
}

// updateMemberClusterStatus is used to update member cluster status.
func (r *Reconciler) updateMemberClusterStatus(ctx context.Context, mc *fleetv1alpha1.MemberCluster) error {
	klog.V(5).InfoS("updateMemberClusterStatus", "memberCluster", klog.KObj(mc))
	backOffPeriod := retry.DefaultRetry
	backOffPeriod.Cap = time.Second * time.Duration(mc.Spec.HeartbeatPeriodSeconds/2)

	return retry.OnError(backOffPeriod,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err)
		},
		func() error {
			return r.Client.Status().Update(ctx, mc, client.FieldOwner(mc.GetUID()))
		})
}

// aggregateJoinedCondition is used to calculate the joined or left status for member cluster based on conditions from all agents.
func (r *Reconciler) aggregateJoinedCondition(mc *fleetv1alpha1.MemberCluster) {
	klog.V(5).InfoS("syncJoinedCondition", "memberCluster", klog.KObj(mc))
	if len(mc.Status.AgentStatus) < numberOfAgents {
		return
	}
	joined := true
	for _, agentStatus := range mc.Status.AgentStatus {
		conditions := agentStatus.Conditions
		condition := meta.FindStatusCondition(conditions, string(fleetv1alpha1.AgentJoined))
		if condition != nil {
			agentJoin := true
			if condition.Status == metav1.ConditionFalse {
				agentJoin = false
			}
			joined = joined && agentJoin
		}
	}

	if joined {
		markMemberClusterJoined(r.recorder, mc)
	} else {
		markMemberClusterLeft(r.recorder, mc)
	}
}

// markMemberClusterReadyToJoin is used to update the ReadyToJoin condition of member cluster.
func markMemberClusterReadyToJoin(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.V(5).InfoS("markMemberClusterReadyToJoin", "memberCluster", klog.KObj(mc))
	newCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin,
		Status:             metav1.ConditionTrue,
		Reason:             reasonMemberClusterReadyToJoin,
		ObservedGeneration: mc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := mc.GetCondition(newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		recorder.Event(mc, corev1.EventTypeNormal, reasonMemberClusterReadyToJoin, "member cluster ready to join")
		klog.V(2).InfoS("member cluster ready to join", "memberCluster", klog.KObj(mc))
	}

	mc.SetConditions(newCondition)
}

// markMemberClusterJoined is used to the update the status of the member cluster to have the joined condition.
func markMemberClusterJoined(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.V(5).InfoS("markMemberClusterJoined", "memberCluster", klog.KObj(mc))
	newCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMemberClusterJoin,
		Status:             metav1.ConditionTrue,
		Reason:             reasonMemberClusterJoined,
		ObservedGeneration: mc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := mc.GetCondition(newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		recorder.Event(mc, corev1.EventTypeNormal, reasonMemberClusterJoined, "member cluster joined")
		klog.V(2).InfoS("memberCluster joined", "memberCluster", klog.KObj(mc))
		metrics.ReportJoinResultMetric()
	}

	mc.SetConditions(newCondition)
}

// markMemberClusterLeft is used to update the status of the member cluster to have the left condition.
func markMemberClusterLeft(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.V(5).InfoS("markMemberClusterLeft", "memberCluster", klog.KObj(mc))
	newCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMemberClusterJoin,
		Status:             metav1.ConditionFalse,
		Reason:             reasonMemberClusterLeft,
		ObservedGeneration: mc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := mc.GetCondition(newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		recorder.Event(mc, corev1.EventTypeNormal, reasonMemberClusterJoined, "member cluster left")
		klog.V(2).InfoS("memberCluster left", "memberCluster", klog.KObj(mc))
		metrics.ReportJoinResultMetric()
	}

	mc.SetConditions(newCondition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("memberCluster")
	if r.NetworkingAgentsEnabled {
		numberOfAgents = 3
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.MemberCluster{}).
		Owns(&fleetv1alpha1.InternalMemberCluster{}).
		Complete(r)
}
