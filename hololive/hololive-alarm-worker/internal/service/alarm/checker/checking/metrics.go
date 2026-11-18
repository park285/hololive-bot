package checking

import (
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
)

type checkerMetricSet struct {
	upcomingDecisionTotal *prometheus.CounterVec
	liveCatchupTotal      *prometheus.CounterVec
	persistedLiveTotal    *prometheus.CounterVec
	liveGuardrailTotal    *prometheus.CounterVec
}

var checkerMetrics = sync.OnceValue(func() *checkerMetricSet {
	return &checkerMetricSet{
		upcomingDecisionTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_youtube_upcoming_decisions_total",
				Help: "Total YouTube upcoming alarm decisions by result, minute, selection, and evaluation window flags.",
			},
			[]string{"result", "minute", "selection", "window_capped", "initial_observation"},
		),
		liveCatchupTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_youtube_live_catchup_decisions_total",
				Help: "Total YouTube live catchup alarm decisions by result.",
			},
			[]string{"result"},
		),
		persistedLiveTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_youtube_persisted_live_sessions_total",
				Help: "Total YouTube persisted live session fallback observations by result and stream status.",
			},
			[]string{"result", "status"},
		),
		liveGuardrailTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_youtube_live_guardrail_total",
				Help: "Total YouTube live dispatch guardrail observations by result.",
			},
			[]string{"result"},
		),
	}
})

func observeYouTubeUpcomingDecision(result string, minute int, selection string, window targetpolicy.EvaluationWindow) {
	checkerMetrics().upcomingDecisionTotal.WithLabelValues(
		result,
		alarmMinuteLabel(minute),
		selection,
		strconv.FormatBool(window.Capped),
		strconv.FormatBool(window.InitialObservation),
	).Inc()
}

func observeYouTubeUpcomingNoMinuteDecision(result string, window targetpolicy.EvaluationWindow) {
	checkerMetrics().upcomingDecisionTotal.WithLabelValues(
		result,
		"none",
		"none",
		strconv.FormatBool(window.Capped),
		strconv.FormatBool(window.InitialObservation),
	).Inc()
}

func observeYouTubeLiveCatchup(result string) {
	checkerMetrics().liveCatchupTotal.WithLabelValues(result).Inc()
}

func observeYouTubePersistedLiveSessions(result, status string, count int) {
	if count <= 0 {
		return
	}

	checkerMetrics().persistedLiveTotal.WithLabelValues(result, status).Add(float64(count))
}

func observeYouTubeLiveGuardrail(result string) {
	checkerMetrics().liveGuardrailTotal.WithLabelValues(result).Inc()
}

func alarmMinuteLabel(minute int) string {
	if minute < 0 {
		return "negative"
	}

	if minute <= 60 {
		return strconv.Itoa(minute)
	}

	return alarmMinuteBucket(minute)
}

func alarmMinuteBucket(minute int) string {
	switch {
	case minute <= 120:
		return "61_120"
	case minute <= 360:
		return "121_360"
	default:
		return "360_plus"
	}
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
