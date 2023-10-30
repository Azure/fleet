/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/apis"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/utils"
)

const (
	eventReasonNamespaceCreated       = "NamespaceCreated"
	eventReasonNamespacePatched       = "NamespacePatched"
	eventReasonRoleCreated            = "RoleCreated"
	eventReasonRoleUpdated            = "RoleUpdated"
	eventReasonRoleBindingCreated     = "RoleBindingCreated"
	eventReasonRoleBindingUpdated     = "RoleBindingUpdated"
	eventReasonIMCCreated             = "InternalMemberClusterCreated"
	eventReasonIMCSpecUpdated         = "InternalMemberClusterSpecUpdated"
	reasonMemberClusterReadyToJoin    = "MemberClusterReadyToJoin"
	reasonMemberClusterNotReadyToJoin = "MemberClusterNotReadyToJoin"
	reasonMemberClusterJoined         = "MemberClusterJoined"
	reasonMemberClusterLeft           = "MemberClusterLeft"
	reasonMemberClusterUnknown        = "MemberClusterUnknown"
)

// Reconciler reconciles a MemberCluster object
type Reconciler struct {
	client.Client
	recorder record.EventRecorder
	// Need to update MC based on the IMC conditions based on the agent list.
	NetworkingAgentsEnabled bool
	// agents are used as hashset to query the expected agent type, so the value will be ignored.
	agents map[fleetv1alpha1.AgentType]bool
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.V(2).InfoS("Reconcile", "memberCluster", req.NamespacedName)
	var oldMC fleetv1alpha1.MemberCluster
	if err := r.Client.Get(ctx, req.NamespacedName, &oldMC); err != nil {
		klog.ErrorS(err, "failed to get member cluster", "memberCluster", req.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deleting member cluster, garbage collect all the resources in the cluster namespace
	if !oldMC.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("the member cluster is in the process of being deleted", "memberCluster", klog.KObj(&oldMC))
		return r.garbageCollectWork(ctx, &oldMC)
	}

	mc := oldMC.DeepCopy()
	mcObjRef := klog.KObj(mc)
	// Add the finalizer to the member cluster
	if err := r.ensureFinalizer(ctx, mc); err != nil {
		klog.ErrorS(err, "failed to add the finalizer to member cluster", "memberCluster", mcObjRef)
		return ctrl.Result{}, err
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
		if err := r.join(ctx, mc, currentImc); err != nil {
			klog.ErrorS(err, "failed to join", "memberCluster", mcObjRef)
			return ctrl.Result{}, err
		}

	case fleetv1alpha1.ClusterStateLeave:
		if err := r.leave(ctx, mc, currentImc); err != nil {
			klog.ErrorS(err, "failed to leave", "memberCluster", mcObjRef)
			return ctrl.Result{}, err
		}

	default:
		klog.Errorf("encountered a fatal error. unknown state %v in MemberCluster: %s", mc.Spec.State, mcObjRef)
		return ctrl.Result{}, nil
	}

	// Copy status from InternalMemberCluster to MemberCluster.
	r.syncInternalMemberClusterStatus(currentImc, mc)
	if err := r.updateMemberClusterStatus(ctx, mc); err != nil {
		if apierrors.IsConflict(err) {
			klog.V(2).InfoS("failed to update status due to conflicts", "memberCluster", mcObjRef)
		} else {
			klog.ErrorS(err, "failed to update status", "memberCluster", mcObjRef)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return ctrl.Result{}, nil
}

// garbageCollectWork remove all the finalizers on the work that are in the cluster namespace
func (r *Reconciler) garbageCollectWork(ctx context.Context, mc *fleetv1alpha1.MemberCluster) (ctrl.Result, error) {
	var works workv1alpha1.WorkList
	var clusterNS corev1.Namespace
	// check if the namespace still exist
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	if err := r.Client.Get(ctx, types.NamespacedName{Name: namespaceName}, &clusterNS); apierrors.IsNotFound(err) {
		klog.V(2).InfoS("the member cluster namespace is successfully deleted", "memberCluster", klog.KObj(mc))
		return ctrl.Result{}, nil
	}
	// list all the work object we created in the member cluster namespace
	listOpts := []client.ListOption{
		client.MatchingLabels{utils.LabelFleetObj: utils.LabelFleetObjValue},
		client.InNamespace(namespaceName),
	}
	if err := r.Client.List(ctx, &works, listOpts...); err != nil {
		klog.ErrorS(err, "failed to list all the work object", "memberCluster", klog.KObj(mc))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	for _, work := range works.Items {
		staleWork := work.DeepCopy()
		staleWork.SetFinalizers(nil)
		if updateErr := r.Update(ctx, staleWork, &client.UpdateOptions{}); updateErr != nil {
			klog.ErrorS(updateErr, "failed to remove the finalizer from the work",
				"memberCluster", klog.KObj(mc), "work", klog.KObj(staleWork))
			return ctrl.Result{}, updateErr
		}
	}
	klog.V(2).InfoS("successfully removed all the work finalizers in the cluster namespace",
		"memberCluster", klog.KObj(mc), "number of work", len(works.Items))
	controllerutil.RemoveFinalizer(mc, fleetv1alpha1.MemberClusterFinalizer)
	return ctrl.Result{}, r.Update(ctx, mc, &client.UpdateOptions{})
}

// ensureFinalizer makes sure that the member cluster CR has a finalizer on it
func (r *Reconciler) ensureFinalizer(ctx context.Context, mc *fleetv1alpha1.MemberCluster) error {
	if controllerutil.ContainsFinalizer(mc, fleetv1alpha1.MemberClusterFinalizer) {
		return nil
	}
	klog.InfoS("add the member cluster finalizer", "memberCluster", klog.KObj(mc))
	controllerutil.AddFinalizer(mc, fleetv1alpha1.MemberClusterFinalizer)
	return r.Update(ctx, mc, client.FieldOwner(utils.MCControllerFieldManagerName))
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
	klog.V(2).InfoS("join", "memberCluster", klog.KObj(mc))

	namespaceName, err := r.syncNamespace(ctx, mc)
	if err != nil {
		return fmt.Errorf("failed to sync namespace: %w", err)
	}

	roleName, err := r.syncRole(ctx, mc, namespaceName)
	if err != nil {
		return fmt.Errorf("failed to sync role: %w", err)
	}

	err = r.syncRoleBinding(ctx, mc, namespaceName, roleName)
	if err != nil {
		return fmt.Errorf("failed to sync role binding: %w", err)
	}

	if _, err := r.syncInternalMemberCluster(ctx, mc, namespaceName, imc); err != nil {
		return fmt.Errorf("failed to sync internal member cluster spec: %w", err)
	}

	markMemberClusterReadyToJoin(r.recorder, mc)
	return nil
}

// leave notifies member cluster to leave by setting InternalMemberCluster's state to Leave.
//
// Note that leave doesn't delete any of the resources created by join(). Instead, deleting MemberCluster will delete them.
func (r *Reconciler) leave(ctx context.Context, mc *fleetv1alpha1.MemberCluster, imc *fleetv1alpha1.InternalMemberCluster) error {
	klog.V(2).InfoS("leave", "memberCluster", klog.KObj(mc))
	// Never joined successfully before.
	if imc == nil {
		return nil
	}

	// Copy spec from member cluster to internal member cluster.
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	if _, err := r.syncInternalMemberCluster(ctx, mc, namespaceName, imc); err != nil {
		return fmt.Errorf("failed to sync internal member cluster spec: %w", err)
	}

	return nil
}

// syncNamespace creates or updates the namespace for member cluster.
func (r *Reconciler) syncNamespace(ctx context.Context, mc *fleetv1alpha1.MemberCluster) (string, error) {
	klog.V(2).InfoS("syncNamespace", "memberCluster", klog.KObj(mc))
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	fleetNamespaceLabelValue := "true"
	expectedNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:            namespaceName,
			OwnerReferences: []metav1.OwnerReference{*toOwnerReference(mc)},
			Labels:          map[string]string{fleetv1beta1.FleetResourceLabelKey: fleetNamespaceLabelValue},
		},
	}

	// Creates namespace if not found.
	var currentNS corev1.Namespace
	if err := r.Client.Get(ctx, types.NamespacedName{Name: namespaceName}, &currentNS); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", fmt.Errorf("failed to get namespace %s: %w", namespaceName, err)
		}
		klog.V(2).InfoS("creating namespace", "memberCluster", klog.KObj(mc), "namespace", namespaceName)
		// Make sure the entire namespace is removed if the member cluster is deleted.
		if err = r.Client.Create(ctx, &expectedNS, client.FieldOwner(utils.MCControllerFieldManagerName)); err != nil {
			return "", fmt.Errorf("failed to create namespace %s: %w", namespaceName, err)
		}
		r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonNamespaceCreated, "Namespace was created")
		klog.V(2).InfoS("created namespace", "memberCluster", klog.KObj(mc), "namespace", namespaceName)
		return namespaceName, nil
	}

	// migration: To add new label to all existing member cluster namespaces.
	if currentNS.GetLabels()[fleetv1beta1.FleetResourceLabelKey] == "" {
		klog.V(2).InfoS("patching namespace", "memberCluster", klog.KObj(mc), "namespace", namespaceName)
		patch := client.MergeFrom(currentNS.DeepCopy())
		currentNS.ObjectMeta.Labels[fleetv1beta1.FleetResourceLabelKey] = fleetNamespaceLabelValue
		if err := r.Client.Patch(ctx, &currentNS, patch, client.FieldOwner(utils.MCControllerFieldManagerName)); err != nil {
			return "", fmt.Errorf("failed to patch namespace %s: %w", namespaceName, err)
		}
		r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonNamespacePatched, "Namespace was patched")
		klog.V(2).InfoS("patched namespace", "memberCluster", klog.KObj(mc), "namespace", namespaceName)
	}
	return namespaceName, nil
}

