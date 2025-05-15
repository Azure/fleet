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

//goland:noinspection ALL
import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	imcv1alpha1 "go.goms.io/fleet/pkg/controllers/internalmembercluster/v1alpha1"
	imcv1beta1 "go.goms.io/fleet/pkg/controllers/internalmembercluster/v1beta1"
	"go.goms.io/fleet/pkg/controllers/workapplier"
	workv1alpha1controller "go.goms.io/fleet/pkg/controllers/workv1alpha1"
	fleetmetrics "go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/propertyprovider"
	"go.goms.io/fleet/pkg/propertyprovider/azure"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/httpclient"
	"go.goms.io/fleet/pkg/utils/parallelizer"
	//+kubebuilder:scaffold:imports
)

const (
	// The list of available property provider names.
	azurePropertyProvider = "azure"
)

var (
	scheme               = runtime.NewScheme()
	useCertificateAuth   = flag.Bool("use-ca-auth", false, "Use key and certificate to authenticate the member agent.")
	tlsClientInsecure    = flag.Bool("tls-insecure", false, "Enable TLSClientConfig.Insecure property. Enabling this will make the connection inSecure (should be 'true' for testing purpose only.)")
	hubProbeAddr         = flag.String("hub-health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	hubMetricsAddr       = flag.String("hub-metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	probeAddr            = flag.String("health-probe-bind-address", ":8091", "The address the probe endpoint binds to.")
	metricsAddr          = flag.String("metrics-bind-address", ":8090", "The address the metric endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	leaderElectionNamespace      = flag.String("leader-election-namespace", "kube-system", "The namespace in which the leader election resource will be created.")
	enableV1Alpha1APIs           = flag.Bool("enable-v1alpha1-apis", true, "If set, the agents will watch for the v1alpha1 APIs.")
	enableV1Beta1APIs            = flag.Bool("enable-v1beta1-apis", false, "If set, the agents will watch for the v1beta1 APIs.")
	propertyProvider             = flag.String("property-provider", "none", "The property provider to use for the agent.")
	region                       = flag.String("region", "", "The region where the member cluster resides.")
	cloudConfigFile              = flag.String("cloud-config", "/etc/kubernetes/provider/config.json", "The path to the cloud cloudconfig file.")
	availabilityCheckInterval    = flag.Int("availability-check-interval", 5, "The interval in seconds between attempts to check for resource availability when resources are not yet available.")
	driftDetectionInterval       = flag.Int("drift-detection-interval", 15, "The interval in seconds between attempts to detect configuration drifts in the cluster.")
	watchWorkWithPriorityQueue   = flag.Bool("enable-watch-work-with-priority-queue", false, "If set, the apply_work controller will watch/reconcile work objects that are created new or have recent updates")
	watchWorkReconcileAgeMinutes = flag.Int("watch-work-reconcile-age", 60, "maximum age (in minutes) of work objects for apply_work controller to watch/reconcile")
)

func init() {
	klog.InitFlags(nil)

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	utilruntime.Must(workv1alpha1.AddToScheme(scheme))
	utilruntime.Must(clusterv1beta1.AddToScheme(scheme))
	utilruntime.Must(placementv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	metrics.Registry.MustRegister(
		fleetmetrics.JoinResultMetrics,
		fleetmetrics.LeaveResultMetrics,
		fleetmetrics.FleetWorkProcessingRequestsTotal,
		fleetmetrics.FleetManifestProcessingRequestsTotal,
	)
}

func main() {
	flag.Parse()
	utilrand.Seed(time.Now().UnixNano())
	defer klog.Flush()

	flag.VisitAll(func(f *flag.Flag) {
		klog.InfoS("flag:", "name", f.Name, "value", f.Value)
	})

	// Set up controller-runtime logger
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	// Validate flags
	if !*enableV1Alpha1APIs && !*enableV1Beta1APIs {
		klog.ErrorS(errors.New("either enable-v1alpha1-apis or enable-v1beta1-apis is required"), "Invalid APIs flags")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	hubURL := os.Getenv("HUB_SERVER_URL")

	if hubURL == "" {
		klog.ErrorS(errors.New("hub server api cannot be empty"), "Failed to read URL for the hub cluster")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	hubConfig, err := buildHubConfig(hubURL, *useCertificateAuth, *tlsClientInsecure)
	if err != nil {
		klog.ErrorS(err, "Failed to build Kubernetes client configuration for the hub cluster")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	mcName := os.Getenv("MEMBER_CLUSTER_NAME")
	if mcName == "" {
		klog.ErrorS(errors.New("member cluster name cannot be empty"), "Failed to read name for the member cluster")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	mcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)

	memberConfig := ctrl.GetConfigOrDie()
	// we place the leader election lease on the member cluster to avoid adding load to the hub
	hubOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: *hubMetricsAddr,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: 8443,
		}),
		HealthProbeBindAddress:  *hubProbeAddr,
		LeaderElection:          *enableLeaderElection,
		LeaderElectionNamespace: *leaderElectionNamespace,
		LeaderElectionConfig:    memberConfig,
		LeaderElectionID:        "136224848560.hub.fleet.azure.com",
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				mcNamespace: {},
			},
		},
	}

	memberOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: *metricsAddr,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: 9443,
		}),
		HealthProbeBindAddress:  *probeAddr,
		LeaderElection:          hubOpts.LeaderElection,
		LeaderElectionNamespace: *leaderElectionNamespace,
		LeaderElectionID:        "136224848560.member.fleet.azure.com",
	}
	//+kubebuilder:scaffold:builder

	if err := Start(ctrl.SetupSignalHandler(), hubConfig, memberConfig, hubOpts, memberOpts); err != nil {
		klog.ErrorS(err, "Failed to start the controllers for the member agent")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
}

