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

package readiness

import (
	"fmt"
	"net/http"

	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
)

// TO-DO (chenyu1): the readiness check below verifies if all the caches have been sync'd;
// as an informer is considered synced once it has performed its initial list, checking the
// sync status repeatedly (be default every 10 seconds) might not be very performant. We should
// find a better way to handle this situation.

// InformerReadinessChecker creates a readiness check function that verifies
// all resource informer caches are synced before marking the pod as ready.
// This prevents components from processing requests before the discovery cache is populated.
func InformerReadinessChecker(resourceInformer informer.Manager) func(*http.Request) error {
	return func(_ *http.Request) error {
		if resourceInformer == nil {
			klog.V(2).InfoS("Readiness check failed: resource informer is nil")
			return fmt.Errorf("resource informer is nil")
		}

		// Require ALL informer caches to be synced before marking ready
		allResources := resourceInformer.GetAllResources()
		if len(allResources) == 0 {
			// This can happen during startup when the ResourceInformer is created but the InformerPopulator
			// hasn't discovered and registered any resources yet via AddDynamicResources().
			klog.V(2).InfoS("Readiness check failed: no resources registered in resource informer yet")
			return fmt.Errorf("resource informer not ready: no resources registered")
		}

		// Check that ALL informers have synced
		unsyncedResources := []schema.GroupVersionResource{}
		for _, gvr := range allResources {
			if !resourceInformer.IsInformerSynced(gvr) {
				unsyncedResources = append(unsyncedResources, gvr)
			}
		}

		if len(unsyncedResources) > 0 {
			klog.V(2).InfoS("Readiness check failed: some resource informers are not synced yet",
				"countOfUnsyncedInformers", len(unsyncedResources),
				"countOfTotalInformers", len(allResources))
			return fmt.Errorf("resource informer not ready: %d/%d informers not synced yet", len(unsyncedResources), len(allResources))
		}

		klog.V(5).InfoS("All resource informers synced", "totalInformers", len(allResources))
		return nil
	}
}
