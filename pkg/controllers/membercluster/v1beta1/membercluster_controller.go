/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	runtime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"go.goms.io/fleet/apis"
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
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
	reasonMemberClusterUnknown        = "MemberClusterJoinStateUnknown"
)

// Reconciler reconciles a MemberCluster object
type Reconciler struct {
	client.Client
	recorder record.EventRecorder
	// Need to update MC based on the IMC conditions based on the agent list.
	NetworkingAgentsEnabled bool
	// the max number of concurrent reconciles per controller.
	MaxConcurrentReconciles int
	// the wait time in minutes before we force delete a member cluster.
	ForceDeleteWaitTime time.Duration
	// agents are used as hashset to query the expected agent type, so the value will be ignored.
	agents map[clusterv1beta1.AgentType]bool
}

func (r *Reconciler) Reconcile(ctx context.Context, req runtime.Request) (runtime.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("MemberCluster reconciliation starts", "memberCluster", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("MemberCluster reconciliation ends", "memberCluster", req.NamespacedName, "latency", latency)
	}()

	var mc clusterv1beta1.MemberCluster
	if err := r.Client.Get(ctx, req.NamespacedName, &mc); err != nil {
		klog.ErrorS(err, "Failed to get member cluster", "memberCluster", req.Name)
		return runtime.Result{}, client.IgnoreNotFound(err)
	}
	mcObjRef := klog.KObj(&mc)

	// Handle deleting/leaving member cluster, garbage collect all the resources in the cluster namespace
	if !mc.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("The member cluster is leaving", "memberCluster", mcObjRef)
		return r.handleDelete(ctx, mc.DeepCopy())
	}

	// Add the finalizer to the member cluster
	if err := r.ensureFinalizer(ctx, &mc); err != nil {
		klog.ErrorS(err, "Failed to add the finalizer to member cluster", "memberCluster", mcObjRef)
		return runtime.Result{}, err
	}
	currentIMC, err := r.getInternalMemberCluster(ctx, mc.GetName())
	if err != nil {
		return runtime.Result{}, err
	}
	if err := r.join(ctx, &mc, currentIMC); err != nil {
		klog.ErrorS(err, "Failed to join", "memberCluster", mcObjRef)
		return runtime.Result{}, err
	}

	// Copy status from InternalMemberCluster to MemberCluster.
	r.syncInternalMemberClusterStatus(currentIMC, &mc)
	if err := r.updateMemberClusterStatus(ctx, &mc); err != nil {
		if apierrors.IsConflict(err) {
			klog.V(2).InfoS("Failed to update status due to conflicts", "memberCluster", mcObjRef)
		} else {
			klog.ErrorS(err, "Failed to update status", "memberCluster", mcObjRef)
		}
		return runtime.Result{}, client.IgnoreNotFound(err)
	}

	return runtime.Result{}, nil
}

