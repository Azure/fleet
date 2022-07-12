/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package main

import (
	"flag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	fleetmetrics "go.goms.io/fleet/pkg/metrics"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/controllers/membercluster"
	//+kubebuilder:scaffold:imports
)

var (
	scheme               = runtime.NewScheme()
	probeAddr            = flag.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	metricsAddr          = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	kubeconfigPath = flag.String("kubeconfig-path", "", "Absolute path to the kubeconfig file. Required only when running out of cluster.")
)

func init() {
	klog.InitFlags(nil)

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	metrics.Registry.MustRegister(fleetmetrics.JoinResultMetrics, fleetmetrics.LeaveResultMetrics)
}

func getConfigOrDie(kubeconfigPath string) *rest.Config {
	klog.V(5).InfoS("Get kubeconfig", "kubeconfigPath", kubeconfigPath)
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		klog.ErrorS(err, "Failed to build kubeconfig", "kubeconfigPath", kubeconfigPath)
		os.Exit(1)
	}
	klog.V(5).InfoS("Got kubeconfig", "host", config.Host)
	return config
}

func main() {
	flag.Parse()
	defer klog.Flush()

	mgr, err := ctrl.NewManager(getConfigOrDie(*kubeconfigPath), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     *metricsAddr,
		Port:                   9446,
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "984738fa.hub.fleet.azure.com",
	})
	if err != nil {
		klog.V(3).ErrorS(err, "unable to start controller manager.")
		os.Exit(1)
	}

	klog.V(2).InfoS("starting hubagent")

	if err = (&membercluster.Reconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr); err != nil {
		klog.V(3).ErrorS(err, "unable to create controller", "controller", "MemberCluster")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.V(2).ErrorS(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.V(2).ErrorS(err, "unable to set up ready check")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.V(2).ErrorS(err, "problem starting manager")
		os.Exit(1)
	}
}
