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
	"log/slog"
	"os"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
)

// Run: 봇 애플리케이션을 실행하고 종료 신호(SIGINT, SIGTERM)를 대기한다. (블로킹).
func (r *BotRuntime) Run() {
	_ = lifecycle.Run(lifecycle.Options{
		ShutdownTimeout: constants.AppTimeout.Shutdown,
		Start: func(ctx context.Context, errCh chan<- error) {
			r.Start(ctx, errCh)
			r.Logger.Info("Bot started, waiting for signals...")
		},
		OnSignal: func(sig os.Signal) {
			r.Logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
		},
		OnError: func(err error) {
			r.Logger.Error("Server error", slog.Any("error", err))
		},
		BeforeShutdown: func() {
			r.Logger.Info("Shutting down gracefully...")
		},
		Shutdown: func(ctx context.Context) error {
			r.Shutdown(ctx)
			return nil
		},
	})
	r.Logger.Info("Shutdown complete")
}
