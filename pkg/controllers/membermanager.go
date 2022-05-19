/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package controllers

import (
	"context"
	"os"

	"github.com/Azure/fleet/pkg/controllers/internalmembercluster"
	"github.com/Azure/fleet/pkg/controllers/membership"
	"github.com/pkg/errors"

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// Start the controllers with the supplied config
func Start(ctx context.Context, hubCfg, memberCfg *rest.Config, setupLog logr.Logger, opts ctrl.Options) error {
	hubMgr, err := ctrl.NewManager(hubCfg, opts)
	if err != nil {
		klog.Error(err, "unable to start hub manager")
		os.Exit(1)
	}

	memberOpts := ctrl.Options{
		Scheme:             opts.Scheme,
		LeaderElection:     opts.LeaderElection,
		MetricsBindAddress: ":4848",
		Port:               8443,
	}
	memberMgr, err := ctrl.NewManager(memberCfg, memberOpts)
	if err != nil {
		klog.Error(err, "unable to start member manager")
		os.Exit(1)
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(memberCfg, apiutil.WithLazyDiscovery)
	if err != nil {
		klog.Error(err, "unable to start member manager")
		os.Exit(1)
	}

	if err = internalmembercluster.NewMemberInternalMemberReconciler(
		hubMgr.GetClient(), memberMgr.GetClient(), restMapper).SetupWithManager(memberMgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "internalMemberCluster_member")
		return err
	}

	if err = (&membership.Reconciler{
		Client: memberMgr.GetClient(),
		Scheme: memberMgr.GetScheme(),
	}).SetupWithManager(memberMgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "membership")
		os.Exit(1)
	}

	go func() error {
		klog.Info("starting hub manager")
		defer klog.Info("shutting down hub manager")
		if err := hubMgr.Start(ctx); err != nil {
			return errors.Wrap(err, "problem running hub manager")
		}
		return nil
	}()

	if err := memberMgr.Start(ctx); err != nil {
		klog.Error(err, "problem running member manager")
		return err
	}
	return nil
}
