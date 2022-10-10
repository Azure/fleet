/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package options

import (
	"flag"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	componentbaseconfig "k8s.io/component-base/config"

	"go.goms.io/fleet/pkg/utils"
)

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
	// HealthProbeAddress is the TCP address that the is used to serve the heath probes from k8s
	HealthProbeAddress string
	// MetricsBindAddress is the TCP address that the controller should bind to
	// for serving prometheus metrics.
	// It can be set to "0" to disable the metrics serving.
	// Defaults to ":8080".
	MetricsBindAddress string
	// EnableWebhook indicates if we will run a webhook
	EnableWebhook bool
	// Sets the connection type for the webhook.
	WebhookClientConnectionType string
	// NetworkingAgentsEnabled indicates if we enable network agents
	NetworkingAgentsEnabled bool
	// ClusterUnhealthyThreshold is the duration of failure for the cluster to be considered unhealthy.
	ClusterUnhealthyThreshold metav1.Duration
	// WorkPendingGracePeriod represents the grace period after a work is created/updated.
	// We consider a work failed if a work's last applied condition doesn't change after period.
	WorkPendingGracePeriod metav1.Duration
	// SkippedPropagatingAPIs indicates comma separated resources that should be skipped for propagating.
	SkippedPropagatingAPIs string
	// SkippedPropagatingNamespaces is a list of namespaces that will be skipped for propagating.
	SkippedPropagatingNamespaces string
	// HubQPS is the QPS to use while talking with hub-apiserver. Default is 20.0.
	HubQPS float64
	// HubBurst is the burst to allow while talking with hub-apiserver. Default is 100.
	HubBurst int
	// ResyncPeriod is the base frequency the informers are resynced. Defaults is 5 minutes.
	ResyncPeriod metav1.Duration
	// ConcurrentClusterPlacementSyncs is the number of cluster `placement` reconcilers that are
	// allowed to sync concurrently.
	ConcurrentClusterPlacementSyncs int
	// ConcurrentResourceChangeSyncs is the number of resource change reconcilers that are
	// allowed to sync concurrently.
	ConcurrentResourceChangeSyncs int
	// ConcurrentMemberClusterSyncs is the number of `memberCluster` reconcilers that are
	// allowed to sync concurrently.
	ConcurrentMemberClusterSyncs int
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
			ResourceName:      "136224848560.hub.fleet.azure.com",
		},
		ConcurrentClusterPlacementSyncs: 1,
		ConcurrentResourceChangeSyncs:   1,
		ConcurrentMemberClusterSyncs:    1,
	}
}

// AddFlags adds flags to the specified FlagSet.
func (o *Options) AddFlags(flags *flag.FlagSet) {
	flags.StringVar(&o.HealthProbeAddress, "health-probe-bind-address", ":8081",
		"The IP address on which to listen for the --secure-port port.")
	flags.StringVar(&o.MetricsBindAddress, "metrics-bind-address", ":8080", "The TCP address that the controller should bind to for serving prometheus metrics(e.g. 127.0.0.1:8088, :8088)")
	flags.BoolVar(&o.LeaderElection.LeaderElect, "leader-elect", false, "Start a leader election client and gain leadership before executing the main loop. Enable this when running replicated components for high availability.")
	flags.DurationVar(&o.LeaderElection.LeaseDuration.Duration, "leader-lease-duration", 15*time.Second, "This is effectively the maximum duration that a leader can be stopped before someone else will replace it.")
	flag.StringVar(&o.LeaderElection.ResourceNamespace, "leader-election-namespace", utils.FleetSystemNamespace, "The namespace in which the leader election resource will be created.")
	flag.BoolVar(&o.EnableWebhook, "enable-webhook", false, "If set, the fleet webhook is enabled.")
	flag.StringVar(&o.WebhookClientConnectionType, "webhook-client-connection-type", "service", "Sets the connection type used by the webhook client. Only URL or Service is valid.")
	flag.BoolVar(&o.NetworkingAgentsEnabled, "networking-agents-enabled", false, "Whether the networking agents are enabled or not.")
	flags.DurationVar(&o.ClusterUnhealthyThreshold.Duration, "cluster-unhealthy-threshold", 60*time.Second, "The duration for a member cluster to be in a degraded state before considered unhealthy.")
	flags.DurationVar(&o.WorkPendingGracePeriod.Duration, "work-pending-grace-period", 15*time.Second,
		"Specifies the grace period of allowing a manifest to be pending before marking it as failed.")
	flags.StringVar(&o.SkippedPropagatingAPIs, "skipped-propagating-apis", "", "Semicolon separated resources that should be skipped from propagating in addition to the default skip list(cluster.fleet.io;policy.fleet.io;work.fleet.io). Supported formats are:\n"+
		"<group> for skip resources with a specific API group(e.g. networking.k8s.io),\n"+
		"<group>/<version> for skip resources with a specific API version(e.g. networking.k8s.io/v1beta1),\n"+
		"<group>/<version>/<kind>,<kind> for skip one or more specific resource(e.g. networking.k8s.io/v1beta1/Ingress,IngressClass) where the kinds are case-insensitive.")
	flags.StringVar(&o.SkippedPropagatingNamespaces, "skipped-propagating-namespaces", "",
		"Comma-separated namespaces that should be skipped from propagating in addition to the default skipped namespaces(fleet-system, namespaces prefixed by kube- and fleet-work-).")
	flags.Float64Var(&o.HubQPS, "hub-api-qps", 20.0, "QPS to use while talking with fleet-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags.")
	flags.IntVar(&o.HubBurst, "hub-api-burst", 100, "Burst to use while talking with fleet-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags.")
	flags.DurationVar(&o.ResyncPeriod.Duration, "resync-period", 300*time.Second, "Base frequency the informers are resynced.")
	flags.IntVar(&o.ConcurrentClusterPlacementSyncs, "concurrent-cluster-placement-syncs", 1, "The number of cluster placement reconcilers to run concurrently.")
	flags.IntVar(&o.ConcurrentResourceChangeSyncs, "concurrent-resource-change-syncs", 20, "The number of resourceChange reconcilers that are allowed to run concurrently.")
	flags.IntVar(&o.ConcurrentMemberClusterSyncs, "concurrent-member-cluster-syncs", 1, "The number of member cluster reconcilers that are allowed to run concurrently.")

	o.RateLimiterOpts.AddFlags(flags)
}
