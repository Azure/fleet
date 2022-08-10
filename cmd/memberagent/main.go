/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
	workcontrollers "sigs.k8s.io/work-api/pkg/controllers"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/controllers/internalmembercluster"
	fleetmetrics "go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/utils"
	//+kubebuilder:scaffold:imports
)

var (
	scheme               = runtime.NewScheme()
	tlsClientInsecure    = flag.Bool("tls-insecure", false, "Enable TLSClientConfig.Insecure property. Enabling this will make the connection inSecure (should be 'true' for testing purpose only.)")
	hubProbeAddr         = flag.String("hub-health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	hubMetricsAddr       = flag.String("hub-metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	probeAddr            = flag.String("health-probe-bind-address", ":8091", "The address the probe endpoint binds to.")
	metricsAddr          = flag.String("metrics-bind-address", ":8090", "The address the metric endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	leaderElectionNamespace = flag.String("leader-election-namespace", "kube-system", "The namespace in which the leader election resource will be created.")
)

func init() {
	klog.InitFlags(nil)

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	utilruntime.Must(workv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	metrics.Registry.MustRegister(fleetmetrics.JoinResultMetrics, fleetmetrics.LeaveResultMetrics)
}

func main() {
	flag.Parse()

	defer klog.Flush()
	hubURL := os.Getenv("HUB_SERVER_URL")

	if hubURL == "" {
		klog.ErrorS(errors.New("hub server api cannot be empty"), "error has occurred retrieving HUB_SERVER_URL")
		os.Exit(1)
	}
	tokenFilePath := os.Getenv("CONFIG_PATH")

	if tokenFilePath == "" {
		klog.ErrorS(errors.New("hub token file path cannot be empty"), "error has occurred retrieving CONFIG_PATH")
		os.Exit(1)
	}

	mcName := os.Getenv("MEMBER_CLUSTER_NAME")
	if mcName == "" {
		klog.ErrorS(errors.New("Member cluster name cannot be empty"), "error has occurred retrieving MEMBER_CLUSTER_NAME")
		os.Exit(1)
	}

	mcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)

	err := retry.OnError(retry.DefaultRetry, func(e error) bool {
		return true
	}, func() error {
		// Stat returns file info. It will return
		// an error if there is no file.
		_, err := os.Stat(tokenFilePath)
		return err
	})
	if err != nil {
		klog.ErrorS(err, " cannot retrieve token file from the path %s", tokenFilePath)
		os.Exit(1)
	}

	var hubConfig rest.Config
	if *tlsClientInsecure {
		hubConfig = rest.Config{
			BearerTokenFile: tokenFilePath,
			Host:            hubURL,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: *tlsClientInsecure,
			},
		}
	} else {
		hubCA := os.Getenv("HUB_CERTIFICATE_AUTHORITY")
		if hubCA == "" {
			klog.ErrorS(errors.New("hub certificate authority cannot be empty"), "error has occurred retrieving HUB_CERTIFICATE_AUTHORITY")
			os.Exit(1)
		}
		decodedClusterCaCertificate, err := base64.StdEncoding.DecodeString(hubCA)
		if err != nil {
			klog.ErrorS(err, "cannot decode hub cluster certificate authority data")
			os.Exit(1)
		}
		hubConfig = rest.Config{
			BearerTokenFile: tokenFilePath,
			Host:            hubURL,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: *tlsClientInsecure,
				CAData:   decodedClusterCaCertificate,
			},
		}
	}

	hubOpts := ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      *hubMetricsAddr,
		Port:                    8443,
		HealthProbeBindAddress:  *hubProbeAddr,
		LeaderElection:          *enableLeaderElection,
		LeaderElectionNamespace: mcNamespace, // This requires we have access to resource "leases" in API group "coordination.k8s.io" under namespace $mcHubNamespace
		LeaderElectionID:        "136224848560.hub.fleet.azure.com",
		Namespace:               mcNamespace,
	}

	memberOpts := ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      *metricsAddr,
		Port:                    9443,
		HealthProbeBindAddress:  *probeAddr,
		LeaderElection:          hubOpts.LeaderElection,
		LeaderElectionNamespace: *leaderElectionNamespace,
		LeaderElectionID:        "136224848560.member.fleet.azure.com",
	}
	//+kubebuilder:scaffold:builder

	if err := Start(ctrl.SetupSignalHandler(), &hubConfig, hubOpts, memberOpts); err != nil {
		klog.ErrorS(err, "problem running controllers")
		os.Exit(1)
	}
}

// Start the member controllers with the supplied config
func Start(ctx context.Context, hubCfg *rest.Config, hubOpts, memberOpts ctrl.Options) error {
	hubMgr, err := ctrl.NewManager(hubCfg, hubOpts)
	if err != nil {
		return errors.Wrap(err, "unable to start hub manager")
	}

	memberConfig := ctrl.GetConfigOrDie()
	memberMgr, err := ctrl.NewManager(memberConfig, memberOpts)
	if err != nil {
		return errors.Wrap(err, "unable to start member manager")
	}

	if err := hubMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up health check for hub manager")
		os.Exit(1)
	}
	if err := hubMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up ready check for hub manager")
		os.Exit(1)
	}

	if err := memberMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up health check for member manager")
		os.Exit(1)
	}
	if err := memberMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up ready check for member manager")
		os.Exit(1)
	}

	spokeDynamicClient, err := dynamic.NewForConfig(memberConfig)
	if err != nil {
		klog.ErrorS(err, "unable to create spoke dynamic client")
		os.Exit(1)
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(memberConfig, apiutil.WithLazyDiscovery)
	if err != nil {
		klog.ErrorS(err, "unable to create spoke rest mapper")
		os.Exit(1)
	}

	if err != nil {
		klog.ErrorS(err, "unable to create spoke client")
		os.Exit(1)
	}

	if err = workcontrollers.NewWorkStatusReconciler(
		hubMgr.GetClient(),
		spokeDynamicClient,
		memberMgr.GetClient(),
		restMapper,
		hubMgr.GetEventRecorderFor("work_status_controller"),
		3,
	).SetupWithManager(hubMgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "WorkStatus")
		return err
	}

	if err = workcontrollers.NewApplyWorkReconciler(
		hubMgr.GetClient(),
		spokeDynamicClient,
		memberMgr.GetClient(),
		restMapper,
		hubMgr.GetEventRecorderFor("work_controller"),
		3,
	).SetupWithManager(hubMgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "Work")
		return err
	}

	if err = workcontrollers.NewFinalizeWorkReconciler(
		hubMgr.GetClient(),
		memberMgr.GetClient(),
		hubMgr.GetEventRecorderFor("WorkFinalizer_controller"),
	).SetupWithManager(hubMgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "WorkFinalize")
		return err
	}

	if err = internalmembercluster.NewReconciler(hubMgr.GetClient(), memberMgr.GetClient()).SetupWithManager(hubMgr); err != nil {
		return errors.Wrap(err, "unable to create controller hub_member")
	}

	klog.V(3).InfoS("starting hub manager")
	startErr := make(chan error)
	go func() {
		defer klog.V(3).InfoS("shutting down hub manager")
		err := hubMgr.Start(ctx)
		if err != nil {
			startErr <- errors.Wrap(err, "problem starting hub manager")
			return
		}
	}()

	klog.V(3).InfoS("starting member manager")
	defer klog.V(3).InfoS("shutting down member manager")
	if err := memberMgr.Start(ctx); err != nil {
		return errors.Wrap(err, "problem starting member manager")
	}

	return nil
}
