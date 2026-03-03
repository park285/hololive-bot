package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
)

// CommandRouter: 봇 명령어의 라우팅 및 실행을 담당하는 컴포넌트
type CommandRouter struct {
	registry    *command.Registry
	logger      *slog.Logger
	sendMessage func(ctx context.Context, room, message string) error
}

// NewCommandRouter: 새로운 CommandRouter 인스턴스를 생성합니다.
func NewCommandRouter(registry *command.Registry, logger *slog.Logger, sendMessage func(ctx context.Context, room, message string) error) *CommandRouter {
	return &CommandRouter{
		registry:    registry,
		logger:      logger,
		sendMessage: sendMessage,
	}
}

// Execute: 명령어 타입과 파라미터를 기반으로 명령어를 실행합니다.
func (r *CommandRouter) Execute(ctx context.Context, cmdCtx *domain.CommandContext, cmdType domain.CommandType, params map[string]any) error {
	if r.registry == nil {
		return fmt.Errorf("command registry is not initialized")
	}

	key, normalizedParams := r.normalizeCommand(cmdType, params)

	if err := r.registry.Execute(ctx, cmdCtx, key, normalizedParams); err != nil {
		if errors.Is(err, command.ErrUnknownCommand) {
			r.logger.Warn("Unknown command", slog.String("type", cmdType.String()))
			if sendErr := r.sendMessage(ctx, cmdCtx.Room, adapter.ErrUnknownCommand); sendErr != nil {
				return fmt.Errorf("failed to send unknown command message: %w", sendErr)
			}
			return nil
		}
		return fmt.Errorf("execute command: %w", err)
	}

	return nil
}

// normalizeCommand: 명령어 타입과 파라미터를 정규화합니다.
func (r *CommandRouter) normalizeCommand(cmdType domain.CommandType, params map[string]any) (string, map[string]any) {
	return normalizeCommandKey(cmdType, params)
}
