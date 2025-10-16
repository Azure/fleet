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

// Package shared contains metrics emitted by both the hub-agent and member-agent.
package shared

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
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

func init() {
	metrics.Registry.MustRegister(
		JoinResultMetrics,
		LeaveResultMetrics,
	)
}
