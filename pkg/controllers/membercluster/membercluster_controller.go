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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"go.goms.io/fleet/apis"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	namespaceCreated                 = "NamespaceCreated"
	namespaceDeleted                 = "NamespaceDeleted"
	roleCreated                      = "RoleCreated"
	roleUpdated                      = "RoleUpdated"
	roleBindingCreated               = "RoleBindingCreated"
	roleBindingUpdated               = "RoleBindingUpdated"
	internalMemberClusterCreated     = "InternalMemberClusterCreated"
	internalMemberClusterSpecUpdated = "InternalMemberClusterSpecUpdated"
	memberClusterJoined              = "MemberClusterJoined"
	memberClusterLeft                = "MemberClusterLeft"
	heartBeatReceived                = "HeartBeatReceived"
	namespaceFinalizer               = "NamespaceFinalizer"
)

var (
	InternalMemberClusterKind = fleetv1alpha1.GroupVersion.WithKind("InternalMemberCluster")
)

// Reconciler reconciles a MemberCluster object
type Reconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	recorder record.EventRecorder
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

	return ctrl.Result{}, nil
}

// join is used to complete the Join workflow for the Hub agent
// where we check and create namespace role, role binding, internal member cluster and update member cluster status.
func (r *Reconciler) join(ctx context.Context, mc *fleetv1alpha1.MemberCluster) (ctrl.Result, error) {
	if err := r.addNamespaceFinalizer(ctx, mc); err != nil {
		klog.ErrorS(err, "failed to add namespace finalizer to member cluster", "memberCluster", mc.Name)
		return ctrl.Result{}, err
	}

	namespaceName, err := r.checkAndCreateNamespace(ctx, mc)
	if err != nil {
		klog.ErrorS(err, "failed to check and create namespace for member cluster in hub agent", "memberCluster", mc.Name, "namespace", namespaceName)
		return ctrl.Result{}, err
	}

	roleName, err := r.checkAndCreateRole(ctx, mc, namespaceName)
	if err != nil {
		klog.ErrorS(err, "failed to check and create role for member cluster in hub agent", "memberCluster", mc.Name, "role", roleName)
		return ctrl.Result{}, err
	}

	err = r.checkAndCreateRoleBinding(ctx, mc, namespaceName, roleName, mc.Spec.Identity)
	if err != nil {
		klog.ErrorS(err, "failed to check and create role binding for member cluster in hub agent", "memberCluster", mc.Name, "roleBinding", fmt.Sprintf(utils.RoleBindingNameFormat, mc.Name))
		return ctrl.Result{}, err
	}

	imc, err := r.checkAndCreateInternalMemberCluster(ctx, mc, namespaceName)
	if err != nil {
		klog.ErrorS(err, "failed to check and create internal member cluster %s in hub agent", "memberCluster", mc.Name, "internalMemberCluster", mc.Name)
		return ctrl.Result{}, err
	}

	joinedCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
	heartBeatCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat)

	if (joinedCondition != nil && joinedCondition.Status == metav1.ConditionTrue) && (heartBeatCondition != nil && heartBeatCondition.Status == metav1.ConditionTrue) {
		if err := r.updateMemberClusterStatusWithRetryAsJoined(ctx, mc, imc.Status); err != nil {
			klog.ErrorS(err, "cannot update member cluster status as Joined", "internalMemberCluster", imc.Name)
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// leave is used to complete the Leave workflow for the Hub agent.
func (r *Reconciler) leave(ctx context.Context, mc *fleetv1alpha1.MemberCluster) (ctrl.Result, error) {
	if err := r.updateInternalMemberClusterSpec(ctx, mc); err != nil {
		klog.ErrorS(err, "Internal Member cluster's spec cannot be updated", "memberCluster", mc.Name, "internalMemberCluster", mc.Name)
		return ctrl.Result{}, err
	}

	var imc fleetv1alpha1.InternalMemberCluster
	if err := r.Client.Get(ctx, types.NamespacedName{Name: mc.Name, Namespace: fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)}, &imc); err != nil {
		klog.ErrorS(err, "Internal Member cluster cannot be retrieved", "memberCluster", mc.Name, "internalMemberCluster", mc.Name)
		return ctrl.Result{}, err
	}

	internalMemberClusterLeftCondition := imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)
	memberClusterLeftCondition := mc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin)

	if (internalMemberClusterLeftCondition != nil && internalMemberClusterLeftCondition.Status == metav1.ConditionFalse) && memberClusterLeftCondition != nil {
		// marking member cluster as Left so that the fleet provider could trigger member cluster deletion after which we initiate the clean-up for all resources created for the member cluster
		if err := r.updateMemberClusterStatusWithRetryAsLeft(ctx, mc); err != nil {
			klog.ErrorS(err, "failed to update member cluster as Left", "memberCluster", mc)
			return ctrl.Result{}, err
		}

		if mc.GetDeletionTimestamp() != nil && memberClusterLeftCondition.Status == metav1.ConditionFalse {
			if err := r.deleteNamespace(ctx, mc); err != nil {
				klog.ErrorS(err, "failed to delete namespace", "memberCluster", mc.Name)
				return ctrl.Result{}, err
			}

			if err := r.removeNamespaceFinalizer(ctx, mc); err != nil {
				klog.ErrorS(err, "failed to remove finalizers for member cluster", "memberCluster", mc.Name)
				return ctrl.Result{}, err
			}
		}
	}
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
		if apierrors.IsNotFound(err) {
			klog.InfoS("namespace doesn't exist for member cluster", "namespace", nsName, "memberCluster", memberCluster.Name)
			namespace = corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
				},
			}
			if err = r.Client.Create(ctx, &namespace); err != nil {
				return "", err
			}
			r.recorder.Event(memberCluster, corev1.EventTypeNormal, namespaceCreated, "Namespace was created")
			klog.InfoS("namespace was successfully created for member cluster", "namespace", nsName, "memberCluster", memberCluster.Name)
			return namespace.Name, nil
		}
		return "", err
	}
	return namespace.Name, nil
}

