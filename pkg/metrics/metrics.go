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
