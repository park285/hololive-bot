package handlers

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	imageTextFallbackReasonRenderFailed   = "render_failed"
	imageTextFallbackReasonSendFailed     = "send_failed"
	imageTextFallbackReasonOutcomeUnknown = "outcome_unknown"
)

var imageTextFallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_bot_image_text_fallback_total",
	Help: "Help and calendar image replies replaced with text, by command and reason (render_failed, send_failed, outcome_unknown without text).",
}, []string{"command", "reason"})

func observeImageTextFallback(command, reason string) {
	imageTextFallbackTotal.WithLabelValues(command, reason).Inc()
}
