/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"flag"
	"os"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/cmd/hubagent/options"
	"go.goms.io/fleet/cmd/hubagent/workload"
	mcv1alpha1 "go.goms.io/fleet/pkg/controllers/membercluster/v1alpha1"
	mcv1beta1 "go.goms.io/fleet/pkg/controllers/membercluster/v1beta1"
	fleetmetrics "go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/webhook"
	// +kubebuilder:scaffold:imports
)

var (
	scheme         = runtime.NewScheme()
	handleExitFunc = func() {
		klog.Flush()
	}

	exitWithErrorFunc = func() {
		handleExitFunc()
		os.Exit(1)
	}
)

const (
	FleetWebhookSvcName   = "fleetwebhook"
	FleetGuardRailSvcName = "fleetguardrail"
	FleetWebhookCertDir   = "/tmp/fleet-webhook-server/serving-certs"
	FleetGuardRailCertDir = "/tmp/fleet-guard-rail-server/serving-certs"
	FleetWebhookPort      = 9443
	FleetGuardRailPort    = 9444
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	utilruntime.Must(workv1alpha1.AddToScheme(scheme))
	utilruntime.Must(placementv1beta1.AddToScheme(scheme))
	utilruntime.Must(clusterv1beta1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
	klog.InitFlags(nil)

	metrics.Registry.MustRegister(fleetmetrics.JoinResultMetrics, fleetmetrics.LeaveResultMetrics, fleetmetrics.PlacementApplyFailedCount, fleetmetrics.PlacementApplySucceedCount)
}

func main() {
	// The wait group for synchronizing the steps (esp. the exit) of the controller manager and the scheduler.
	var wg sync.WaitGroup

	opts := options.NewOptions()
	opts.AddFlags(flag.CommandLine)

	flag.Parse()
	defer handleExitFunc()

	flag.VisitAll(func(f *flag.Flag) {
		klog.InfoS("flag:", "name", f.Name, "value", f.Value)
	})
	if errs := opts.Validate(); len(errs) != 0 {
		klog.ErrorS(errs.ToAggregate(), "invalid parameter")
		exitWithErrorFunc()
	}
	config := ctrl.GetConfigOrDie()
	config.QPS, config.Burst = float32(opts.HubQPS), opts.HubBurst

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                     scheme,
		SyncPeriod:                 &opts.ResyncPeriod.Duration,
		LeaderElection:             opts.LeaderElection.LeaderElect,
		LeaderElectionID:           opts.LeaderElection.ResourceName,
		LeaderElectionNamespace:    opts.LeaderElection.ResourceNamespace,
		LeaderElectionResourceLock: opts.LeaderElection.ResourceLock,
		HealthProbeBindAddress:     opts.HealthProbeAddress,
		MetricsBindAddress:         opts.MetricsBindAddress,
		Port:                       FleetWebhookPort,
		CertDir:                    FleetWebhookCertDir,
	})
	if err != nil {
		klog.ErrorS(err, "unable to start controller manager.")
		exitWithErrorFunc()
	}

	guardRailMgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                     scheme,
		SyncPeriod:                 &opts.ResyncPeriod.Duration,
		LeaderElection:             opts.LeaderElection.LeaderElect,
		LeaderElectionID:           opts.LeaderElection.ResourceName,
		LeaderElectionNamespace:    opts.LeaderElection.ResourceNamespace,
		LeaderElectionResourceLock: opts.LeaderElection.ResourceLock,
		HealthProbeBindAddress:     "0",
		MetricsBindAddress:         "0",
		Port:                       FleetGuardRailPort,
		CertDir:                    FleetGuardRailCertDir,
	})
	if err != nil {
		klog.ErrorS(err, "unable to start guard rail controller manager")
		exitWithErrorFunc()
	}

	klog.V(2).InfoS("starting hubagent")
	if opts.EnableV1Alpha1APIs {
		klog.Info("Setting up memberCluster v1alpha1 controller")
		if err = (&mcv1alpha1.Reconciler{
			Client:                  mgr.GetClient(),
			NetworkingAgentsEnabled: opts.NetworkingAgentsEnabled,
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "unable to create v1alpha1 controller", "controller", "MemberCluster")
			exitWithErrorFunc()
		}
	}
	if opts.EnableV1Beta1APIs {
		klog.Info("Setting up memberCluster v1beta1 controller")
		if err = (&mcv1beta1.Reconciler{
			Client:                  mgr.GetClient(),
			NetworkingAgentsEnabled: opts.NetworkingAgentsEnabled,
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "unable to create v1beta1 controller", "controller", "MemberCluster")
			exitWithErrorFunc()
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up health check")
		exitWithErrorFunc()
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up ready check")
		exitWithErrorFunc()
	}

	if opts.EnableWebhook {
		if err := SetupFleetWebhook(mgr, options.WebhookClientConnectionType(opts.WebhookClientConnectionType)); err != nil {
			klog.ErrorS(err, "unable to set up webhook")
			exitWithErrorFunc()
		}
	}

	if opts.EnableGuardRail {
		whiteListedUsers := strings.Split(opts.WhiteListedUsers, ",")
		if err := SetupFleetGuardRailWebhook(mgr, options.WebhookClientConnectionType(opts.WebhookClientConnectionType), whiteListedUsers); err != nil {
			klog.ErrorS(err, "unable to set up webhook")
			exitWithErrorFunc()
		}
	}

	ctx := ctrl.SetupSignalHandler()
	if err := workload.SetupControllers(ctx, &wg, mgr, config, opts); err != nil {
		klog.ErrorS(err, "unable to set up ready check")
		exitWithErrorFunc()
	}

	// +kubebuilder:scaffold:builder

	wg.Add(2)
	go func() {
		defer wg.Done()

		// Start() is a blocking call and it is set to exit on context cancellation.
		if err := mgr.Start(ctx); err != nil {
			klog.ErrorS(err, "problem starting manager")
			exitWithErrorFunc()
		}

		klog.InfoS("The controller manager has exited")
	}()

	go func() {
		defer wg.Done()

		if err := guardRailMgr.Start(ctx); err != nil {
			klog.ErrorS(err, "problem starting guard rail manager")
			exitWithErrorFunc()
		}

		klog.InfoS("the fleet guard rail manager has exited")
	}()

	// Wait for the controller manager and the scheduler to exit.
	wg.Wait()
}

