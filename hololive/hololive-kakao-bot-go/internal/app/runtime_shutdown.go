package app

import (
	"context"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
)

// Shutdown: 봇의 모든 구성 요소를 안전하게 종료하고 리소스를 정리합니다.
func (r *BotRuntime) Shutdown(ctx context.Context) {
	if r == nil {
		return
	}

	if r.clearAlarmSchedulerCancel() {
		r.logInfo("Alarm runtime scheduler cancellation signaled")
	}

	if err := r.ShutdownHTTPServer(ctx); err != nil {
		r.logError("HTTP server shutdown error", err)
	}
	if r.webhookHandlerCloser != nil {
		if err := r.webhookHandlerCloser.Close(); err != nil {
			r.logError("Iris webhook handler shutdown error", err)
		} else {
			r.logInfo("Iris webhook handler stopped")
		}
	}
	if err := notification.CloseAllAlarmServices(ctx); err != nil {
		r.logError("Alarm service shutdown error", err)
	} else {
		r.logInfo("Alarm services stopped")
	}
	if r.Bot != nil {
		if err := r.Bot.Shutdown(ctx); err != nil {
			r.logError("Error during shutdown", err)
		}
	}
}
