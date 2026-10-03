package handlers

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 도움말·달력 이미지 응답의 텍스트 대체 예외 계약(docs/current/services/hololive-api.md)의 telemetry다.
// 사유 render_failed·send_failed는 텍스트를 보낸 경우이고, outcome_unknown은 이미지가 이미 전달됐을 수 있어 텍스트를 보내지 않은 경우다.
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
