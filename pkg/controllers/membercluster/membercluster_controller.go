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
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	eventReasonNamespaceDeleted    = "NamespaceDeleted"
	eventReasonRoleCreated         = "RoleCreated"
	eventReasonRoleUpdated         = "RoleUpdated"
	eventReasonRoleBindingCreated  = "RoleBindingCreated"
	eventReasonRoleBindingUpdated  = "RoleBindingUpdated"
	eventReasonIMCCreated          = "InternalMemberClusterCreated"
	eventReasonIMCSpecUpdated      = "InternalMemberClusterSpecUpdated"
	reasonMemberClusterReadyToJoin = "MemberClusterReadyToJoin"
	reasonMemberClusterJoined      = "MemberClusterJoined"
	reasonMemberClusterLeft        = "MemberClusterLeft"

	reasonMCSControllerJoined = "MCSControllerJoined"
	reasonMCSControllerLeft   = "MCSControllerLeft"

	reasonServiceExportImportControllerJoined = "ServiceExportImportControllerJoined"
	reasonServiceExportImportControllerLeft   = "ServiceExportImportControllerLeft"
)

// Reconciler reconciles a MemberCluster object
type Reconciler struct {
	client.Client
	recorder               record.EventRecorder
	NetControllersRequired bool // Whether it needs to handle networking controllers join/unjoin.
}