// checkAndCreateRole checks to see if the role exists for given memberClusterName
// if the role doesn't exist it creates it.
func (r *Reconciler) checkAndCreateRole(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster, namespaceName string) (string, error) {
	var role rbacv1.Role
	// Role name is created using member cluster name.
	roleName := fmt.Sprintf(utils.RoleNameFormat, memberCluster.Name)
	// Check to see if Role exists for Member Cluster, if it doesn't exist create it.
	if err := r.Client.Get(ctx, types.NamespacedName{Name: roleName, Namespace: namespaceName}, &role); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("role doesn't exist for member cluster", "role", roleName, "memberCluster", memberCluster.Name)
			role = createRole(roleName, namespaceName)
			if err = r.Client.Create(ctx, &role); err != nil {
				return "", err
			}
			r.recorder.Event(memberCluster, corev1.EventTypeNormal, roleCreated, "role was created")
			klog.InfoS("role was successfully created for member cluster", "role", roleName, "memberCluster", memberCluster.Name)
			return role.Name, nil
		}
		return "", err
	}
	expectedRole := createRole(roleName, namespaceName)
	if !cmp.Equal(role.Rules, expectedRole.Rules) {
		klog.InfoS("the role has more or less permissions than expected, hence it will be updated", "role", roleName, "memberCluster", memberCluster.Name)
		if err := r.Client.Update(ctx, &expectedRole); err != nil {
			klog.ErrorS(err, "cannot update role for member cluster", "memberCluster", memberCluster.Name, "role", roleName)
			return "", err
		}
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, roleUpdated, "role was updated")
	}
	return role.Name, nil
}

// checkAndCreateRoleBinding checks to see if the Role binding exists for given memberClusterName and namespaceName
// if the Role binding doesn't exist it creates it.
func (r *Reconciler) checkAndCreateRoleBinding(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster, namespaceName string, roleName string, identity rbacv1.Subject) error {
	var rb rbacv1.RoleBinding
	// Role binding name is created using member cluster name
	roleBindingName := fmt.Sprintf(utils.RoleBindingNameFormat, memberCluster.Name)
	// Check to see if Role Binding exists for Member Cluster, if it doesn't exist create it.
	if err := r.Client.Get(ctx, types.NamespacedName{Name: roleBindingName, Namespace: namespaceName}, &rb); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("role binding doesn't exist for member cluster", "roleBinding", roleBindingName, "memberCluster", memberCluster.Name)
			rb := createRoleBinding(roleName, roleBindingName, namespaceName, identity)
			if err = r.Client.Create(ctx, &rb); err != nil {
				return err
			}
			r.recorder.Event(memberCluster, corev1.EventTypeNormal, roleBindingCreated, "role binding was created")
			klog.InfoS("role binding was successfully created for member cluster", "roleBinding", roleBindingName, "memberCluster", memberCluster.Name)
			return nil
		}
		return err
	}
	expectedRoleBinding := createRoleBinding(roleName, roleBindingName, namespaceName, identity)
	if !cmp.Equal(rb.Subjects, expectedRoleBinding.Subjects) && !cmp.Equal(rb.RoleRef, expectedRoleBinding.RoleRef) {
		klog.InfoS("the role binding is different from what is expected, hence it will be updated", "roleBinding", roleBindingName, "memberCluster", memberCluster.Name)
		if err := r.Client.Update(ctx, &expectedRoleBinding); err != nil {
			klog.ErrorS(err, "cannot update role binding for member cluster", "memberCluster", memberCluster.Name, "roleBinding", roleBindingName)
			return err
		}
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, roleBindingUpdated, "role binding was updated")
	}
	return nil
}

