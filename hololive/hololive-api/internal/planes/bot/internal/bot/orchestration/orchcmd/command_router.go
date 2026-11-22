package orchcmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	sharedlog "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
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

func (r *CommandRouter) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, cmdType botcommand.CommandType, params botcommand.Params) error {
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
	cmdCtx *botcommand.CommandContext,
	key string,
	params botcommand.Params,
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

		unknownMessage := r.messageStrings.Text(messagestrings.Key{Namespace: messagestrings.NamespaceError, Name: messaging.ErrUnknownCommand})
		if unknownMessage == "" {
			return registeredCommandResult{handled: true, err: errors.New("unknown command message is not loaded")}
		}

		if sendErr := r.sendMessage(ctx, cmdCtx.Room, unknownMessage); sendErr != nil {
			return registeredCommandResult{handled: true, err: fmt.Errorf("send unknown command message: %w", sendErr)}
		}

		return registeredCommandResult{handled: true}
	}

	failedAttrs := append(append([]slog.Attr{}, attrs...), sharedlog.SinceMS(started))

	failedAttrs = append(failedAttrs, sharedlog.ErrorAttrs(err)...)
	r.logExecutionFailure(ctx, err, failedAttrs)

	return registeredCommandResult{handled: true, err: fmt.Errorf("execute command: %w", err)}
}

func (r *CommandRouter) logExecutionFailure(ctx context.Context, err error, attrs []slog.Attr) {
	if transport.IsReplyOutcomeUnknown(err) {
		sharedlog.Warn(ctx, r.logger, EventBotReplyOutcomeUnknown, "command reply outcome unknown", attrs...)

		return
	}

	sharedlog.Error(ctx, r.logger, EventBotCommandExecuteFailed, "command execution failed", attrs...)
}

func (r *CommandRouter) handleAdmissionError(ctx context.Context, cmdCtx *botcommand.CommandContext, err error) error {
	if !errors.Is(err, errCommandRateLimited) {
		return fmt.Errorf("admit command: %w", err)
	}

	if r.sendMessage == nil || cmdCtx == nil || cmdCtx.Room == "" {
		return fmt.Errorf("report command rate limit: %w", err)
	}

	rateLimitMessage := r.messageStrings.Text(messagestrings.Key{Namespace: messagestrings.NamespaceError, Name: messaging.ErrCommandRateLimited})
	if rateLimitMessage == "" {
		return errors.New("command rate limit message is not loaded")
	}

	if sendErr := r.sendMessage(ctx, cmdCtx.Room, rateLimitMessage); sendErr != nil {
		return fmt.Errorf("send command rate limit message: %w", sendErr)
	}

	return nil
}

func (r *CommandRouter) NormalizeCommand(cmdType botcommand.CommandType, params botcommand.Params) (commandKey string, normalizedParams botcommand.Params) {
	return NormalizeCommandKey(cmdType, params)
}