//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberclusters/finalizers,verbs=update

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var mc fleetv1alpha1.MemberCluster
	if err := r.Client.Get(ctx, req.NamespacedName, &mc); err != nil {
		klog.ErrorS(err, "failed to get the member cluster in hub agent", "memberCluster", req.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if mc.Spec.State == fleetv1alpha1.ClusterStateJoin {
		return r.join(ctx, &mc)
	}

	if mc.Spec.State == fleetv1alpha1.ClusterStateLeave {
		return r.leave(ctx, &mc)
	}

	return ctrl.Result{}, fmt.Errorf("we get an unexpected cluster state %s for cluster %v", mc.Spec.State, req.NamespacedName)
}

// join is used to complete the Join workflow for the Hub agent
// where we check and create namespace role, role binding, internal member cluster and update member cluster status.
func (r *Reconciler) join(ctx context.Context, mc *fleetv1alpha1.MemberCluster) (ctrl.Result, error) {
	// Create the namespace associated with the member cluster Obj
	namespaceName, err := r.checkAndCreateNamespace(ctx, mc)
	if err != nil {
		klog.ErrorS(err, "failed to check and create namespace for member cluster in the hub cluster",
			"memberCluster", mc.Name, "namespace", namespaceName)
		return ctrl.Result{}, err
	}

	imc, err := r.markInternalMemberClusterStateJoin(ctx, mc, namespaceName)
	if err != nil {
		klog.ErrorS(err, "failed to check and create internal member cluster %s in the hub cluster",
			"memberCluster", mc.Name, "internalMemberCluster", mc.Name)
		return ctrl.Result{}, err
	}

	roleName, err := r.syncRole(ctx, mc, namespaceName)
	if err != nil {
		klog.ErrorS(err, "failed to check and create role for member cluster in the hub cluster",
			"memberCluster", mc.Name, "role", roleName)
		return ctrl.Result{}, err
	}

	err = r.syncRoleBinding(ctx, mc, namespaceName, roleName, mc.Spec.Identity)
	if err != nil {
		klog.ErrorS(err, "failed to check and create role binding for member cluster in the hub cluster",
			"memberCluster", mc.Name, "roleBinding", fmt.Sprintf(utils.RoleBindingNameFormat, mc.Name))
		return ctrl.Result{}, err
	}

	markMemberClusterReadyToJoin(r.recorder, mc)
	joinedCond := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
	if joinedCond != nil && joinedCond.Status == metav1.ConditionTrue {
		r.copyMemberClusterStatusFromInternalMC(mc, imc)
	}

	if r.NetControllersRequired {
		r.handleNetworkingControllersJoin(ctx, mc, imc)
	}

	if err := r.updateMemberClusterStatus(ctx, mc); err != nil {
		klog.ErrorS(err, "cannot update the member cluster status",
			"internalMemberCluster", klog.KObj(imc))
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) handleNetworkingControllersJoin(ctx context.Context, mc *fleetv1alpha1.MemberCluster, imc *fleetv1alpha1.InternalMemberCluster) {
	joinedCond := imc.GetCondition(fleetv1alpha1.ConditionTypeIMCMCSControllerJoin)
	if joinedCond != nil && joinedCond.Status == metav1.ConditionTrue {
		r.updateMCSControllerStatus(mc, imc)
	}

	joinedCond = imc.GetCondition(fleetv1alpha1.ConditionTypeServiceExportImportControllerJoin)
	if joinedCond != nil && joinedCond.Status == metav1.ConditionTrue {
		r.updateServiceExportImportControllerStatus(mc, imc)
	}
}

// TODO: reduce cyclo
// leave is used to complete the Leave workflow for the Hub agent.
func (r *Reconciler) leave(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster) (ctrl.Result, error) {
	imcExists := true
	imcLeft := false
	memberClusterLeftCondition := memberCluster.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
	if memberClusterLeftCondition != nil && memberClusterLeftCondition.Status == metav1.ConditionFalse {
		klog.V(3).InfoS("Member cluster is marked as left", "memberCluster", memberCluster.Name)
		return ctrl.Result{}, nil
	}

	var imc fleetv1alpha1.InternalMemberCluster
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster.Name)
	if err := r.Client.Get(ctx, types.NamespacedName{Name: memberCluster.Name, Namespace: namespaceName}, &imc); err != nil {
		// TODO: make sure we still get not Found error if the namespace does not exist
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "failed to get the internal Member cluster ", "memberCluster", memberCluster.Name)
			return ctrl.Result{}, err
		}
		klog.V(3).InfoS("Internal Member cluster doesn't exist for member cluster", "memberCluster", memberCluster.Name)
		imcExists = false
		imcLeft = true
	}

	if r.NetControllersRequired {
		return ctrl.Result{}, r.handleNetworkingControllersLeave(ctx, imcExists, memberCluster, &imc)
	}

	if imcExists {
		internalMemberClusterLeftCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
		if internalMemberClusterLeftCondition != nil && internalMemberClusterLeftCondition.Status == metav1.ConditionFalse {
			imcLeft = true
		} else {
			if err := r.syncInternalMemberClusterState(ctx, memberCluster, &imc); err != nil {
				klog.ErrorS(err, "Internal Member cluster's spec cannot be updated tp be left",
					"memberCluster", memberCluster.Name, "internalMemberCluster", memberCluster.Name)
				return ctrl.Result{}, err
			}
		}
	}

	if imcLeft {
		return ctrl.Result{}, r.cleanupAndMarkMemberClusterLeft(ctx, memberCluster)
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) cleanupAndMarkMemberClusterLeft(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster) error {
	mcKObj := klog.KObj(memberCluster)
	klog.V(3).InfoS("Marking member cluster as left", "memberCluster", mcKObj)
	// when the error is not found, we still need to update the memberCluster.
	if err := r.deleteNamespace(ctx, memberCluster); err != nil && !apierrors.IsNotFound(err) {
		klog.ErrorS(err, "Failed to delete namespace", "memberCluster", mcKObj)
		return err
	}

	// marking member cluster as Left after all the associated resources are removed
	markMemberClusterLeft(r.recorder, memberCluster)
	klog.V(3).InfoS("Updating member cluster", "memberCluster", mcKObj, "status", memberCluster.Status)
	if err := r.updateMemberClusterStatus(ctx, memberCluster); err != nil {
		klog.ErrorS(err, "Failed to update member cluster as Left", "memberCluster", mcKObj)
		return err
	}
	metrics.ReportLeaveResultMetric()
	return nil
}

