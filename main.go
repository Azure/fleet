package main

import (
	"flag"
	v1alphafleet "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/informerfactory"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/klog"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	controllercfg "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	v1alphaworker "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
	"strings"
)

var (
	probeAddr            = flag.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	metricsAddr          = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
)

func StartFleetControllers(mgr manager.Manager, stopCh <-chan struct{}) error {
	v1alphafleet.AddToScheme(mgr.GetScheme())
	v1alphaworker.AddToScheme(mgr.GetScheme())

	factory := informerfactory.NewInformerFactory(mgr.GetConfig())
	placementQueue := informerfactory.NewPlacementQueue(factory, mgr.GetClient(), mgr.GetScheme(), stopCh)
	// start processing placement queue
	placementQueue.Run()

	// dynamic
	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(mgr.GetConfig())
	_, allResources, err := discoveryClient.ServerGroupsAndResources()
	if err != nil {
		return err
	}

	watchableResources := discovery.FilteredBy(discovery.SupportsAllVerbs{Verbs: []string{"list", "watch"}}, allResources)
	for _, apiResources := range watchableResources {
		gv, err := schema.ParseGroupVersion(apiResources.GroupVersion)
		if err != nil {
			return err
		}
		if gv.Group == "fleet.azure.com" || strings.HasSuffix(gv.Group, "gatekeeper.sh") || strings.HasSuffix(gv.Group, "x-k8s.io") {
			// ignore aks reserved group
			continue
		}
		for _, apiResource := range apiResources.APIResources {
			if apiResource.Kind == "Event" || apiResource.Kind == "Pod" || apiResource.Kind == "CustomResourceDefinition" {
				// ignore event, pod, crd
				continue
			}
			gvk := schema.GroupVersionKind{
				Group:   gv.Group,
				Version: gv.Version,
				Kind:    apiResource.Kind,
			}
			klog.Infof("observed api resource %s/%s/%s", gv.Group, gv.Version, apiResource.Kind)
			if apiResource.Name == "configmaps" || apiResource.Name == "deployments" {
				im, err := factory.UnstructuredResource(gvk, apiResource)
				if err != nil {
					klog.Errorf("failed to init informer for %s: err: %+v", gvk.String(), err)
					continue
				}
				handler := informerfactory.NewResourceHandler(apiResource, im, mgr.GetClient(), placementQueue, stopCh)
				im.AddResourceEventHandler(handler)
				handler.Run()
			}
		}
	}
	// static
	placementController := &informerfactory.PlacementController{}
	err = placementController.Add(mgr, placementQueue)
	if err != nil {
		return err
	}
	memberClusterController := &informerfactory.MemberClusterController{}
	err = memberClusterController.Add(mgr, placementQueue)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	stopCh := signals.SetupSignalHandler()
	kubeConfg, err := controllercfg.GetConfig()
	if err != nil {
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(kubeConfg, ctrl.Options{
		Scheme:                 v1alphafleet.Scheme,
		MetricsBindAddress:     *metricsAddr,
		Port:                   9446,
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "984738fa.hub.fleet.azure.com",
	})

	err = StartFleetControllers(mgr, stopCh.Done())
	if err != nil {
		klog.Errorf("failed to start fleet controllers: %+v", err)
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.Errorf("unable to set up health check: %+v", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.Errorf("unable to set up readyz check: %+v", err)
		os.Exit(1)
	}
	if err := mgr.Start(stopCh); err != nil {
		klog.Errorf("unable to start controller manager: %+v", err)
		os.Exit(1)
	}
}
