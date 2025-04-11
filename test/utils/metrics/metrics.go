/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package metrics

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	prometheusclientmodel "github.com/prometheus/client_model/go"
)

var (
	MetricsCmpOptions = []cmp.Option{
		cmpopts.SortSlices(func(a, b *prometheusclientmodel.Metric) bool {
			return a.GetGauge().GetValue() < b.GetGauge().GetValue() // sort by time
		}),
		cmpopts.SortSlices(func(a, b *prometheusclientmodel.LabelPair) bool {
			if a.Name == nil || b.Name == nil {
				return a.Name == nil
			}
			return *a.Name < *b.Name // Sort by label
		}),
		cmp.Comparer(func(a, b *prometheusclientmodel.Gauge) bool {
			return (a.GetValue() > 0) == (b.GetValue() > 0)
		}),
		cmpopts.IgnoreUnexported(prometheusclientmodel.Metric{}, prometheusclientmodel.LabelPair{}, prometheusclientmodel.Gauge{}),
	}
)