// syncRole creates or updates the role for member cluster to access its namespace in hub cluster.
func (r *Reconciler) syncRole(ctx context.Context, mc *fleetv1alpha1.MemberCluster, namespaceName string) (string, error) {
	klog.V(2).InfoS("syncRole", "memberCluster", klog.KObj(mc))
	// Role name is created using member cluster name.
	roleName := fmt.Sprintf(utils.RoleNameFormat, mc.Name)
	expectedRole := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            roleName,
			Namespace:       namespaceName,
			OwnerReferences: []metav1.OwnerReference{*toOwnerReference(mc)},
		},
		Rules: []rbacv1.PolicyRule{utils.FleetRule, utils.EventRule, utils.FleetNetworkRule, utils.WorkRule},
	}

	// Creates role if not found.
	var currentRole rbacv1.Role
	if err := r.Client.Get(ctx, types.NamespacedName{Name: roleName, Namespace: namespaceName}, &currentRole); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", fmt.Errorf("failed to get role %s: %w", roleName, err)
		}
		klog.V(2).InfoS("creating role", "memberCluster", klog.KObj(mc), "role", roleName)
		if err = r.Client.Create(ctx, &expectedRole, client.FieldOwner(utils.MCControllerFieldManagerName)); err != nil {
			return "", fmt.Errorf("failed to create role %s with rules %+v: %w", roleName, expectedRole.Rules, err)
		}
		r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonRoleCreated, "role was created")
		klog.V(2).InfoS("created role", "memberCluster", klog.KObj(mc), "role", roleName)
		return roleName, nil
	}

	// Updates role if currentRole != expectedRole.
	if cmp.Equal(currentRole.Rules, expectedRole.Rules) {
		return roleName, nil
	}
	currentRole.Rules = expectedRole.Rules
	klog.V(2).InfoS("updating role", "memberCluster", klog.KObj(mc), "role", roleName)
	if err := r.Client.Update(ctx, &currentRole, client.FieldOwner(utils.MCControllerFieldManagerName)); err != nil {
		return "", fmt.Errorf("failed to update role %s with rules %+v: %w", roleName, currentRole.Rules, err)
	}
	r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonRoleUpdated, "role was updated")
	klog.V(2).InfoS("updated role", "memberCluster", klog.KObj(mc), "role", roleName)
	return roleName, nil
}

