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
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5, 0.7, 0.9, 1.0,
			1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5, 7, 9, 10, 15, 20, 30, 60, 120},
	}, []string{"name"})
	PlacementApplyFailedCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "placement_apply_failed_counter",
		Help: "Number of failed to apply cluster resource placement",
	}, []string{"name"})
	PlacementApplySucceedCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "placement_apply_succeed_counter",
		Help: "Number of successfully applied cluster resource placement",
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

// The scheduler related metrics.
var (
	// SchedulingCycleDurationMilliseconds is a Fleet scheduler metric that tracks how long it
	// takes to complete a scheduling loop run.
	SchedulingCycleDurationMilliseconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "scheduling_cycle_duration_milliseconds",
			Help: "The duration of a scheduling cycle run in milliseconds",
			Buckets: []float64{
				10, 50, 100, 500, 1000, 5000, 10000, 50000,
			},
		},
		[]string{
			"is_failed",
			"needs_requeue",
		},
	)

	// SchedulerActiveWorkers is a prometheus metric which holds the number of active scheduler loop.
	SchedulerActiveWorkers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "scheduling_active_workers",
		Help: "Number of currently running scheduling loop",
	}, []string{})
)