// handleDelete handles the delete event of the member cluster, makes sure the agent has finished leaving the fleet first and
// then garbage collects all the resources in the cluster namespace.
func (r *Reconciler) handleDelete(ctx context.Context, mc *clusterv1beta1.MemberCluster) (runtime.Result, error) {
	mcObjRef := klog.KObj(mc)
	if !controllerutil.ContainsFinalizer(mc, placementv1beta1.MemberClusterFinalizer) {
		klog.V(2).InfoS("No need to do anything for the deleting member cluster without a finalizer", "memberCluster", mcObjRef)
		return runtime.Result{}, nil
	}
	// check if the namespace still exist
	var currentNS corev1.Namespace
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	if err := r.Client.Get(ctx, types.NamespacedName{Name: namespaceName}, &currentNS); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to get the member cluster namespace", "memberCluster", mcObjRef)
			return runtime.Result{}, controller.NewAPIServerError(true, err)
		}
		klog.V(2).InfoS("The member cluster namespace is not found, remove the finalizer", "memberCluster", mcObjRef)
		controllerutil.RemoveFinalizer(mc, placementv1beta1.MemberClusterFinalizer)
		return runtime.Result{}, controller.NewUpdateIgnoreConflictError(r.Update(ctx, mc))
	}
	// check if the namespace is being deleted already, just wait for it to be deleted
	if !currentNS.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("The member cluster namespace is still being deleted", "memberCluster", mcObjRef, "deleteTimestamp", currentNS.DeletionTimestamp)
		var stuckErr error
		if time.Now().After(currentNS.DeletionTimestamp.Add(5 * time.Minute)) {
			// alert if the namespace is stuck in deleting for more than 5 minutes
			stuckErr = controller.NewUnexpectedBehaviorError(fmt.Errorf("the member cluster namespace %s has been deleting since %s", namespaceName, currentNS.DeletionTimestamp.Format(time.RFC3339)))
		}
		return runtime.Result{RequeueAfter: time.Second}, stuckErr
	}
	currentImc := &clusterv1beta1.InternalMemberCluster{}
	imcNamespacedName := types.NamespacedName{Namespace: namespaceName, Name: mc.Name}
	if err := r.Client.Get(ctx, imcNamespacedName, currentImc); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to get internal member cluster", "internalMemberCluster", imcNamespacedName)
			return runtime.Result{}, controller.NewAPIServerError(true, err)
		}
		// this is possible since we garbage collect the internal member cluster first before deleting the MC
		klog.V(2).Info("InternalMemberCluster not found, start garbage collecting", "memberCluster", mcObjRef)
		return runtime.Result{Requeue: true}, r.garbageCollect(ctx, mc)
	}
	// calculate the current status of the member cluster from imc status
	r.syncInternalMemberClusterStatus(currentImc, mc)
	// check if the cluster is already left
	mcJoinedCondition := meta.FindStatusCondition(mc.Status.Conditions, string(clusterv1beta1.ConditionTypeMemberClusterJoined))
	if condition.IsConditionStatusFalse(mcJoinedCondition, mc.GetGeneration()) {
		klog.V(2).InfoS("Agent already left, start garbage collecting", "memberCluster", mcObjRef)
		if gcErr := r.garbageCollect(ctx, mc); gcErr != nil {
			return runtime.Result{}, gcErr
		}
		return runtime.Result{Requeue: true}, controller.NewUpdateIgnoreConflictError(r.updateMemberClusterStatus(ctx, mc))
	}
	// check to see if we can force delete member cluster.
	if currentImc.Spec.State == clusterv1beta1.ClusterStateLeave && time.Since(mc.DeletionTimestamp.Time) >= r.ForceDeleteWaitTime {
		klog.V(2).InfoS("Force delete the member cluster, by garbage collecting owned resources", "memberCluster", mcObjRef)
		return runtime.Result{Requeue: true}, r.garbageCollect(ctx, mc)
	}
	klog.V(2).InfoS("Need to wait for the agent to leave", "memberCluster", mcObjRef, "joinedCondition", mcJoinedCondition)
	// mark the imc as left to make sure the agent is leaving the fleet
	if err := r.leave(ctx, mc, currentImc); err != nil {
		klog.ErrorS(err, "Failed to mark the imc as leave", "memberCluster", mcObjRef)
		return runtime.Result{}, err
	}
	// update the mc status to track the leaving status while we wait for all the agents to leave.
	// once the imc is updated, the mc controller will reconcile again ,or we reconcile to force delete
	// the member cluster after force delete wait time.
	return runtime.Result{RequeueAfter: r.ForceDeleteWaitTime}, controller.NewUpdateIgnoreConflictError(r.updateMemberClusterStatus(ctx, mc))
}

func (r *Reconciler) getInternalMemberCluster(ctx context.Context, name string) (*clusterv1beta1.InternalMemberCluster, error) {
	// Get current internal member cluster.
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, name)
	imcNamespacedName := types.NamespacedName{Namespace: namespaceName, Name: name}
	currentIMC := &clusterv1beta1.InternalMemberCluster{}
	if err := r.Client.Get(ctx, imcNamespacedName, currentIMC); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to get internal member cluster", "internalMemberCluster", imcNamespacedName)
			return nil, err
		}
		// Not found.
		currentIMC = nil
	}
	return currentIMC, nil
}

// garbageCollectWork remove all the finalizers on the work that are in the cluster namespace
func (r *Reconciler) garbageCollectWork(ctx context.Context, mc *clusterv1beta1.MemberCluster, namespaceName string) error {
	// list all the work object we created in the member cluster namespace
	var works placementv1beta1.WorkList
	listOpts := []client.ListOption{
		client.InNamespace(namespaceName),
	}
	if err := r.Client.List(ctx, &works, listOpts...); err != nil {
		klog.ErrorS(err, "Failed to list all the work object", "memberCluster", klog.KObj(mc))
		return client.IgnoreNotFound(err)
	}
	// remove all the finalizers on the work objects in parallel
	errs, cctx := errgroup.WithContext(ctx)
	for _, work := range works.Items {
		staleWork := work.DeepCopy()
		errs.Go(func() error {
			staleWork.SetFinalizers(nil)
			if updateErr := r.Update(cctx, staleWork, &client.UpdateOptions{}); updateErr != nil {
				klog.ErrorS(updateErr, "Failed to remove the finalizer from the work",
					"memberCluster", klog.KObj(mc), "work", klog.KObj(staleWork))
				return updateErr
			}
			return nil
		})
	}
	klog.V(2).InfoS("Try to remove all the work finalizers in the cluster namespace",
		"memberCluster", klog.KObj(mc), "number of work", len(works.Items))
	return controller.NewUpdateIgnoreConflictError(errs.Wait())
}

