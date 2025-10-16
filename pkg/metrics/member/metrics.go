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

// Package member contains metrics exclusively emitted by the member-agent.
package member

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// This metric is used in v1alpha1 controller, should be soon deprecated.
	WorkApplyTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "work_apply_time_seconds",
		Help: "Length of time between when a work resource is created/updated to when it is applied on the member cluster",
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5, 0.7, 0.9, 1.0,
			1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5, 7, 9, 10, 15, 20, 30, 60, 120},
	}, []string{"name"})
)

// The work applier related metrics.
var (
	// FleetWorkProcessingRequestsTotal is a prometheus metric which counts the
	// total number of work object processing requests.
	//
	// The following labels are available:
	// * apply_status: the apply status of the processing request; see the list of
	//   work apply condition reasons in the work applier source code
	//   (pkg/controller/workapplier/controller.go) for possible values.
	//   if the work object does not need to be applied, the value is "Skipped".
	// * availability_status: the availability check status of the processing request;
	//   see the list of availability check condition reasons in the work applier source
	//   code (pkg/controller/workapplier/controller.go) for possible values.
	//   if the work object does not need an availability check, the value is "Skipped".
	// * diff_reporting_status: the diff reporting status of the processing request;
	//   see the list of diff reporting condition reasons in the work applier source
	//   code (pkg/controller/workapplier/controller.go) for possible values.
	//   if the work object does not need a diff reporting, the value is "Skipped".
	FleetWorkProcessingRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "fleet_work_processing_requests_total",
		Help: "Total number of processing requests of work objects, including retries and periodic checks",
	}, []string{"apply_status", "availability_status", "diff_reporting_status"})

	// FleetManifestProcessingRequestsTotal is a prometheus metric which counts the
	// total number of manifest object processing requests.
	//
	// The following labels are available:
	// * apply_status: the apply status of the processing request; see the list of
	//   apply result types in the work applier source code for possible values.
	//   if the manifest object does not need to be applied, the value is "Skipped".
	// * availability_status: the availability check status of the processing request;
	//   see the list of availability check result types in the work applier source code
	//   for possible values.
	//   if the manifest object does not need an availability check, the value is "Skipped".
	// * diff_reporting_status: the diff reporting status of the processing request;
	//   see the list of diff reporting result types in the work applier source code
	//   for possible values.
	//   if the manifest object does not need a diff reporting, the value is "Skipped".
	// * drift_detection_status: the drift detection status of the processing request;
	//   values can be "Found" and "NotFound".
	// * diff_detection_status: the diff detection status of the processing request;
	//   values can be "Found" and "NotFound".
	FleetManifestProcessingRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "fleet_manifest_processing_requests_total",
		Help: "Total number of processing requests of manifest objects, including retries and periodic checks",
	}, []string{"apply_status", "availability_status", "diff_reporting_status", "drift_detection_status", "diff_detection_status"})
)

func init() {
	metrics.Registry.MustRegister(
		WorkApplyTime,
		FleetWorkProcessingRequestsTotal,
		FleetManifestProcessingRequestsTotal,
	)
}
