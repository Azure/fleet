/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	namespaceCreated             = "NamespaceCreated"
	roleCreated                  = "RoleCreated"
	roleUpdated                  = "RoleUpdated"
	roleBindingCreated           = "RoleBindingCreated"
	roleBindingUpdated           = "RoleBindingUpdated"
	internalMemberClusterCreated = "InternalMemberClusterCreated"
	memberClusterJoined          = "MemberClusterJoined"
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
	// TODO: The current condition is for the Join workflow, another condition for Leave needs to be added.
	if mc.Spec.State == fleetv1alpha1.ClusterStateJoin {
		namespaceName, err := r.checkAndCreateNamespace(ctx, &mc)
		if err != nil {
			klog.ErrorS(err, "failed to check and create namespace for member cluster in hub agent", "memberCluster", mc.Name, "namespace", namespaceName)
			return ctrl.Result{}, err
		}

		roleName, err := r.checkAndCreateRole(ctx, &mc, namespaceName)
		if err != nil {
			klog.ErrorS(err, "failed to check and create role for member cluster in hub agent", "memberCluster", mc.Name, "role", roleName)
			return ctrl.Result{}, err
		}

		err = r.checkAndCreateRoleBinding(ctx, &mc, namespaceName, roleName, mc.Spec.Identity)
		if err != nil {
			klog.ErrorS(err, "failed to check and create role binding for member cluster in hub agent", "memberCluster", mc.Name, "roleBinding", fmt.Sprintf(utils.RoleBindingNameFormat, mc.Name))
			return ctrl.Result{}, err
		}

		imc, err := r.checkAndCreateInternalMemberCluster(ctx, &mc, namespaceName)
		if err != nil {
			klog.ErrorS(err, "failed to check and create internal member cluster %s in hub agent", "memberCluster", mc.Name, "internalMemberCluster", mc.Name)
			return ctrl.Result{}, err
		}

		if imc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterJoin) != nil && mc.GetCondition(fleetv1alpha1.ConditionTypeMemberClusterJoin) == nil {
			err := r.updateMemberClusterStatus(ctx, imc.Name, imc.Status)
			if err != nil {
				klog.ErrorS(err, "cannot update member cluster status as Joined", "internalMemberCluster", imc.Name)
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

// checkAndCreateRolebinding checks to see if the Role binding exists for given memberClusterName and namespaceName
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
			klog.InfoS("internal member cluster doesn't exist for member cluster", "internalMemberCluster", memberCluster.Name, "memberCluster", memberCluster.Name)
			controllerBool := true
			ownerRef := metav1.OwnerReference{APIVersion: memberCluster.APIVersion, Kind: memberCluster.Kind, Name: memberCluster.Name, UID: memberCluster.UID, Controller: &controllerBool}
			imc := fleetv1alpha1.InternalMemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       InternalMemberClusterKind.Kind,
					APIVersion: InternalMemberClusterKind.GroupVersion().Version,
				},
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
			return &imc, err
		}
		return nil, err
	}
	return &imc, nil
}

// updateMemberClusterStatus is used to update the status of the member cluster with the internal member cluster's status.
func (r *Reconciler) updateMemberClusterStatus(ctx context.Context, mcName string, status fleetv1alpha1.InternalMemberClusterStatus) error {
	var mc fleetv1alpha1.MemberCluster
	if err := r.Client.Get(ctx, types.NamespacedName{Name: mcName}, &mc); err != nil {
		klog.ErrorS(err, "failed retrieve member cluster", "memberCluster", mcName)
		return err
	}
	patch := client.MergeFrom(mc.DeepCopyObject().(client.Object))
	klog.InfoS("update member cluster status with internal member cluster status", "memberCluster", mcName)
	mc.Status.Capacity = status.Capacity
	mc.Status.Allocatable = status.Allocatable
	markMemberClusterJoined(r.recorder, &mc)
	return r.Client.Status().Patch(ctx, &mc, patch, client.FieldOwner(mc.GetUID()))
}

// markMemberClusterJoined is used to the update the status of the member cluster to have the joined condition.
func markMemberClusterJoined(recorder record.EventRecorder, mc apis.ConditionedObj) {
	klog.InfoS("mark the member Cluster as Joined",
		"namespace", mc.GetNamespace(), "memberService", mc.GetName())
	// Recording events show up on "describe"
	recorder.Event(mc, corev1.EventTypeNormal, memberClusterJoined, "member cluster is joined")
	joinedCondition := metav1.Condition{
		Type:               fleetv1alpha1.ConditionTypeMemberClusterJoin,
		Status:             metav1.ConditionTrue,
		Reason:             memberClusterJoined,
		ObservedGeneration: mc.GetGeneration(),
	}
	mc.SetConditions(joinedCondition)
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
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
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
		TypeMeta: metav1.TypeMeta{
			Kind:       "RoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
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
