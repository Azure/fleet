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

package workload

import (
	"context"
	"math"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	clusterinventory "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/cmd/hubagent/options"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/bindingwatcher"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/clusterinventory/clusterprofile"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/clusterresourceplacementeviction"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/clusterresourceplacementstatuswatcher"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/memberclusterplacement"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/overrider"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/placement"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/placementwatcher"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/resourcechange"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/rollout"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/schedulingpolicysnapshot"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/updaterun"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workgenerator"
	"github.com/kubefleet-dev/kubefleet/pkg/resourcewatcher"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/clustereligibilitychecker"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/profile"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
	schedulerbindingwatcher "github.com/kubefleet-dev/kubefleet/pkg/scheduler/watchers/binding"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/watchers/membercluster"
	schedulerplacementwatcher "github.com/kubefleet-dev/kubefleet/pkg/scheduler/watchers/placement"
	schedulerspswatcher "github.com/kubefleet-dev/kubefleet/pkg/scheduler/watchers/schedulingpolicysnapshot"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/validator"
)

const (
	crpControllerName        = "cluster-resource-placement-controller"
	crpControllerV1Beta1Name = crpControllerName + "-v1beta1"
	rpControllerName         = "resource-placement-controller"
	placementControllerName  = "placement-controller"

	resourceChangeControllerName = "resource-change-controller"

	schedulerQueueName        = "scheduler-queue"
	mcPlacementControllerName = "memberCluster-placement-controller"
)

var (
	v1Beta1RequiredGVKs = []schema.GroupVersionKind{
		clusterv1beta1.GroupVersion.WithKind(clusterv1beta1.MemberClusterKind),
		clusterv1beta1.GroupVersion.WithKind(clusterv1beta1.InternalMemberClusterKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterResourcePlacementKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterResourceBindingKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterResourceSnapshotKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterSchedulingPolicySnapshotKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.WorkKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterResourceOverrideKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterResourceOverrideSnapshotKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ResourceOverrideKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ResourceOverrideSnapshotKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterResourcePlacementStatusKind),
	}

	// There's a prerequisite that v1Beta1RequiredGVKs must be installed too.
	rpRequiredGVKs = []schema.GroupVersionKind{
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ResourcePlacementKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ResourceBindingKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ResourceSnapshotKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.SchedulingPolicySnapshotKind),
	}

	clusterStagedUpdateRunGVKs = []schema.GroupVersionKind{
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterStagedUpdateRunKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterStagedUpdateStrategyKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterApprovalRequestKind),
	}

	stagedUpdateRunGVKs = []schema.GroupVersionKind{
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.StagedUpdateRunKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.StagedUpdateStrategyKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ApprovalRequestKind),
	}

	clusterInventoryGVKs = []schema.GroupVersionKind{
		clusterinventory.GroupVersion.WithKind("ClusterProfile"),
	}

	evictionGVKs = []schema.GroupVersionKind{
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterResourcePlacementEvictionKind),
		placementv1beta1.GroupVersion.WithKind(placementv1beta1.ClusterResourcePlacementDisruptionBudgetKind),
	}
)

