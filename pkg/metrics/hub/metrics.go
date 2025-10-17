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
	// These 2 metrics are used in v1alpha1 controller, should be soon deprecated.
	PlacementApplyFailedCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "placement_apply_failed_counter",
		Help: "Number of failed to apply cluster resource placement",
	}, []string{"name"})
	PlacementApplySucceedCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "placement_apply_succeed_counter",
		Help: "Number of successfully applied cluster resource placement",
	}, []string{"name"})

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
	}, []string{"namespace", "name", "generation", "condition", "status", "reason"})
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
		PlacementApplyFailedCount,
		PlacementApplySucceedCount,
		FleetPlacementStatusLastTimeStampSeconds,
		FleetEvictionStatus,
		FleetUpdateRunStatusLastTimestampSeconds,
		SchedulingCycleDurationMilliseconds,
		SchedulerActiveWorkers,
	)
}
