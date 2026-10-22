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
	"time"

	"go.goms.io/fleet/pkg/clients/azure/compute"
	"go.goms.io/fleet/pkg/clients/httputil"
	"go.goms.io/fleet/pkg/propertychecker/azure"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/clusteraffinity"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	clusterinventory "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kfrolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/cmd/hubagent/options"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/bindingwatcher"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/clusterinventory/clusterprofile"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/clusterresourceplacementeviction"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/clusterresourceplacementstatuswatcher"
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
	"github.com/kubefleet-dev/kubefleet/pkg/utils/parallelizer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resourceeligibility"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/validator"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/controllers/annotationplacement"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/controllers/placementpolicy"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/controllers/stagedupdaterun"
	v1workgenerator "github.com/kubefleet-dev/kubefleet/pkg/v1/controllers/workgenerator"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/managers/placementresourcesnapshot"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

const (
	crpControllerName        = "cluster-resource-placement-controller"
	crpControllerV1Beta1Name = crpControllerName + "-v1beta1"
	rpControllerName         = "resource-placement-controller"
	placementControllerName  = "placement-controller"

	resourceChangeControllerName = "resource-change-controller"

	schedulerQueueName = "scheduler-queue"
)

// placementResourceSnapshotSlots is the snapshot manager's lock striping; its minimum.
const placementResourceSnapshotSlots int32 = 256

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

	placementPolicyGVKs = []schema.GroupVersionKind{
		kfplacementv1alpha1.GroupVersion.WithKind(kfplacementv1alpha1.PlacementPolicyKind),
		kfplacementv1alpha1.GroupVersion.WithKind(kfplacementv1alpha1.ClusterPlacementPolicyKind),
		kfplacementv1alpha1.GroupVersion.WithKind(kfplacementv1alpha1.ClusterClaimKind),
		kfplacementv1alpha1.GroupVersion.WithKind(kfplacementv1alpha1.PlacementBindingKind),
		kfplacementv1alpha1.GroupVersion.WithKind(kfplacementv1alpha1.ClusterPlacementBindingKind),
		kfplacementv1alpha1.GroupVersion.WithKind(kfplacementv1alpha1.PlacementResourceSnapshotKind),
		kfplacementv1alpha1.GroupVersion.WithKind(kfplacementv1alpha1.ClusterPlacementResourceSnapshotKind),
		// The hub field indexes and the work generator cover Works too, which starts an informer for them.
		kfplacementv1alpha1.GroupVersion.WithKind(kfplacementv1alpha1.WorkKind),
		// The staged update run controller.
		kfrolloutv1alpha1.GroupVersion.WithKind(kfrolloutv1alpha1.StagedUpdateRunKind),
		kfrolloutv1alpha1.GroupVersion.WithKind(kfrolloutv1alpha1.ClusterStagedUpdateRunKind),
		kfrolloutv1alpha1.GroupVersion.WithKind(kfrolloutv1alpha1.StagedUpdateStrategyKind),
		kfrolloutv1alpha1.GroupVersion.WithKind(kfrolloutv1alpha1.ClusterStagedUpdateStrategyKind),
		kfrolloutv1alpha1.GroupVersion.WithKind(kfrolloutv1alpha1.ApprovalRequestKind),
		kfrolloutv1alpha1.GroupVersion.WithKind(kfrolloutv1alpha1.ClusterApprovalRequestKind),
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

	var gvkEligibilityMode resourceeligibility.GVKEligibilityMode
	var parsedGVKs []schema.GroupVersionKind
	if opts.PlacementMgmtOpts.AllowedPropagatingAPIs == "" {
		// Prepare a deny list for placement eligible GVKs.
		gvkEligibilityMode = resourceeligibility.GVKEligibilityModeDenyList
		parsedGVKs, err = resourceeligibility.ParseGVKs(opts.PlacementMgmtOpts.SkippedPropagatingAPIs)
		if err != nil {
			// Normally this branch would never run as the input has been validated beforehand.
			return err
		}
	} else {
		// Prepare an allow list for placement eligible GVKs.
		gvkEligibilityMode = resourceeligibility.GVKEligibilityModeAllowList
		parsedGVKs, err = resourceeligibility.ParseGVKs(opts.PlacementMgmtOpts.AllowedPropagatingAPIs)
		if err != nil {
			// Normally this branch would never run as the input has been validated beforehand.
			return err
		}
	}

	// Prepare a namespace deny list for namespaces.
	var userDefinedSkippedNSNames []string
	for _, ns := range strings.Split(opts.PlacementMgmtOpts.SkippedPropagatingNamespaces, ";") {
		if ns != "" {
			userDefinedSkippedNSNames = append(userDefinedSkippedNSNames, ns)
		}
	}

	// Set up the resource eligibility checker (V0 APIs only).
	customResourceEligibilityChecker := resourceeligibility.New(resourceeligibility.CheckerOptions{
		RestMapper:      mgr.GetRESTMapper(),
		SkipNSCheck:     len(userDefinedSkippedNSNames) == 0,
		SkipGVKRCheck:   len(parsedGVKs) == 0,
		SkipObjectCheck: true,
	})
	customResourceEligibilityChecker.SetNamespaceCheckList(resourceeligibility.NamespaceEligibilityModeDenyList, userDefinedSkippedNSNames, nil)
	if err := customResourceEligibilityChecker.SetGVKCheckList(gvkEligibilityMode, parsedGVKs); err != nil {
		klog.ErrorS(err, "Failed to set GVK check list for the custom resource eligibility checker")
		return err
	}

	// Build the composite resource eligibility checker for v0 APIs.
	defaultResourceEligibilityCheckerForV0APIs := resourceeligibility.DefaultForV0APIs(mgr.GetRESTMapper())
	customResourceEligibilityChecker.Wraps(defaultResourceEligibilityCheckerForV0APIs)

	// The same user-supplied filters for the v1 APIs (placement policies); the v1 built-in ones deny a few more kinds than v0.
	v1ResourceEligibilityChecker := customResourceEligibilityChecker.DeepCopy()
	v1ResourceEligibilityChecker.Wraps(resourceeligibility.DefaultForV1APIs(mgr.GetRESTMapper()))

	// the manager for all the dynamically created informers
	dynamicInformerManager := informer.NewInformerManager(dynamicClient, opts.CtrlMgrOpts.ResyncPeriod.Duration, ctx.Done())
	validator.ResourceInformer = dynamicInformerManager // webhook needs this to check resource scope
	validator.RestMapper = mgr.GetRESTMapper()          // webhook needs this to validate GVK of resource selector

	// Set up  a custom controller to reconcile placement objects
	resourceSelectorResolver := controller.ResourceSelectorResolver{
		RestMapper:                 mgr.GetRESTMapper(),
		InformerManager:            dynamicInformerManager,
		ResourceEligibilityChecker: customResourceEligibilityChecker,
		EnableWorkload:             opts.WebhookAndAdmissionPolicyOpts.EnableWorkload,
	}
	resourceSnapshotResolver := controller.NewResourceSnapshotResolver(mgr.GetClient(), mgr.GetScheme())
	resourceSnapshotResolver.Config = controller.NewResourceSnapshotConfig(opts.PlacementMgmtOpts.ResourceSnapshotCreationMinimumInterval, opts.PlacementMgmtOpts.ResourceChangesCollectionDuration)
	pc := &placement.Reconciler{
		Client:                   mgr.GetClient(),
		Recorder:                 mgr.GetEventRecorder(placementControllerName),
		Scheme:                   mgr.GetScheme(),
		UncachedReader:           mgr.GetAPIReader(),
		ResourceSelectorResolver: resourceSelectorResolver,
		ResourceSnapshotResolver: resourceSnapshotResolver,
	}

	rateLimiter := options.DefaultControllerRateLimiter(opts.PlacementMgmtOpts.PlacementControllerWorkQueueRateLimiterOpts)
	var clusterResourcePlacementControllerV1Beta1 controller.Controller
	var resourcePlacementController controller.Controller
	var placementPolicies *placementpolicy.Reconciler
	var annotatedSources *annotationplacement.SourceQueue

	if opts.FeatureFlags.EnableV1Beta1APIs {
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

		if opts.FeatureFlags.EnableResourcePlacementAPIs {
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
			MaxConcurrentReconciles: int(math.Ceil(float64(opts.PlacementMgmtOpts.MaxFleetSize)/30) * math.Ceil(float64(opts.PlacementMgmtOpts.MaxConcurrentClusterPlacement)/10)),
			InformerManager:         dynamicInformerManager,
		}).SetupWithManagerForClusterResourcePlacement(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up rollout controller for clusterResourcePlacement")
			return err
		}

		if opts.FeatureFlags.EnableResourcePlacementAPIs {
			if err := (&rollout.Reconciler{
				Client:                  mgr.GetClient(),
				UncachedReader:          mgr.GetAPIReader(),
				MaxConcurrentReconciles: int(math.Ceil(float64(opts.PlacementMgmtOpts.MaxFleetSize)/30) * math.Ceil(float64(opts.PlacementMgmtOpts.MaxConcurrentClusterPlacement)/10)),
				InformerManager:         dynamicInformerManager,
			}).SetupWithManagerForResourcePlacement(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up rollout controller for resourcePlacement")
				return err
			}
		}

		if opts.FeatureFlags.EnablePlacementPolicyAPIs {
			for _, gvk := range placementPolicyGVKs {
				if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
					klog.ErrorS(err, "Unable to find the required CRD", "GVK", gvk)
					return err
				}
			}
			klog.Info("Setting up the placement policy controllers")
			if err := fieldindexers.SetupWithHubAgentControllerManager(ctx, mgr); err != nil {
				klog.ErrorS(err, "Unable to set up the field indexes for the placement policy controllers")
				return err
			}
			snapshots, snapshotErr := placementresourcesnapshot.New(mgr,
				dynamicClient, dynamicInformerManager,
				v1ResourceEligibilityChecker, mgr.GetRESTMapper(),
				placementResourceSnapshotSlots,
				placementresourcesnapshot.DefaultMaxPerSnapshotResourceDataSizeBytes,
				placementresourcesnapshot.DefaultMaxPerSnapshotResourceCnt)
			if snapshotErr != nil {
				klog.ErrorS(snapshotErr, "Unable to set up the placement resource snapshot manager")
				return snapshotErr
			}
			// Added to the manager so its snapshot GC runs (leader only).
			if err := mgr.Add(snapshots); err != nil {
				klog.ErrorS(err, "Unable to add the placement resource snapshot manager")
				return err
			}
			placementPolicies = placementpolicy.NewReconciler(mgr.GetClient(), mgr.GetAPIReader(), snapshots)
			if err := placementPolicies.SetupWithManagerForPlacementPolicy(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up the placement policy controller")
				return err
			}
			if err := placementPolicies.SetupWithManagerForClusterPlacementPolicy(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up the cluster placement policy controller")
				return err
			}
			klog.Info("Setting up the placement policy work generator")
			if err := v1workgenerator.New(mgr.GetClient(), parallelizer.DefaultNumOfWorkers).
				SetupWithManager(mgr, int(math.Ceil(float64(opts.PlacementMgmtOpts.MaxFleetSize)/10)*math.Ceil(float64(opts.PlacementMgmtOpts.MaxConcurrentClusterPlacement)/10))); err != nil {
				klog.ErrorS(err, "Unable to set up the placement policy work generator")
				return err
			}
			klog.Info("Setting up the placement policy staged update run controller")
			// Nil rate limiters pick the controller defaults; 30s is the shortest refresh period it accepts.
			stagedUpdateRuns, surErr := stagedupdaterun.New(mgr.GetClient(), mgr.GetAPIReader(), snapshots,
				parallelizer.NewParallelizer(parallelizer.DefaultNumOfWorkers), nil, nil, 30*time.Second)
			if surErr != nil {
				klog.ErrorS(surErr, "Unable to create the placement policy staged update run controller")
				return surErr
			}
			if err := stagedUpdateRuns.SetupWithManager(mgr, int(math.Ceil(float64(opts.PlacementMgmtOpts.MaxConcurrentClusterPlacement)/10))); err != nil {
				klog.ErrorS(err, "Unable to set up the placement policy staged update run controller")
				return err
			}
			// Annotation-based placement generates the policies above; the resource watcher feeds it.
			annotatedSources = &annotationplacement.SourceQueue{}
			enableWorkload := opts.WebhookAndAdmissionPolicyOpts.EnableWorkload
			if err := (&annotationplacement.Reconciler{
				Client:     mgr.GetClient(),
				APIReader:  mgr.GetAPIReader(),
				RESTMapper: mgr.GetRESTMapper(),
				Recorder:   mgr.GetEventRecorder("annotation-placement-controller"),
				// The v1 kind and namespace filters, plus the object filter the resource watcher applies,
				// which the checker does not cover.
				ShouldPlace: func(source *unstructured.Unstructured) (bool, error) {
					if !v1ResourceEligibilityChecker.AllowsGVK(source.GroupVersionKind()) ||
						!v1ResourceEligibilityChecker.AllowsNamespace(annotationplacement.SourceNamespace(source)) {
						return false, nil
					}
					return controller.ShouldPropagateObj(dynamicInformerManager, source, enableWorkload)
				},
			}).SetupWithManager(mgr, annotatedSources); err != nil {
				klog.ErrorS(err, "Unable to set up the annotation placement controller")
				return err
			}
		}

		if opts.FeatureFlags.EnableEvictionAPIs {
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
		if opts.FeatureFlags.EnableStagedUpdateRunAPIs {
			for _, gvk := range clusterStagedUpdateRunGVKs {
				if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
					klog.ErrorS(err, "Unable to find the required CRD", "GVK", gvk)
					return err
				}
			}
			klog.Info("Setting up clusterStagedUpdateRun controller")
			if err = (&updaterun.Reconciler{
				Client:                   mgr.GetClient(),
				InformerManager:          dynamicInformerManager,
				ResourceSelectorResolver: resourceSelectorResolver,
				ResourceSnapshotResolver: resourceSnapshotResolver,
			}).SetupWithManagerForClusterStagedUpdateRun(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up clusterStagedUpdateRun controller")
				return err
			}

			if opts.FeatureFlags.EnableResourcePlacementAPIs {
				for _, gvk := range stagedUpdateRunGVKs {
					if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
						klog.ErrorS(err, "Unable to find the required CRD", "GVK", gvk)
						return err
					}
				}
				klog.Info("Setting up stagedUpdateRun controller")
				if err = (&updaterun.Reconciler{
					Client:                   mgr.GetClient(),
					InformerManager:          dynamicInformerManager,
					ResourceSelectorResolver: resourceSelectorResolver,
					ResourceSnapshotResolver: resourceSnapshotResolver,
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
			MaxConcurrentReconciles: int(math.Ceil(float64(opts.PlacementMgmtOpts.MaxFleetSize)/10) * math.Ceil(float64(opts.PlacementMgmtOpts.MaxConcurrentClusterPlacement)/10)),
			InformerManager:         dynamicInformerManager,
		}).SetupWithManagerForClusterResourceBinding(mgr); err != nil {
			klog.ErrorS(err, "Unable to set up work generator for clusterResourceBinding")
			return err
		}

		if opts.FeatureFlags.EnableResourcePlacementAPIs {
			if err := (&workgenerator.Reconciler{
				Client:                  mgr.GetClient(),
				MaxConcurrentReconciles: int(math.Ceil(float64(opts.PlacementMgmtOpts.MaxFleetSize)/10) * math.Ceil(float64(opts.PlacementMgmtOpts.MaxConcurrentClusterPlacement)/10)),
				InformerManager:         dynamicInformerManager,
			}).SetupWithManagerForResourceBinding(mgr); err != nil {
				klog.ErrorS(err, "Unable to set up work generator for resourceBinding")
				return err
			}
		}

		// Set up the scheduler
		klog.Info("Setting up scheduler")
		schedulerProfile := profile.NewDefaultProfile()
		if opts.AzurePropertyCheckerOpts.IsEnabled {
			klog.Info("Azure property checker is enabled for cluster property validation")
			client, err := compute.NewAttributeBasedVMSizeRecommenderClient(opts.AzurePropertyCheckerOpts.ComputeServiceAddressWithBasePath, httputil.DefaultClientForAzure)
			if err != nil {
				klog.ErrorS(err, "Unable to create Azure vm size recommender client")
				return err
			}
			klog.Info("Setting up cluster affinity plugin with Azure property checker")
			clusterAffinityPlugin := clusteraffinity.New(clusteraffinity.WithPropertyChecker(azure.NewPropertyChecker(*client)))
			profileOpts := profile.Options{
				ClusterAffinityPlugin: &clusterAffinityPlugin,
			}
			schedulerProfile = profile.NewProfile(profileOpts)
		}
		defaultFramework := framework.NewFramework(schedulerProfile, mgr)
		defaultSchedulingQueue := queue.NewSimplePlacementSchedulingQueue(
			schedulerQueueName, nil,
		)
		// we use one scheduler for every 10 concurrent placement
		defaultScheduler := scheduler.NewScheduler("DefaultScheduler", defaultFramework, defaultSchedulingQueue, mgr,
			int(math.Ceil(float64(opts.PlacementMgmtOpts.MaxFleetSize)/50)*math.Ceil(float64(opts.PlacementMgmtOpts.MaxConcurrentClusterPlacement)/10)))
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

		if opts.FeatureFlags.EnableResourcePlacementAPIs {
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
			EnableResourcePlacement:   opts.FeatureFlags.EnableResourcePlacementAPIs,
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
		if opts.FeatureFlags.EnableClusterInventoryAPIs {
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
				ClusterUnhealthyThreshold: opts.ClusterMgmtOpts.UnhealthyThreshold.Duration,
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
		Recorder:                    mgr.GetEventRecorder(resourceChangeControllerName),
		RestMapper:                  mgr.GetRESTMapper(),
		InformerManager:             dynamicInformerManager,
		PlacementControllerV1Beta1:  clusterResourcePlacementControllerV1Beta1,
		ResourcePlacementController: resourcePlacementController,
	}
	if placementPolicies != nil {
		rcr.EnqueuePlacementPolicies = placementPolicies.EnqueuePoliciesSelecting
	}
	resourceChangeController := controller.NewController(resourceChangeControllerName, controller.ClusterWideKeyFunc, rcr.Reconcile, rateLimiter)

	// Set up the InformerPopulator that runs on ALL pods (leader and followers)
	// This ensures all pods have synced informer caches for webhook validation
	klog.Info("Setting up informer populator")
	informerPopulator := &resourcewatcher.InformerPopulator{
		DiscoveryClient:            discoverClient,
		RESTMapper:                 mgr.GetRESTMapper(),
		InformerManager:            dynamicInformerManager,
		ResourceEligibilityChecker: customResourceEligibilityChecker,
	}

	if err := mgr.Add(informerPopulator); err != nil {
		klog.ErrorS(err, "Failed to setup informer populator")
		return err
	}

	// Set up a runner that starts all the custom controllers we created above
	// This runs ONLY on the leader and adds event handlers to the informers created by InformerPopulator
	resourceChangeDetector := &resourcewatcher.ChangeDetector{
		DiscoveryClient: discoverClient,
		RESTMapper:      mgr.GetRESTMapper(),
		ClusterResourcePlacementControllerV1Beta1: clusterResourcePlacementControllerV1Beta1,
		ResourcePlacementController:               resourcePlacementController,
		ResourceChangeController:                  resourceChangeController,
		AnnotatedSources:                          annotatedSources,
		InformerManager:                           dynamicInformerManager,
		ResourceEligibilityChecker:                customResourceEligibilityChecker,
		ConcurrentPlacementWorker:                 int(math.Ceil(float64(opts.PlacementMgmtOpts.MaxConcurrentClusterPlacement) / 10)),
		ConcurrentResourceChangeWorker:            opts.PlacementMgmtOpts.ConcurrentResourceChangeSyncs,
		EnableWorkload:                            opts.WebhookAndAdmissionPolicyOpts.EnableWorkload,
	}

	if err := mgr.Add(resourceChangeDetector); err != nil {
		klog.ErrorS(err, "Failed to setup resource detector")
		return err
	}
	return nil
}