// handleNetworkingControllersLeave handles the networking controllers leave and makes sure memberCluster will always
// be the last one to mark as left.
func (r *Reconciler) handleNetworkingControllersLeave(ctx context.Context, imcExists bool, memberCluster *fleetv1alpha1.MemberCluster, imc *fleetv1alpha1.InternalMemberCluster) error {
	if !imcExists {
		markMCSControllerLeft(r.recorder, memberCluster)
		markServiceImportExportControllerLeft(r.recorder, memberCluster)
		return r.cleanupAndMarkMemberClusterLeft(ctx, memberCluster)
	}

	internalMemberClusterLeftCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
	mcsCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeIMCMCSControllerJoin)
	serviceExportImportControllerCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeIMCServiceExportImportControllerJoin)

	mcsControllerLeft := mcsCondition != nil &&
		mcsCondition.Status == metav1.ConditionFalse
	serviceExportImportControllerLeft := serviceExportImportControllerCondition != nil &&
		serviceExportImportControllerCondition.Status == metav1.ConditionFalse
	internalMemberClusterLeft := internalMemberClusterLeftCondition != nil &&
		internalMemberClusterLeftCondition.Status == metav1.ConditionFalse

	if mcsControllerLeft {
		markMCSControllerLeft(r.recorder, memberCluster)
	}
	if serviceExportImportControllerLeft {
		markServiceImportExportControllerLeft(r.recorder, memberCluster)
	}

	if internalMemberClusterLeft && mcsControllerLeft && serviceExportImportControllerLeft {
		return r.cleanupAndMarkMemberClusterLeft(ctx, memberCluster)
	}

	mcKObj := klog.KObj(memberCluster)
	if err := r.syncInternalMemberClusterState(ctx, memberCluster, imc); err != nil {
		klog.ErrorS(err, "Internal Member cluster's spec cannot be updated tp be left",
			"memberCluster", mcKObj, "internalMemberCluster", klog.KObj(imc))
		return err
	}

	if mcsControllerLeft || serviceExportImportControllerLeft {
		klog.V(3).InfoS("Updating member cluster", "memberCluster", mcKObj, "status", memberCluster.Status)
		if err := r.updateMemberClusterStatus(ctx, memberCluster); err != nil {
			klog.ErrorS(err, "Failed to update member cluster as Left", "memberCluster", mcKObj)
			return err
		}
	}
	return nil
}

// checkAndCreateNamespace checks to see if the namespace exists for given memberClusterName
// if the namespace doesn't exist it creates it.
func (r *Reconciler) checkAndCreateNamespace(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster) (string, error) {
	var namespace corev1.Namespace
	// Namespace name is created using member cluster name.
	nsName := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster.Name)
	// Check to see if namespace exists, if it doesn't exist create it.
	if err := r.Client.Get(ctx, types.NamespacedName{Name: nsName}, &namespace); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", err
		}
		klog.V(3).InfoS("namespace doesn't exist for member cluster",
			"namespace", nsName, "memberCluster", memberCluster.Name)
		// make sure the entire namespace is removed if the member cluster is deleted
		ownerRef := metav1.OwnerReference{APIVersion: fleetv1alpha1.GroupVersion.String(), Kind: fleetv1alpha1.MemberClusterKind,
			Name: memberCluster.Name, UID: memberCluster.UID, Controller: pointer.Bool(true)}
		namespace = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:            nsName,
				OwnerReferences: []metav1.OwnerReference{ownerRef},
			},
		}
		if err = r.Client.Create(ctx, &namespace, client.FieldOwner(memberCluster.GetUID())); err != nil {
			return "", err
		}
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, eventReasonNamespaceCreated, "Namespace was created")
		klog.V(2).InfoS("namespace was successfully created for member cluster",
			"namespace", nsName, "memberCluster", memberCluster.Name)
		return namespace.Name, nil
	}
	return namespace.Name, nil
}

