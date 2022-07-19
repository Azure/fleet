/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package app

import (
	"context"
	"flag"
	"net"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/cmd/hub-manager/app/options"
	"go.goms.io/fleet/pkg/controllers/clusterresourceplacement"
	"go.goms.io/fleet/pkg/controllers/membercluster"
	"go.goms.io/fleet/pkg/controllers/resourcechange"
	"go.goms.io/fleet/pkg/resourcewatcher"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	clusterResourcePlacementName = "cluster-resource-placement-controller"
	resourceChangeName           = "resource-change-controller"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// NewhubManagerCommand creates a *cobra.Command object with default parameters
func NewhubManagerCommand(ctx context.Context) *cobra.Command {
	opts := options.NewOptions()

	cmd := &cobra.Command{
		Use:  "fleet-hub-manager",
		Long: `The fleet hub manager runs a bunch of controllers`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// validate options
			if errs := opts.Validate(); len(errs) != 0 {
				return errs.ToAggregate()
			}

			return Run(ctx, opts)
		},
	}

	fss := cliflag.NamedFlagSets{}

	genericFlagSet := fss.FlagSet("generic")
	genericFlagSet.AddGoFlagSet(flag.CommandLine)
	opts.AddFlags(genericFlagSet)

	// Set klog flags
	logsFlagSet := fss.FlagSet("logs")
	flagSetShim := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	klog.InitFlags(flagSetShim)
	logsFlagSet.AddGoFlagSet(flagSetShim)

	cmd.Flags().AddFlagSet(logsFlagSet)

	return cmd
}

// Run runs the controller-manager with options. This should never exit.
func Run(ctx context.Context, opts *options.Options) error {
	config := ctrl.GetConfigOrDie()
	config.QPS, config.Burst = opts.HubQPS, opts.HubBurst

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                     scheme,
		SyncPeriod:                 &opts.ResyncPeriod.Duration,
		LeaderElection:             opts.LeaderElection.LeaderElect,
		LeaderElectionID:           opts.LeaderElection.ResourceName,
		LeaderElectionNamespace:    opts.LeaderElection.ResourceNamespace,
		LeaderElectionResourceLock: opts.LeaderElection.ResourceLock,
		HealthProbeBindAddress:     net.JoinHostPort(opts.BindAddress, strconv.Itoa(opts.SecurePort)),
		LivenessEndpointName:       "/healthz",
		MetricsBindAddress:         opts.MetricsBindAddress,
	})
	if err != nil {
		klog.ErrorS(err, "failed to build controller manager")
		return err
	}

	if err = mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		klog.ErrorS(err, "failed to add health check endpoint")
		return err
	}
	klog.Info("starting the hub agent")

	if err = (&membercluster.Reconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "MemberCluster")
		return err
	}

	if err = setupCustomControllers(ctx, mgr, config, opts); err != nil {
		klog.ErrorS(err, "problem setting up the custom controllers")
		return err
	}

	if err = mgr.Start(ctx); err != nil {
		klog.ErrorS(err, "problem starting manager")
		return err
	}

	return nil
}

func setupCustomControllers(ctx context.Context, mgr ctrl.Manager, config *rest.Config, opts *options.Options) error {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		klog.ErrorS(err, "unable to create the dynamic client")
		return err
	}

	discoverClient := discovery.NewDiscoveryClientForConfigOrDie(config)
	if err != nil {
		klog.ErrorS(err, "unable to create the discover client")
		return err
	}

	if err = utils.CheckCRDInstalled(discoverClient,
		fleetv1alpha1.GroupVersion.WithKind(fleetv1alpha1.MemberClusterKind)); err != nil {
		klog.ErrorS(err, "unable to find the memberCluster definition")
		return err
	}

	if err = utils.CheckCRDInstalled(discoverClient,
		fleetv1alpha1.GroupVersion.WithKind(fleetv1alpha1.InternalMemberClusterKind)); err != nil {
		klog.ErrorS(err, "unable to find the InternalMemberCluster definition")
		return err
	}

	disabledResourceConfig := utils.NewDisabledResourceConfig()
	if err := disabledResourceConfig.Parse(opts.SkippedPropagatingAPIs); err != nil {
		// The program will never go here because the parameters have been checked
		return err
	}

	skippedNamespaces := make(map[string]bool, len(opts.SkippedPropagatingNamespaces))
	skippedNamespaces["fleet-system"] = true
	skippedNamespaces["kube-system"] = true
	skippedNamespaces["kube-public"] = true
	skippedNamespaces["kube-node-lease"] = true

	for _, ns := range opts.SkippedPropagatingNamespaces {
		skippedNamespaces[ns] = true
	}

	// the manager for all the dynamically created informers
	dynamicInformerManager := utils.NewInformerManager(dynamicClient, opts.ResyncPeriod.Duration, ctx.Done())

	// Setup a custom controller to reconcile cluster resource placement
	klog.Info("Setting up clusterResourcePlacement controller")
	crpc := &clusterresourceplacement.Reconciler{
		Recorder:               mgr.GetEventRecorderFor(clusterResourcePlacementName),
		RestMapper:             mgr.GetRESTMapper(),
		InformerManager:        dynamicInformerManager,
		DisabledResourceConfig: disabledResourceConfig,
	}

	ratelimiter := options.DefaultControllerRateLimiter(opts.RateLimiterOpts)
	clusterResourcePlacementController := controller.NewController(clusterResourcePlacementName, controller.MetaNamespaceKeyFunc, crpc.Reconcile, ratelimiter)
	if err != nil {
		klog.ErrorS(err, "unable to set up clusterResourcePlacement controller")
		return err
	}

	// Setup a new controller to reconcile any resources in the cluster
	klog.Info("Setting up resource change controller")
	rcr := &resourcechange.Reconciler{
		Client:              mgr.GetClient(),
		Recorder:            mgr.GetEventRecorderFor(resourceChangeName),
		DynamicClient:       dynamicClient,
		RestMapper:          mgr.GetRESTMapper(),
		InformerManager:     dynamicInformerManager,
		PlacementController: clusterResourcePlacementController,
	}

	resourceChangeController := controller.NewController(resourceChangeName, controller.ClusterWideKeyFunc, rcr.Reconcile, ratelimiter)
	if err != nil {
		klog.ErrorS(err, "unable to set up resource change  controller")
		return err
	}

	resourceChangeDetector := &resourcewatcher.ChangeDetector{
		DiscoveryClientSet:                 discoverClient,
		RESTMapper:                         mgr.GetRESTMapper(),
		ClusterResourcePlacementController: clusterResourcePlacementController,
		ResourceChangeController:           resourceChangeController,
		InformerManager:                    dynamicInformerManager,
		DisabledResourceConfig:             disabledResourceConfig,
		SkippedNamespaces:                  skippedNamespaces,
		ConcurrentClusterPlacementWorker:   opts.ConcurrentClusterPlacementSyncs,
		ConcurrentResourceChangeWorker:     opts.ConcurrentResourceChangeSyncs,
	}

	if err := mgr.Add(resourceChangeDetector); err != nil {
		klog.ErrorS(err, "Failed to setup resource detector")
		return err
	}
	return nil
}
