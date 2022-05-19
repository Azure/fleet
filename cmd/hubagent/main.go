/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package main

import (
	"flag"
	"os"

	fleetv1alpha1 "github.com/Azure/fleet/apis/v1alpha1"
	"github.com/Azure/fleet/pkg/controllers/internalmembercluster"
	"github.com/Azure/fleet/pkg/controllers/membercluster"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	//+kubebuilder:scaffold:imports
)

var (
	scheme               = runtime.NewScheme()
	setupLog             = ctrl.Log.WithName("setup")
	metricsAddr          = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", false,
		"Enable leader election for controller manager."+
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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: *metricsAddr,
		Port:               9446,
		LeaderElection:     *enableLeaderElection,
	})
	if err != nil {
		setupLog.Error(err, "unable to start controller manager.")
		os.Exit(1)
	}

	klog.Info("starting hubagent")

	if err = (&membercluster.Reconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "MemberCluster")
		os.Exit(1)
	}

	if err = (&internalmembercluster.HubInternalMemberReconciler{
		HubClient: mgr.GetClient(),
	}).SetupWithManager(mgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "internalMemberCluster_hub")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
