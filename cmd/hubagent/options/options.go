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

package options

import (
	"flag"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	componentbaseconfig "k8s.io/component-base/config"

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
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
	// Webhook service name
	WebhookServiceName string
	// EnableGuardRail indicates if we will enable fleet guard rail webhook configurations.
	EnableGuardRail bool
	// WhiteListedUsers indicates the list of user who are allowed to modify fleet resources
	WhiteListedUsers string
	// Sets the connection type for the webhook.
	WebhookClientConnectionType string
	// NetworkingAgentsEnabled indicates if we enable network agents
	NetworkingAgentsEnabled bool
	// ClusterUnhealthyThreshold is the duration of failure for the cluster to be considered unhealthy.
	ClusterUnhealthyThreshold metav1.Duration
	// WorkPendingGracePeriod represents the grace period after a work is created/updated.
	// We consider a work failed if a work's last applied condition doesn't change after period.
	WorkPendingGracePeriod metav1.Duration
	// SkippedPropagatingAPIs and AllowedPropagatingAPIs options are used to control the propagation of resources.
	// If none of them are set, the default skippedPropagatingAPIs list will be used.
	// SkippedPropagatingAPIs indicates semicolon separated resources that should be skipped for propagating.
	SkippedPropagatingAPIs string
	// AllowedPropagatingAPIs indicates semicolon separated resources that should be allowed for propagating.
	// This is mutually exclusive with SkippedPropagatingAPIs.
	AllowedPropagatingAPIs string
	// SkippedPropagatingNamespaces is a list of namespaces that will be skipped for propagating.
	SkippedPropagatingNamespaces string
	// HubQPS is the QPS to use while talking with hub-apiserver. Default is 20.0.
	HubQPS float64
	// HubBurst is the burst to allow while talking with hub-apiserver. Default is 100.
	HubBurst int
	// ResyncPeriod is the base frequency the informers are resynced. Defaults is 5 minutes.
	ResyncPeriod metav1.Duration
	// MaxConcurrentClusterPlacement is the number of cluster placement that are allowed to run concurrently.
	MaxConcurrentClusterPlacement int
	// ConcurrentResourceChangeSyncs is the number of resource change reconcilers that are allowed to sync concurrently.
	ConcurrentResourceChangeSyncs int
	// MaxFleetSizeSupported is the max number of member clusters this fleet supports.
	// We will set the max concurrency of related reconcilers (membercluster, rollout,workgenerator)
	// according to this value.
	MaxFleetSizeSupported int
	// RateLimiterOpts is the ratelimit parameters for the work queue
	RateLimiterOpts RateLimitOptions
	// EnableV1Alpha1APIs enables the agents to watch the v1alpha1 CRs.
	// TODO(weiweng): remove this field soon. Only kept for backward compatibility.
	EnableV1Alpha1APIs bool
	// EnableV1Beta1APIs enables the agents to watch the v1beta1 CRs.
	EnableV1Beta1APIs bool
	// EnableClusterInventoryAPIs enables the agents to watch the cluster inventory CRs.
	EnableClusterInventoryAPIs bool
	// ForceDeleteWaitTime is the duration the hub agent waits before force deleting a member cluster.
	ForceDeleteWaitTime metav1.Duration
	// EnableStagedUpdateRunAPIs enables the agents to watch the clusterStagedUpdateRun CRs.
	EnableStagedUpdateRunAPIs bool
	// EnableEvictionAPIs enables to agents to watch the eviction and placement disruption budget CRs.
	EnableEvictionAPIs bool
	// EnableResourcePlacement enables the agents to watch the ResourcePlacement APIs.
	EnableResourcePlacement bool
	// EnablePprof enables the pprof profiling.
	EnablePprof bool
	// PprofPort is the port for pprof profiling.
	PprofPort int
	// DenyModifyMemberClusterLabels indicates if the member cluster labels cannot be modified by groups (excluding system:masters)
	DenyModifyMemberClusterLabels bool
	// ResourceSnapshotCreationMinimumInterval is the minimum interval at which resource snapshots could be created.
	// Whether the resource snapshot is created or not depends on the both ResourceSnapshotCreationMinimumInterval and ResourceChangesCollectionDuration.
	ResourceSnapshotCreationMinimumInterval time.Duration
	// ResourceChangesCollectionDuration is the duration for collecting resource changes into one snapshot.
	ResourceChangesCollectionDuration time.Duration
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
		MaxConcurrentClusterPlacement:           10,
		ConcurrentResourceChangeSyncs:           1,
		MaxFleetSizeSupported:                   100,
		EnableV1Alpha1APIs:                      false,
		EnableClusterInventoryAPIs:              true,
		EnableStagedUpdateRunAPIs:               true,
		EnableResourcePlacement:                 true,
		EnablePprof:                             false,
		PprofPort:                               6065,
		ResourceSnapshotCreationMinimumInterval: 30 * time.Second,
		ResourceChangesCollectionDuration:       15 * time.Second,
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
	flag.BoolVar(&o.EnableWebhook, "enable-webhook", true, "If set, the fleet webhook is enabled.")
	// set a default value 'fleetwebhook' for webhook service name for backward compatibility. The service name was hard coded to 'fleetwebhook' in the past.
	flag.StringVar(&o.WebhookServiceName, "webhook-service-name", "fleetwebhook", "Fleet webhook service name.")
	flag.BoolVar(&o.EnableGuardRail, "enable-guard-rail", false, "If set, the fleet guard rail webhook configurations are enabled.")
	flag.StringVar(&o.WhiteListedUsers, "whitelisted-users", "", "If set, white listed users can modify fleet related resources.")
	flag.StringVar(&o.WebhookClientConnectionType, "webhook-client-connection-type", "url", "Sets the connection type used by the webhook client. Only URL or Service is valid.")
	flag.BoolVar(&o.NetworkingAgentsEnabled, "networking-agents-enabled", false, "Whether the networking agents are enabled or not.")
	flags.DurationVar(&o.ClusterUnhealthyThreshold.Duration, "cluster-unhealthy-threshold", 60*time.Second, "The duration for a member cluster to be in a degraded state before considered unhealthy.")
	flags.DurationVar(&o.WorkPendingGracePeriod.Duration, "work-pending-grace-period", 15*time.Second,
		"Specifies the grace period of allowing a manifest to be pending before marking it as failed.")
	flags.StringVar(&o.AllowedPropagatingAPIs, "allowed-propagating-apis", "", "Semicolon separated resources that should be allowed for propagation. Supported formats are:\n"+
		"<group> for allowing resources with a specific API group(e.g. networking.k8s.io),\n"+
		"<group>/<version> for allowing resources with a specific API version(e.g. networking.k8s.io/v1beta1),\n"+
		"<group>/<version>/<kind>,<kind> for allowing one or more specific resources (e.g. networking.k8s.io/v1beta1/Ingress,IngressClass) where the Kinds are case-insensitive.")
	flags.StringVar(&o.SkippedPropagatingAPIs, "skipped-propagating-apis", "", "Semicolon separated resources that should be skipped from propagating in addition to the default skip list(cluster.fleet.io;policy.fleet.io;work.fleet.io). Supported formats are:\n"+
		"<group> for skip resources with a specific API group(e.g. networking.k8s.io),\n"+
		"<group>/<version> for skip resources with a specific API version(e.g. networking.k8s.io/v1beta1),\n"+
		"<group>/<version>/<kind>,<kind> for skip one or more specific resource(e.g. networking.k8s.io/v1beta1/Ingress,IngressClass) where the kinds are case-insensitive.")
	flags.StringVar(&o.SkippedPropagatingNamespaces, "skipped-propagating-namespaces", "",
		"Comma-separated namespaces that should be skipped from propagating in addition to the default skipped namespaces(fleet-system, namespaces prefixed by kube- and fleet-work-).")
	flags.Float64Var(&o.HubQPS, "hub-api-qps", 250, "QPS to use while talking with fleet-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags.")
	flags.IntVar(&o.HubBurst, "hub-api-burst", 1000, "Burst to use while talking with fleet-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags.")
	flags.DurationVar(&o.ResyncPeriod.Duration, "resync-period", 6*time.Hour, "Base frequency the informers are resynced.")
	flags.IntVar(&o.MaxConcurrentClusterPlacement, "max-concurrent-cluster-placement", 100, "The max number of concurrent cluster placement to run concurrently.")
	flags.IntVar(&o.ConcurrentResourceChangeSyncs, "concurrent-resource-change-syncs", 20, "The number of resourceChange reconcilers that are allowed to run concurrently.")
	flags.IntVar(&o.MaxFleetSizeSupported, "max-fleet-size", 100, "The max number of member clusters supported in this fleet")
	flags.BoolVar(&o.EnableV1Alpha1APIs, "enable-v1alpha1-apis", false, "If set, the agents will watch for the v1alpha1 APIs.")
	flags.BoolVar(&o.EnableV1Beta1APIs, "enable-v1beta1-apis", true, "If set, the agents will watch for the v1beta1 APIs.")
	flags.BoolVar(&o.EnableClusterInventoryAPIs, "enable-cluster-inventory-apis", true, "If set, the agents will watch for the ClusterInventory APIs.")
	flags.DurationVar(&o.ForceDeleteWaitTime.Duration, "force-delete-wait-time", 15*time.Minute, "The duration the hub agent waits before force deleting a member cluster.")
	flags.BoolVar(&o.EnableStagedUpdateRunAPIs, "enable-staged-update-run-apis", true, "If set, the agents will watch for the ClusterStagedUpdateRun APIs.")
	flags.BoolVar(&o.EnableEvictionAPIs, "enable-eviction-apis", true, "If set, the agents will watch for the Eviction and PlacementDisruptionBudget APIs.")
	flags.BoolVar(&o.EnableResourcePlacement, "enable-resource-placement", true, "If set, the agents will watch for the ResourcePlacement APIs.")
	flags.BoolVar(&o.EnablePprof, "enable-pprof", false, "If set, the pprof profiling is enabled.")
	flags.IntVar(&o.PprofPort, "pprof-port", 6065, "The port for pprof profiling.")
	flags.BoolVar(&o.DenyModifyMemberClusterLabels, "deny-modify-member-cluster-labels", false, "If set, users not in the system:masters cannot modify member cluster labels.")
	flags.DurationVar(&o.ResourceSnapshotCreationMinimumInterval, "resource-snapshot-creation-minimum-interval", 30*time.Second, "The minimum interval at which resource snapshots could be created.")
	flags.DurationVar(&o.ResourceChangesCollectionDuration, "resource-changes-collection-duration", 15*time.Second,
		"The duration for collecting resource changes into one snapshot. The default is 15 seconds, which means that the controller will collect resource changes for 15 seconds before creating a resource snapshot.")
	o.RateLimiterOpts.AddFlags(flags)
}
