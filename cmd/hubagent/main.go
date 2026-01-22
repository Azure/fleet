/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"sync"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	clusterinventory "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/cmd/hubagent/options"
	"go.goms.io/fleet/cmd/hubagent/workload"
	mcv1beta1 "go.goms.io/fleet/pkg/controllers/membercluster/v1beta1"
	readiness "go.goms.io/fleet/pkg/utils/informer/readiness"
	"go.goms.io/fleet/pkg/utils/validator"
	"go.goms.io/fleet/pkg/webhook"
	"go.goms.io/fleet/pkg/webhook/managedresource"
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
	FleetWebhookPort = 9443
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(placementv1beta1.AddToScheme(scheme))
	utilruntime.Must(clusterv1beta1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(fleetnetworkingv1alpha1.AddToScheme(scheme))
	utilruntime.Must(placementv1alpha1.AddToScheme(scheme))
	utilruntime.Must(clusterinventory.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
	klog.InitFlags(nil)
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

	// Set up controller-runtime logger
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	config := ctrl.GetConfigOrDie()
	config.QPS, config.Burst = float32(opts.HubQPS), opts.HubBurst

	mgrOpts := ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			SyncPeriod:       &opts.ResyncPeriod.Duration,
			DefaultTransform: cache.TransformStripManagedFields(),
		},
		LeaderElection:             opts.LeaderElection.LeaderElect,
		LeaderElectionID:           opts.LeaderElection.ResourceName,
		LeaderElectionNamespace:    opts.LeaderElection.ResourceNamespace,
		LeaderElectionResourceLock: opts.LeaderElection.ResourceLock,
		HealthProbeBindAddress:     opts.HealthProbeAddress,
		Metrics: metricsserver.Options{
			BindAddress: opts.MetricsBindAddress,
		},
		WebhookServer: ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port:    FleetWebhookPort,
			CertDir: webhook.FleetWebhookCertDir,
		}),
	}
	if opts.EnablePprof {
		mgrOpts.PprofBindAddress = fmt.Sprintf(":%d", opts.PprofPort)
	}
	mgr, err := ctrl.NewManager(config, mgrOpts)
	if err != nil {
		klog.ErrorS(err, "unable to start controller manager.")
		exitWithErrorFunc()
	}

	klog.V(2).InfoS("starting hubagent")
	if opts.EnableV1Beta1APIs {
		klog.Info("Setting up memberCluster v1beta1 controller")
		if err = (&mcv1beta1.Reconciler{
			Client:                  mgr.GetClient(),
			NetworkingAgentsEnabled: opts.NetworkingAgentsEnabled,
			MaxConcurrentReconciles: int(math.Ceil(float64(opts.MaxFleetSizeSupported) / 100)), //one member cluster reconciler routine per 100 member clusters
			ForceDeleteWaitTime:     opts.ForceDeleteWaitTime.Duration,
		}).SetupWithManager(mgr, "membercluster-controller"); err != nil {
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
		// Generate webhook configuration with certificates
		webhookConfig, err := webhook.NewWebhookConfigFromOptions(mgr, opts, FleetWebhookPort)
		if err != nil {
			klog.ErrorS(err, "unable to create webhook config")
			exitWithErrorFunc()
		}

		// Setup webhooks with the manager
		if err := SetupWebhook(mgr, webhookConfig); err != nil {
			klog.ErrorS(err, "unable to set up webhook")
			exitWithErrorFunc()
		}

		// When using cert-manager, add a readiness check to ensure CA bundles are injected before marking ready.
		// This prevents the pod from accepting traffic before cert-manager has populated the webhook CA bundles,
		// which would cause webhook calls to fail.
		if opts.UseCertManager {
			if err := mgr.AddReadyzCheck("cert-manager-ca-injection", func(req *http.Request) error {
				return webhookConfig.CheckCAInjection(req.Context())
			}); err != nil {
				klog.ErrorS(err, "unable to set up cert-manager CA injection readiness check")
				exitWithErrorFunc()
			}
			klog.V(2).InfoS("Added cert-manager CA injection readiness check")
		}
	}

	ctx := ctrl.SetupSignalHandler()
	if err := workload.SetupControllers(ctx, &wg, mgr, config, opts); err != nil {
		klog.ErrorS(err, "unable to set up controllers")
		exitWithErrorFunc()
	}

	// Add readiness check for dynamic informer cache AFTER controllers are set up.
	// This ensures the discovery cache is populated before the hub agent is marked ready,
	// which is critical for all controllers that rely on dynamic resource discovery.
	// AddReadyzCheck adds additional readiness check instead of replacing the one registered earlier provided the name is different.
	// Both registered checks need to pass for the manager to be considered ready.
	if err := mgr.AddReadyzCheck("informer-cache", readiness.InformerReadinessChecker(validator.ResourceInformer)); err != nil {
		klog.ErrorS(err, "unable to set up informer cache readiness check")
		exitWithErrorFunc()
	}

	// +kubebuilder:scaffold:builder

	wg.Add(1)
	go func() {
		defer wg.Done()

		// Start() is a blocking call and it is set to exit on context cancellation.
		if err := mgr.Start(ctx); err != nil {
			klog.ErrorS(err, "problem starting manager")
			exitWithErrorFunc()
		}

		klog.InfoS("The controller manager has exited")
	}()

	// Wait for the cache to sync before creating managed resources
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		klog.Error("failed to wait for cache sync")
		exitWithErrorFunc()
	}

	if err := managedresource.EnsureVAP(ctx, mgr.GetClient()); err != nil {
		klog.Errorf("unable to create managed resource validating admission policy: %s", err)
		exitWithErrorFunc()
	}
	klog.Info("managed resource validating admission policy is successfully setup")

	// Wait for the controller manager and the scheduler to exit.
	wg.Wait()
}

// SetupWebhook registers the webhook config and webhook handlers with the manager.
func SetupWebhook(mgr manager.Manager, webhookConfig *webhook.Config) error {
	if err := mgr.Add(webhookConfig); err != nil {
		klog.ErrorS(err, "unable to add WebhookConfig")
		return err
	}
	if err := webhook.AddToManager(mgr, webhookConfig); err != nil {
		klog.ErrorS(err, "unable to register webhooks to the manager")
		return err
	}
	return nil
}