// checkAndCreateInternalMemberCluster checks to see if the internal member cluster exists for given memberClusterName and namespaceName
// if the internal member cluster doesn't exist it creates it.
func (r *Reconciler) checkAndCreateInternalMemberCluster(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster, namespaceName string) (*fleetv1alpha1.InternalMemberCluster, error) {
	var imc fleetv1alpha1.InternalMemberCluster
	if err := r.Client.Get(ctx, types.NamespacedName{Name: memberCluster.Name, Namespace: namespaceName}, &imc); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("creating the internal member cluster", "internalMemberCluster", memberCluster.Name, "memberCluster", memberCluster.Name)
			controllerBool := true
			ownerRef := metav1.OwnerReference{APIVersion: memberCluster.APIVersion, Kind: memberCluster.Kind, Name: memberCluster.Name, UID: memberCluster.UID, Controller: &controllerBool}
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
			if err = r.Client.Create(ctx, &imc); err != nil {
				return nil, err
			}
			r.recorder.Event(memberCluster, corev1.EventTypeNormal, internalMemberClusterCreated, "Internal member cluster was created")
			klog.InfoS("internal member cluster was created successfully", "internalMemberCluster", memberCluster.Name, "memberCluster", memberCluster.Name)
			return &imc, nil
		}
		return nil, err
	}
	return &imc, nil
}

// updateMemberClusterStatus is used to update member cluster status to indicate that the member cluster has Joined/Left.
func (r *Reconciler) updateMemberClusterStatusWithRetryAsJoined(ctx context.Context, mc *fleetv1alpha1.MemberCluster, status fleetv1alpha1.InternalMemberClusterStatus) error {
	backOffPeriod := retry.DefaultBackoff
	backOffPeriod.Cap = time.Second * time.Duration(mc.Spec.HeartbeatPeriodSeconds)

	return retry.OnError(backOffPeriod,
		func(err error) bool {
			if apierrors.IsNotFound(err) || apierrors.IsInvalid(err) {
				return false
			}
			return true
		},
		func() error {
			patch := client.MergeFrom(mc.DeepCopyObject().(client.Object))
			mc.Status.Capacity = status.Capacity
			mc.Status.Allocatable = status.Allocatable
			markMemberClusterJoined(r.recorder, mc)
			markMemberClusterHeartbeatReceived(r.recorder, mc)
			err := r.Client.Status().Patch(ctx, mc, patch, client.FieldOwner(mc.GetUID()))
			if err != nil {
				klog.ErrorS(err, "cannot update member cluster status", "memberCluster", mc.Name)
			}
			return err
		})
}

// updateMemberClusterStatus is used to update member cluster status to indicate that the member cluster has Joined/Left.
func (r *Reconciler) updateMemberClusterStatusWithRetryAsLeft(ctx context.Context, mc *fleetv1alpha1.MemberCluster) error {
	backOffPeriod := retry.DefaultBackoff
	backOffPeriod.Cap = time.Second * time.Duration(mc.Spec.HeartbeatPeriodSeconds)

	return retry.OnError(backOffPeriod,
		func(err error) bool {
			if apierrors.IsNotFound(err) || apierrors.IsInvalid(err) {
				return false
			}
			return true
		},
		func() error {
			patch := client.MergeFrom(mc.DeepCopyObject().(client.Object))
			markMemberClusterLeft(r.recorder, mc)
			err := r.Client.Status().Patch(ctx, mc, patch, client.FieldOwner(mc.GetUID()))
			if err != nil {
				klog.ErrorS(err, "cannot update member cluster status", "memberCluster", mc.Name)
			}
			return err
		})
}

