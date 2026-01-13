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

package resourcewatcher

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/informer"
)

const (
	// informerPopulatorDiscoveryPeriod is how often the InformerPopulator rediscovers API resources
	informerPopulatorDiscoveryPeriod = 30 * time.Second
)

// make sure that our InformerPopulator implements controller runtime interfaces
var (
	_ manager.Runnable               = &InformerPopulator{}
	_ manager.LeaderElectionRunnable = &InformerPopulator{}
)

// InformerPopulator discovers API resources and creates informers for them WITHOUT adding event handlers.
// This allows follower pods to have synced informer caches for webhook validation while the leader's
// ChangeDetector adds event handlers and runs controllers.
type InformerPopulator struct {
	// DiscoveryClient is used to do resource discovery.
	DiscoveryClient discovery.DiscoveryInterface

	// RESTMapper is used to convert between GVK and GVR
	RESTMapper meta.RESTMapper

	// InformerManager manages all the dynamic informers created by the discovery client
	InformerManager informer.Manager

	// ResourceConfig contains all the API resources that we won't select based on the allowed or skipped propagating APIs option.
	ResourceConfig *utils.ResourceConfig
}

// Start runs the informer populator, discovering resources and creating informers.
// This runs on ALL pods (leader and followers) to ensure all have synced caches.
func (p *InformerPopulator) Start(ctx context.Context) error {
	klog.InfoS("Starting the informer populator")
	defer klog.InfoS("The informer populator is stopped")

	// Run initial discovery to create informers
	p.discoverAndCreateInformers()

	// Wait for initial cache sync
	p.InformerManager.WaitForCacheSync()
	klog.InfoS("Informer populator: initial cache sync complete")

	// Continue discovering resources periodically to handle CRD installations
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		p.discoverAndCreateInformers()
	}, informerPopulatorDiscoveryPeriod)

	return nil
}

// discoverAndCreateInformers discovers API resources and creates informers WITHOUT adding event handlers
func (p *InformerPopulator) discoverAndCreateInformers() {
	resourcesToWatch := discoverWatchableResources(p.DiscoveryClient, p.RESTMapper, p.ResourceConfig)

	// Create informers directly without adding event handlers.
	// This avoids adding any event handlers on follower pods
	for _, res := range resourcesToWatch {
		p.InformerManager.CreateInformerForResource(res)
	}

	// Start any newly created informers
	p.InformerManager.Start()

	klog.V(2).InfoS("Informer populator: discovered resources", "count", len(resourcesToWatch))
}

// NeedLeaderElection implements LeaderElectionRunnable interface.
// Returns false so this runs on ALL pods (leader and followers).
func (p *InformerPopulator) NeedLeaderElection() bool {
	return false
}
