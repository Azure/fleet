/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalmembercluster

import (
	"context"

	fleetv1alpha1 "github.com/Azure/fleet/apis/v1alpha1"

	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// MemberInternalMemberReconciler reconciles a InternalMemberCluster object in the member cluster.
type MemberInternalMemberReconciler struct {
	hubClient    client.Client
	memberClient client.Client
	restMapper   meta.RESTMapper
}

func NewMemberInternalMemberReconciler(hubClient client.Client, memberClient client.Client,
	restMapper meta.RESTMapper) *MemberInternalMemberReconciler {
	return &MemberInternalMemberReconciler{
		hubClient:    hubClient,
		memberClient: memberClient,
		restMapper:   restMapper,
	}
}

//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/finalizers,verbs=update

func (r *MemberInternalMemberReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MemberInternalMemberReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.InternalMemberCluster{}).
		Complete(r)
}
