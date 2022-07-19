/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package options

import (
	"time"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	componentbaseconfig "k8s.io/component-base/config"

	"go.goms.io/fleet/pkg/utils"
)

const (
	defaultBindAddress = "0.0.0.0"
	defaultPort        = 10357
)

// TODO: Clean up the options we don't need and add the ones we need

// Options contains everything necessary to create and run controller-manager.
type Options struct {
	// Controllers is the list of controllers to enable or disable
	// '*' means "all enabled by default controllers"
	// 'foo' means "enable 'foo'"
	// '-foo' means "disable 'foo'"
	// first item for a particular name wins
	Controllers []string
	// LeaderElection defines the configuration of leader election client.
	LeaderElection componentbaseconfig.LeaderElectionConfiguration
	// BindAddress is the IP address on which to listen for the --secure-port port.
	BindAddress string
	// SecurePort is the port that the the server serves at.
	// Note: We hope support https in the future once controller-runtime provides the functionality.
	SecurePort int
	// ClusterUnhealthyThreshold is the duration of failure for the cluster to be considered unhealthy.
	ClusterUnhealthyThreshold metav1.Duration
	// ClusterDegradedGracePeriod represents the grace period after last cluster health probe time.
	// If it doesn't receive update for this amount of time, it will start posting
	// "ClusterReady==ConditionUnknown".
	ClusterDegradedGracePeriod metav1.Duration
	// SkippedPropagatingAPIs indicates comma separated resources that should be skipped for propagating.
	SkippedPropagatingAPIs string
	// SkippedPropagatingNamespaces is a list of namespaces that will be skipped for propagating.
	SkippedPropagatingNamespaces []string
	// HubQPS is the QPS to use while talking with hub-apiserver. Default is 20.0.
	HubQPS float32
	// HubBurst is the burst to allow while talking with hub-apiserver. Default is 100.
	HubBurst int
	// ResyncPeriod is the base frequency the informers are resynced. Defaults is 5 minutes.
	ResyncPeriod metav1.Duration
	// MetricsBindAddress is the TCP address that the controller should bind to
	// for serving prometheus metrics.
	// It can be set to "0" to disable the metrics serving.
	// Defaults to ":8080".
	MetricsBindAddress string
	// ConcurrentClusterPlacementSyncs is the number of cluster `placement` reconcilers that are
	// allowed to sync concurrently.
	ConcurrentClusterPlacementSyncs int
	// ConcurrentResourceChangeSyncs is the number of resource change reconcilers that are
	// allowed to sync concurrently.
	ConcurrentResourceChangeSyncs int
	// ConcurrentWorkSyncs is the number of `work` reconcilers that are
	// allowed to sync concurrently.
	ConcurrentWorkSyncs int
	// RateLimiterOpts is the ratelimit parameters for the work queue
	RateLimiterOpts RateLimitOptions
}

// NewOptions builds an empty options.
func NewOptions() *Options {
	return &Options{
		LeaderElection: componentbaseconfig.LeaderElectionConfiguration{
			LeaderElect:       true,
			ResourceLock:      resourcelock.LeasesResourceLock,
			ResourceNamespace: utils.FleetSystemNamespace,
			ResourceName:      "fleet-hub-manager",
		},
	}
}

// AddFlags adds flags to the specified FlagSet.
func (o *Options) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.BindAddress, "bind-address", defaultBindAddress,
		"The IP address on which to listen for the --secure-port port.")
	flags.IntVar(&o.SecurePort, "secure-port", defaultPort,
		"The secure port on which to serve HTTPS.")
	flags.BoolVar(&o.LeaderElection.LeaderElect, "leader-elect", false, "Start a leader election client and gain leadership before executing the main loop. Enable this when running replicated components for high availability.")
	flags.StringVar(&o.LeaderElection.ResourceNamespace, "leader-elect-resource-namespace", utils.FleetSystemNamespace, "The namespace of resource object that is used for locking during leader election.")
	flags.DurationVar(&o.ClusterUnhealthyThreshold.Duration, "cluster-unhealthy-threshold", 30*time.Second, "The duration for a member cluster to be in a degraded state before considered unhealthy.")
	flags.DurationVar(&o.ClusterDegradedGracePeriod.Duration, "cluster-degraded-grace-period", 60*time.Second,
		"Specifies the grace period of allowing a running cluster to be unresponsive before marking it as degraded.")
	flags.StringVar(&o.SkippedPropagatingAPIs, "skipped-propagating-apis", "", "Semicolon separated resources that should be skipped from propagating in addition to the default skip list(cluster.fleet.io;policy.fleet.io;work.fleet.io). Supported formats are:\n"+
		"<group> for skip resources with a specific API group(e.g. networking.k8s.io),\n"+
		"<group>/<version> for skip resources with a specific API version(e.g. networking.k8s.io/v1beta1),\n"+
		"<group>/<version>/<kind>,<kind> for skip one or more specific resource(e.g. networking.k8s.io/v1beta1/Ingress,IngressClass) where the kinds are case-insensitive.")
	flags.StringSliceVar(&o.SkippedPropagatingNamespaces, "skipped-propagating-namespaces", []string{},
		"Comma-separated namespaces that should be skipped from propagating in addition to the default skipped namespaces(fleet-system, namespaces prefixed by kube- and fleet-work-).")
	flags.Float32Var(&o.HubQPS, "hub-api-qps", 20.0, "QPS to use while talking with fleet-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags.")
	flags.IntVar(&o.HubBurst, "hub-api-burst", 100, "Burst to use while talking with fleet-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags.")
	flags.DurationVar(&o.ResyncPeriod.Duration, "resync-period", time.Second*300, "Base frequency the informers are resynced.")
	flags.StringVar(&o.MetricsBindAddress, "metrics-bind-address", ":8080", "The TCP address that the controller should bind to for serving prometheus metrics(e.g. 127.0.0.1:8088, :8088)")
	flags.IntVar(&o.ConcurrentClusterPlacementSyncs, "concurrent-cluster-placement-syncs", 5, "The number of cluster placement reconcilers to run concurrently.")
	flags.IntVar(&o.ConcurrentResourceChangeSyncs, "concurrent-resource-change-syncs", 20, "The number of resourceChange reconcilers that are allowed to run concurrently.")
	flags.IntVar(&o.ConcurrentWorkSyncs, "concurrent-work-syncs", 5, "The number of work reconcilers that are allowed to run concurrently.")

	o.RateLimiterOpts.AddFlags(flags)
}
