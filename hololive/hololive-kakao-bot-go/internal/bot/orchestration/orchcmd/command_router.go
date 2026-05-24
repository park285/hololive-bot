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

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedlog "github.com/park285/shared-go/pkg/logging"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
)

type CommandRouter struct {
	registry    *command.Registry
	logger      *slog.Logger
	sendMessage func(ctx context.Context, room, message string) error
}

func NewCommandRouter(registry *command.Registry, logger *slog.Logger, sendMessage func(ctx context.Context, room, message string) error) *CommandRouter {
	return &CommandRouter{
		registry:    registry,
		logger:      logger,
		sendMessage: sendMessage,
	}
}

func (r *CommandRouter) Execute(ctx context.Context, cmdCtx *domain.CommandContext, cmdType domain.CommandType, params map[string]any) error {
	if r.registry == nil {
		return errors.New("command registry is not initialized")
	}

	key, normalizedParams := r.NormalizeCommand(cmdType, params)
	ctx = sharedlog.WithRuntime(ctx, "bot")
	ctx = sharedlog.WithComponent(ctx, "command")

	started := time.Now()
	attrs := commandExecutionAttrs(cmdCtx, key, cmdType)
	sharedlog.Info(ctx, r.logger, EventBotCommandExecuteStarted, "command execution started", attrs...)

	if err := r.registry.Execute(ctx, cmdCtx, key, normalizedParams); err != nil {
		if errors.Is(err, command.ErrUnknownCommand) {
			warnAttrs := append([]slog.Attr{}, attrs...)
			warnAttrs = append(warnAttrs, sharedlog.SinceMS(started))
			sharedlog.Warn(ctx, r.logger, EventBotCommandUnknown, "unknown command", warnAttrs...)

			if sendErr := r.sendMessage(ctx, cmdCtx.Room, adapter.ErrUnknownCommand); sendErr != nil {
				return fmt.Errorf("failed to send unknown command message: %w", sendErr)
			}

			return nil
		}

		failedAttrs := append([]slog.Attr{}, attrs...)
		failedAttrs = append(failedAttrs, sharedlog.SinceMS(started))
		failedAttrs = append(failedAttrs, sharedlog.ErrorAttrs(err)...)
		sharedlog.Error(ctx, r.logger, EventBotCommandExecuteFailed, "command execution failed", failedAttrs...)

		return fmt.Errorf("execute command: %w", err)
	}

	successAttrs := append([]slog.Attr{}, attrs...)
	successAttrs = append(successAttrs, sharedlog.SinceMS(started))
	sharedlog.Info(ctx, r.logger, EventBotCommandExecuteSucceeded, "command execution succeeded", successAttrs...)

	return nil
}

// NormalizeCommand 명령어 타입과 파라미터를 정규화합니다.
func (r *CommandRouter) NormalizeCommand(cmdType domain.CommandType, params map[string]any) (string, map[string]any) {
	return NormalizeCommandKey(cmdType, params)
}
