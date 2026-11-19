package subscriptions

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