// syncRole checks to see if the role exists for given memberClusterName
// if the role doesn't exist it creates it.
func (r *Reconciler) syncRole(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster, namespaceName string) (string, error) {
	var role rbacv1.Role
	// Role name is created using member cluster name.
	roleName := fmt.Sprintf(utils.RoleNameFormat, memberCluster.Name)
	// Check to see if Role exists for Member Cluster, if it doesn't exist create it.
	if err := r.Client.Get(ctx, types.NamespacedName{Name: roleName, Namespace: namespaceName}, &role); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", err
		}
		klog.V(3).InfoS("role doesn't exist for member cluster", "role", roleName, "memberCluster", memberCluster.Name)
		role = createRole(roleName, namespaceName)
		if err = r.Client.Create(ctx, &role, client.FieldOwner(memberCluster.GetUID())); err != nil {
			return "", err
		}
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, eventReasonRoleCreated, "role was created")
		klog.V(2).InfoS("role was successfully created for member cluster", "role", roleName, "memberCluster", memberCluster.Name)
		return role.Name, nil
	}
	expectedRole := createRole(roleName, namespaceName)
	if !cmp.Equal(role.Rules, expectedRole.Rules) {
		klog.V(2).InfoS("the role has more or less permissions than expected, updating ", "role", roleName, "memberCluster", memberCluster.Name)
		if err := r.Client.Update(ctx, &expectedRole, client.FieldOwner(memberCluster.GetUID())); err != nil {
			klog.ErrorS(err, "cannot update role for member cluster",
				"memberCluster", memberCluster.Name, "role", roleName)
			return "", err
		}
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, eventReasonRoleUpdated, "role was updated")
	}
	return role.Name, nil
}

// syncRoleBinding checks to see if the Role binding exists for given memberClusterName and namespaceName
// if the Role binding doesn't exist it creates it.
func (r *Reconciler) syncRoleBinding(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster, namespaceName string, roleName string, identity rbacv1.Subject) error {
	var rb rbacv1.RoleBinding
	// Role binding name is created using member cluster name
	roleBindingName := fmt.Sprintf(utils.RoleBindingNameFormat, memberCluster.Name)
	// Check to see if Role Binding exists for Member Cluster, if it doesn't exist create it.
	if err := r.Client.Get(ctx, types.NamespacedName{Name: roleBindingName, Namespace: namespaceName}, &rb); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		klog.V(3).InfoS("role binding doesn't exist for member cluster", "roleBinding", roleBindingName, "memberCluster", memberCluster.Name)
		rb := createRoleBinding(roleName, roleBindingName, namespaceName, identity)
		if err = r.Client.Create(ctx, &rb, client.FieldOwner(memberCluster.GetUID())); err != nil {
			return err
		}
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, eventReasonRoleBindingCreated, "role binding was created")
		klog.V(2).InfoS("role binding was successfully created for member cluster",
			"roleBinding", roleBindingName, "memberCluster", memberCluster.Name)
		return nil
	}
	expectedRoleBinding := createRoleBinding(roleName, roleBindingName, namespaceName, identity)
	if !cmp.Equal(rb.Subjects, expectedRoleBinding.Subjects) || !cmp.Equal(rb.RoleRef, expectedRoleBinding.RoleRef) {
		klog.V(2).InfoS("the role binding is different from what is expected, updating", "roleBinding", roleBindingName, "memberCluster", memberCluster.Name)
		if err := r.Client.Update(ctx, &expectedRoleBinding, client.FieldOwner(memberCluster.GetUID())); err != nil {
			klog.ErrorS(err, "cannot update role binding for member cluster", "memberCluster", memberCluster.Name, "roleBinding", roleBindingName)
			return err
		}
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, eventReasonRoleBindingUpdated, "role binding was updated")
	}
	return nil
}

// markInternalMemberClusterStateJoin update the internal member cluster state as join
// if the internal member cluster doesn't exist it creates it.
func (r *Reconciler) markInternalMemberClusterStateJoin(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster, namespaceName string) (*fleetv1alpha1.InternalMemberCluster, error) {
	var imc fleetv1alpha1.InternalMemberCluster
	if err := r.Client.Get(ctx, types.NamespacedName{Name: memberCluster.Name, Namespace: namespaceName}, &imc); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
		klog.V(3).InfoS("creating the internal member cluster", "internalMemberCluster", memberCluster.Name, "memberCluster", memberCluster.Name)
		ownerRef := metav1.OwnerReference{APIVersion: fleetv1alpha1.GroupVersion.String(), Kind: fleetv1alpha1.MemberClusterKind,
			Name: memberCluster.Name, UID: memberCluster.UID, Controller: pointer.Bool(true)}
		imc := fleetv1alpha1.InternalMemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:            memberCluster.Name,
				Namespace:       namespaceName,
				OwnerReferences: []metav1.OwnerReference{ownerRef},
			},
			Spec: fleetv1alpha1.InternalMemberClusterSpec{
				State:                  fleetv1alpha1.ClusterStateJoin,
				HeartbeatPeriodSeconds: memberCluster.Spec.HeartbeatPeriodSeconds,
			},
		}
		if err = r.Client.Create(ctx, &imc, client.FieldOwner(memberCluster.GetUID())); err != nil {
			return nil, err
		}
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, eventReasonIMCCreated, "Internal member cluster was created")
		klog.V(2).InfoS("internal member cluster was created successfully", "internalMemberCluster", memberCluster.Name, "memberCluster", memberCluster.Name)
		return &imc, nil
	}

	err := r.syncInternalMemberClusterState(ctx, memberCluster, &imc)
	return &imc, err
}

