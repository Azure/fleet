/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package controllers

import (
	"context"

	"github.com/Azure/fleet/pkg/controllers/internalmembercluster"
	"github.com/Azure/fleet/pkg/controllers/membership"
	"github.com/pkg/errors"

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// Start Start the member controllers with the supplied config
func Start(ctx context.Context, memberCfg *rest.Config, setupLog logr.Logger, opts ctrl.Options) error {
	//TODO: Call getMSI to get Hub config
	var hubCfg *rest.Config = ctrl.GetConfigOrDie()

	hubMgr, err := ctrl.NewManager(hubCfg, opts)
	if err != nil {
		return errors.Wrap(err, "unable to start hub manager")
	}

	memberOpts := ctrl.Options{
		Scheme:             opts.Scheme,
		LeaderElection:     opts.LeaderElection,
		MetricsBindAddress: ":4848",
		Port:               8443,
	}
	memberMgr, err := ctrl.NewManager(memberCfg, memberOpts)
	if err != nil {
		return errors.Wrap(err, "unable to start member manager")
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(memberCfg, apiutil.WithLazyDiscovery)
	if err != nil {
		return errors.Wrap(err, "unable to start member manager")
	}

	if err = internalmembercluster.NewMemberInternalMemberReconciler(
		hubMgr.GetClient(), memberMgr.GetClient(), restMapper).SetupWithManager(memberMgr); err != nil {
		return errors.Wrap(err, "unable to create controller internalMemberCluster_member")
	}

	if err = (&membership.Reconciler{
		Client: memberMgr.GetClient(),
		Scheme: memberMgr.GetScheme(),
	}).SetupWithManager(memberMgr); err != nil {
		return errors.Wrap(err, "unable to create controller membership")
	}

	klog.Info("starting hub manager")
	defer klog.Info("shutting down hub manager")
	if err := hubMgr.Start(ctx); err != nil {
		return errors.Wrap(err, "problem running hub manager")
	}

	klog.Info("starting member manager")
	defer klog.Info("shutting down member manager")
	if err := memberMgr.Start(ctx); err != nil {
		return errors.Wrap(err, "problem running member manager")
	}
	return nil
}
