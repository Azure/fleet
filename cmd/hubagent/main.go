/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package main

import (
	"context"
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetmetrics "go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/webhook"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/controllers/membercluster"
	//+kubebuilder:scaffold:imports
)

var (
	scheme                  = runtime.NewScheme()
	probeAddr               = flag.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	metricsAddr             = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	enableLeaderElection    = flag.Bool("leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	leaderElectionNamespace = flag.String("leader-election-namespace", "kube-system", "The namespace in which the leader election resource will be created.")
	enableWebhook           = flag.Bool("enable-webhook", false, "If set, the fleet webhook is enabled.")
	networkingAgentsEnabled = flag.Bool("networking-agents-enabled", false, "Whether the networking agents are enabled or not.")
)

const (
	FleetWebhookCertDir = "/tmp/k8s-webhook-server/serving-certs"
	FleetWebhookPort    = 9443
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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      *metricsAddr,
		Port:                    FleetWebhookPort,
		CertDir:                 FleetWebhookCertDir,
		HealthProbeBindAddress:  *probeAddr,
		LeaderElection:          *enableLeaderElection,
		LeaderElectionNamespace: *leaderElectionNamespace,
		LeaderElectionID:        "13622se4848560.hub.fleet.azure.com",
	})
	if err != nil {
		klog.ErrorS(err, "unable to start controller manager.")
		os.Exit(1)
	}

	klog.V(2).InfoS("starting hubagent")

	if err = (&membercluster.Reconciler{
		Client:                  mgr.GetClient(),
		NetworkingAgentsEnabled: *networkingAgentsEnabled,
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "MemberCluster")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up ready check")
		os.Exit(1)
	}

	if *enableWebhook {
		// Generate self-signed key and crt files in FleetWebhookCertDir for the webhook server to start
		caPEM, err := webhook.GenCertificate(FleetWebhookCertDir)
		if err != nil {
			klog.ErrorS(err, "fail to generate certificates for webhook server")
			os.Exit(1)
		}

		if err := mgr.Add(&webhookApiserverConfigurator{
			mgr:   mgr,
			caPEM: caPEM,
			port:  FleetWebhookPort,
		}); err != nil {
			klog.ErrorS(err, "unable to add webhookApiserverConfigurator")
			os.Exit(1)
		}
		if err := webhook.AddToManager(mgr); err != nil {
			klog.ErrorS(err, "unable to register webhooks to the manager")
			os.Exit(1)
		}
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.ErrorS(err, "problem starting manager")
		os.Exit(1)
	}
}

type webhookApiserverConfigurator struct {
	mgr   manager.Manager
	caPEM []byte
	port  int
}

var _ manager.Runnable = &webhookApiserverConfigurator{}

func (c *webhookApiserverConfigurator) Start(ctx context.Context) error {
	klog.V(2).InfoS("setting up webhooks in apiserver from the leader")
	if err := webhook.CreateFleetWebhookConfiguration(ctx, c.mgr.GetClient(), c.caPEM, c.port); err != nil {
		klog.ErrorS(err, "unable to setup webhook configurations in apiserver")
		os.Exit(1)
	}
	return nil
}
