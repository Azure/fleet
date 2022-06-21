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
	"github.com/prometheus/client_golang/prometheus"
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
	"go.goms.io/fleet/pkg/utils"
)

const (
	eventReasonNamespaceCreated   = "NamespaceCreated"
	eventReasonNamespaceDeleted   = "NamespaceDeleted"
	eventReasonRoleCreated        = "RoleCreated"
	eventReasonRoleUpdated        = "RoleUpdated"
	eventReasonRoleBindingCreated = "RoleBindingCreated"
	eventReasonRoleBindingUpdated = "RoleBindingUpdated"
	eventReasonIMCCreated         = "InternalMemberClusterCreated"
	eventReasonIMCSpecUpdated     = "InternalMemberClusterSpecUpdated"
	reasonMemberClusterJoined     = "MemberClusterJoined"
	reasonMemberClusterLeft       = "MemberClusterLeft"

	internalMemberClusterKind = "InternalMemberCluster"
)

// Reconciler reconciles a MemberCluster object
type Reconciler struct {
	client.Client
	recorder                    record.EventRecorder
	reportJoinLeaveResultMetric func(operation utils.MetricsOperation, successful bool)
}

type HubAgentJoinLeaveMetrics struct {
	JoinSucceedCounter  prometheus.Counter
	JoinFailCounter     prometheus.Counter
	LeaveSucceedCounter prometheus.Counter
	LeaveFailCounter    prometheus.Counter
}

// NewReconciler creates a new Reconciler for member cluster
func NewReconciler(hubClient client.Client,
	reportJoinLeaveResultMetric func(operation utils.MetricsOperation, successful bool)) *Reconciler {
	return &Reconciler{
		Client:                      hubClient,
		reportJoinLeaveResultMetric: reportJoinLeaveResultMetric,
	}
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
		r.reportJoinLeaveResultMetric(utils.MetricsOperationJoin, false)
		return ctrl.Result{}, err
	}

	imc, err := r.markInternalMemberClusterStateJoin(ctx, mc, namespaceName)
	if err != nil {
		klog.ErrorS(err, "failed to check and create internal member cluster %s in the hub cluster",
			"memberCluster", mc.Name, "internalMemberCluster", mc.Name)
		r.reportJoinLeaveResultMetric(utils.MetricsOperationJoin, false)
		return ctrl.Result{}, err
	}

	roleName, err := r.syncRole(ctx, mc, namespaceName)
	if err != nil {
		klog.ErrorS(err, "failed to check and create role for member cluster in the hub cluster",
			"memberCluster", mc.Name, "role", roleName)
		r.reportJoinLeaveResultMetric(utils.MetricsOperationJoin, false)
		return ctrl.Result{}, err
	}

	err = r.syncRoleBinding(ctx, mc, namespaceName, roleName, mc.Spec.Identity)
	if err != nil {
		klog.ErrorS(err, "failed to check and create role binding for member cluster in the hub cluster",
			"memberCluster", mc.Name, "roleBinding", fmt.Sprintf(utils.RoleBindingNameFormat, mc.Name))
		r.reportJoinLeaveResultMetric(utils.MetricsOperationJoin, false)
		return ctrl.Result{}, err
	}

	joinedCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
	if joinedCondition != nil && joinedCondition.Status == metav1.ConditionTrue {
		if err := r.copyMemberClusterStatusFromInternalMC(ctx, mc, imc); err != nil {
			klog.ErrorS(err, "cannot update member cluster status as Joined",
				"internalMemberCluster", imc.Name)
			r.reportJoinLeaveResultMetric(utils.MetricsOperationJoin, false)
			return ctrl.Result{}, err
		}
	}
	r.reportJoinLeaveResultMetric(utils.MetricsOperationJoin, true)
	return ctrl.Result{}, nil
}

