/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package main

import (
	"flag"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.goms.io/fleet/pkg/utils"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

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
)

var (
	joinSucceedCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "successful_join_cnt_hub_agent",
		Help: "counts the number of successful Join operations for hub agent",
	})
	joinFailCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "failed_join_cnt_hub_agent",
		Help: "counts the number of failed Join operations for hub agent",
	})
	leaveSucceedCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "successful_leave_cnt_hub_agent",
		Help: "counts the number of successful Leave operations for hub agent",
	})
	leaveFailCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "failed_leave_cnt_hub_agent",
		Help: "counts the number of failed Leave operations for hub agent",
	})
)

var (
	joinLeaveResultMetrics = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "join_leave_result",
		Help: "Number of failed and successful Join/leaves operations",
	}, []string{"operation", "result"})
)

var (
	metricsResultMap = map[bool]string{
		true:  utils.MetricsSuccessResult,
		false: utils.MetricsFailureResult,
	}
)

var (
	reportJoinLeaveResultMetric = func(operation utils.MetricsOperation, successful bool) {
		joinLeaveResultMetrics.With(prometheus.Labels{
			"operation": string(operation),
			"result":    metricsResultMap[successful],
		}).Inc()
	}
)

func init() {
	klog.InitFlags(nil)

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	metrics.Registry.MustRegister(joinSucceedCounter, joinFailCounter, leaveSucceedCounter, leaveFailCounter)
}

func main() {
	flag.Parse()

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     *metricsAddr,
		Port:                   9446,
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "984738fa.hub.fleet.azure.com",
	})
	if err != nil {
		klog.Error(err, "unable to start controller manager.")
		os.Exit(1)
	}

	klog.Info("starting hubagent")

	if err = (membercluster.NewReconciler(mgr.GetClient(), reportJoinLeaveResultMetric)).SetupWithManager(mgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "MemberCluster")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.Error(err, "problem starting manager")
		os.Exit(1)
	}
}