// syncRoleBinding creates or updates the role binding for member cluster to access its namespace in hub cluster.
func (r *Reconciler) syncRoleBinding(ctx context.Context, mc *fleetv1alpha1.MemberCluster, namespaceName string, roleName string) error {
	klog.V(2).InfoS("syncRoleBinding", "memberCluster", klog.KObj(mc))
	// Role binding name is created using member cluster name
	roleBindingName := fmt.Sprintf(utils.RoleBindingNameFormat, mc.Name)
	expectedRoleBinding := rbacv1.RoleBinding{
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
	var currentRoleBinding rbacv1.RoleBinding
	if err := r.Client.Get(ctx, types.NamespacedName{Name: roleBindingName, Namespace: namespaceName}, &currentRoleBinding); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to get role binding %s: %w", roleBindingName, err)
		}
		klog.V(2).InfoS("creating role binding", "memberCluster", klog.KObj(mc), "subject", mc.Spec.Identity)
		if err = r.Client.Create(ctx, &expectedRoleBinding, client.FieldOwner(utils.MCControllerFieldManagerName)); err != nil {
			return fmt.Errorf("failed to create role binding %s: %w", roleBindingName, err)
		}
		r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonRoleBindingCreated, "role binding was created")
		klog.V(2).InfoS("created role binding", "memberCluster", klog.KObj(mc), "subject", mc.Spec.Identity)
		return nil
	}

	// Updates role binding if currentRoleBinding != expectedRoleBinding.
	if cmp.Equal(currentRoleBinding.Subjects, expectedRoleBinding.Subjects) && cmp.Equal(currentRoleBinding.RoleRef, expectedRoleBinding.RoleRef) {
		return nil
	}
	currentRoleBinding.Subjects = expectedRoleBinding.Subjects
	currentRoleBinding.RoleRef = expectedRoleBinding.RoleRef
	klog.V(2).InfoS("updating role binding", "memberCluster", klog.KObj(mc), "subject", mc.Spec.Identity)
	if err := r.Client.Update(ctx, &expectedRoleBinding, client.FieldOwner(utils.MCControllerFieldManagerName)); err != nil {
		return fmt.Errorf("failed to update role binding %s: %w", roleBindingName, err)
	}
	r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonRoleBindingUpdated, "role binding was updated")
	klog.V(2).InfoS("updated role binding", "memberCluster", klog.KObj(mc), "subject", mc.Spec.Identity)
	return nil
}

