/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"flag"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/cmd/hubagent/options"
	"go.goms.io/fleet/cmd/hubagent/workload"
	"go.goms.io/fleet/pkg/controllers/membercluster"
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
	FleetWebhookCertDir = "/tmp/k8s-webhook-server/serving-certs"
	FleetWebhookPort    = 9443
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	utilruntime.Must(workv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
	klog.InitFlags(nil)

	metrics.Registry.MustRegister(fleetmetrics.JoinResultMetrics, fleetmetrics.LeaveResultMetrics, fleetmetrics.PlacementApplyFailedCount, fleetmetrics.PlacementApplySucceedCount)
}

func main() {
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

	klog.V(2).InfoS("starting hubagent")

	if err = (&membercluster.Reconciler{
		Client:                  mgr.GetClient(),
		NetworkingAgentsEnabled: opts.NetworkingAgentsEnabled,
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "MemberCluster")
		exitWithErrorFunc()
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
		if err := SetupWebhook(mgr, opts.WebhookClientConnectionType); err != nil {
			klog.ErrorS(err, "unable to set up webhook")
			exitWithErrorFunc()
		}
	}

	ctx := ctrl.SetupSignalHandler()
	if err := workload.SetupControllers(ctx, mgr, config, opts); err != nil {
		klog.ErrorS(err, "unable to set up ready check")
		exitWithErrorFunc()
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.Start(ctx); err != nil {
		klog.ErrorS(err, "problem starting manager")
		exitWithErrorFunc()
	}
}

// SetupWebhook generate the webhook cert and then set up the webhook configurator
func SetupWebhook(mgr manager.Manager, webhookClientConnectionType string) error {
	var connectionType options.WebhookClientConnectionType
	switch strings.ToLower(webhookClientConnectionType) {
	case "url":
		connectionType = options.URL
	case "service":
		connectionType = options.Service
	}

	// Generate self-signed key and crt files in FleetWebhookCertDir for the webhook server to start
	caPEM, err := webhook.GenCertificate(FleetWebhookCertDir)
	if err != nil {
		klog.ErrorS(err, "fail to generate certificates for webhook server")
		return err
	}

	if err := mgr.Add(&webhookApiserverConfigurator{
		mgr:            mgr,
		caPEM:          caPEM,
		port:           FleetWebhookPort,
		connectionType: &connectionType,
	}); err != nil {
		klog.ErrorS(err, "unable to add webhookApiserverConfigurator")
		return err
	}
	if err := webhook.AddToManager(mgr); err != nil {
		klog.ErrorS(err, "unable to register webhooks to the manager")
		return err
	}
	return nil
}

type webhookApiserverConfigurator struct {
	mgr            manager.Manager
	caPEM          []byte
	port           int
	connectionType *options.WebhookClientConnectionType
}

var _ manager.Runnable = &webhookApiserverConfigurator{}

func (c *webhookApiserverConfigurator) Start(ctx context.Context) error {
	klog.V(2).InfoS("setting up webhooks in apiserver from the leader")
	if err := webhook.CreateFleetWebhookConfiguration(ctx, c.mgr.GetClient(), c.caPEM, c.port, c.connectionType); err != nil {
		klog.ErrorS(err, "unable to setup webhook configurations in apiserver")
		return err
	}
	return nil
}