// updateInternalMemberClusterSpec is used to update the internal member cluster's spec to Leave.
func (r *Reconciler) updateInternalMemberClusterSpec(ctx context.Context, memberCluster *fleetv1alpha1.MemberCluster) error {
	var imc fleetv1alpha1.InternalMemberCluster
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster.Name)
	if err := r.Client.Get(ctx, types.NamespacedName{Name: memberCluster.Name, Namespace: namespaceName}, &imc); err != nil {
		klog.InfoS("Internal Member cluster doesn't exist for member cluster", "memberCluster", memberCluster.Name, "internalMemberCluster", memberCluster.Name)
		return err
	}
	if imc.Spec.State == fleetv1alpha1.ClusterStateLeave {
		return nil
	}
	patch := client.MergeFrom(imc.DeepCopyObject().(client.Object))
	imc.Spec.State = fleetv1alpha1.ClusterStateLeave
	if err := r.Client.Patch(ctx, &imc, patch, client.FieldOwner(imc.GetUID())); err != nil {
		klog.InfoS("Internal Member cluster cannot be updated", "memberCluster", memberCluster.Name, "internalMemberCluster", memberCluster.Name)
		return err
	}
	r.recorder.Event(memberCluster, corev1.EventTypeNormal, internalMemberClusterSpecUpdated, "internal member cluster spec is marked as leave")
	return nil
}

// addMemberClusterFinalizers is used to add finalizer strings for all the resources created for a member cluster.
func (r *Reconciler) addNamespaceFinalizer(ctx context.Context, mc *fleetv1alpha1.MemberCluster) error {
	if !controllerutil.ContainsFinalizer(mc, namespaceFinalizer) {
		patch := client.MergeFrom(mc.DeepCopyObject().(client.Object))
		controllerutil.AddFinalizer(mc, namespaceFinalizer)
		return r.Client.Patch(ctx, mc, patch, client.FieldOwner(mc.GetUID()))
	}
	return nil
}

// removeMemberClusterFinalizers is used to remove finalizer strings for all the resources created for a member cluster.
func (r *Reconciler) removeNamespaceFinalizer(ctx context.Context, mc *fleetv1alpha1.MemberCluster) error {
	if controllerutil.ContainsFinalizer(mc, namespaceFinalizer) {
		patch := client.MergeFrom(mc.DeepCopyObject().(client.Object))
		controllerutil.RemoveFinalizer(mc, namespaceFinalizer)
		return r.Client.Patch(ctx, mc, patch, client.FieldOwner(mc.GetUID()))
	}
	return nil
}

// deleteNamespace is used to delete the namespace created for a member cluster.
func (r *Reconciler) deleteNamespace(ctx context.Context, mc *fleetv1alpha1.MemberCluster) error {
	var namespace corev1.Namespace
	namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	if err := r.Client.Get(ctx, types.NamespacedName{Name: namespaceName}, &namespace); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		klog.InfoS("Namespace cannot be retrieved for deletion", "memberCluster", mc.Name, "namespace", namespaceName)
	}
	if err := r.Client.Delete(ctx, &namespace); err != nil {
		klog.InfoS("Namespace cannot be deleted", "memberCluster", mc.Name, "namespace", namespaceName)
		return err
	}
	r.recorder.Event(mc, corev1.EventTypeNormal, namespaceDeleted, "namespace is deleted for member cluster")
	return nil
}

// markMemberClusterJoined is used to the update the status of the member cluster to have the joined condition.
func markMemberClusterJoined(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.InfoS("mark the member Cluster as Joined", "memberService", mc.GetName())
	recorder.Event(mc, corev1.EventTypeNormal, memberClusterJoined, "member cluster is joined")
	joinedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMemberClusterJoin,
		Status:             metav1.ConditionTrue,
		Reason:             memberClusterJoined,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(joinedCondition)
}

// markMemberClusterHeartbeatReceived is used to update the status of the member cluster to have the heart beat received condition.
func markMemberClusterHeartbeatReceived(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.InfoS("mark member cluster heartbeat received", "memberCluster", mc.GetName())
	recorder.Event(mc, corev1.EventTypeNormal, heartBeatReceived, "member cluster heartbeat received")
	heartBeatReceivedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat,
		Status:             metav1.ConditionTrue,
		Reason:             heartBeatReceived,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(heartBeatReceivedCondition, utils.ReconcileSuccessCondition())
}

// markMemberClusterLeft is used to update the status of the member cluster to have the left condition.
func markMemberClusterLeft(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.InfoS("mark member cluster left", "memberCluster", mc.GetName())
	recorder.Event(mc, corev1.EventTypeNormal, memberClusterLeft, "member cluster has left")
	leftCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMemberClusterJoin,
		Status:             metav1.ConditionFalse,
		Reason:             memberClusterLeft,
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
