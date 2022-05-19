/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"flag"
	"os"

	fleetv1alpha1 "github.com/Azure/fleet/apis/v1alpha1"

	"github.com/go-logr/logr"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/Azure/fleet/pkg/controllers/internalmembercluster"
	"github.com/Azure/fleet/pkg/controllers/membership"
	"github.com/pkg/errors"

	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	//+kubebuilder:scaffold:imports
)

var (
	scheme               = runtime.NewScheme()
	setupLog             = ctrl.Log.WithName("setup")
	metricsAddr          = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	flag.Parse()

	// Set the Klog format, as the Serialize format shouldn't be used anymore.
	// This makes sure that the logs are formatted correctly, i.e.:
	// * JSON logging format: msg isn't serialized twice
	// * text logging format: values are formatted with their .String() func.
	ctrl.SetLogger(klogr.NewWithOptions(klogr.WithFormat(klogr.FormatKlog)))

	opts := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: *metricsAddr,
		LeaderElection:     *enableLeaderElection,
		Port:               9443,
	}

	//+kubebuilder:scaffold:builder

	klog.Info("starting memebragent")
	if err := Start(ctrl.SetupSignalHandler(), ctrl.GetConfigOrDie(), setupLog, opts); err != nil {
		klog.Error(err, "problem running controllers")
		os.Exit(1)
	}
}

// Start Start the member controllers with the supplied config
func Start(ctx context.Context, membershipCfg *rest.Config, setupLog logr.Logger, opts ctrl.Options) error {
	internalMemberMrg, err := ctrl.NewManager(membershipCfg, opts)
	if err != nil {
		return errors.Wrap(err, "unable to start internalMember manager")
	}

	memberOpts := ctrl.Options{
		Scheme:             opts.Scheme,
		LeaderElection:     opts.LeaderElection,
		MetricsBindAddress: ":4848",
		Port:               8443,
	}
	membershipMgr, err := ctrl.NewManager(membershipCfg, memberOpts)
	if err != nil {
		return errors.Wrap(err, "unable to start membership manager")
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(membershipCfg, apiutil.WithLazyDiscovery)
	if err != nil {
		return errors.Wrap(err, "unable to start membership manager")
	}

	if err = internalmembercluster.NewMemberInternalMemberReconciler(
		internalMemberMrg.GetClient(), membershipMgr.GetClient(), restMapper).SetupWithManager(membershipMgr); err != nil {
		return errors.Wrap(err, "unable to create controller internalMemberCluster_member")
	}

	if err = (&membership.Reconciler{
		Client: membershipMgr.GetClient(),
		Scheme: membershipMgr.GetScheme(),
	}).SetupWithManager(membershipMgr); err != nil {
		return errors.Wrap(err, "unable to create controller membership")
	}

	klog.Info("starting internalMember manager")
	startErr := make(chan error)
	go func() {
		klog.Info("shutting down internalMember manager")
		err := internalMemberMrg.Start(ctx)
		if err != nil {
			startErr <- errors.Wrap(err, "problem running internalMember manager")
			return
		}
	}()

	klog.Info("starting membership manager")
	defer klog.Info("shutting down member manager")
	if err := membershipMgr.Start(ctx); err != nil {
		return errors.Wrap(err, "problem running member manager")
	}
	return nil
}