// garbageCollect is used to garbage collect all the resources in the cluster namespace associated with the member cluster.
func (r *Reconciler) garbageCollect(ctx context.Context, mc *clusterv1beta1.MemberCluster) error {
	// check if the namespace still exist
	var clusterNS corev1.Namespace
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	if err := r.Client.Get(ctx, types.NamespacedName{Name: namespaceName}, &clusterNS); err != nil {
		klog.ErrorS(err, "Failed to get the member cluster namespace", "memberCluster", klog.KObj(mc))
		return controller.NewAPIServerError(true, err)
	}
	if err := r.garbageCollectWork(ctx, mc, namespaceName); err != nil {
		return err
	}
	if err := r.Delete(ctx, &clusterNS); err != nil {
		klog.ErrorS(err, "Failed to remove the cluster namespace", "memberCluster", klog.KObj(mc), "namespace", namespaceName)
		return controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Deleted the member cluster namespace", "memberCluster", klog.KObj(mc))
	return nil
}

// ensureFinalizer makes sure that the member cluster CR has a finalizer on it
func (r *Reconciler) ensureFinalizer(ctx context.Context, mc *clusterv1beta1.MemberCluster) error {
	if controllerutil.ContainsFinalizer(mc, placementv1beta1.MemberClusterFinalizer) {
		return nil
	}
	klog.InfoS("Added the member cluster finalizer", "memberCluster", klog.KObj(mc))
	controllerutil.AddFinalizer(mc, placementv1beta1.MemberClusterFinalizer)
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
func (r *Reconciler) join(ctx context.Context, mc *clusterv1beta1.MemberCluster, imc *clusterv1beta1.InternalMemberCluster) error {
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
func (r *Reconciler) leave(ctx context.Context, mc *clusterv1beta1.MemberCluster, imc *clusterv1beta1.InternalMemberCluster) error {
	klog.V(2).InfoS("Mark the internal cluster state as `Leave`", "memberCluster", klog.KObj(mc))

	// Copy spec from member cluster to internal member cluster.
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	if _, err := r.syncInternalMemberCluster(ctx, mc, namespaceName, imc); err != nil {
		return fmt.Errorf("failed to sync internal member cluster spec: %w", err)
	}

	return nil
}

// syncNamespace creates or updates the namespace for member cluster.
func (r *Reconciler) syncNamespace(ctx context.Context, mc *clusterv1beta1.MemberCluster) (string, error) {
	klog.V(2).InfoS("Sync the namespace for the member cluster", "memberCluster", klog.KObj(mc))
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	fleetNamespaceLabelValue := "true"
	expectedNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:            namespaceName,
			OwnerReferences: []metav1.OwnerReference{*toOwnerReference(mc)},
			Labels:          map[string]string{placementv1beta1.FleetResourceLabelKey: fleetNamespaceLabelValue},
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
	if currentNS.GetLabels()[placementv1beta1.FleetResourceLabelKey] == "" {
		klog.V(2).InfoS("patching namespace", "memberCluster", klog.KObj(mc), "namespace", namespaceName)
		patch := client.MergeFrom(currentNS.DeepCopy())
		currentNS.ObjectMeta.Labels[placementv1beta1.FleetResourceLabelKey] = fleetNamespaceLabelValue
		if err := r.Client.Patch(ctx, &currentNS, patch, client.FieldOwner(utils.MCControllerFieldManagerName)); err != nil {
			return "", fmt.Errorf("failed to patch namespace %s: %w", namespaceName, err)
		}
		r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonNamespacePatched, "Namespace was patched")
		klog.V(2).InfoS("patched namespace", "memberCluster", klog.KObj(mc), "namespace", namespaceName)
	}
	return namespaceName, nil
}

// syncRole creates or updates the role for member cluster to access its namespace in hub cluster.
func (r *Reconciler) syncRole(ctx context.Context, mc *clusterv1beta1.MemberCluster, namespaceName string) (string, error) {
	klog.V(2).InfoS("Sync the role for the member cluster", "memberCluster", klog.KObj(mc))
	// Role name is created using member cluster name.
	roleName := fmt.Sprintf(utils.RoleNameFormat, mc.Name)
	expectedRole := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            roleName,
			Namespace:       namespaceName,
			OwnerReferences: []metav1.OwnerReference{*toOwnerReference(mc)},
		},
		Rules: []rbacv1.PolicyRule{utils.FleetClusterRule, utils.FleetPlacementRule, utils.FleetNetworkRule, utils.EventRule},
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
	if reflect.DeepEqual(currentRole.Rules, expectedRole.Rules) {
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
func (r *Reconciler) syncRoleBinding(ctx context.Context, mc *clusterv1beta1.MemberCluster, namespaceName string, roleName string) error {
	klog.V(2).InfoS("Sync the roleBinding for the member cluster", "memberCluster", klog.KObj(mc))
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
	if reflect.DeepEqual(currentRoleBinding.Subjects, expectedRoleBinding.Subjects) && reflect.DeepEqual(currentRoleBinding.RoleRef, expectedRoleBinding.RoleRef) {
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
func (r *Reconciler) syncInternalMemberCluster(ctx context.Context, mc *clusterv1beta1.MemberCluster,
	namespaceName string, currentImc *clusterv1beta1.InternalMemberCluster) (*clusterv1beta1.InternalMemberCluster, error) {
	klog.V(2).InfoS("Sync internalMemberCluster spec from member cluster", "memberCluster", klog.KObj(mc))
	expectedImc := clusterv1beta1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:            mc.Name,
			Namespace:       namespaceName,
			OwnerReferences: []metav1.OwnerReference{*toOwnerReference(mc)},
		},
		Spec: clusterv1beta1.InternalMemberClusterSpec{
			HeartbeatPeriodSeconds: mc.Spec.HeartbeatPeriodSeconds,
		},
	}
	if mc.GetDeletionTimestamp().IsZero() {
		expectedImc.Spec.State = clusterv1beta1.ClusterStateJoin
	} else {
		expectedImc.Spec.State = clusterv1beta1.ClusterStateLeave
	}

	// Creates internal member cluster if not found.
	if currentImc == nil {
		klog.V(2).InfoS("creating internal member cluster", "InternalMemberCluster", klog.KObj(&expectedImc), "spec", expectedImc.Spec)
		if err := r.Client.Create(ctx, &expectedImc, client.FieldOwner(utils.MCControllerFieldManagerName)); err != nil {
			return nil, controller.NewAPIServerError(false, fmt.Errorf("failed to create internal member cluster %s with spec %+v: %w", klog.KObj(&expectedImc), expectedImc.Spec, err))
		}
		r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonIMCCreated, "Internal member cluster was created")
		klog.V(2).InfoS("created internal member cluster", "InternalMemberCluster", klog.KObj(&expectedImc), "spec", expectedImc.Spec)
		return &expectedImc, nil
	}

	// Updates internal member cluster if currentImc != expectedImc.
	if reflect.DeepEqual(currentImc.Spec, expectedImc.Spec) {
		return currentImc, nil
	}
	currentImc.Spec = expectedImc.Spec
	klog.V(2).InfoS("updating internal member cluster", "InternalMemberCluster", klog.KObj(currentImc), "spec", currentImc.Spec)
	if err := r.Client.Update(ctx, currentImc, client.FieldOwner(utils.MCControllerFieldManagerName)); err != nil {
		return nil, controller.NewAPIServerError(false, fmt.Errorf("failed to update internal member cluster %s with spec %+v: %w", klog.KObj(currentImc), currentImc.Spec, err))
	}
	r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonIMCSpecUpdated, "internal member cluster spec updated")
	klog.V(2).InfoS("updated internal member cluster", "InternalMemberCluster", klog.KObj(currentImc), "spec", currentImc.Spec)
	return currentImc, nil
}

func toOwnerReference(memberCluster *clusterv1beta1.MemberCluster) *metav1.OwnerReference {
	return &metav1.OwnerReference{APIVersion: clusterv1beta1.GroupVersion.String(), Kind: clusterv1beta1.MemberClusterKind,
		Name: memberCluster.Name, UID: memberCluster.UID, Controller: ptr.To(true)}
}

// syncInternalMemberClusterStatus is used to sync status from InternalMemberCluster to MemberCluster & aggregate join conditions from all agents.
func (r *Reconciler) syncInternalMemberClusterStatus(imc *clusterv1beta1.InternalMemberCluster, mc *clusterv1beta1.MemberCluster) {
	klog.V(2).InfoS("Sync the internalMemberCluster status", "memberCluster", klog.KObj(mc))
	if imc == nil {
		return
	}

	// TODO: We didn't handle condition type: clusterv1beta1.ConditionTypeMemberClusterHealthy.
	// Copy Agent status.
	mc.Status.AgentStatus = imc.Status.AgentStatus
	r.aggregateJoinedCondition(mc)
	// Copy resource usages.
	mc.Status.ResourceUsage = imc.Status.ResourceUsage
	// Copy additional conditions.
	for idx := range imc.Status.Conditions {
		cond := imc.Status.Conditions[idx]
		cond.ObservedGeneration = mc.GetGeneration()
		meta.SetStatusCondition(&mc.Status.Conditions, cond)
	}
	// Copy the cluster properties.
	mc.Status.Properties = imc.Status.Properties
}

// updateMemberClusterStatus is used to update member cluster status.
func (r *Reconciler) updateMemberClusterStatus(ctx context.Context, mc *clusterv1beta1.MemberCluster) error {
	klog.V(2).InfoS("Update the memberCluster status", "memberCluster", klog.KObj(mc))
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
func (r *Reconciler) aggregateJoinedCondition(mc *clusterv1beta1.MemberCluster) {
	klog.V(2).InfoS("Aggregate joined condition from all agents", "memberCluster", klog.KObj(mc))
	if len(mc.Status.AgentStatus) < len(r.agents) {
		markMemberClusterUnknown(r.recorder, mc)
		return
	}
	joined := true
	left := true
	reportedAgents := make(map[clusterv1beta1.AgentType]bool)
	for _, agentStatus := range mc.Status.AgentStatus {
		if !r.agents[agentStatus.Type] {
			_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("find an unexpected agent type %s for member cluster %s", agentStatus.Type, mc.Name))
			continue // ignore any unexpected agent type
		}
		condition := meta.FindStatusCondition(agentStatus.Conditions, string(clusterv1beta1.AgentJoined))
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
	klog.V(2).InfoS("Mark the member cluster ReadyToJoin", "memberCluster", klog.KObj(mc))
	newCondition := metav1.Condition{
		Type:               string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
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
	klog.V(2).InfoS("Mark the member cluster joined", "memberCluster", klog.KObj(mc))
	newCondition := metav1.Condition{
		Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
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
	klog.V(2).InfoS("Mark the member cluster left", "memberCluster", klog.KObj(mc))
	newCondition := metav1.Condition{
		Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
		Status:             metav1.ConditionFalse,
		Reason:             reasonMemberClusterLeft,
		ObservedGeneration: mc.GetGeneration(),
	}
	notReadyCondition := metav1.Condition{
		Type:               string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
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
	klog.V(2).InfoS("Mark the member cluster join condition unknown", "memberCluster", klog.KObj(mc))
	newCondition := metav1.Condition{
		Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
		Status:             metav1.ConditionUnknown,
		Reason:             reasonMemberClusterUnknown,
		ObservedGeneration: mc.GetGeneration(),
	}

	// Joined status changed.
	existingCondition := mc.GetCondition(newCondition.Type)
	if existingCondition == nil || existingCondition.Status != newCondition.Status {
		recorder.Event(mc, corev1.EventTypeWarning, reasonMemberClusterUnknown, "member cluster join state unknown")
		klog.V(2).InfoS("memberCluster join state unknown", "memberCluster", klog.KObj(mc))
	}

	mc.SetConditions(newCondition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr runtime.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("mcv1beta1")
	r.agents = make(map[clusterv1beta1.AgentType]bool)
	r.agents[clusterv1beta1.MemberAgent] = true

	if r.NetworkingAgentsEnabled {
		r.agents[clusterv1beta1.MultiClusterServiceAgent] = true
		r.agents[clusterv1beta1.ServiceExportImportAgent] = true
	}
	return runtime.NewControllerManagedBy(mgr).
		WithOptions(ctrl.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}). // set the max number of concurrent reconciles
		For(&clusterv1beta1.MemberCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&clusterv1beta1.InternalMemberCluster{}).
		Complete(r)
}
