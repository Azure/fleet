/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package work

import (
	"time"

	"k8s.io/klog/v2"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/utils"
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