// SetupFleetWebhook generates the webhook cert and then set up the webhook configurator for fleet.
func SetupFleetWebhook(mgr manager.Manager, webhookClientConnectionType options.WebhookClientConnectionType) error {
	// Generate self-signed key and crt files in FleetWebhookCertDir for the webhook server to start.
	w, err := webhook.NewWebhookConfig(mgr, FleetWebhookPort, &webhookClientConnectionType, FleetWebhookSvcName, FleetWebhookCertDir)
	if err != nil {
		klog.ErrorS(err, "fail to generate WebhookConfig")
		return err
	}
	if err = mgr.Add(w); err != nil {
		klog.ErrorS(err, "unable to add WebhookConfig")
		return err
	}
	if err = webhook.AddToFleetManager(mgr); err != nil {
		klog.ErrorS(err, "unable to register webhooks to the manager")
		return err
	}
	return nil
}

// SetupFleetGuardRailWebhook generates the webhook cert and then set up the webhook configurator for the fleet guard rail.
func SetupFleetGuardRailWebhook(mgr manager.Manager, webhookClientConnectionType options.WebhookClientConnectionType, whiteListedUsers []string) error {
	// Generate self-signed key and crt files in FleetWebhookCertDir for the webhook server to start.
	w, err := webhook.NewWebhookConfig(mgr, FleetWebhookPort, &webhookClientConnectionType, FleetGuardRailSvcName, FleetGuardRailCertDir)
	if err != nil {
		klog.ErrorS(err, "fail to generate WebhookConfig")
		return err
	}
	if err = mgr.Add(w); err != nil {
		klog.ErrorS(err, "unable to add WebhookConfig")
		return err
	}
	if err = webhook.AddToFleetGuardRailManager(mgr, whiteListedUsers); err != nil {
		klog.ErrorS(err, "unable to register webhooks to the manager")
		return err
	}
	return nil
}
