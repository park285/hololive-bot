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

package orchcmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	sharedlog "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type CommandRouter struct {
	registry       *handlers.Registry
	logger         *slog.Logger
	sendMessage    func(ctx context.Context, room, message string) error
	messageStrings *messagestrings.Store
	admission      *commandAdmissionPolicy
}

func NewCommandRouter(registry *handlers.Registry, logger *slog.Logger, sendMessage func(ctx context.Context, room, message string) error, messageStrings *messagestrings.Store, cacheClient cache.LowLevelCache) *CommandRouter {
	return &CommandRouter{
		registry:       registry,
		logger:         logger,
		sendMessage:    sendMessage,
		messageStrings: messageStrings,
		admission:      newCommandAdmissionPolicy(cacheClient),
	}
}

func (r *CommandRouter) Execute(ctx context.Context, cmdCtx *domain.CommandContext, cmdType domain.CommandType, params map[string]any) error {
	if r.registry == nil {
		return errors.New("command registry is not initialized")
	}

	key, normalizedParams := r.NormalizeCommand(cmdType, params)

	ctx = sharedlog.WithRuntime(ctx, "bot")
	ctx = sharedlog.WithComponent(ctx, "command")

	if err := r.admission.Admit(ctx, cmdCtx, key); err != nil {
		if handleErr := r.handleAdmissionError(ctx, cmdCtx, err); handleErr != nil {
			return fmt.Errorf("handle admission error: %w", handleErr)
		}

		return nil
	}

	started := time.Now()
	attrs := commandExecutionAttrs(cmdCtx, key, cmdType)
	sharedlog.Debug(ctx, r.logger, EventBotCommandExecuteStarted, "command execution started", attrs...)

	result := r.executeRegisteredCommand(ctx, cmdCtx, key, normalizedParams, started, attrs)
	if result.err != nil {
		return result.err
	}

	if result.handled {
		return nil
	}

	successAttrs := append([]slog.Attr{}, attrs...)

	successAttrs = append(successAttrs, sharedlog.SinceMS(started))
	sharedlog.Info(ctx, r.logger, EventBotCommandExecuteSucceeded, "command execution succeeded", successAttrs...)

	return nil
}

type registeredCommandResult struct {
	handled bool
	err     error
}

func (r *CommandRouter) executeRegisteredCommand(
	ctx context.Context,
	cmdCtx *domain.CommandContext,
	key string,
	params map[string]any,
	started time.Time,
	attrs []slog.Attr,
) registeredCommandResult {
	err := r.registry.Execute(ctx, cmdCtx, key, params)
	if err == nil {
		return registeredCommandResult{}
	}

	if errors.Is(err, handlers.ErrUnknownCommand) {
		warnAttrs := append(append([]slog.Attr{}, attrs...), sharedlog.SinceMS(started))
		sharedlog.Warn(ctx, r.logger, EventBotCommandUnknown, "unknown command", warnAttrs...)

		unknownMessage := r.messageStrings.GetOrContext(ctx, messagestrings.NamespaceError, "unknown_command", messagestrings.FallbackSentinel)
		if sendErr := r.sendMessage(ctx, cmdCtx.Room, unknownMessage); sendErr != nil {
			return registeredCommandResult{handled: true, err: fmt.Errorf("failed to send unknown command message: %w", sendErr)}
		}

		return registeredCommandResult{handled: true}
	}

	failedAttrs := append(append([]slog.Attr{}, attrs...), sharedlog.SinceMS(started))

	failedAttrs = append(failedAttrs, sharedlog.ErrorAttrs(err)...)
	r.logExecutionFailure(ctx, err, failedAttrs)

	return registeredCommandResult{handled: true, err: fmt.Errorf("execute command: %w", err)}
}

// outcome unknown은 실패가 아니라 미확정이므로 Error 집계에서 분리한다.
func (r *CommandRouter) logExecutionFailure(ctx context.Context, err error, attrs []slog.Attr) {
	if errors.Is(err, transport.ErrReplyOutcomeUnknown) {
		sharedlog.Warn(ctx, r.logger, EventBotReplyOutcomeUnknown, "command reply outcome unknown", attrs...)

		return
	}

	sharedlog.Error(ctx, r.logger, EventBotCommandExecuteFailed, "command execution failed", attrs...)
}

func (r *CommandRouter) handleAdmissionError(ctx context.Context, cmdCtx *domain.CommandContext, err error) error {
	if !errors.Is(err, errCommandRateLimited) {
		return fmt.Errorf("admit command: %w", err)
	}

	if r.sendMessage == nil || cmdCtx == nil || cmdCtx.Room == "" {
		return fmt.Errorf("report command rate limit: %w", err)
	}

	if sendErr := r.sendMessage(ctx, cmdCtx.Room, expensiveHistoryRateLimitMessage); sendErr != nil {
		return fmt.Errorf("send command rate limit message: %w", sendErr)
	}

	return nil
}

// NormalizeCommand 명령어 타입과 파라미터를 정규화합니다.
func (r *CommandRouter) NormalizeCommand(cmdType domain.CommandType, params map[string]any) (commandKey string, normalizedParams map[string]any) {
	return NormalizeCommandKey(cmdType, params)
}
