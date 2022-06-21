package utils

type MetricsOperation string

const (
	MetricsOperationJoin  MetricsOperation = "join" //
	MetricsOperationLeave MetricsOperation = "leave"
)

const (
	MetricsSuccessResult = "success"
	MetricsFailureResult = "failure"
)