// syncInternalMemberCluster is used to sync spec from MemberCluster to InternalMemberCluster.
func (r *Reconciler) syncInternalMemberCluster(ctx context.Context, mc *fleetv1alpha1.MemberCluster,
	namespaceName string, currentImc *fleetv1alpha1.InternalMemberCluster) (*fleetv1alpha1.InternalMemberCluster, error) {
	klog.V(2).InfoS("syncInternalMemberCluster", "memberCluster", klog.KObj(mc))
	expectedImc := fleetv1alpha1.InternalMemberCluster{
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
	if currentImc == nil {
		klog.V(2).InfoS("creating internal member cluster", "InternalMemberCluster", klog.KObj(&expectedImc), "spec", expectedImc.Spec)
		if err := r.Client.Create(ctx, &expectedImc, client.FieldOwner(utils.MCControllerFieldManagerName)); err != nil {
			return nil, fmt.Errorf("failed to create internal member cluster %s with spec %+v: %w", klog.KObj(&expectedImc), expectedImc.Spec, err)
		}
		r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonIMCCreated, "Internal member cluster was created")
		klog.V(2).InfoS("created internal member cluster", "InternalMemberCluster", klog.KObj(&expectedImc), "spec", expectedImc.Spec)
		return &expectedImc, nil
	}

	// Updates internal member cluster if currentImc != expectedImc.
	if cmp.Equal(currentImc.Spec, expectedImc.Spec) {
		return currentImc, nil
	}
	currentImc.Spec = expectedImc.Spec
	klog.V(2).InfoS("updating internal member cluster", "InternalMemberCluster", klog.KObj(currentImc), "spec", currentImc.Spec)
	if err := r.Client.Update(ctx, currentImc, client.FieldOwner(utils.MCControllerFieldManagerName)); err != nil {
		return nil, fmt.Errorf("failed to update internal member cluster %s with spec %+v: %w", klog.KObj(currentImc), currentImc.Spec, err)
	}
	r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonIMCSpecUpdated, "internal member cluster spec updated")
	klog.V(2).InfoS("updated internal member cluster", "InternalMemberCluster", klog.KObj(currentImc), "spec", currentImc.Spec)
	return currentImc, nil
}

func toOwnerReference(memberCluster *fleetv1alpha1.MemberCluster) *metav1.OwnerReference {
	return &metav1.OwnerReference{APIVersion: fleetv1alpha1.GroupVersion.String(), Kind: fleetv1alpha1.MemberClusterKind,
		Name: memberCluster.Name, UID: memberCluster.UID, Controller: pointer.Bool(true)}
}

// syncInternalMemberClusterStatus is used to sync status from InternalMemberCluster to MemberCluster & aggregate join conditions from all agents.
func (r *Reconciler) syncInternalMemberClusterStatus(imc *fleetv1alpha1.InternalMemberCluster, mc *fleetv1alpha1.MemberCluster) {
	klog.V(2).InfoS("syncInternalMemberClusterStatus", "memberCluster", klog.KObj(mc))
	if imc == nil {
		return
	}

	// TODO: We didn't handle condition type: fleetv1alpha1.ConditionTypeMemberClusterHealthy.
	// Copy Agent status.
	mc.Status.AgentStatus = imc.Status.AgentStatus
	r.aggregateJoinedCondition(mc)
	// Copy resource usages.
	mc.Status.ResourceUsage = imc.Status.ResourceUsage
}

