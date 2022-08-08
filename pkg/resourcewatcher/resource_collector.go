/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package resourcewatcher

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"

	"go.goms.io/fleet/pkg/utils"
)

// getWatchableResources returns all api resources from discoveryClient that we can watch.
// More specifically, all api resources which support the 'list', and 'watch' verbs.
// All discovery errors are considered temporary. Upon encountering any error,
// getWatchableResources will log and return any discovered resources it was able to process (which may be none).
func (d *ChangeDetector) getWatchableResources() ([]utils.APIResourceMeta, error) {
	// Get all the resources this cluster has. We only need to care about the preferred version as the informers watch
	// the preferred version will get watch event for resources on the other versions since there is only one version in etcd.
	allResources, discoverError := d.DiscoveryClient.ServerPreferredResources()
	allErr := make([]error, 0)
	if discoverError != nil {
		if discovery.IsGroupDiscoveryFailedError(discoverError) {
			klog.Warningf("failed to discover some groups: %v", discoverError.(*discovery.ErrGroupDiscoveryFailed).Groups) //nolint
		} else {
			klog.Warningf("failed to discover some resources: %v", discoverError)
		}
		allErr = append(allErr, discoverError)
	}
	if allResources == nil {
		return nil, discoverError
	}

	watchableGroupVersionResources := make([]utils.APIResourceMeta, 0)

	// This is extracted from discovery.GroupVersionResources to only watch watchable resources
	watchableResources := discovery.FilteredBy(discovery.SupportsAllVerbs{Verbs: []string{"list", "watch"}}, allResources)
	for _, rl := range watchableResources {
		gv, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			klog.Warningf("ignoring invalid discovered resource %q: %v", rl.GroupVersion, err)
			allErr = append(allErr, err)
			continue
		}
		for i := range rl.APIResources {
			gvr := schema.GroupVersionResource{Group: gv.Group, Version: gv.Version, Resource: rl.APIResources[i].Name}
			watchableGroupVersionResources = append(watchableGroupVersionResources, utils.APIResourceMeta{
				GroupVersionResource: gvr,
				IsClusterScoped:      !rl.APIResources[i].Namespaced,
			})
		}
	}

	return watchableGroupVersionResources, errors.NewAggregate(allErr)
}
