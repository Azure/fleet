/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "github.com/Azure/fleet/apis/v1alpha1"
	"github.com/Azure/fleet/pkg/utils"
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

	// TODO: Reconcile method is under development. The retrieved member cluster and namespace will be used in the following task to setup RBAC.
	if err := r.Client.Get(ctx, req.NamespacedName, &mc); err != nil {
		klog.ErrorS(err, "memberCluster", klog.KRef(req.Namespace, req.Name))
		return ctrl.Result{}, errors.Wrapf(client.IgnoreNotFound(err), "failed to get the member cluster %s in hub agent", req.Name)
	}

	if _, err := r.checkAndCreateNamespace(ctx, mc.Name); err != nil {
		klog.ErrorS(err, "memberCluster", klog.KRef(req.Namespace, req.Name))
		return ctrl.Result{}, errors.Wrapf(err, "failed to check and create namespace for member cluster %s in hub agent", req.Name)
	}

	return ctrl.Result{}, nil
}

// checkAndCreateNamespace checks to see if the namespace exists for given memberClusterName
// if the namespace doesn't exist it creates it.
func (r *Reconciler) checkAndCreateNamespace(ctx context.Context, mcName string) (*corev1.Namespace, error) {
	var namespace corev1.Namespace
	// Namespace name is created using member cluster name.
	nsName := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
	// Check to see if namespace exists, if it doesn't exist create it.
	if err := r.Client.Get(ctx, types.NamespacedName{Name: nsName}, &namespace); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("namespace doesn't exist for member cluster", "namespace", nsName, "memberCluster", mcName)
			namespace = corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
				},
			}
			if err = r.Client.Create(ctx, &namespace); err != nil {
				return nil, err
			}
			klog.InfoS("namespace was successfully created for member cluster", "namespace", nsName, "memberCluster", mcName)
			return &namespace, nil
		}
		return nil, err
	}
	return &namespace, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.MemberCluster{}).
		Build(r)
	if err != nil {
		return errors.Wrap(err, "Failed to setup with a controller manager")
	}

	r.recorder = mgr.GetEventRecorderFor("memberCluster")
	return nil
}
