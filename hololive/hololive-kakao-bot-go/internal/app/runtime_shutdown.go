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