// updateMemberClusterStatus is used to update member cluster status.
func (r *Reconciler) updateMemberClusterStatus(ctx context.Context, mc *fleetv1alpha1.MemberCluster) error {
	klog.V(2).InfoS("updateMemberClusterStatus", "memberCluster", klog.KObj(mc))
	backOffPeriod := retry.DefaultRetry
	backOffPeriod.Cap = time.Second * time.Duration(mc.Spec.HeartbeatPeriodSeconds/2)

	return retry.OnError(backOffPeriod,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err)
		},
		func() error {
			return r.Client.Status().Update(ctx, mc)
		})
}

// aggregateJoinedCondition is used to calculate and mark the joined or left status for member cluster based on join conditions from all agents.
func (r *Reconciler) aggregateJoinedCondition(mc *fleetv1alpha1.MemberCluster) {
	klog.V(2).InfoS("syncJoinedCondition", "memberCluster", klog.KObj(mc))
	if len(mc.Status.AgentStatus) < len(r.agents) {
		markMemberClusterUnknown(r.recorder, mc)
		return
	}
	joined := true
	left := true
	reportedAgents := make(map[fleetv1alpha1.AgentType]bool)
	for _, agentStatus := range mc.Status.AgentStatus {
		if !r.agents[agentStatus.Type] {
			klog.V(2).InfoS("Ignoring unexpected agent type status", "agentStatus", agentStatus)
			continue // ignore any unexpected agent type
		}
		condition := meta.FindStatusCondition(agentStatus.Conditions, string(fleetv1alpha1.AgentJoined))
		if condition == nil {
			markMemberClusterUnknown(r.recorder, mc)
			return
		}

		joined = joined && condition.Status == metav1.ConditionTrue
		left = left && condition.Status == metav1.ConditionFalse
		reportedAgents[agentStatus.Type] = true
	}

	if len(reportedAgents) < len(r.agents) {
		markMemberClusterUnknown(r.recorder, mc)
		return
	}

	if joined && !left {
		markMemberClusterJoined(r.recorder, mc)
	} else if !joined && left {
		markMemberClusterLeft(r.recorder, mc)
	} else {
		markMemberClusterUnknown(r.recorder, mc)
	}
}

// markMemberClusterReadyToJoin is used to update the ReadyToJoin condition as true of member cluster.
func markMemberClusterReadyToJoin(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.V(2).InfoS("markMemberClusterReadyToJoin", "memberCluster", klog.KObj(mc))
	newCondition := metav1.Condition{
		Type:               string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
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
	klog.V(2).InfoS("markMemberClusterJoined", "memberCluster", klog.KObj(mc))
	newCondition := metav1.Condition{
		Type:               string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
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

// markMemberClusterLeft is used to update the status of the member cluster to have the left condition and mark member cluster as not ready to join.
func markMemberClusterLeft(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.V(2).InfoS("markMemberClusterLeft", "memberCluster", klog.KObj(mc))
	newCondition := metav1.Condition{
		Type:               string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
		Status:             metav1.ConditionFalse,
		Reason:             reasonMemberClusterLeft,
		ObservedGeneration: mc.GetGeneration(),
	}
	notReadyCondition := metav1.Condition{
		Type:               string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
		Status:             metav1.ConditionFalse,
		Reason:             reasonMemberClusterNotReadyToJoin,
		ObservedGeneration: mc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := mc.GetCondition(newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		recorder.Event(mc, corev1.EventTypeNormal, reasonMemberClusterJoined, "member cluster left")
		klog.V(2).InfoS("memberCluster left", "memberCluster", klog.KObj(mc))
		metrics.ReportLeaveResultMetric()
	}

	mc.SetConditions(newCondition, notReadyCondition)
}

// markMemberClusterUnknown is used to update the status of the member cluster to have the left condition.
func markMemberClusterUnknown(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.V(2).InfoS("markMemberClusterUnknown", "memberCluster", klog.KObj(mc))
	newCondition := metav1.Condition{
		Type:               string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
		Status:             metav1.ConditionUnknown,
		Reason:             reasonMemberClusterUnknown,
		ObservedGeneration: mc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := mc.GetCondition(newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		recorder.Event(mc, corev1.EventTypeWarning, reasonMemberClusterUnknown, "member cluster unknown")
		klog.V(2).InfoS("memberCluster unknown", "memberCluster", klog.KObj(mc))
	}

	mc.SetConditions(newCondition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("mcv1alpha1")
	r.agents = make(map[fleetv1alpha1.AgentType]bool)
	r.agents[fleetv1alpha1.MemberAgent] = true

	if r.NetworkingAgentsEnabled {
		r.agents[fleetv1alpha1.MultiClusterServiceAgent] = true
		r.agents[fleetv1alpha1.ServiceExportImportAgent] = true
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.MemberCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&fleetv1alpha1.InternalMemberCluster{}).
		Complete(r)
}
