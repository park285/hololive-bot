package holodexprovider

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type officialScheduleFallbackReason string

const (
	officialScheduleFallbackReasonMatched        officialScheduleFallbackReason = "matched"
	officialScheduleFallbackReasonEmpty          officialScheduleFallbackReason = "empty"
	officialScheduleFallbackReasonNetwork        officialScheduleFallbackReason = "network"
	officialScheduleFallbackReasonParse          officialScheduleFallbackReason = "parse"
	officialScheduleFallbackReasonStructureDrift officialScheduleFallbackReason = "structure_drift"
	officialScheduleFallbackReasonUnknown        officialScheduleFallbackReason = "unknown"
)

var (
	officialScheduleMetricsOnce   sync.Once
	officialScheduleFallbackTotal *prometheus.CounterVec
)

type officialScheduleReasonError struct {
	reason officialScheduleFallbackReason
	err    error
}

func (e *officialScheduleReasonError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *officialScheduleReasonError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func initOfficialScheduleMetrics() {
	officialScheduleMetricsOnce.Do(func() {
		officialScheduleFallbackTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_holodex_official_schedule_fallback_total",
				Help: "Total official schedule fallback outcomes grouped by operation and reason.",
			},
			[]string{"operation", "outcome", "reason"},
		)
	})
}

func observeOfficialScheduleFallback(operation, outcome string, reason officialScheduleFallbackReason) {
	initOfficialScheduleMetrics()
	officialScheduleFallbackTotal.WithLabelValues(operation, outcome, string(reason)).Inc()
}

func wrapOfficialScheduleError(reason officialScheduleFallbackReason, err error) error {
	if err == nil {
		return nil
	}
	return &officialScheduleReasonError{reason: reason, err: err}
}

func classifyOfficialScheduleFallbackReason(err error, matchedStreams int) officialScheduleFallbackReason {
	if err == nil {
		return classifyOfficialScheduleMatchedReason(matchedStreams)
	}

	if IsStructureError(err) {
		return officialScheduleFallbackReasonStructureDrift
	}

	if reason, ok := officialScheduleWrappedReason(err); ok {
		return reason
	}

	if isOfficialScheduleNetworkError(err) {
		return officialScheduleFallbackReasonNetwork
	}

	return classifyOfficialScheduleErrorMessage(err.Error())
}

func classifyOfficialScheduleMatchedReason(matchedStreams int) officialScheduleFallbackReason {
	if matchedStreams > 0 {
		return officialScheduleFallbackReasonMatched
	}
	return officialScheduleFallbackReasonEmpty
}

func officialScheduleWrappedReason(err error) (officialScheduleFallbackReason, bool) {
	var reasonErr *officialScheduleReasonError
	if errors.As(err, &reasonErr) && reasonErr.reason != "" {
		return reasonErr.reason, true
	}
	return "", false
}

func isOfficialScheduleNetworkError(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func classifyOfficialScheduleErrorMessage(message string) officialScheduleFallbackReason {
	switch {
	case strings.Contains(message, "HTML parse failed"):
		return officialScheduleFallbackReasonParse
	case strings.Contains(message, "HTTP request failed"):
		return officialScheduleFallbackReasonNetwork
	default:
		return officialScheduleFallbackReasonUnknown
	}
}
