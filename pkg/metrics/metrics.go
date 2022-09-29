/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	JoinResultMetrics = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "join_result_counter",
		Help: "Number of successful Join operations",
	}, []string{"result"})
	LeaveResultMetrics = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "leave_result_counter",
		Help: "Number of successful Leave operations",
	}, []string{"result"})
	WorkApplyTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "work_apply_time_seconds",
		Help: "Length of time between when a work resource is created/updated to when it is applied on the member cluster",
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
			1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5, 6, 7, 8, 9, 10, 15, 20, 25, 30, 40, 50, 60, 90, 120},
	}, []string{"name"})
	PlacementApplyFailedCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "placement_apply_failed_counter",
		Help: "Number of failed to apply cluster resource placement",
	}, []string{"name"})
	PlacementApplySucceedCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "placement_apply_succeed_counter",
		Help: "Number of successfully applied cluster resource placement",
	}, []string{"name"})
	PlacementApplyTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "placement_apply_time_seconds",
		Help: "Length of time between when a cluster resource placement is scheduled to when all of its resources are successfully applied on the selected cluster",
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
			1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5, 6, 7, 8, 9, 10, 15, 20, 25, 30, 40, 50, 60, 90, 120, 150, 180, 300},
	}, []string{"name"})
)

var (
	ReportJoinResultMetric = func() {
		JoinResultMetrics.With(prometheus.Labels{
			// Per team agreement, the failure result won't be reported from the agents as k8s controller would retry
			// failed reconciliations.
			"result": "success",
		}).Inc()
	}
	ReportLeaveResultMetric = func() {
		LeaveResultMetrics.With(prometheus.Labels{
			// Per team agreement, the failure result won't be reported from the agents as k8s controller would retry
			// failed reconciliations.
			"result": "success",
		}).Inc()
	}
)
