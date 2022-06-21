package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Operation string

const (
	OperationJoin  Operation = "join"  // metrics for agent join operation
	OperationLeave Operation = "leave" // metrics for agent leave operation
)

const (
	SuccessResult = "success" // marks successful operation
	FailureResult = "failure" // marks failed operation
)

var (
	JoinLeaveResultMetrics = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "join_leave_result",
		Help: "Number of failed and successful Join/leaves operations",
	}, []string{"operation", "result"})
)

var (
	metricsResultMap = map[bool]string{
		true:  SuccessResult,
		false: FailureResult,
	}
)

var (
	ReportJoinLeaveResultMetric = func(operation Operation, successful bool) {
		JoinLeaveResultMetrics.With(prometheus.Labels{
			"operation": string(operation),
			"result":    metricsResultMap[successful],
		}).Inc()
	}
)
