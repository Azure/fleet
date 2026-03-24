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
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/cmd/memberagent/options"
	imcv1beta1 "github.com/kubefleet-dev/kubefleet/pkg/controllers/internalmembercluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workapplier"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider/azure"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/httpclient"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/parallelizer"
	//+kubebuilder:scaffold:imports
)

const (
	// The list of available property provider names.
	azurePropertyProvider = "azure"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	klog.InitFlags(nil)

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1beta1.AddToScheme(scheme))
	utilruntime.Must(placementv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	opts := options.NewOptions()
	opts.AddFlags(flag.CommandLine)

	flag.Parse()
	utilrand.Seed(time.Now().UnixNano())
	defer klog.Flush()

	flag.VisitAll(func(f *flag.Flag) {
		klog.InfoS("flag:", "name", f.Name, "value", f.Value)
	})

	// Set up controller-runtime logger
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	hubURL := os.Getenv("HUB_SERVER_URL")

	if hubURL == "" {
		klog.ErrorS(errors.New("hub server api cannot be empty"), "Failed to read URL for the hub cluster")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	hubConfig, err := buildHubConfig(hubURL, opts.HubConnectivityOpts.UseCertificateAuth, opts.HubConnectivityOpts.UseInsecureTLSClient)
	if err != nil {
		klog.ErrorS(err, "Failed to build Kubernetes client configuration for the hub cluster")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	hubConfig.QPS = float32(opts.CtrlManagerOptions.HubManagerOpts.QPS)
	hubConfig.Burst = opts.CtrlManagerOptions.HubManagerOpts.Burst

	mcName := os.Getenv("MEMBER_CLUSTER_NAME")
	if mcName == "" {
		klog.ErrorS(errors.New("member cluster name cannot be empty"), "Failed to read name for the member cluster")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	mcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)

	memberConfig := ctrl.GetConfigOrDie()
	memberConfig.QPS = float32(opts.CtrlManagerOptions.MemberManagerOpts.QPS)
	memberConfig.Burst = opts.CtrlManagerOptions.MemberManagerOpts.Burst
	// we place the leader election lease on the member cluster to avoid adding load to the hub
	hubOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: opts.CtrlManagerOptions.HubManagerOpts.MetricsBindAddress,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: 8443,
		}),
		HealthProbeBindAddress:  opts.CtrlManagerOptions.HubManagerOpts.HealthProbeBindAddress,
		LeaderElection:          opts.CtrlManagerOptions.LeaderElectionOpts.LeaderElect,
		LeaderElectionNamespace: opts.CtrlManagerOptions.LeaderElectionOpts.ResourceNamespace,
		LeaseDuration:           &opts.CtrlManagerOptions.LeaderElectionOpts.LeaseDuration.Duration,
		RenewDeadline:           &opts.CtrlManagerOptions.LeaderElectionOpts.RenewDeadline.Duration,
		RetryPeriod:             &opts.CtrlManagerOptions.LeaderElectionOpts.RetryPeriod.Duration,
		LeaderElectionConfig:    memberConfig,
		LeaderElectionID:        "136224848560.hub.fleet.azure.com",
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				mcNamespace: {},
			},
			// Strip managed fields from the objects to reduce memory usage.
			//
			// Note (chenyu1): the work applier does read the managedFields field,
			// however, it concerns only objects from the member cluster side, which
			// are not managed by this cache.
			DefaultTransform: cache.TransformStripManagedFields(),
		},
	}

	memberOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: opts.CtrlManagerOptions.MemberManagerOpts.MetricsBindAddress,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: 9443,
		}),
		HealthProbeBindAddress:  opts.CtrlManagerOptions.MemberManagerOpts.HealthProbeBindAddress,
		LeaderElection:          hubOpts.LeaderElection,
		LeaderElectionNamespace: opts.CtrlManagerOptions.LeaderElectionOpts.ResourceNamespace,
		LeaseDuration:           &opts.CtrlManagerOptions.LeaderElectionOpts.LeaseDuration.Duration,
		RenewDeadline:           &opts.CtrlManagerOptions.LeaderElectionOpts.RenewDeadline.Duration,
		RetryPeriod:             &opts.CtrlManagerOptions.LeaderElectionOpts.RetryPeriod.Duration,
		LeaderElectionID:        "136224848560.member.fleet.azure.com",
	}
	//+kubebuilder:scaffold:builder
	if opts.CtrlManagerOptions.EnablePprof {
		memberOpts.PprofBindAddress = fmt.Sprintf(":%d", opts.CtrlManagerOptions.MemberManagerOpts.PprofPort)
		hubOpts.PprofBindAddress = fmt.Sprintf(":%d", opts.CtrlManagerOptions.HubManagerOpts.PprofPort)
	}

	if err := Start(ctrl.SetupSignalHandler(), hubConfig, memberConfig, hubOpts, memberOpts, *opts); err != nil {
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
func Start(ctx context.Context, hubCfg, memberConfig *rest.Config, hubOpts, memberOpts ctrl.Options, globalOpts options.Options) error {
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

	gvk := placementv1beta1.GroupVersion.WithKind(placementv1beta1.AppliedWorkKind)
	if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
		klog.ErrorS(err, "unable to find the required CRD", "GVK", gvk)
		return err
	}
	// Set up the work applier. Note that it is referenced by the InternalMemberCluster controller.

	// Set up the requeue rate limiter for the work applier.
	//
	// With default settings, the rate limiter will:
	// * allow 1 attempt of fixed delay; this helps give objects a bit of headroom to get available (or have
	//   diffs reported).
	// * use a fixed delay of 5 seconds for the first attempt.
	//
	//   Important (chenyu1): before the introduction of the requeue rate limiter, the work
	//   applier uses static requeue intervals, specifically 5 seconds (if the work object is unavailable),
	//   and 15 seconds (if the work object is available). There are a number of test cases that
	//   implicitly assume this behavior (e.g., a test case might expect that the availability check completes
	//   w/in 10 seconds), which is why the rate limiter uses the 5 seconds fixed requeue delay by default.
	//   If you need to change this value and see that some test cases begin to fail, update the test
	//   cases accordingly.
	// * after completing all attempts with fixed delay, switch to slow exponential backoff with a base of
	//   1.2 with an initial delay of 2 seconds and a cap of 15 seconds (12 requeues in total, ~90 seconds in total);
	//   this is to allow fast checkups in cases where objects are not yet available or have not yet reported diffs.
	// * after completing the slow backoff stage, switch to a fast exponential backoff with a base of 1.5
	//   with an initial delay of 15 seconds and a cap of 15 minutes (10 requeues in total, ~42 minutes in total).
	// * for Work objects that are available or have diffs reported, skip the slow backoff stage and
	//   start fast backoff immediately.
	//
	// The requeue pattern is essentially:
	// * 1 attempts of requeue with fixed delay (5 seconds); then
	// * 12 attempts of requeues with slow exponential backoff (factor of 1.2, ~90 seconds in total); then
	// * 10 attempts of requeues with fast exponential backoff (factor of 1.5, ~42 minutes in total);
	// * afterwards, requeue with a delay of 15 minutes indefinitely.
	requeueRateLimiter := workapplier.NewRequeueMultiStageWithExponentialBackoffRateLimiter(
		globalOpts.ApplierOpts.RequeueRateLimiterAttemptsWithFixedDelay,
		float64(globalOpts.ApplierOpts.RequeueRateLimiterFixedDelaySeconds),
		globalOpts.ApplierOpts.RequeueRateLimiterExponentialBaseForSlowBackoff,
		float64(globalOpts.ApplierOpts.RequeueRateLimiterInitialSlowBackoffDelaySeconds),
		float64(globalOpts.ApplierOpts.RequeueRateLimiterMaxSlowBackoffDelaySeconds),
		globalOpts.ApplierOpts.RequeueRateLimiterExponentialBaseForFastBackoff,
		float64(globalOpts.ApplierOpts.RequeueRateLimiterMaxFastBackoffDelaySeconds),
		globalOpts.ApplierOpts.RequeueRateLimiterSkipToFastBackoffForAvailableOrDiffReportedWorkObjs,
	)

	workApplier := workapplier.NewReconciler(
		"work-applier",
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
		parallelizer.NewParallelizer(parallelizer.DefaultNumOfWorkers),
		time.Minute*time.Duration(globalOpts.ApplierOpts.ResourceForceDeletionWaitTimeMinutes),
		requeueRateLimiter,
		globalOpts.ApplierOpts.EnablePriorityQueue,
		&globalOpts.ApplierOpts.PriorityLinearEquationCoEffA,
		&globalOpts.ApplierOpts.PriorityLinearEquationCoEffB,
	)

	if err = workApplier.SetupWithManager(hubMgr); err != nil {
		klog.ErrorS(err, "Failed to create v1beta1 controller", "controller", "work")
		return err
	}

	klog.Info("Setting up the internalMemberCluster v1beta1 controller")
	// Set up a provider provider (if applicable).
	var pp propertyprovider.PropertyProvider
	switch {
	case globalOpts.PropertyProviderOpts.Name == azurePropertyProvider:
		klog.V(2).Info("setting up the Azure property provider")
		// Note that the property provider, though initialized here, is not started until
		// the specific instance wins the leader election.
		klog.V(1).InfoS("Property Provider is azure, loading cloud config", "cloudConfigFile", globalOpts.PropertyProviderOpts.CloudConfigFilePath)
		// TODO (britaniar): load cloud config for Azure property provider.
		pp = azure.New(
			&globalOpts.PropertyProviderOpts.Region,
			globalOpts.PropertyProviderOpts.EnableAzProviderCostProperties,
			globalOpts.PropertyProviderOpts.EnableAzProviderAvailableResourceProperties,
			globalOpts.PropertyProviderOpts.EnableAzProviderNamespaceCollection)
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
		workApplier,
		pp)
	if err != nil {
		klog.ErrorS(err, "Failed to create InternalMemberCluster v1beta1 reconciler")
		return fmt.Errorf("failed to create InternalMemberCluster v1beta1 reconciler: %w", err)
	}
	if err := imcReconciler.SetupWithManager(hubMgr, "internalmembercluster-controller"); err != nil {
		klog.ErrorS(err, "Failed to set up InternalMemberCluster v1beta1 controller with the controller manager")
		return fmt.Errorf("failed to set up InternalMemberCluster v1beta1 controller with the controller manager: %w", err)
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
