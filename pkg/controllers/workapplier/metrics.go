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

package workapplier

import (
	"time"

	"k8s.io/klog/v2"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/metrics"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

func trackWorkApplyLatencyMetric(work *fleetv1beta1.Work) {
	// Calculate the work apply latency.
	lastUpdateTime, ok := work.GetAnnotations()[utils.LastWorkUpdateTimeAnnotationKey]
	if ok {
		workUpdateTime, parseErr := time.Parse(time.RFC3339, lastUpdateTime)
		if parseErr != nil {
			klog.ErrorS(parseErr, "Failed to parse the last update timestamp on the work", "work", klog.KObj(work))
			return
		}

		latency := time.Since(workUpdateTime)
		metrics.WorkApplyTime.WithLabelValues(work.GetName()).Observe(latency.Seconds())
		klog.V(2).InfoS("Work has been applied", "work", klog.KObj(work), "latency", latency.Milliseconds())
	}

	klog.V(2).InfoS("No last update timestamp found on the Work object", "work", klog.KObj(work))
}
