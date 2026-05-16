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

package orchestration

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedlog "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
)

const asyncCommandBackpressureMessage = "요청이 많아 잠시 후 다시 시도해주세요."

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
	task := b.asyncCommandTask(asyncCtx, cancel, cmdCtx, cmdType, params, commandType, chatID)

	if b.workerPool == nil {
		sharedlog.Warn(ctx, b.logger, EventBotCommandAsyncRejected, "async command worker pool missing; running synchronously",
			slog.String("command", commandType),
		)

		task()
		return
	}

	submitErr := b.workerPool.Submit(task)
	if submitErr == nil {
		return
	}

	b.handleAsyncCommandSubmitError(cancel, submitErr, commandType, chatID)
}

func (b *Bot) asyncCommandTask(
	ctx context.Context,
	cancel context.CancelFunc,
	cmdCtx *domain.CommandContext,
	cmdType domain.CommandType,
	params map[string]any,
	commandType string,
	chatID string,
) func() {
	return func() {
		defer cancel()
		defer b.recoverAsyncCommandPanic(commandType)

		if err := b.executeCommand(ctx, cmdCtx, cmdType, params); err != nil {
			b.handleAsyncCommandError(ctx, err, commandType, chatID)
		}
	}
}

func (b *Bot) recoverAsyncCommandPanic(commandType string) {
	if r := recover(); r != nil {
		sharedlog.Error(
			context.Background(),
			b.logger,
			EventBotCommandPanic,
			"panic in async command handler",
			slog.Any("panic", r),
			slog.String("command", commandType),
		)
	}
}

func (b *Bot) handleAsyncCommandError(ctx context.Context, err error, commandType string, chatID string) {
	errorMsg := b.getErrorMessage(err, commandType)
	if chatID == "" {
		return
	}

	if sendErr := b.sendError(ctx, chatID, errorMsg); sendErr != nil {
		attrs := []slog.Attr{
			slog.String("chat_id", chatID),
			slog.String("command", commandType),
		}
		attrs = append(attrs, sharedlog.ErrorAttrs(sendErr)...)
		sharedlog.Error(ctx, b.logger, EventBotCommandErrorResponseFailed, "failed to send command error response", attrs...)
	}
}

func (b *Bot) handleAsyncCommandSubmitError(
	cancel context.CancelFunc,
	submitErr error,
	commandType string,
	chatID string,
) {
	attrs := []slog.Attr{
		slog.String("command", commandType),
	}
	attrs = append(attrs, sharedlog.ErrorAttrs(submitErr)...)
	sharedlog.Warn(context.Background(), b.logger, EventBotCommandAsyncRejected, "async command rejected by worker pool", attrs...)

	cancel()

	if chatID == "" {
		return
	}

	notifyCtx, notifyCancel := context.WithTimeout(context.Background(), constants.RequestTimeout.WebhookProcessing)
	defer notifyCancel()

	if err := b.sendError(notifyCtx, chatID, asyncCommandBackpressureMessage); err != nil && b.logger != nil {
		attrs := []slog.Attr{
			slog.String("chat_id", chatID),
			slog.String("command", commandType),
		}
		attrs = append(attrs, sharedlog.ErrorAttrs(err)...)
		sharedlog.Error(notifyCtx, b.logger, EventBotCommandErrorResponseFailed, "failed to send async backpressure message", attrs...)
	}
}
