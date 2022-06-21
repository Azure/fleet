package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Operation string

const (
	OperationJoin  Operation = "join"  // metrics for agent join operation
	OperationLeave Operation = "leave" // metrics for agent leave operation
)

var (
	JoinLeaveResultMetrics = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "join_leave_result",
		Help: "Number of failed and successful Join/leaves operations",
	}, []string{"operation", "result"})
)

var (
	ReportJoinLeaveResultMetric = func(operation Operation) {
		JoinLeaveResultMetrics.With(prometheus.Labels{
			"operation": string(operation),
			// Per team agreement, the failure result won't be reported from the agents as k8s controller would retry
			// failed reconciliations.
			"result": "success",
		}).Inc()
	}
)
