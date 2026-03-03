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

// Package hub contains metrics exclusively emitted by the hub-agent.
package hub

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// FleetPlacementStatusLastTimeStampSeconds is a prometheus metric which keeps track of the last placement status.
	FleetPlacementStatusLastTimeStampSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "fleet_workload_placement_status_last_timestamp_seconds",
		Help: "Last update timestamp of placement status in seconds",
	}, []string{"namespace", "name", "generation", "conditionType", "status", "reason"})

	// FleetEvictionStatus is prometheus metrics which holds the
	// status of eviction completion.
	FleetEvictionStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "fleet_workload_eviction_complete",
		Help: "Last update timestamp of eviction complete status in seconds",
	}, []string{"name", "isCompleted", "isValid"})

	// FleetUpdateRunStatusLastTimestampSeconds is a prometheus metric which holds the
	// last update timestamp of update run status in seconds.
	FleetUpdateRunStatusLastTimestampSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "fleet_workload_update_run_status_last_timestamp_seconds",
		Help: "Last update timestamp of update run status in seconds",
	}, []string{"namespace", "name", "state", "condition", "status", "reason"})

	// FleetUpdateRunApprovalRequestLatencySeconds tracks how long users take to approve approval requests.
	FleetUpdateRunApprovalRequestLatencySeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleet_workload_update_run_approval_request_latency_seconds",
		Help: "The latency from approval request creation to user approval in seconds",
		// Buckets: 1min, 5min, 15min, 30min, 1hr, 2hr, 6hr, 12hr, 24hr
		Buckets: []float64{60, 300, 900, 1800, 3600, 7200, 21600, 43200, 86400},
	}, []string{"namespace", "name", "taskType"})

	// FleetUpdateRunStageClusterUpdatingDurationSeconds tracks the duration of each stage of an update run, excluding the execution time of stage tasks.
	FleetUpdateRunStageClusterUpdatingDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleet_workload_update_run_stage_cluster_updating_duration_seconds",
		Help: "The duration the stage of an update run in seconds without stage tasks execution time",
		// Buckets: 15s, 30s, 1min, 2min, 5min, 10min, 30min, 1hr
		Buckets: []float64{15, 30, 60, 120, 300, 600, 1800, 3600},
	}, []string{"namespace", "name"})
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

func init() {
	metrics.Registry.MustRegister(
		FleetPlacementStatusLastTimeStampSeconds,
		FleetEvictionStatus,
		FleetUpdateRunStatusLastTimestampSeconds,
		FleetUpdateRunApprovalRequestLatencySeconds,
		FleetUpdateRunStageClusterUpdatingDurationSeconds,
		SchedulingCycleDurationMilliseconds,
		SchedulerActiveWorkers,
	)
}
