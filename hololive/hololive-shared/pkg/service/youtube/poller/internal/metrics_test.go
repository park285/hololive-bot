package polling

import (
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPollerMetricsUseDomainAwareNamesAndLabels(t *testing.T) {
	m, reg := newIsolatedPollerMetrics(t)

	m.SchedulerRegisteredJobs.Set(2)
	m.SchedulerDispatchDefer.WithLabelValues("").Inc()
	m.SchedulerPollDuration.WithLabelValues("videos", "success").Observe(0.25)
	m.ObserveJobClaim("videos", "acquired")
	m.ObserveJobLeaseRenew("", "success")
	m.ObserveJobMarkCompleted("resolver", "lost")
	m.ObserveJobDefer("resolver", "success")
	m.ObserveJobRelease("resolver", "error")
	m.ObserveOutboxInsert(domain.OutboxKindNewVideo, "success", 2)
	m.CommunityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeShorts)).Add(3)

	families, err := reg.Gather()
	require.NoError(t, err)
	assertMetricNamesAreDomainScoped(t, families)
	assertMetricLabelsAreUnique(t, families)

	assertGaugeValue(t, families, "youtube_poller_scheduler_job_count", nil, 2)
	assertCounterValue(t, families, "youtube_poller_scheduler_dispatch_deferred_total", map[string]string{
		"reason": "",
	}, 1)
	assertHistogramLabels(t, families, "youtube_poller_poll_duration_seconds", map[string]string{
		"poller": "videos",
		"status": "success",
	})
	assertCounterValue(t, families, "youtube_poller_job_claim_total", map[string]string{
		"poller": "videos",
		"result": "acquired",
	}, 1)
	assertCounterValue(t, families, "youtube_poller_job_lease_renew_total", map[string]string{
		"poller": "",
		"result": "success",
	}, 1)
	assertCounterValue(t, families, "youtube_poller_job_mark_completed_total", map[string]string{
		"poller": "resolver",
		"result": "lost",
	}, 1)
	assertCounterValue(t, families, "youtube_poller_job_defer_total", map[string]string{
		"poller": "resolver",
		"result": "success",
	}, 1)
	assertCounterValue(t, families, "youtube_poller_job_release_total", map[string]string{
		"poller": "resolver",
		"result": "error",
	}, 1)
	assertCounterValue(t, families, "youtube_poller_outbox_insert_total", map[string]string{
		"kind":   string(domain.OutboxKindNewVideo),
		"result": "success",
	}, 2)
	assertCounterValue(t, families, "youtube_poller_community_shorts_detected_posts_total", map[string]string{
		"alarm_type": string(domain.AlarmTypeShorts),
	}, 3)
}

func TestObserveOutboxInsertSkipsNonPositiveCounts(t *testing.T) {
	m, reg := newIsolatedPollerMetrics(t)

	m.ObserveOutboxInsert(domain.OutboxKindNewVideo, "success", 0)
	m.ObserveOutboxInsert(domain.OutboxKindNewVideo, "success", -1)

	families, err := reg.Gather()
	require.NoError(t, err)
	assert.Nil(t, metricFamilyByName(families, "youtube_poller_outbox_insert_total"))
}

func TestBoolResultMapsClaimOutcomesToMetricLabels(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
		err  error
		want string
	}{
		{name: "nil error and true result", ok: true, err: nil, want: "success"},
		{name: "nil error and false result", ok: false, err: nil, want: "lost"},
		{name: "error overrides true result", ok: true, err: errors.New("renew failed"), want: "error"},
		{name: "error overrides false result", ok: false, err: errors.New("release failed"), want: "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BoolResult(tt.ok, tt.err))
		})
	}
}

func newIsolatedPollerMetrics(t *testing.T) (*Metrics, *prometheus.Registry) {
	t.Helper()

	reg := prometheus.NewRegistry()
	oldRegisterer := prometheus.DefaultRegisterer
	oldGatherer := prometheus.DefaultGatherer

	prometheus.DefaultRegisterer = reg
	prometheus.DefaultGatherer = reg

	m := &Metrics{}
	m.registerSchedulerMetrics()
	m.registerJobClaimMetrics()
	m.registerContentMetrics()

	t.Cleanup(func() {
		prometheus.DefaultRegisterer = oldRegisterer
		prometheus.DefaultGatherer = oldGatherer
	})

	return m, reg
}

func assertMetricNamesAreDomainScoped(t *testing.T, families []*dto.MetricFamily) {
	t.Helper()

	require.NotEmpty(t, families)
	for _, family := range families {
		assert.True(t, strings.HasPrefix(family.GetName(), "youtube_poller_"), "metric name %q", family.GetName())
	}
}

func assertMetricLabelsAreUnique(t *testing.T, families []*dto.MetricFamily) {
	t.Helper()

	for _, family := range families {
		for _, metric := range family.GetMetric() {
			seen := make(map[string]struct{}, len(metric.GetLabel()))
			for _, label := range metric.GetLabel() {
				name := label.GetName()
				if _, ok := seen[name]; ok {
					t.Fatalf("metric %s has duplicate label key %q", family.GetName(), name)
				}
				seen[name] = struct{}{}
			}
		}
	}
}

func assertCounterValue(t *testing.T, families []*dto.MetricFamily, name string, labels map[string]string, want float64) {
	t.Helper()

	metric := requireMetric(t, families, name, labels)
	require.NotNil(t, metric.GetCounter(), "metric %s must be a counter", name)
	assert.Equal(t, want, metric.GetCounter().GetValue())
}

func assertGaugeValue(t *testing.T, families []*dto.MetricFamily, name string, labels map[string]string, want float64) {
	t.Helper()

	metric := requireMetric(t, families, name, labels)
	require.NotNil(t, metric.GetGauge(), "metric %s must be a gauge", name)
	assert.Equal(t, want, metric.GetGauge().GetValue())
}

func assertHistogramLabels(t *testing.T, families []*dto.MetricFamily, name string, labels map[string]string) {
	t.Helper()

	metric := requireMetric(t, families, name, labels)
	require.NotNil(t, metric.GetHistogram(), "metric %s must be a histogram", name)
	assert.Equal(t, uint64(1), metric.GetHistogram().GetSampleCount())
}

func requireMetric(t *testing.T, families []*dto.MetricFamily, name string, labels map[string]string) *dto.Metric {
	t.Helper()

	family := metricFamilyByName(families, name)
	require.NotNil(t, family, "metric family %s", name)
	for _, metric := range family.GetMetric() {
		if labelsMatch(metric.GetLabel(), labels) {
			return metric
		}
	}
	require.Failf(t, "metric labels not found", "metric %s labels %v", name, labels)
	return nil
}

func metricFamilyByName(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	return nil
}

func labelsMatch(labels []*dto.LabelPair, want map[string]string) bool {
	if len(labels) != len(want) {
		return false
	}
	got := make(map[string]string, len(labels))
	for _, label := range labels {
		got[label.GetName()] = label.GetValue()
	}
	for key, value := range want {
		if got[key] != value {
			return false
		}
	}
	return true
}