// copyMemberClusterStatusFromInternalMC is used to update member cluster status given the internal member cluster status
func (r *Reconciler) copyMemberClusterStatusFromInternalMC(mc *fleetv1alpha1.MemberCluster, imc *fleetv1alpha1.InternalMemberCluster) {
	mc.Status.Capacity = imc.Status.Capacity
	mc.Status.Allocatable = imc.Status.Allocatable
	memberClusterHearBeatCondition := mc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat)
	if memberClusterHearBeatCondition == nil {
		klog.V(3).InfoS("set heartbeat condition for member cluster", "memberCluster", mc.Name)
		mc.SetConditions(*imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat))
	} else {
		internalMemberClusterHeartBeatCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat)
		klog.V(3).InfoS("updating last transition for member cluster", "memberCluster", mc.Name)
		memberClusterHearBeatCondition.LastTransitionTime = internalMemberClusterHeartBeatCondition.LastTransitionTime
		memberClusterHearBeatCondition.ObservedGeneration = internalMemberClusterHeartBeatCondition.ObservedGeneration
	}
	if mc.GetCondition(fleetv1alpha1.ConditionTypeMemberClusterJoin) == nil {
		markMemberClusterJoined(r.recorder, mc)
		metrics.ReportJoinResultMetric()
	}
}

// updateMCSControllerStatus is used to the update the status of the mcs controller to have the joined & heartbeat
// condition.
func (r *Reconciler) updateMCSControllerStatus(mc *fleetv1alpha1.MemberCluster, imc *fleetv1alpha1.InternalMemberCluster) {
	mcKObj := klog.KObj(mc)
	heartbeatCondition := mc.GetCondition(fleetv1alpha1.ConditionTypeIMCMCSControllerHeartbeat)
	if heartbeatCondition == nil {
		klog.V(3).InfoS("Set heartbeat condition for mcs controller", "memberCluster", mcKObj)
		mc.SetConditions(*imc.GetCondition(fleetv1alpha1.ConditionTypeIMCMCSControllerHeartbeat))
	} else {
		imcHeartbeatCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat)
		klog.V(3).InfoS("Updating last transition for mcs controller", "memberCluster", mcKObj)
		heartbeatCondition.LastTransitionTime = imcHeartbeatCondition.LastTransitionTime
		heartbeatCondition.ObservedGeneration = imcHeartbeatCondition.ObservedGeneration
	}

	joinedCond := mc.GetCondition(fleetv1alpha1.ConditionTypeMCSControllerJoin)
	if joinedCond != nil && joinedCond.Status == metav1.ConditionTrue {
		return
	}
	klog.V(2).InfoS("Mark the MultiClusterService controller as Joined", "memberService", mcKObj)
	r.recorder.Event(mc, corev1.EventTypeNormal, reasonMCSControllerJoined, "MultiClusterService controller is joined")
	joinedCond = &metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMCSControllerJoin,
		Status:             metav1.ConditionTrue,
		Reason:             reasonMCSControllerJoined,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(*joinedCond)

	// TODO, need to distinguish different controller type for join
	//metrics.ReportJoinResultMetric()
}