// SetupControllers set up the customized controllers we developed
func SetupControllers(ctx context.Context, wg *sync.WaitGroup, mgr ctrl.Manager, config *rest.Config, opts *options.Options) error { //nolint:gocyclo
	// TODO: Try to reduce the complexity of this last measured at 33 (failing at > 30) and remove the // nolint:gocyclo
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		klog.ErrorS(err, "unable to create the dynamic client")
		return err
	}

	discoverClient := discovery.NewDiscoveryClientForConfigOrDie(config)
	// AllowedPropagatingAPIs and SkippedPropagatingAPIs are mutually exclusive.
	// If none of them are set, the resourceConfig by default stores a list of skipped propagation APIs.
	resourceConfig := utils.NewResourceConfig(opts.AllowedPropagatingAPIs != "")
	if err = resourceConfig.Parse(opts.AllowedPropagatingAPIs); err != nil {
		// The program will never go here because the parameters have been checked.
		return err
	}
	if err = resourceConfig.Parse(opts.SkippedPropagatingAPIs); err != nil {
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
	validator.RestMapper = mgr.GetRESTMapper()          // webhook needs this to validate GVK of resource selector

	// Set up  a custom controller to reconcile placement objects
	pc := &placement.Reconciler{
		Client:                                  mgr.GetClient(),
		Recorder:                                mgr.GetEventRecorderFor(placementControllerName),
		RestMapper:                              mgr.GetRESTMapper(),
		InformerManager:                         dynamicInformerManager,
		ResourceConfig:                          resourceConfig,
		SkippedNamespaces:                       skippedNamespaces,
		Scheme:                                  mgr.GetScheme(),
		UncachedReader:                          mgr.GetAPIReader(),
		ResourceSnapshotCreationMinimumInterval: opts.ResourceSnapshotCreationMinimumInterval,
		ResourceChangesCollectionDuration:       opts.ResourceChangesCollectionDuration,
	}

	rateLimiter := options.DefaultControllerRateLimiter(opts.RateLimiterOpts)
	var clusterResourcePlacementControllerV1Beta1 controller.Controller
	var resourcePlacementController controller.Controller
	var memberClusterPlacementController controller.Controller
	if opts.EnableV1Alpha1APIs {
		klog.Info("Setting up member cluster change controller")
		mcp := &memberclusterplacement.Reconciler{
			InformerManager: dynamicInformerManager,
		}
		memberClusterPlacementController = controller.NewController(mcPlacementControllerName, controller.NamespaceKeyFunc, mcp.Reconcile, rateLimiter)
	}

	if opts.EnableV1Beta1APIs {
		for _, gvk := range v1Beta1RequiredGVKs {
			if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
				klog.ErrorS(err, "unable to find the required CRD", "GVK", gvk)
				return err
			}
		}
		klog.Info("Setting up clusterResourcePlacement v1beta1 controller")
		clusterResourcePlacementControllerV1Beta1 = controller.NewController(crpControllerV1Beta1Name, controller.NamespaceKeyFunc, pc.Reconcile, rateLimiter)
		klog.Info("Setting up clusterResourcePlacement watcher")
		if err := (&placementwatcher.Reconciler{
			PlacementController: clusterResourcePlacementControllerV1Beta1,
		}).SetupWithManagerForClusterResourcePlacement(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up the clusterResourcePlacement watcher")
			return err
		}

		klog.Info("Setting up clusterResourceBinding watcher")
		if err := (&bindingwatcher.Reconciler{
			PlacementController: clusterResourcePlacementControllerV1Beta1,
			Client:              mgr.GetClient(),
		}).SetupWithManagerForClusterResourceBinding(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up the clusterResourceBinding watcher")
			return err
		}

		klog.Info("Setting up clusterResourcePlacementStatus watcher")
		if err := (&clusterresourceplacementstatuswatcher.Reconciler{
			Client:              mgr.GetClient(),
			PlacementController: clusterResourcePlacementControllerV1Beta1,
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up the clusterResourcePlacementStatus watcher")
			return err
		}

		klog.Info("Setting up clusterSchedulingPolicySnapshot watcher")
		if err := (&schedulingpolicysnapshot.Reconciler{
			Client:              mgr.GetClient(),
			PlacementController: clusterResourcePlacementControllerV1Beta1,
		}).SetupWithManagerForClusterSchedulingPolicySnapshot(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up the clusterSchedulingPolicySnapshot watcher")
			return err
		}

		if opts.EnableResourcePlacement {
			for _, gvk := range rpRequiredGVKs {
				if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
					klog.ErrorS(err, "unable to find the required CRD", "GVK", gvk)
					return err
				}
			}
			klog.Info("Setting up resourcePlacement controller")
			resourcePlacementController = controller.NewController(rpControllerName, controller.NamespaceKeyFunc, pc.Reconcile, rateLimiter)
			klog.Info("Setting up resourcePlacement watcher")
			if err := (&placementwatcher.Reconciler{
				PlacementController: resourcePlacementController,
			}).SetupWithManagerForResourcePlacement(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up the resourcePlacement watcher")
				return err
			}

			klog.Info("Setting up resourceBinding watcher")
			if err := (&bindingwatcher.Reconciler{
				PlacementController: resourcePlacementController,
				Client:              mgr.GetClient(),
			}).SetupWithManagerForResourceBinding(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up the resourceBinding watcher")
				return err
			}

			klog.Info("Setting up schedulingPolicySnapshot watcher")
			if err := (&schedulingpolicysnapshot.Reconciler{
				Client:              mgr.GetClient(),
				PlacementController: resourcePlacementController,
			}).SetupWithManagerForSchedulingPolicySnapshot(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up the schedulingPolicySnapshot watcher")
				return err
			}
		}

		// Set up a new controller to do rollout resources according to CRP/RP rollout strategy
		klog.Info("Setting up rollout controller")
		if err := (&rollout.Reconciler{
			Client:                  mgr.GetClient(),
			UncachedReader:          mgr.GetAPIReader(),
			MaxConcurrentReconciles: int(math.Ceil(float64(opts.MaxFleetSizeSupported)/30) * math.Ceil(float64(opts.MaxConcurrentClusterPlacement)/10)),
			InformerManager:         dynamicInformerManager,
		}).SetupWithManagerForClusterResourcePlacement(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up rollout controller for clusterResourcePlacement")
			return err
		}

		if opts.EnableResourcePlacement {
			if err := (&rollout.Reconciler{
				Client:                  mgr.GetClient(),
				UncachedReader:          mgr.GetAPIReader(),
				MaxConcurrentReconciles: int(math.Ceil(float64(opts.MaxFleetSizeSupported)/30) * math.Ceil(float64(opts.MaxConcurrentClusterPlacement)/10)),
				InformerManager:         dynamicInformerManager,
			}).SetupWithManagerForResourcePlacement(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up rollout controller for resourcePlacement")
				return err
			}
		}

		if opts.EnableEvictionAPIs {
			for _, gvk := range evictionGVKs {
				if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
					klog.ErrorS(err, "Unable to find the required CRD", "GVK", gvk)
					return err
				}
			}
			klog.Info("Setting up cluster resource placement eviction controller")
			if err := (&clusterresourceplacementeviction.Reconciler{
				Client:         mgr.GetClient(),
				UncachedReader: mgr.GetAPIReader(),
			}).SetupWithManager(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up cluster resource placement eviction controller")
				return err
			}
		}

		// Set up a controller to do staged update run, rolling out resources to clusters in a stage by stage manner.
		if opts.EnableStagedUpdateRunAPIs {
			for _, gvk := range clusterStagedUpdateRunGVKs {
				if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
					klog.ErrorS(err, "Unable to find the required CRD", "GVK", gvk)
					return err
				}
			}
			klog.Info("Setting up clusterStagedUpdateRun controller")
			if err = (&updaterun.Reconciler{
				Client:          mgr.GetClient(),
				InformerManager: dynamicInformerManager,
			}).SetupWithManagerForClusterStagedUpdateRun(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up clusterStagedUpdateRun controller")
				return err
			}

			if opts.EnableResourcePlacement {
				for _, gvk := range stagedUpdateRunGVKs {
					if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
						klog.ErrorS(err, "Unable to find the required CRD", "GVK", gvk)
						return err
					}
				}
				klog.Info("Setting up stagedUpdateRun controller")
				if err = (&updaterun.Reconciler{
					Client:          mgr.GetClient(),
					InformerManager: dynamicInformerManager,
				}).SetupWithManagerForStagedUpdateRun(mgr); err != nil {
					klog.ErrorS(err, "Unable to set up stagedUpdateRun controller")
					return err
				}
			}
		}

		// Set up the work generator
		klog.Info("Setting up work generator")
		if err := (&workgenerator.Reconciler{
			Client:                  mgr.GetClient(),
			MaxConcurrentReconciles: int(math.Ceil(float64(opts.MaxFleetSizeSupported)/10) * math.Ceil(float64(opts.MaxConcurrentClusterPlacement)/10)),
			InformerManager:         dynamicInformerManager,
		}).SetupWithManagerForClusterResourceBinding(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up work generator for clusterResourceBinding")
			return err
		}

		if opts.EnableResourcePlacement {
			if err := (&workgenerator.Reconciler{
				Client:                  mgr.GetClient(),
				MaxConcurrentReconciles: int(math.Ceil(float64(opts.MaxFleetSizeSupported)/10) * math.Ceil(float64(opts.MaxConcurrentClusterPlacement)/10)),
				InformerManager:         dynamicInformerManager,
			}).SetupWithManagerForResourceBinding(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up work generator for resourceBinding")
				return err
			}
		}

		// Set up the scheduler
		klog.Info("Setting up scheduler")
		defaultProfile := profile.NewDefaultProfile()
		defaultFramework := framework.NewFramework(defaultProfile, mgr)
		defaultSchedulingQueue := queue.NewSimplePlacementSchedulingQueue(
			queue.WithName(schedulerQueueName),
		)
		// we use one scheduler for every 10 concurrent placement
		defaultScheduler := scheduler.NewScheduler("DefaultScheduler", defaultFramework, defaultSchedulingQueue, mgr,
			int(math.Ceil(float64(opts.MaxFleetSizeSupported)/50)*math.Ceil(float64(opts.MaxConcurrentClusterPlacement)/10)))
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
		if err := (&schedulerplacementwatcher.Reconciler{
			Client:             mgr.GetClient(),
			SchedulerWorkQueue: defaultSchedulingQueue,
		}).SetupWithManagerForClusterResourcePlacement(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up clusterResourcePlacement watcher for scheduler")
			return err
		}

		klog.Info("Setting up the clusterSchedulingPolicySnapshot watcher for scheduler")
		if err := (&schedulerspswatcher.Reconciler{
			Client:             mgr.GetClient(),
			SchedulerWorkQueue: defaultSchedulingQueue,
		}).SetupWithManagerForClusterSchedulingPolicySnapshot(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up clusterSchedulingPolicySnapshot watcher for scheduler")
			return err
		}

		klog.Info("Setting up the clusterResourceBinding watcher for scheduler")
		if err := (&schedulerbindingwatcher.Reconciler{
			Client:             mgr.GetClient(),
			SchedulerWorkQueue: defaultSchedulingQueue,
		}).SetupWithManagerForClusterResourceBinding(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up clusterResourceBinding watcher for scheduler")
			return err
		}

		if opts.EnableResourcePlacement {
			klog.Info("Setting up the resourcePlacement watcher for scheduler")
			if err := (&schedulerplacementwatcher.Reconciler{
				Client:             mgr.GetClient(),
				SchedulerWorkQueue: defaultSchedulingQueue,
			}).SetupWithManagerForResourcePlacement(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up resourcePlacement watcher for scheduler")
				return err
			}

			klog.Info("Setting up the schedulingPolicySnapshot watcher for scheduler")
			if err := (&schedulerspswatcher.Reconciler{
				Client:             mgr.GetClient(),
				SchedulerWorkQueue: defaultSchedulingQueue,
			}).SetupWithManagerForSchedulingPolicySnapshot(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up schedulingPolicySnapshot watcher for scheduler")
				return err
			}

			klog.Info("Setting up the resourceBinding watcher for scheduler")
			if err := (&schedulerbindingwatcher.Reconciler{
				Client:             mgr.GetClient(),
				SchedulerWorkQueue: defaultSchedulingQueue,
			}).SetupWithManagerForResourceBinding(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up resourceBinding watcher for scheduler")
				return err
			}
		}

		klog.Info("Setting up the memberCluster watcher for scheduler")
		if err := (&membercluster.Reconciler{
			Client:                    mgr.GetClient(),
			SchedulerWorkQueue:        defaultSchedulingQueue,
			ClusterEligibilityChecker: clustereligibilitychecker.New(),
			EnableResourcePlacement:   opts.EnableResourcePlacement,
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up memberCluster watcher for scheduler")
			return err
		}

		// Set up the controllers for overriding resources.
		klog.Info("Setting up the clusterResourceOverride controller")
		if err := (&overrider.ClusterResourceReconciler{
			Reconciler: overrider.Reconciler{
				Client: mgr.GetClient(),
			},
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up clusterResourceOverride controller")
			return err
		}

		klog.Info("Setting up the resourceOverride controller")
		if err := (&overrider.ResourceReconciler{
			Reconciler: overrider.Reconciler{
				Client: mgr.GetClient(),
			},
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up resourceOverride controller")
			return err
		}

		// Verify cluster inventory CRD installation status.
		if opts.EnableClusterInventoryAPIs {
			for _, gvk := range clusterInventoryGVKs {
				if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
					klog.ErrorS(err, "unable to find the required CRD", "GVK", gvk)
					return err
				}
			}
			klog.Info("Setting up cluster profile controller")
			if err = (&clusterprofile.Reconciler{
				Client:                    mgr.GetClient(),
				ClusterProfileNamespace:   utils.FleetSystemNamespace,
				ClusterUnhealthyThreshold: opts.ClusterUnhealthyThreshold.Duration,
			}).SetupWithManager(mgr); err != nil {
				klog.ErrorS(err, "unable to set up ClusterProfile controller")
				return err
			}
		}
	}

	// Set up a new controller to reconcile any resources in the cluster
	klog.Info("Setting up resource change controller")
	rcr := &resourcechange.Reconciler{
		DynamicClient:               dynamicClient,
		Recorder:                    mgr.GetEventRecorderFor(resourceChangeControllerName),
		RestMapper:                  mgr.GetRESTMapper(),
		InformerManager:             dynamicInformerManager,
		PlacementControllerV1Beta1:  clusterResourcePlacementControllerV1Beta1,
		ResourcePlacementController: resourcePlacementController,
	}
	resourceChangeController := controller.NewController(resourceChangeControllerName, controller.ClusterWideKeyFunc, rcr.Reconcile, rateLimiter)

	// Set up a runner that starts all the custom controllers we created above
	resourceChangeDetector := &resourcewatcher.ChangeDetector{
		DiscoveryClient: discoverClient,
		RESTMapper:      mgr.GetRESTMapper(),
		ClusterResourcePlacementControllerV1Beta1: clusterResourcePlacementControllerV1Beta1,
		ResourcePlacementController:               resourcePlacementController,
		ResourceChangeController:                  resourceChangeController,
		MemberClusterPlacementController:          memberClusterPlacementController,
		InformerManager:                           dynamicInformerManager,
		ResourceConfig:                            resourceConfig,
		SkippedNamespaces:                         skippedNamespaces,
		ConcurrentPlacementWorker:                 int(math.Ceil(float64(opts.MaxConcurrentClusterPlacement) / 10)),
		ConcurrentResourceChangeWorker:            opts.ConcurrentResourceChangeSyncs,
	}

	if err := mgr.Add(resourceChangeDetector); err != nil {
		klog.ErrorS(err, "Failed to setup resource detector")
		return err
	}
	return nil
}
