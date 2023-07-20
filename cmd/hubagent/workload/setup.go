/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workload

import (
	"context"
	"strings"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/cmd/hubagent/options"
	"go.goms.io/fleet/pkg/controllers/clusterresourceplacement"
	"go.goms.io/fleet/pkg/controllers/memberclusterplacement"
	"go.goms.io/fleet/pkg/controllers/resourcechange"
	"go.goms.io/fleet/pkg/controllers/rollout"
	"go.goms.io/fleet/pkg/controllers/workgenerator"
	"go.goms.io/fleet/pkg/resourcewatcher"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
)

const (
	crpControllerName            = "cluster-resource-placement-controller"
	resourceChangeControllerName = "resource-change-controller"
	mcPlacementControllerName    = "memberCluster-placement-controller"
	rolloutControllerName        = "rollout-controller"
)

// SetupControllers set up the customized controllers we developed
func SetupControllers(ctx context.Context, mgr ctrl.Manager, config *rest.Config, opts *options.Options) error {
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

	// setup namespaces we skip propagation
	skippedNamespaces := make(map[string]bool)
	skippedNamespaces["default"] = true
	optionalSkipNS := strings.Split(opts.SkippedPropagatingNamespaces, ";")
	for _, ns := range optionalSkipNS {
		if len(ns) > 0 {
			klog.InfoS("user specified a namespace to skip", "namespace", ns)
			skippedNamespaces[ns] = true
		}
	}

	// the manager for all the dynamically created informers
	dynamicInformerManager := informer.NewInformerManager(dynamicClient, opts.ResyncPeriod.Duration, ctx.Done())
	fleetv1alpha1.ResourceInformer = dynamicInformerManager // webhook needs this to check resource scope

	// Set up  a custom controller to reconcile cluster resource placement
	klog.Info("Setting up clusterResourcePlacement controller")
	crpc := &clusterresourceplacement.Reconciler{
		Client:                 mgr.GetClient(),
		Recorder:               mgr.GetEventRecorderFor(crpControllerName),
		RestMapper:             mgr.GetRESTMapper(),
		InformerManager:        dynamicInformerManager,
		DisabledResourceConfig: disabledResourceConfig,
		SkippedNamespaces:      skippedNamespaces,
		Scheme:                 mgr.GetScheme(),
	}

	ratelimiter := options.DefaultControllerRateLimiter(opts.RateLimiterOpts)
	clusterResourcePlacementController := controller.NewController(crpControllerName, controller.NamespaceKeyFunc, crpc.ReconcileV1Alpha1, ratelimiter)
	if err != nil {
		klog.ErrorS(err, "unable to set up clusterResourcePlacement controller")
		return err
	}

	// Set up  a new controller to reconcile any resources in the cluster
	klog.Info("Setting up resource change controller")
	rcr := &resourcechange.Reconciler{
		DynamicClient:       dynamicClient,
		Recorder:            mgr.GetEventRecorderFor(resourceChangeControllerName),
		RestMapper:          mgr.GetRESTMapper(),
		InformerManager:     dynamicInformerManager,
		PlacementController: clusterResourcePlacementController,
	}

	resourceChangeController := controller.NewController(resourceChangeControllerName, controller.ClusterWideKeyFunc, rcr.Reconcile, ratelimiter)
	if err != nil {
		klog.ErrorS(err, "unable to set up resource change  controller")
		return err
	}

	// Set up  a new controller to reconcile memberCluster resources related to placement
	klog.Info("Setting up member cluster change controller")
	mcp := &memberclusterplacement.Reconciler{
		InformerManager:     dynamicInformerManager,
		PlacementController: clusterResourcePlacementController,
	}
	memberClusterPlacementController := controller.NewController(mcPlacementControllerName, controller.NamespaceKeyFunc, mcp.Reconcile, ratelimiter)
	if err != nil {
		klog.ErrorS(err, "unable to set up resource change  controller")
		return err
	}

	// Set up  a new controller to do rollout resources according to CRP rollout strategy
	klog.Info("Setting up rollout controller")
	if err = (&rollout.Reconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor(crpControllerName),
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to set up rollout controller")
		return err
	}

	// Set up the work generator
	klog.Info("Setting up work generator")
	if err = (&workgenerator.Reconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to set up work generator")
		return err
	}

	// Set up a runner that starts all the custom controllers we created above
	resourceChangeDetector := &resourcewatcher.ChangeDetector{
		DiscoveryClient:                    discoverClient,
		RESTMapper:                         mgr.GetRESTMapper(),
		ClusterResourcePlacementController: clusterResourcePlacementController,
		ResourceChangeController:           resourceChangeController,
		MemberClusterPlacementController:   memberClusterPlacementController,
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
