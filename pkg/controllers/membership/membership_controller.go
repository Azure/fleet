/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membership

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	fleetv1alpha1 "github.com/Azure/fleet/apis/v1alpha1"
)

// Reconciler reconciles a Membership object
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberships,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberships/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fleet.azure.com,resources=memberships/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Membership object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.Membership{}).
		Complete(r)
}