// leave is used to complete the Leave workflow for the Hub agent.
func (r *Reconciler) leave(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster) (ctrl.Result, error) {
	imcExists := true
	imcLeft := false
	memberClusterLeftCondition := memberCluster.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
	if memberClusterLeftCondition != nil && memberClusterLeftCondition.Status == metav1.ConditionFalse {
		klog.InfoS("Member cluster is marked as left", "memberCluster", memberCluster.Name)
		return ctrl.Result{}, nil
	}

	var imc fleetv1alpha1.InternalMemberCluster
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster.Name)
	if err := r.Client.Get(ctx, types.NamespacedName{Name: memberCluster.Name, Namespace: namespaceName}, &imc); err != nil {
		// TODO: make sure we still get not Found error if the namespace does not exist
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "failed to get the internal Member cluster ", "memberCluster", memberCluster.Name)
			r.reportJoinLeaveResultMetric(utils.MetricsOperationLeave, false)
			return ctrl.Result{}, err
		}
		klog.InfoS("Internal Member cluster doesn't exist for member cluster", "memberCluster", memberCluster.Name)
		imcExists = false
		imcLeft = true
	}

	if imcExists {
		internalMemberClusterLeftCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
		if internalMemberClusterLeftCondition != nil && internalMemberClusterLeftCondition.Status == metav1.ConditionFalse {
			imcLeft = true
		} else {
			if err := r.syncInternalMemberClusterState(ctx, memberCluster, &imc); err != nil {
				klog.ErrorS(err, "Internal Member cluster's spec cannot be updated tp be left",
					"memberCluster", memberCluster.Name, "internalMemberCluster", memberCluster.Name)
				r.reportJoinLeaveResultMetric(utils.MetricsOperationLeave, false)
				return ctrl.Result{}, err
			}
		}
	}

	if imcLeft {
		// delete every resource that is associated with the internal member cluster
		if err := r.deleteNamespace(ctx, memberCluster); err != nil {
			if apierrors.IsNotFound(err) {
				return ctrl.Result{}, nil
			}
			klog.ErrorS(err, "failed to delete namespace", "memberCluster", memberCluster.Name)
			r.reportJoinLeaveResultMetric(utils.MetricsOperationLeave, false)
			return ctrl.Result{}, err
		}

		// marking member cluster as Left after all the associated resources are removed
		if err := r.updateMemberClusterStatusAsLeft(ctx, memberCluster); err != nil {
			klog.ErrorS(err, "failed to update member cluster as Left", "memberCluster", memberCluster)
			r.reportJoinLeaveResultMetric(utils.MetricsOperationLeave, false)
			return ctrl.Result{}, err
		}
	}
	r.reportJoinLeaveResultMetric(utils.MetricsOperationLeave, true)
	return ctrl.Result{}, nil
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
		klog.InfoS("namespace doesn't exist for member cluster",
			"namespace", nsName, "memberCluster", memberCluster.Name)
		// make sure the entire namespace is removed if the member cluster is deleted
		ownerRef := metav1.OwnerReference{APIVersion: memberCluster.APIVersion, Kind: memberCluster.Kind,
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
		klog.InfoS("namespace was successfully created for member cluster",
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
		klog.InfoS("role doesn't exist for member cluster", "role", roleName, "memberCluster", memberCluster.Name)
		role = createRole(roleName, namespaceName)
		if err = r.Client.Create(ctx, &role, client.FieldOwner(memberCluster.GetUID())); err != nil {
			return "", err
		}
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, eventReasonRoleCreated, "role was created")
		klog.InfoS("role was successfully created for member cluster", "role", roleName, "memberCluster", memberCluster.Name)
		return role.Name, nil
	}
	expectedRole := createRole(roleName, namespaceName)
	if !cmp.Equal(role.Rules, expectedRole.Rules) {
		klog.InfoS("the role has more or less permissions than expected, hence it will be updated", "role", roleName, "memberCluster", memberCluster.Name)
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
		klog.InfoS("role binding doesn't exist for member cluster", "roleBinding", roleBindingName, "memberCluster", memberCluster.Name)
		rb := createRoleBinding(roleName, roleBindingName, namespaceName, identity)
		if err = r.Client.Create(ctx, &rb, client.FieldOwner(memberCluster.GetUID())); err != nil {
			return err
		}
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, eventReasonRoleBindingCreated, "role binding was created")
		klog.InfoS("role binding was successfully created for member cluster",
			"roleBinding", roleBindingName, "memberCluster", memberCluster.Name)
		return nil
	}
	expectedRoleBinding := createRoleBinding(roleName, roleBindingName, namespaceName, identity)
	if !cmp.Equal(rb.Subjects, expectedRoleBinding.Subjects) && !cmp.Equal(rb.RoleRef, expectedRoleBinding.RoleRef) {
		klog.InfoS("the role binding is different from what is expected, hence it will be updated", "roleBinding", roleBindingName, "memberCluster", memberCluster.Name)
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
		klog.InfoS("creating the internal member cluster", "internalMemberCluster", memberCluster.Name, "memberCluster", memberCluster.Name)
		ownerRef := metav1.OwnerReference{APIVersion: memberCluster.APIVersion, Kind: memberCluster.Kind,
			Name: memberCluster.Name, UID: memberCluster.UID, Controller: pointer.Bool(true)}
		imc := fleetv1alpha1.InternalMemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:            memberCluster.Name,
				Namespace:       namespaceName,
				OwnerReferences: []metav1.OwnerReference{ownerRef},
			},
			Spec: fleetv1alpha1.InternalMemberClusterSpec{
				State: fleetv1alpha1.ClusterStateJoin,
			},
		}
		if err = r.Client.Create(ctx, &imc, client.FieldOwner(memberCluster.GetUID())); err != nil {
			return nil, err
		}
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, eventReasonIMCCreated, "Internal member cluster was created")
		klog.InfoS("internal member cluster was created successfully", "internalMemberCluster", memberCluster.Name, "memberCluster", memberCluster.Name)
		return &imc, nil
	}

	err := r.syncInternalMemberClusterState(ctx, memberCluster, &imc)
	return &imc, err
}