// updateServiceExportImportControllerStatus is used to the update the status of the serviceexportimport controller
// to have the joined & heartbeat condition.
func (r *Reconciler) updateServiceExportImportControllerStatus(mc *fleetv1alpha1.MemberCluster, imc *fleetv1alpha1.InternalMemberCluster) {
	mcKObj := klog.KObj(mc)
	heartbeatCondition := mc.GetCondition(fleetv1alpha1.ConditionTypeIMCServiceExportImportControllerHeartbeat)
	if heartbeatCondition == nil {
		klog.V(3).InfoS("Set heartbeat condition for serviceexportimport controller", "memberCluster", mcKObj)
		mc.SetConditions(*imc.GetCondition(fleetv1alpha1.ConditionTypeIMCServiceExportImportControllerHeartbeat))
	} else {
		imcHeartbeatCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat)
		klog.V(3).InfoS("Updating last transition for serviceexportimport controller", "memberCluster", mcKObj)
		heartbeatCondition.LastTransitionTime = imcHeartbeatCondition.LastTransitionTime
		heartbeatCondition.ObservedGeneration = imcHeartbeatCondition.ObservedGeneration
	}

	joinedCond := mc.GetCondition(fleetv1alpha1.ConditionTypeServiceExportImportControllerJoin)
	if joinedCond != nil && joinedCond.Status == metav1.ConditionTrue {
		return
	}
	klog.V(2).InfoS("Mark the ServiceExportImport controller as Joined", "memberService", mcKObj)
	r.recorder.Event(mc, corev1.EventTypeNormal, reasonServiceExportImportControllerJoined, "ServiceExportImport controller is joined")
	joinedCond = &metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeServiceExportImportControllerJoin,
		Status:             metav1.ConditionTrue,
		Reason:             reasonServiceExportImportControllerJoined,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(*joinedCond)
	// TODO, need to distinguish different controller type for join
	//metrics.ReportJoinResultMetric()
}

// updateMemberClusterStatus is used to update member cluster status to indicate that the member cluster has Joined/Left.
func (r *Reconciler) updateMemberClusterStatus(ctx context.Context, mc *fleetv1alpha1.MemberCluster) error {
	backOffPeriod := retry.DefaultRetry
	backOffPeriod.Cap = time.Second * time.Duration(mc.Spec.HeartbeatPeriodSeconds/2)

	return retry.OnError(backOffPeriod,
		func(err error) bool {
			if apierrors.IsConflict(err) || apierrors.IsServerTimeout(err) {
				return true
			}
			return false
		},
		func() error {
			err := r.Client.Status().Update(ctx, mc, client.FieldOwner(mc.GetUID()))
			if err != nil {
				klog.ErrorS(err, "cannot update member cluster status", "memberCluster", mc.Name)
			}
			return err
		})
}

// syncInternalMemberClusterState is used to update the internal member cluster's state to be the same as memberCluster.
func (r *Reconciler) syncInternalMemberClusterState(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster, imc *fleetv1alpha1.InternalMemberCluster) error {
	if imc.Spec.State == memberCluster.Spec.State {
		return nil
	}
	imc.Spec.State = memberCluster.Spec.State
	if err := r.Client.Update(ctx, imc, client.FieldOwner(memberCluster.GetUID())); err != nil {
		return err
	}
	klog.V(3).InfoS("internal Member cluster spec is updated to be the same as the member cluster",
		"memberCluster", memberCluster.Name, "internalMemberCluster", imc.Name, "state", imc.Spec.State)
	r.recorder.Event(memberCluster, corev1.EventTypeNormal, eventReasonIMCSpecUpdated, fmt.Sprintf("internal member cluster spec is marked as %s", memberCluster.Spec.State))
	return nil
}

// deleteNamespace is used to delete the namespace created for a member cluster.
func (r *Reconciler) deleteNamespace(ctx context.Context, mc *fleetv1alpha1.MemberCluster) error {
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	}
	if err := r.Client.Delete(ctx, &namespace, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	klog.V(2).InfoS("Namespace is deleted", "memberCluster", mc.Name, "namespace", namespaceName)
	r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonNamespaceDeleted, "namespace is deleted for member cluster")
	return nil
}

