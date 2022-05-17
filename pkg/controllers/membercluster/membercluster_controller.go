/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "github.com/Azure/fleet/apis/v1alpha1"
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
	var mc fleetv1alpha1.MemberCluster

	//Retrieve Member cluster object
	if err := r.Client.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, &mc); err != nil {
		klog.Error(err, "cannot get member cluster")
		return ctrl.Result{}, nil
	}

	if _, err := r.checkAndCreateNamespace(ctx, mc.Name); err != nil {
		klog.Error(err)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) checkAndCreateNamespace(ctx context.Context, memberClusterName string) (*corev1.Namespace, error) {
	var namespace corev1.Namespace
	// Namespace name is created using member cluster name.
	namespaceName := memberClusterName + "-namespace"
	// Check to see if namespace exists, if it doesn't exist create it.
	if err := r.Client.Get(ctx, types.NamespacedName{Name: namespaceName}, &namespace); err != nil {
		if apierrors.IsNotFound(err) {
			namespace := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			if err := r.Client.Create(ctx, &namespace); err != nil {
				return nil, err
			}
			klog.Info("namespace was successfully created")
		}
		return nil, err
	}
	return &namespace, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.MemberCluster{}).
		Complete(r)
}