// copyMemberClusterStatusFromInternalMC is used to update member cluster status given the internal member cluster status
func (r *Reconciler) copyMemberClusterStatusFromInternalMC(ctx context.Context, mc *fleetv1alpha1.MemberCluster, imc *fleetv1alpha1.InternalMemberCluster) error {
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
			mc.Status.Capacity = imc.Status.Capacity
			mc.Status.Allocatable = imc.Status.Allocatable
			mc.SetConditions(*imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat))
			markMemberClusterJoined(r.recorder, mc)
			err := r.Client.Status().Update(ctx, mc, client.FieldOwner(mc.GetUID()))
			if err != nil {
				klog.ErrorS(err, "cannot update member cluster status as join", "memberCluster", mc.Name)
			}
			return err
		})
}

// updateMemberClusterStatus is used to update member cluster status to indicate that the member cluster has Joined/Left.
func (r *Reconciler) updateMemberClusterStatusAsLeft(ctx context.Context, mc *fleetv1alpha1.MemberCluster) error {
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
			markMemberClusterLeft(r.recorder, mc)
			err := r.Client.Status().Update(ctx, mc, client.FieldOwner(mc.GetUID()))
			if err != nil {
				klog.ErrorS(err, "cannot update member cluster status as left", "memberCluster", mc.Name)
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
	klog.InfoS("internal Member cluster spec is updated to be the same as the member cluster",
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
	klog.InfoS("Namespace is deleted", "memberCluster", mc.Name, "namespace", namespaceName)
	r.recorder.Event(mc, corev1.EventTypeNormal, eventReasonNamespaceDeleted, "namespace is deleted for member cluster")
	return nil
}

// markMemberClusterJoined is used to the update the status of the member cluster to have the joined condition.
func markMemberClusterJoined(recorder record.EventRecorder, mc apis.ConditionedObj) {
	joinedCondition := mc.GetCondition(fleetv1alpha1.ConditionTypeMemberClusterJoin)
	if joinedCondition != nil && joinedCondition.Status == metav1.ConditionTrue {
		return
	}
	klog.InfoS("mark the member Cluster as Joined", "memberService", mc.GetName())
	recorder.Event(mc, corev1.EventTypeNormal, reasonMemberClusterJoined, "member cluster is joined")
	joinedCondition = &metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMemberClusterJoin,
		Status:             metav1.ConditionTrue,
		Reason:             reasonMemberClusterJoined,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(*joinedCondition)
}

// markMemberClusterLeft is used to update the status of the member cluster to have the left condition.
func markMemberClusterLeft(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.InfoS("mark member cluster left", "memberCluster", mc.GetName())
	recorder.Event(mc, corev1.EventTypeNormal, reasonMemberClusterLeft, "member cluster has left")
	leftCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMemberClusterJoin,
		Status:             metav1.ConditionFalse,
		Reason:             reasonMemberClusterLeft,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(leftCondition, utils.ReconcileSuccessCondition())
}

// createRole creates role for member cluster.
func createRole(roleName, namespaceName string) rbacv1.Role {
	// TODO: More API groups and verbs will be added as new member agents are added apart from the Join agent.
	verbs := []string{"get", "list", "update", "patch", "watch"}
	apiGroups := []string{"", fleetv1alpha1.GroupVersion.Group}
	resources := []string{"*"}

	rule := rbacv1.PolicyRule{
		Verbs:     verbs,
		APIGroups: apiGroups,
		Resources: resources,
	}
	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: namespaceName,
		},
		Rules: []rbacv1.PolicyRule{rule},
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
