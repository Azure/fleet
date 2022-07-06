/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// FleetReconcileTotal is a prometheus counter metrics which holds the total
	// number of reconciliations per controller. It has two labels. controller label refers
	// to the controller name and result label refers to the reconcile result i.e
	// success, error, requeue, requeue_after.
	FleetReconcileTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "fleet_workload_reconcile_total",
		Help: "Total number of reconciliations per controller",
	}, []string{"controller", "result"})

	// FleetReconcileErrors is a prometheus counter metrics which holds the total
	// number of errors from the Reconciler.
	FleetReconcileErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "fleet_workload_reconcile_errors_total",
		Help: "Total number of reconciliation errors per controller",
	}, []string{"controller"})

	// FleetReconcileTime is a prometheus metric which keeps track of the duration
	// of reconciliations.
	FleetReconcileTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleet_workload_reconcile_time_seconds",
		Help: "Length of time per reconciliation per controller",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.35, 0.4, 0.45, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
			1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5, 6, 7, 8, 9, 10, 15, 20, 25, 30, 40, 50, 60},
	}, []string{"controller"})

	// FleetWorkerCount is a prometheus metric which holds the number of
	// concurrent reconciles per controller.
	FleetWorkerCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "fleet_workload_max_concurrent_reconciles",
		Help: "Maximum number of concurrent reconciles per controller",
	}, []string{"controller"})

	// FleetActiveWorkers is a prometheus metric which holds the number
	// of active workers per controller.
	FleetActiveWorkers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "fleet_workload_active_workers",
		Help: "Number of currently used workers per controller",
	}, []string{"controller"})
)

func init() {
	metrics.Registry.MustRegister(
		FleetReconcileTotal,
		FleetReconcileErrors,
		FleetReconcileTime,
		FleetWorkerCount,
		FleetActiveWorkers,
	)
}
