package checker

import (
	"strconv"
	"sync"

	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	checkerMetricsInitOnce sync.Once

	youtubeUpcomingDecisionTotal *prometheus.CounterVec
	youtubeLiveCatchupTotal      *prometheus.CounterVec
	youtubePersistedLiveTotal    *prometheus.CounterVec
	youtubeLiveGuardrailTotal    *prometheus.CounterVec
)

func initCheckerMetrics() {
	checkerMetricsInitOnce.Do(func() {
		youtubeUpcomingDecisionTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_youtube_upcoming_decisions_total",
				Help: "Total YouTube upcoming alarm decisions by result, minute, selection, and evaluation window flags.",
			},
			[]string{"result", "minute", "selection", "window_capped", "initial_observation"},
		)

		youtubeLiveCatchupTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_youtube_live_catchup_decisions_total",
				Help: "Total YouTube live catchup alarm decisions by result.",
			},
			[]string{"result"},
		)

		youtubePersistedLiveTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_youtube_persisted_live_sessions_total",
				Help: "Total YouTube persisted live session fallback observations by result and stream status.",
			},
			[]string{"result", "status"},
		)

		youtubeLiveGuardrailTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_youtube_live_guardrail_total",
				Help: "Total YouTube live dispatch guardrail observations by result.",
			},
			[]string{"result"},
		)
	})
}

func observeYouTubeUpcomingDecision(result string, minute int, selection string, window sharedchecker.EvaluationWindow) {
	initCheckerMetrics()
	youtubeUpcomingDecisionTotal.WithLabelValues(
		result,
		alarmMinuteLabel(minute),
		selection,
		strconv.FormatBool(window.Capped),
		strconv.FormatBool(window.InitialObservation),
	).Inc()
}

func observeYouTubeUpcomingNoMinuteDecision(result string, window sharedchecker.EvaluationWindow) {
	initCheckerMetrics()
	youtubeUpcomingDecisionTotal.WithLabelValues(
		result,
		"none",
		"none",
		strconv.FormatBool(window.Capped),
		strconv.FormatBool(window.InitialObservation),
	).Inc()
}

func observeYouTubeLiveCatchup(result string) {
	initCheckerMetrics()
	youtubeLiveCatchupTotal.WithLabelValues(result).Inc()
}

func observeYouTubePersistedLiveSessions(result, status string, count int) {
	if count <= 0 {
		return
	}
	initCheckerMetrics()
	youtubePersistedLiveTotal.WithLabelValues(result, status).Add(float64(count))
}

func observeYouTubeLiveGuardrail(result string) {
	initCheckerMetrics()
	youtubeLiveGuardrailTotal.WithLabelValues(result).Inc()
}

func alarmMinuteLabel(minute int) string {
	if minute < 0 {
		return "negative"
	}
	return strconv.Itoa(minute)
}

func youtubeUpcomingSelectionLabel(selected, current int, crossed bool) string {
	if !crossed {
		return "schedule_change_only"
	}
	if selected > current {
		return "recovered_crossing"
	}
	if selected == current {
		return "current_bucket"
	}
	return "lower_than_current"
}
