/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package memberinternalmembercluster

import (
	"context"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	fleetv1alpha1 "github.com/Azure/fleet/apis/v1alpha1"
	"github.com/Azure/fleet/pkg/utils"
)

// MemberReconciler reconciles a InternalMemberCluster object in the member cluster.
type MemberReconciler struct {
	hubClient    client.Client
	memberClient client.Client
	restMapper   meta.RESTMapper
	recorder     record.EventRecorder
}

func NewMemberReconciler(hubClient client.Client, memberClient client.Client,
	restMapper meta.RESTMapper) *MemberReconciler {
	return &MemberReconciler{
		hubClient:    hubClient,
		memberClient: memberClient,
		restMapper:   restMapper,
	}
}

//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/finalizers,verbs=update

func (r *MemberReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO (mng): This is placeholder for implementation of GetConfigWithSecret
	var secret v1.Secret
	_, _ = utils.GetConfigWithSecret(secret)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MemberReconciler) SetupWithManager(mgr ctrl.Manager) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.InternalMemberCluster{}).
		Build(r)
	if err != nil {
		return errors.Wrap(err, "Failed to setup with a controller manager")
	}

	r.recorder = mgr.GetEventRecorderFor("InternalMemberCluster_member")
	return nil
}
