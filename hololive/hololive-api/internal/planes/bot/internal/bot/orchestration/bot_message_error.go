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
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/iris-client-go/v2/iris"
	sharedlog "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
	appErrors "github.com/kapu/hololive-shared/pkg/apperrors"
)

func (b *Bot) sendError(ctx context.Context, room, errorMsg string) error {
	if err := b.ensureTransport().SendError(ctx, room, errorMsg); err != nil {
		return fmt.Errorf("send error reply: %w", err)
	}

	return nil
}

func (b *Bot) sendMessage(ctx context.Context, room, message string) error {
	if err := b.ensureTransport().SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (b *Bot) sendImage(ctx context.Context, room string, imageData []byte, opts ...iris.SendOption) error {
	if err := b.ensureTransport().SendImage(ctx, room, imageData, opts...); err != nil {
		return fmt.Errorf("send image: %w", err)
	}

	return nil
}

func (b *Bot) sendImages(ctx context.Context, room string, images [][]byte, opts ...iris.SendOption) error {
	if err := b.ensureTransport().SendImages(ctx, room, images, opts...); err != nil {
		return fmt.Errorf("send images: %w", err)
	}

	return nil
}

// outcome이 unknown이면 reply가 이미 전달됐을 수 있어, 오류 응답을 덧붙이면 중복 발화가 된다.
func (b *Bot) skipErrorResponseOnUnknownOutcome(ctx context.Context, chatID, commandType string, err error) bool {
	if !errors.Is(err, transport.ErrReplyOutcomeUnknown) {
		return false
	}

	errorAttrs := sharedlog.ErrorAttrs(err)
	attrs := make([]slog.Attr, 0, 2+len(errorAttrs))

	attrs = append(attrs,
		privacylog.ChatIDAttr(chatID),
		slog.String("command", commandType),
	)
	attrs = append(attrs, errorAttrs...)
	sharedlog.Warn(ctx, b.logger, EventBotReplyOutcomeUnknown, "iris reply outcome unknown; suppressing error response", attrs...)

	return true
}

func (b *Bot) getErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	if serviceErr, ok := errors.AsType[*appErrors.ServiceError](err); ok && serviceErr.Service == serviceNameIris {
		return messaging.ErrIrisConnectionFailed
	}

	if _, ok := errors.AsType[*appErrors.APIError](err); ok {
		return messaging.ErrExternalAPICallFailed
	}

	if _, ok := errors.AsType[*appErrors.KeyRotationError](err); ok {
		return messaging.ErrExternalAPICallFailed
	}

	if _, ok := errors.AsType[*appErrors.CacheError](err); ok {
		return messaging.ErrCacheConnectionFailed
	}

	if _, ok := errors.AsType[*appErrors.ValidationError](err); ok {
		return messaging.ErrCommandProcessingFailed
	}

	return messaging.ErrCommandProcessingFailed
}