// markMemberClusterReadyToJoin is used to the update the status of the member cluster to ready to join condition.
func markMemberClusterReadyToJoin(recorder record.EventRecorder, mc apis.ConditionedObj) {
	readyToJoinCond := mc.GetCondition(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin)
	if readyToJoinCond != nil {
		return
	}
	klog.V(2).InfoS("mark the member Cluster as ready to", "memberService", mc.GetName())
	recorder.Event(mc, corev1.EventTypeNormal, reasonMemberClusterReadyToJoin, "member cluster is ready to join")
	readyToJoinCondition := &metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin,
		Status:             metav1.ConditionTrue,
		Reason:             reasonMemberClusterReadyToJoin,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(*readyToJoinCondition)
}

// markMemberClusterJoined is used to the update the status of the member cluster to have the joined condition.
func markMemberClusterJoined(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.V(2).InfoS("mark the member Cluster as Joined", "memberService", mc.GetName())
	recorder.Event(mc, corev1.EventTypeNormal, reasonMemberClusterJoined, "member cluster is joined")
	joinedCondition := &metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMemberClusterJoin,
		Status:             metav1.ConditionTrue,
		Reason:             reasonMemberClusterJoined,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(*joinedCondition)
}

// markMemberClusterLeft is used to update the status of the member cluster to have the left condition.
func markMemberClusterLeft(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.V(2).InfoS("mark member cluster left", "memberCluster", mc.GetName())
	recorder.Event(mc, corev1.EventTypeNormal, reasonMemberClusterLeft, "member cluster has left")
	leftCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMemberClusterJoin,
		Status:             metav1.ConditionFalse,
		Reason:             reasonMemberClusterLeft,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(leftCondition, utils.ReconcileSuccessCondition())
}

// markMCSControllerLeft is used to update the status of the mcs controller to have the left condition.
func markMCSControllerLeft(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.V(2).InfoS("mark mcs controller left", "memberCluster", mc.GetName())
	recorder.Event(mc, corev1.EventTypeNormal, reasonMemberClusterLeft, "mcs controller has left")
	leftCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMCSControllerJoin,
		Status:             metav1.ConditionFalse,
		Reason:             reasonMCSControllerLeft,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(leftCondition, utils.ReconcileSuccessCondition())
}

// markServiceImportExportControllerLeft is used to update the status of the serviceexportimport controller to have the
// left condition.
func markServiceImportExportControllerLeft(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.V(2).InfoS("mark serviceexportimport controller left", "memberCluster", mc.GetName())
	recorder.Event(mc, corev1.EventTypeNormal, reasonMemberClusterLeft, "serviceexportimport controller has left")
	leftCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeServiceExportImportControllerJoin,
		Status:             metav1.ConditionFalse,
		Reason:             reasonServiceExportImportControllerLeft,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(leftCondition, utils.ReconcileSuccessCondition())
}

// createRole creates role for member cluster.
func createRole(roleName, namespaceName string) rbacv1.Role {
	// TODO: More API groups and verbs will be added as new member agents are added apart from the Join agent.
	fleetRule := rbacv1.PolicyRule{
		Verbs:     []string{"get", "list", "update", "patch", "watch"},
		APIGroups: []string{fleetv1alpha1.GroupVersion.Group},
		Resources: []string{"*"},
	}
	eventRule := rbacv1.PolicyRule{
		Verbs:     []string{"get", "list", "update", "patch", "watch", "create"},
		APIGroups: []string{""},
		Resources: []string{"events"},
	}
	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: namespaceName,
		},
		Rules: []rbacv1.PolicyRule{fleetRule, eventRule},
	}
	return role
}

// createRoleBinding created role binding for member cluster.
func createRoleBinding(roleName, roleBindingName, namespaceName string, identity rbacv1.Subject) rbacv1.RoleBinding {
	roleRef := rbacv1.RoleRef{
		APIGroup: rbacv1.GroupName,
		Kind:     "Role",
		Name:     roleName,
	}
	rb := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: namespaceName,
		},
		Subjects: []rbacv1.Subject{identity},
		RoleRef:  roleRef,
	}
	return rb
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("memberCluster")
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.MemberCluster{}).
		Owns(&fleetv1alpha1.InternalMemberCluster{}).
		Complete(r)
}
