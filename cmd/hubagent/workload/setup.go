/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workload

import (
	"context"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/cmd/hubagent/options"
	"go.goms.io/fleet/pkg/controllers/clusterresourceplacement"
	"go.goms.io/fleet/pkg/controllers/clusterresourceplacementwatcher"
	"go.goms.io/fleet/pkg/controllers/clusterschedulingpolicysnapshot"
	"go.goms.io/fleet/pkg/controllers/memberclusterplacement"
	"go.goms.io/fleet/pkg/controllers/resourcechange"
	"go.goms.io/fleet/pkg/controllers/rollout"
	"go.goms.io/fleet/pkg/controllers/workgenerator"
	"go.goms.io/fleet/pkg/resourcewatcher"
	"go.goms.io/fleet/pkg/scheduler"
	"go.goms.io/fleet/pkg/scheduler/clustereligibilitychecker"
	"go.goms.io/fleet/pkg/scheduler/framework"
	"go.goms.io/fleet/pkg/scheduler/profile"
	"go.goms.io/fleet/pkg/scheduler/queue"
	schedulercrpwatcher "go.goms.io/fleet/pkg/scheduler/watchers/clusterresourceplacement"
	schedulercspswatcher "go.goms.io/fleet/pkg/scheduler/watchers/clusterschedulingpolicysnapshot"
	"go.goms.io/fleet/pkg/scheduler/watchers/membercluster"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
	"go.goms.io/fleet/pkg/utils/validator"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

const (
	crpControllerName         = "cluster-resource-placement-controller"
	crpControllerV1Alpha1Name = crpControllerName + "-v1alpha1"
	crpControllerV1Beta1Name  = crpControllerName + "-v1beta1"

	resourceChangeControllerName = "resource-change-controller"
	mcPlacementControllerName    = "memberCluster-placement-controller"
)

var (
	v1Alpha1RequiredGVKs = []schema.GroupVersionKind{
		fleetv1alpha1.GroupVersion.WithKind(fleetv1alpha1.MemberClusterKind),
		fleetv1alpha1.GroupVersion.WithKind(fleetv1alpha1.InternalMemberClusterKind),
		fleetv1alpha1.GroupVersion.WithKind(fleetv1alpha1.ClusterResourcePlacementKind),
		workv1alpha1.SchemeGroupVersion.WithKind(workv1alpha1.WorkKind),
	}

	v1Beta1RequiredGVKs = []schema.GroupVersionKind{
		clusterv1beta1.GroupVersion.WithKind(clusterv1beta1.MemberClusterKind),
		clusterv1beta1.GroupVersion.WithKind(clusterv1beta1.InternalMemberClusterKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterResourcePlacementKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterResourceBindingKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterResourceSnapshotKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterSchedulingPolicySnapshotKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.WorkKind),
	}
)

// SetupControllers set up the customized controllers we developed
func SetupControllers(ctx context.Context, wg *sync.WaitGroup, mgr ctrl.Manager, config *rest.Config, opts *options.Options) error {
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

	// Verify CRD installation status.
	if opts.EnableV1Alpha1APIs {
		for _, gvk := range v1Alpha1RequiredGVKs {
			if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
				klog.ErrorS(err, "unable to find the required CRD", "GVK", gvk)
				return err
			}
		}
	}

	if opts.EnableV1Beta1APIs {
		for _, gvk := range v1Beta1RequiredGVKs {
			if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
				klog.ErrorS(err, "unable to find the required CRD", "GVK", gvk)
				return err
			}
		}
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
	validator.ResourceInformer = dynamicInformerManager // webhook needs this to check resource scope

	// Set up  a custom controller to reconcile cluster resource placement
	crpc := &clusterresourceplacement.Reconciler{
		Client:                 mgr.GetClient(),
		Recorder:               mgr.GetEventRecorderFor(crpControllerName),
		RestMapper:             mgr.GetRESTMapper(),
		InformerManager:        dynamicInformerManager,
		DisabledResourceConfig: disabledResourceConfig,
		SkippedNamespaces:      skippedNamespaces,
		Scheme:                 mgr.GetScheme(),
		UncachedReader:         mgr.GetAPIReader(),
	}

	rateLimiter := options.DefaultControllerRateLimiter(opts.RateLimiterOpts)
	var clusterResourcePlacementControllerV1Alpha1 controller.Controller
	var clusterResourcePlacementControllerV1Beta1 controller.Controller

	if opts.EnableV1Alpha1APIs {
		klog.Info("Setting up clusterResourcePlacement v1alpha1 controller")
		clusterResourcePlacementControllerV1Alpha1 = controller.NewController(crpControllerV1Alpha1Name, controller.NamespaceKeyFunc, crpc.ReconcileV1Alpha1, rateLimiter)
	}

	if opts.EnableV1Beta1APIs {
		klog.Info("Setting up clusterResourcePlacement v1beta1 controller")
		clusterResourcePlacementControllerV1Beta1 = controller.NewController(crpControllerV1Beta1Name, controller.NamespaceKeyFunc, crpc.Reconcile, rateLimiter)
	}

	// Set up  a new controller to reconcile any resources in the cluster
	klog.Info("Setting up resource change controller")
	rcr := &resourcechange.Reconciler{
		DynamicClient:               dynamicClient,
		Recorder:                    mgr.GetEventRecorderFor(resourceChangeControllerName),
		RestMapper:                  mgr.GetRESTMapper(),
		InformerManager:             dynamicInformerManager,
		PlacementControllerV1Alpha1: clusterResourcePlacementControllerV1Alpha1,
		PlacementControllerV1Beta1:  clusterResourcePlacementControllerV1Beta1,
	}

	resourceChangeController := controller.NewController(resourceChangeControllerName, controller.ClusterWideKeyFunc, rcr.Reconcile, rateLimiter)

	var memberClusterPlacementController controller.Controller
	if opts.EnableV1Alpha1APIs {
		klog.Info("Setting up member cluster change controller")
		mcp := &memberclusterplacement.Reconciler{
			InformerManager:     dynamicInformerManager,
			PlacementController: clusterResourcePlacementControllerV1Alpha1,
		}
		memberClusterPlacementController = controller.NewController(mcPlacementControllerName, controller.NamespaceKeyFunc, mcp.Reconcile, rateLimiter)
	}

	if opts.EnableV1Beta1APIs {
		klog.Info("Setting up clusterResourcePlacement watcher")
		if err := (&clusterresourceplacementwatcher.Reconciler{
			PlacementController: clusterResourcePlacementControllerV1Beta1,
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up the clusterResourcePlacement watcher")
			return err
		}

		klog.Info("Setting up clusterSchedulingPolicySnapshot controller")
		if err := (&clusterschedulingpolicysnapshot.Reconciler{
			Client:              mgr.GetClient(),
			PlacementController: clusterResourcePlacementControllerV1Beta1,
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up the clusterResourcePlacement watcher")
			return err
		}

		// Set up  a new controller to do rollout resources according to CRP rollout strategy
		klog.Info("Setting up rollout controller")
		if err := (&rollout.Reconciler{
			Client:         mgr.GetClient(),
			UncachedReader: mgr.GetAPIReader(),
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up rollout controller")
			return err
		}

		// Set up the work generator
		klog.Info("Setting up work generator")
		if err := (&workgenerator.Reconciler{
			Client: mgr.GetClient(),
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up work generator")
			return err
		}

		// Set up the scheduler
		klog.Info("Setting up scheduler")
		defaultProfile := profile.NewDefaultProfile()
		defaultFramework := framework.NewFramework(defaultProfile, mgr)
		defaultSchedulingQueue := queue.NewSimpleClusterResourcePlacementSchedulingQueue()
		defaultScheduler := scheduler.NewScheduler("DefaultScheduler", defaultFramework, defaultSchedulingQueue, mgr)
		klog.Info("Starting the scheduler")
		// Scheduler must run in a separate goroutine as Run() is a blocking call.
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Run() blocks and is set to exit on context cancellation.
			defaultScheduler.Run(ctx)

			klog.InfoS("The scheduler has exited")
		}()

		// Set up the watchers for the controller
		klog.Info("Setting up the clusterResourcePlacement watcher for scheduler")
		if err := (&schedulercrpwatcher.Reconciler{
			Client:             mgr.GetClient(),
			SchedulerWorkqueue: defaultSchedulingQueue,
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up clusterResourcePlacement watcher for scheduler")
			return err
		}

		klog.Info("Setting up the clusterSchedulingPolicySnapshot watcher for scheduler")
		if err := (&schedulercspswatcher.Reconciler{
			Client:             mgr.GetClient(),
			SchedulerWorkqueue: defaultSchedulingQueue,
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up clusterSchedulingPolicySnapshot watcher for scheduler")
			return err
		}

		klog.Info("Setting up the memberCluster watcher for scheduler")
		if err := (&membercluster.Reconciler{
			Client:                    mgr.GetClient(),
			SchedulerWorkQueue:        defaultSchedulingQueue,
			ClusterEligibilityChecker: clustereligibilitychecker.New(),
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up memberCluster watcher for scheduler")
			return err
		}
	}

	// Set up a runner that starts all the custom controllers we created above
	resourceChangeDetector := &resourcewatcher.ChangeDetector{
		DiscoveryClient: discoverClient,
		RESTMapper:      mgr.GetRESTMapper(),
		ClusterResourcePlacementControllerV1Alpha1: clusterResourcePlacementControllerV1Alpha1,
		ClusterResourcePlacementControllerV1Beta1:  clusterResourcePlacementControllerV1Beta1,
		ResourceChangeController:                   resourceChangeController,
		MemberClusterPlacementController:           memberClusterPlacementController,
		InformerManager:                            dynamicInformerManager,
		DisabledResourceConfig:                     disabledResourceConfig,
		SkippedNamespaces:                          skippedNamespaces,
		ConcurrentClusterPlacementWorker:           opts.ConcurrentClusterPlacementSyncs,
		ConcurrentResourceChangeWorker:             opts.ConcurrentResourceChangeSyncs,
	}

	if err := mgr.Add(resourceChangeDetector); err != nil {
		klog.ErrorS(err, "Failed to setup resource detector")
		return err
	}
	return nil
}
