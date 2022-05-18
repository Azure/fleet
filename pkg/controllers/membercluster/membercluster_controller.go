/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
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

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	RoleKind           = "Role"
	RoleBindingKind    = "RoleBinding"
	NamespaceCreated   = "NamespaceCreated"
	RoleCreated        = "RoleCreated"
	RoleUpdated        = "RoleUpdated"
	RoleBindingCreated = "RoleBindingCreated"
	RoleBindingUpdated = "RoleBindingUpdated"
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

	// TODO: Reconcile method is still under development.
	if err := r.Client.Get(ctx, req.NamespacedName, &mc); err != nil {
		klog.ErrorS(err, "memberCluster", klog.KRef(req.Namespace, req.Name))
		return ctrl.Result{}, errors.Wrapf(client.IgnoreNotFound(err), "failed to get the member cluster %s in hub agent", req.Name)
	}

	namespaceName, err := r.checkAndCreateNamespace(ctx, &mc)
	if err != nil {
		klog.ErrorS(err, "memberCluster", klog.KRef(req.Namespace, req.Name))
		return ctrl.Result{}, errors.Wrapf(err, "failed to check and create namespace for member cluster %s in hub agent", req.Name)
	}

	roleName, err := r.checkAndCreateRole(ctx, &mc, namespaceName)
	if err != nil {
		klog.ErrorS(err, "memberCluster", klog.KRef(req.Namespace, req.Name), "namespace", namespaceName)
		return ctrl.Result{}, errors.Wrapf(err, "failed to check and create role for member cluster %s in hub agent", req.Name)
	}

	err = r.checkAndCreateRoleBinding(ctx, &mc, namespaceName, roleName, mc.Spec.Identity)
	if err != nil {
		klog.ErrorS(err, "memberCluster", klog.KRef(req.Namespace, req.Name), "namespace", namespaceName, "role", roleName)
		return ctrl.Result{}, errors.Wrapf(err, "failed to check and create role binding for member cluster %s in hub agent", req.Name)
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
			r.recorder.Event(memberCluster, corev1.EventTypeNormal, NamespaceCreated, "Namespace was created")
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
			r.recorder.Event(memberCluster, corev1.EventTypeNormal, RoleCreated, "role was created")
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
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, RoleUpdated, "role was updated")
	}
	return role.Name, nil
}

// checkAndCreateRolebinding checks to see if the Role binding exists for given memberClusterName
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
			r.recorder.Event(memberCluster, corev1.EventTypeNormal, RoleBindingCreated, "role binding was created")
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
		r.recorder.Event(memberCluster, corev1.EventTypeNormal, RoleBindingUpdated, "role binding was updated")
	}
	return nil
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
			Kind:       RoleKind,
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
		Kind:     RoleKind,
		Name:     roleName,
	}
	rb := rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       RoleBindingKind,
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
		Complete(r)
}