func buildHubConfig(hubURL string, useCertificateAuth bool, tlsClientInsecure bool) (*rest.Config, error) {
	var hubConfig = &rest.Config{
		Host: hubURL,
	}
	if useCertificateAuth {
		keyFilePath := os.Getenv("IDENTITY_KEY")
		certFilePath := os.Getenv("IDENTITY_CERT")
		if keyFilePath == "" {
			err := errors.New("identity key file path cannot be empty")
			klog.ErrorS(err, "Failed to retrieve identity key")
			return nil, err
		}

		if certFilePath == "" {
			err := errors.New("identity certificate file path cannot be empty")
			klog.ErrorS(err, "Failed to retrieve identity certificate")
			return nil, err
		}
		hubConfig.TLSClientConfig.CertFile = certFilePath
		hubConfig.TLSClientConfig.KeyFile = keyFilePath
	} else {
		tokenFilePath := os.Getenv("CONFIG_PATH")
		if tokenFilePath == "" {
			err := errors.New("hub token file path cannot be empty if CA auth not used")
			klog.ErrorS(err, "Failed to retrieve token file")
			return nil, err
		}
		err := retry.OnError(retry.DefaultRetry, func(e error) bool {
			return true
		}, func() error {
			// Stat returns file info. It will return
			// an error if there is no file.
			_, err := os.Stat(tokenFilePath)
			return err
		})
		if err != nil {
			klog.ErrorS(err, "Failed to retrieve token file from the path %s", tokenFilePath)
			return nil, err
		}
		hubConfig.BearerTokenFile = tokenFilePath
	}

	hubConfig.TLSClientConfig.Insecure = tlsClientInsecure
	if !tlsClientInsecure {
		caBundle, ok := os.LookupEnv("CA_BUNDLE")
		if ok && caBundle == "" {
			err := errors.New("environment variable CA_BUNDLE should not be empty")
			klog.ErrorS(err, "Failed to validate system variables")
			return nil, err
		}
		hubCA, ok := os.LookupEnv("HUB_CERTIFICATE_AUTHORITY")
		if ok && hubCA == "" {
			err := errors.New("environment variable HUB_CERTIFICATE_AUTHORITY should not be empty")
			klog.ErrorS(err, "Failed to validate system variables")
			return nil, err
		}
		if caBundle != "" && hubCA != "" {
			err := errors.New("environment variables CA_BUNDLE and HUB_CERTIFICATE_AUTHORITY should not be set at same time")
			klog.ErrorS(err, "Failed to validate system variables")
			return nil, err
		}

		if caBundle != "" {
			hubConfig.TLSClientConfig.CAFile = caBundle
		} else if hubCA != "" {
			caData, err := base64.StdEncoding.DecodeString(hubCA)
			if err != nil {
				klog.ErrorS(err, "Failed to decode hub cluster certificate authority data")
				return nil, err
			}
			hubConfig.TLSClientConfig.CAData = caData
		}
	}

	// Sometime the hub cluster need additional http header for authentication or authorization.
	// the "HUB_KUBE_HEADER" to allow sending custom header to hub's API Server for authentication and authorization.
	if header, ok := os.LookupEnv("HUB_KUBE_HEADER"); ok {
		r := textproto.NewReader(bufio.NewReader(strings.NewReader(header)))
		h, err := r.ReadMIMEHeader()
		if err != nil && !errors.Is(err, io.EOF) {
			klog.ErrorS(err, "Failed to parse HUB_KUBE_HEADER %q", header)
			return nil, err
		}
		hubConfig.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
			return httpclient.NewCustomHeadersRoundTripper(http.Header(h), rt)
		}
	}
	return hubConfig, nil
}

