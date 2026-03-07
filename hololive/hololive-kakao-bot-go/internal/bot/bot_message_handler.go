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
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	appErrors "github.com/kapu/hololive-kakao-bot-go/internal/errors"
)

// HandleMessage: Iris webhook으로부터 수신한 메시지를 처리합니다.
// HTTP webhook 핸들러에서 호출하기 위해 public으로 노출됩니다.
func (b *Bot) HandleMessage(ctx context.Context, message *iris.Message) {
	commandType := "unknown"

	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("Panic in handleMessage",
				slog.Any("panic", r),
				slog.String("command", commandType),
			)
		}
	}()

	envelope, ok := b.ensureIngress().Prepare(message)
	if !ok {
		return
	}

	commandType = envelope.CommandType
	cmdCtx := domain.NewCommandContext(
		envelope.ChatID,
		envelope.RoomName,
		envelope.UserID,
		envelope.UserName,
		message.Msg,
		false,
	)

	if err := b.executeCommand(ctx, cmdCtx, envelope.Parsed.Type, envelope.Parsed.Params); err != nil {
		b.logger.Error("Failed to execute command", slog.Any("error", err))
		errorMsg := b.getErrorMessage(err, commandType)
		if envelope.ChatID != "" {
			if sendErr := b.sendError(ctx, envelope.ChatID, errorMsg); sendErr != nil {
				b.logger.Error("Failed to send command error message", slog.Any("error", sendErr), slog.String("chat_id", envelope.ChatID))
			}
		}
	}
}

func (b *Bot) executeCommand(ctx context.Context, cmdCtx *domain.CommandContext, cmdType domain.CommandType, params map[string]any) error {
	return b.ensureCommandExecutor().Execute(ctx, cmdCtx, cmdType, params)
}

func (b *Bot) sendMessage(ctx context.Context, room, message string) error {
	return b.ensureTransport().SendMessage(ctx, room, message)
}

func (b *Bot) sendImage(ctx context.Context, room, imageBase64 string) error {
	return b.ensureTransport().SendImage(ctx, room, imageBase64)
}

func (b *Bot) sendError(ctx context.Context, room, errorMsg string) error {
	return b.ensureTransport().SendError(ctx, room, errorMsg)
}

func (b *Bot) getErrorMessage(err error, commandType string) string {
	if err == nil {
		return ""
	}

	msg := err.Error()

	if strings.Contains(msg, "외부 AI 서비스 장애") {
		return msg
	}

	// 서비스 에러 체크 (Iris 연결 실패)
	var serviceErr *appErrors.ServiceError
	if errors.As(err, &serviceErr) && strings.EqualFold(serviceErr.Service, "iris") {
		return adapter.ErrIrisConnectionFailed
	}

	// API 에러 체크 (외부 API 호출 실패)
	var apiErr *appErrors.APIError
	if errors.As(err, &apiErr) {
		return adapter.ErrExternalAPICallFailed
	}

	// 키 로테이션 에러 체크
	var keyRotationErr *appErrors.KeyRotationError
	if errors.As(err, &keyRotationErr) {
		return adapter.ErrExternalAPICallFailed
	}

	// 캐시 에러 체크
	var cacheErr *appErrors.CacheError
	if errors.As(err, &cacheErr) {
		return adapter.ErrCacheConnectionFailed
	}

	// 유효성 검사 에러 체크
	var validationErr *appErrors.ValidationError
	if errors.As(err, &validationErr) {
		return msg
	}

	if strings.Contains(msg, "Valkey") || strings.Contains(msg, "cache") {
		return adapter.ErrCacheConnectionFailed
	}

	return fmt.Sprintf(adapter.ErrCommandProcessingFailed, commandType)
}
