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

package botruntime

import (
	"context"
	"errors"
	"fmt"

	applifecycle "github.com/kapu/hololive-shared/pkg/applifecycle"
)

func (r *BotRuntime) closeWebhook(ctx context.Context) error {
	if r.webhookHandlerCloser == nil {
		return nil
	}

	if err := r.webhookHandlerCloser.CloseContext(ctx); err != nil {
		return fmt.Errorf("close context: %w", err)
	}

	return nil
}

func (r *BotRuntime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.cancelBackgroundTasks()
	r.stopHTTPRequestAdmission()

	// HTTP → webhook·durability → bot 순서로 종료한다. 앞 단계가 실패해도 뒤 단계를 건너뛰면 webhook 수신이나
	// bot 작업이 남으므로, 세 단계를 모두 실행한 뒤 오류를 합친다.
	httpErr := applifecycle.Shutdown(ctx, applifecycle.ShutdownHooks{
		Logger:             r.Logger,
		ShutdownHTTPServer: r.ShutdownHTTPServer,
	})
	usersErr := r.shutdownWebhookAndDurability(ctx)
	webhookErr := r.reportWebhookShutdown(usersErr)
	botErr := r.shutdownBot(ctx)
	shutdownErr := errors.Join(httpErr, webhookErr, botErr)
	joinErr := r.joinBackgroundTasks(ctx)
	requestErr := r.joinHTTPRequests(ctx)

	err := errors.Join(shutdownErr, joinErr, requestErr)
	quiesced := usersErr == nil && requestErr == nil && r.backgroundTasksJoined()

	r.lifecycleMu.Lock()

	r.shutdownErr = err

	if quiesced {
		r.quiesced.Store(true)
	}

	r.lifecycleMu.Unlock()

	return err
}

func (r *BotRuntime) reportWebhookShutdown(err error) error {
	if err != nil {
		r.logError("Iris webhook handler shutdown error", err)

		return fmt.Errorf("close Iris webhook handler: %w", err)
	}

	r.logInfo("Iris webhook handler stopped")

	return nil
}

func (r *BotRuntime) shutdownBot(ctx context.Context) error {
	if r.Bot == nil {
		return nil
	}

	if err := r.Bot.Shutdown(ctx); err != nil {
		r.logError("Error during shutdown", err)

		return fmt.Errorf("shutdown bot: %w", err)
	}

	return nil
}

func (r *BotRuntime) shutdownWebhookAndDurability(ctx context.Context) error {
	closeErr := r.closeWebhook(ctx)

	if r.durable == nil {
		if closeErr != nil {
			return fmt.Errorf("close webhook: %w", closeErr)
		}

		return nil
	}

	return errors.Join(closeErr, r.durable.Stop(ctx))
}
