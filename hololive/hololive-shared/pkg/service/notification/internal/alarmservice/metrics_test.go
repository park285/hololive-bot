// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package alarmservice

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

const (
	alarmCacheRebuildMetricName         = "hololive_alarm_cache_rebuild_total"
	alarmCacheRebuildDurationMetricName = "hololive_alarm_cache_rebuild_duration_seconds"
	alarmCacheRebuildLoadedMetricName   = "hololive_alarm_cache_rebuild_loaded"
)

func counterValueForLabels(t *testing.T, labels map[string]string) float64 {
	t.Helper()

	metricFamilies := gatherMetrics(t)
	for _, metricFamily := range metricFamilies {
		if metricFamily.GetName() != alarmCacheRebuildMetricName {
			continue
		}

		for _, metric := range metricFamily.GetMetric() {
			if metricLabelsMatch(metric.GetLabel(), labels) {
				return metric.GetCounter().GetValue()
			}
		}
	}

	return 0
}

func gaugeValueForLabels(t *testing.T, labels map[string]string) float64 {
	t.Helper()

	metricFamilies := gatherMetrics(t)
	for _, metricFamily := range metricFamilies {
		if metricFamily.GetName() != alarmCacheRebuildLoadedMetricName {
			continue
		}

		for _, metric := range metricFamily.GetMetric() {
			if metricLabelsMatch(metric.GetLabel(), labels) {
				return metric.GetGauge().GetValue()
			}
		}
	}

	return 0
}

func histogramCountForLabels(t *testing.T, labels map[string]string) uint64 {
	t.Helper()

	metricFamilies := gatherMetrics(t)
	for _, metricFamily := range metricFamilies {
		if metricFamily.GetName() != alarmCacheRebuildDurationMetricName {
			continue
		}

		for _, metric := range metricFamily.GetMetric() {
			if metricLabelsMatch(metric.GetLabel(), labels) {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}

	return 0
}

func gatherMetrics(t *testing.T) []*dto.MetricFamily {
	t.Helper()

	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	return metricFamilies
}

func metricLabelsMatch(labelPairs []*dto.LabelPair, labels map[string]string) bool {
	if len(labelPairs) != len(labels) {
		return false
	}

	for _, labelPair := range labelPairs {
		if labels[labelPair.GetName()] != labelPair.GetValue() {
			return false
		}
	}

	return true
}

func TestAlarmOperationResult(t *testing.T) {
	t.Parallel()

	if got := alarmOperationResult(nil); got != "ok" {
		t.Fatalf("alarmOperationResult(nil) = %q, want %q", got, "ok")
	}

	if got := alarmOperationResult(errors.New("boom")); got != "error" {
		t.Fatalf("alarmOperationResult(err) = %q, want %q", got, "error")
	}
}
