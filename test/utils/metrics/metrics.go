/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package metrics provides utilities for testing emitted Prometheus metrics.
package metrics

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	prometheusclientmodel "github.com/prometheus/client_model/go"
)

var (
	// MetricsCmpOptions are options for comparing Prometheus metrics.
	MetricsCmpOptions = []cmp.Option{
		cmpopts.SortSlices(func(a, b *prometheusclientmodel.Metric) bool {
			return a.GetGauge().GetValue() < b.GetGauge().GetValue() // sort by time
		}),
		cmpopts.SortSlices(func(a, b *prometheusclientmodel.LabelPair) bool {
			return a.GetName() < b.GetName() // sort by label name
		}),
		cmp.Comparer(func(a, b *prometheusclientmodel.Gauge) bool {
			return (a.GetValue() > 0) == (b.GetValue() > 0)
		}),
		cmpopts.IgnoreUnexported(prometheusclientmodel.Metric{}, prometheusclientmodel.LabelPair{}, prometheusclientmodel.Gauge{}),
	}
)
