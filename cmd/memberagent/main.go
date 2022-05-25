/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"flag"
	"os"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/controllers/memberinternalmembercluster"
	"go.goms.io/fleet/pkg/controllers/membership"
	//+kubebuilder:scaffold:imports
)

var (
	scheme               = runtime.NewScheme()
	hubProbeAddr         = flag.String("hub-health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	hubMetricsAddr       = flag.String("hub-metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	probeAddr            = flag.String("health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	metricsAddr          = flag.String("metrics-bind-address", ":8090", "The address the metric endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
)

func init() {
	klog.InitFlags(nil)

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	flag.Parse()

	hubOpts := ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     *hubMetricsAddr,
		Port:                   8443,
		HealthProbeBindAddress: *hubProbeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "984738fa.hub.fleet.azure.com",
	}

	//+kubebuilder:scaffold:builder

	klog.Info("starting memebragent")
	// TODO: to hook MSI token to get hub config
	if err := Start(ctrl.SetupSignalHandler(), ctrl.GetConfigOrDie(), hubOpts); err != nil {
		klog.Error(err, "problem running controllers")
		os.Exit(1)
	}
}

// Start Start the member controllers with the supplied config
func Start(ctx context.Context, hubCfg *rest.Config, hubOpts ctrl.Options) error {
	hubMrg, err := ctrl.NewManager(hubCfg, hubOpts)
	if err != nil {
		return errors.Wrap(err, "unable to start hub manager")
	}

	memberOpts := ctrl.Options{
		Scheme:                 hubOpts.Scheme,
		MetricsBindAddress:     *metricsAddr,
		Port:                   8446,
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         hubOpts.LeaderElection,
		LeaderElectionID:       "984738fa.member.fleet.azure.com",
	}
	memberMgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), memberOpts)
	if err != nil {
		return errors.Wrap(err, "unable to start member manager")
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(hubCfg, apiutil.WithLazyDiscovery)
	if err != nil {
		return errors.Wrap(err, "unable to start member manager")
	}

	if err = memberinternalmembercluster.NewMemberReconciler(
		hubMrg.GetClient(), memberMgr.GetClient(),
		restMapper).SetupWithManager(hubMrg); err != nil {
		return errors.Wrap(err, "unable to create controller hub_member")
	}

	if err := hubMrg.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up health check for hub manager")
		os.Exit(1)
	}
	if err := memberMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up ready check for hub manager")

		os.Exit(1)
	}

	if err = (&membership.Reconciler{
		Client: memberMgr.GetClient(),
		Scheme: memberMgr.GetScheme(),
	}).SetupWithManager(memberMgr); err != nil {
		return errors.Wrap(err, "unable to create controller membership")
	}

	if err := memberMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up health check for member manager")
		os.Exit(1)
	}
	if err := memberMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up ready check for member manager")

		os.Exit(1)
	}

	klog.Info("starting hub manager")
	startErr := make(chan error)
	go func() {
		defer klog.Info("shutting down hub manager")
		err := hubMrg.Start(ctx)
		if err != nil {
			startErr <- errors.Wrap(err, "problem starting hub manager")
			return
		}
	}()

	klog.Info("starting member manager")
	defer klog.Info("shutting down member manager")
	if err := memberMgr.Start(ctx); err != nil {
		return errors.Wrap(err, "problem starting member manager")
	}
	return nil
}