// Start the member controllers with the supplied config
func Start(ctx context.Context, hubCfg, memberConfig *rest.Config, hubOpts, memberOpts ctrl.Options) error {
	hubMgr, err := ctrl.NewManager(hubCfg, hubOpts)
	if err != nil {
		return fmt.Errorf("unable to start hub manager: %w", err)
	}

	memberMgr, err := ctrl.NewManager(memberConfig, memberOpts)
	if err != nil {
		return fmt.Errorf("unable to start member manager: %w", err)
	}

	if err := hubMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Failed to set up health check for hub manager")
		return err
	}
	if err := hubMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Failed to set up ready check for hub manager")
		return err
	}

	if err := memberMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Failed to set up health check for member manager")
		return err
	}
	if err := memberMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Failed to set up ready check for member manager")
		return err
	}

	spokeDynamicClient, err := dynamic.NewForConfig(memberConfig)
	if err != nil {
		klog.ErrorS(err, "Failed to create spoke dynamic client")
		return err
	}

	httpClient, err := rest.HTTPClientFor(memberConfig)
	if err != nil {
		klog.ErrorS(err, "Failed to create spoke HTTP client")
		return err
	}
	restMapper, err := apiutil.NewDynamicRESTMapper(memberConfig, httpClient)
	if err != nil {
		klog.ErrorS(err, "Failed to create spoke rest mapper")
		return err
	}

	// In a recent refresh, the cache in use by the controller runtime has been upgraded to
	// support multiple default namespaces (originally the number of default namespaces is
	// limited to 1); however, the Fleet controllers still assume that only one default
	// namespace is used, and for compatibility reasons, here we simply retrieve the first
	// default namespace set (there should only be one set up anyway) and pass it to the
	// Fleet controllers.
	var targetNS string
	for ns := range hubOpts.Cache.DefaultNamespaces {
		targetNS = ns
		break
	}
	discoverClient := discovery.NewDiscoveryClientForConfigOrDie(memberConfig)

	if *enableV1Alpha1APIs {
		gvk := workv1alpha1.SchemeGroupVersion.WithKind(workv1alpha1.AppliedWorkKind)
		if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
			klog.ErrorS(err, "unable to find the required CRD", "GVK", gvk)
			return err
		}
		// create the work controller, so we can pass it to the internal member cluster reconciler
		workController := workv1alpha1controller.NewApplyWorkReconciler(
			hubMgr.GetClient(),
			spokeDynamicClient,
			memberMgr.GetClient(),
			restMapper, hubMgr.GetEventRecorderFor("work_controller"), 5, targetNS)

		if err = workController.SetupWithManager(hubMgr); err != nil {
			klog.ErrorS(err, "Failed to create v1alpha1 controller", "controller", "work")
			return err
		}

		klog.Info("Setting up the internalMemberCluster v1alpha1 controller")
		if err = imcv1alpha1.NewReconciler(hubMgr.GetClient(), memberMgr.GetClient(), workController).SetupWithManager(hubMgr, "internalmemberclusterv1alpha1-controller"); err != nil {
			klog.ErrorS(err, "Failed to create v1alpha1 controller", "controller", "internalMemberCluster")
			return fmt.Errorf("unable to create internalMemberCluster v1alpha1 controller: %w", err)
		}
	}

	if *enableV1Beta1APIs {
		gvk := placementv1beta1.GroupVersion.WithKind(placementv1beta1.AppliedWorkKind)
		if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
			klog.ErrorS(err, "unable to find the required CRD", "GVK", gvk)
			return err
		}
		// create the work controller, so we can pass it to the internal member cluster reconciler
		workController := workapplier.NewReconciler(
			hubMgr.GetClient(),
			targetNS,
			spokeDynamicClient,
			memberMgr.GetClient(),
			restMapper,
			hubMgr.GetEventRecorderFor("work_applier"),
			// The number of concurrent reconcilations. This is set to 5 to boost performance in
			// resource processing.
			5,
			// Use the default worker count (4) for parallelized manifest processing.
			parallelizer.DefaultNumOfWorkers,
			time.Second*time.Duration(*availabilityCheckInterval),
			time.Second*time.Duration(*driftDetectionInterval),
			*watchWorkWithPriorityQueue,
			*watchWorkReconcileAgeMinutes,
		)

		if err = workController.SetupWithManager(hubMgr); err != nil {
			klog.ErrorS(err, "Failed to create v1beta1 controller", "controller", "work")
			return err
		}

		klog.Info("Setting up the internalMemberCluster v1beta1 controller")
		// Set up a provider provider (if applicable).
		var pp propertyprovider.PropertyProvider
		switch {
		case propertyProvider != nil && *propertyProvider == azurePropertyProvider:
			klog.V(2).Info("setting up the Azure property provider")
			// Note that the property provider, though initialized here, is not started until
			// the specific instance wins the leader election.
			klog.V(1).InfoS("Property Provider is azure, loading cloud config", "cloudConfigFile", *cloudConfigFile)
			// TODO (britaniar): load cloud config for Azure property provider.
			pp = azure.New(region)
		default:
			// Fall back to not using any property provider if the provided type is none or
			// not recognizable.
			klog.V(2).Info("no property provider is specified, or the given type is not recognizable; start with no property provider")
			pp = nil
		}

		// Set up the IMC controller.
		imcReconciler, err := imcv1beta1.NewReconciler(
			ctx,
			hubMgr.GetClient(),
			memberMgr.GetConfig(), memberMgr.GetClient(),
			workController,
			pp)
		if err != nil {
			klog.ErrorS(err, "Failed to create InternalMemberCluster v1beta1 reconciler")
			return fmt.Errorf("failed to create InternalMemberCluster v1beta1 reconciler: %w", err)
		}
		if err := imcReconciler.SetupWithManager(hubMgr, "internalmembercluster-controller"); err != nil {
			klog.ErrorS(err, "Failed to set up InternalMemberCluster v1beta1 controller with the controller manager")
			return fmt.Errorf("failed to set up InternalMemberCluster v1beta1 controller with the controller manager: %w", err)
		}
	}

	klog.InfoS("starting hub manager")
	go func() {
		defer klog.InfoS("shutting down hub manager")
		if err := hubMgr.Start(ctx); err != nil {
			klog.ErrorS(err, "Failed to start controller manager for the hub cluster")
			return
		}
	}()

	klog.InfoS("starting member manager")
	defer klog.InfoS("shutting down member manager")
	if err := memberMgr.Start(ctx); err != nil {
		klog.ErrorS(err, "Failed to start controller manager for the member cluster")
		return fmt.Errorf("problem starting member manager: %w", err)
	}

	return nil
}
