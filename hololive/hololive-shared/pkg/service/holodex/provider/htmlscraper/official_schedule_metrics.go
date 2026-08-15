package htmlscraper

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type officialScheduleReason string

const (
	officialScheduleReasonMatched     officialScheduleReason = "matched"
	officialScheduleReasonEmpty       officialScheduleReason = "empty"
	officialScheduleReasonRequest     officialScheduleReason = "request"
	officialScheduleReasonContext     officialScheduleReason = "context"
	officialScheduleReasonTransport   officialScheduleReason = "transport"
	officialScheduleReasonStatus      officialScheduleReason = "status"
	officialScheduleReasonContentType officialScheduleReason = "content_type"
	officialScheduleReasonDecode      officialScheduleReason = "decode"
	officialScheduleReasonSchema      officialScheduleReason = "schema"
	officialScheduleReasonOversize    officialScheduleReason = "oversize"
	officialScheduleReasonUnknown     officialScheduleReason = "unknown"
)

var (
	officialScheduleMetricsOnce        sync.Once
	officialScheduleFallbackTotal      *prometheus.CounterVec
	officialScheduleRequestsTotal      *prometheus.CounterVec
	officialScheduleRequestDuration    *prometheus.HistogramVec
	officialScheduleResponseBytes      *prometheus.HistogramVec
	officialScheduleRowsTotal          *prometheus.CounterVec
	officialScheduleLastSuccessSeconds *prometheus.GaugeVec
)

func initOfficialScheduleMetrics() {
	officialScheduleMetricsOnce.Do(func() {
		officialScheduleFallbackTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_holodex_official_schedule_fallback_total",
				Help: "Total official schedule fallback outcomes grouped by operation and reason.",
			},
			[]string{"operation", "outcome", "reason"},
		)
		officialScheduleRequestsTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_official_schedule_requests_total",
				Help: "Total official schedule API requests grouped by outcome and reason.",
			},
			[]string{"source", "outcome", "reason"},
		)
		officialScheduleRequestDuration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "hololive_official_schedule_request_duration_seconds",
				Help: "Official schedule API request and decode latency.",
			},
			[]string{"source", "outcome"},
		)
		officialScheduleResponseBytes = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "hololive_official_schedule_response_bytes",
				Help: "Official schedule API response size in bytes.",
			},
			[]string{"source"},
		)
		officialScheduleRowsTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_official_schedule_rows_total",
				Help: "Official schedule API video rows grouped by mapping result.",
			},
			[]string{"source", "result"},
		)
		officialScheduleLastSuccessSeconds = promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "hololive_official_schedule_last_success_timestamp_seconds",
				Help: "Unix timestamp of the latest successful official schedule API response.",
			},
			[]string{"source"},
		)
	})
}

func observeOfficialScheduleFallback(operation, outcome string, reason officialScheduleReason) {
	initOfficialScheduleMetrics()
	officialScheduleFallbackTotal.WithLabelValues(operation, outcome, string(reason)).Inc()
}

func observeOfficialScheduleRequest(outcome string, reason officialScheduleReason, duration time.Duration) {
	initOfficialScheduleMetrics()
	officialScheduleRequestsTotal.WithLabelValues("api", outcome, string(reason)).Inc()
	officialScheduleRequestDuration.WithLabelValues("api", outcome).Observe(duration.Seconds())
}

func observeOfficialScheduleResponseBytes(size int) {
	initOfficialScheduleMetrics()
	officialScheduleResponseBytes.WithLabelValues("api").Observe(float64(size))
}

func observeOfficialScheduleRows(stats officialScheduleRowStats) {
	initOfficialScheduleMetrics()
	observeOfficialScheduleRowResult("valid", stats.Valid)
	observeOfficialScheduleRowResult("invalid", stats.Invalid)
	observeOfficialScheduleRowResult("unmapped", stats.Unmapped)
	observeOfficialScheduleRowResult("duplicate", stats.Duplicate)
}

func observeOfficialScheduleRowResult(result string, count int) {
	if count <= 0 {
		return
	}
	officialScheduleRowsTotal.WithLabelValues("api", result).Add(float64(count))
}

func markOfficialScheduleSuccess() {
	initOfficialScheduleMetrics()
	officialScheduleLastSuccessSeconds.WithLabelValues("api").SetToCurrentTime()
}

func classifyOfficialScheduleReason(err error, matchedStreams int) officialScheduleReason {
	if err == nil {
		if matchedStreams > 0 {
			return officialScheduleReasonMatched
		}
		return officialScheduleReasonEmpty
	}
	if IsStructureError(err) {
		return officialScheduleReasonSchema
	}

	var sourceErr *officialScheduleSourceError
	if errors.As(err, &sourceErr) && sourceErr.reason != "" {
		return sourceErr.reason
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return officialScheduleReasonContext
	}
	return officialScheduleReasonUnknown
}
