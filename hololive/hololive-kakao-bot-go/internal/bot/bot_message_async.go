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

package bot

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (b *Bot) executeCommand(ctx context.Context, cmdCtx *domain.CommandContext, cmdType domain.CommandType, params map[string]any) error {
	return b.ensureCommandExecutor().Execute(ctx, cmdCtx, cmdType, params)
}

func (b *Bot) executeCommandAsync(
	ctx context.Context,
	cmdCtx *domain.CommandContext,
	cmdType domain.CommandType,
	params map[string]any,
	commandType string,
	chatID string,
) {
	base := context.WithoutCancel(ctx)
	asyncCtx, cancel := context.WithTimeout(base, constants.RequestTimeout.WebhookProcessing)

	task := func() {
		defer cancel()

		defer func() {
			if r := recover(); r != nil && b.logger != nil {
				b.logger.Error(
					"Panic in async command handler",
					slog.Any("panic", r),
					slog.String("command", commandType),
				)
			}
		}()

		if err := b.executeCommand(asyncCtx, cmdCtx, cmdType, params); err != nil {
			b.logger.Error("Failed to execute command", slog.Any("error", err))

			errorMsg := b.getErrorMessage(err, commandType)

			if chatID != "" {
				if sendErr := b.sendError(asyncCtx, chatID, errorMsg); sendErr != nil {
					b.logger.Error("Failed to send command error message", slog.Any("error", sendErr), slog.String("chat_id", chatID))
				}
			}
		}
	}

	if b.workerPool != nil {
		submitErr := b.workerPool.Submit(task)
		if submitErr == nil {
			return
		}

		if b.logger != nil {
			b.logger.Warn(
				"Failed to submit async command task to worker pool; falling back to goroutine",
				slog.String("command", commandType),
				slog.Any("error", submitErr),
			)
		}
	}

	go task()
}
