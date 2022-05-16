/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	fleetv1alpha1 "github.com/Azure/fleet/apis/v1alpha1"
)

const (
	rbacAPIGroup   = "rbac.authorization.k8s.io"
	rbacAPIVersion = "rbac.authorization.k8s.io/v1"
)

// Reconciler reconciles a MemberCluster object
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberclusters/finalizers,verbs=update

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	var mc fleetv1alpha1.MemberCluster
	var namespace corev1.Namespace
	var role rbacv1.Role
	var rolebinding rbacv1.RoleBinding
	// Namespace name is created from member cluster name
	namespaceIdentifier := req.Name + "-namespace"

	//Retrieve Member cluster object
	if err := r.Client.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, &mc); err != nil {
		klog.Error(err, "cannot get member cluster")
		return ctrl.Result{}, nil
	}

	// Check to see if namespace exists, if it doesn't exist create it.
	if err := r.Client.Get(ctx, types.NamespacedName{Name: namespaceIdentifier}, &namespace); err != nil {
		klog.Info(err, "namespace doesn't exist, namespace will be created for member cluster", mc.Name)
		namespace := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceIdentifier,
			},
		}
		if err := r.Client.Create(ctx, &namespace); err != nil {
			klog.Error(err, "namespace creation error")
			return ctrl.Result{}, nil
		}
		klog.Info("namespace was successfully created")
	}

	roleName := req.Name + "-role"
	// Check to see if Role exists for Member Cluster, if it doesn't exist create it.
	if err := r.Client.Get(ctx, types.NamespacedName{Name: roleName}, &role); err != nil {
		klog.Info(err, "role doesn't exist, role will be created for member cluster", mc.Name)
		verbs := []string{"get", "list", "watch", "update", "patch"}
		apiGroups := []string{"fleet.azure.com"}
		resources := []string{"internalmemberclusters"}
		rule := rbacv1.PolicyRule{
			Verbs:     verbs,
			APIGroups: apiGroups,
			Resources: resources,
		}
		role := rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Role",
				APIVersion: rbacAPIVersion,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: namespace.Name,
			},
			Rules: []rbacv1.PolicyRule{rule},
		}
		if err := r.Client.Create(ctx, &role); err != nil {
			klog.Error(err, "role creation error")
			return ctrl.Result{}, nil
		}
	}

	rolebindingName := req.Name + "-rolebinding"
	// Check to see if Role Binding exists for Member Cluster, if it doesn't exist create it.
	if err := r.Client.Get(ctx, types.NamespacedName{Name: rolebindingName}, &rolebinding); err != nil {
		klog.Info(err, "role binding doesn't exist, role binding will be created for member cluster", mc.Name)
		roleRef := rbacv1.RoleRef{
			APIGroup: rbacAPIGroup,
			Kind:     "Role",
			Name:     roleName,
		}
		rolebinding := rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RoleBinding",
				APIVersion: rbacAPIVersion,
			},
			ObjectMeta: metav1.ObjectMeta{},
			Subjects:   []rbacv1.Subject{mc.Spec.Identity},
			RoleRef:    roleRef,
		}
		if err := r.Client.Create(ctx, &rolebinding); err != nil {
			klog.Error(err, "role binding creation error")
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.MemberCluster{}).
		Complete(r)
}
