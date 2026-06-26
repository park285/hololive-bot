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

	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	appErrors "github.com/kapu/hololive-shared/pkg/apperrors"
)

func (b *Bot) sendMessage(ctx context.Context, room, message string) error {
	return b.ensureTransport().SendMessage(ctx, room, message)
}

func (b *Bot) sendImage(ctx context.Context, room string, imageData []byte, opts ...iris.SendOption) error {
	return b.ensureTransport().SendImage(ctx, room, imageData, opts...)
}

func (b *Bot) sendError(ctx context.Context, room, errorMsg string) error {
	return b.ensureTransport().SendError(ctx, room, errorMsg)
}

func (b *Bot) getErrorMessage(err error, commandType string) string {
	if err == nil {
		return ""
	}

	var serviceErr *appErrors.ServiceError
	if errors.As(err, &serviceErr) && serviceErr.Service == serviceNameIris {
		return adapter.ErrIrisConnectionFailed
	}

	var apiErr *appErrors.APIError
	if errors.As(err, &apiErr) {
		return adapter.ErrExternalAPICallFailed
	}

	var keyRotationErr *appErrors.KeyRotationError
	if errors.As(err, &keyRotationErr) {
		return adapter.ErrExternalAPICallFailed
	}

	var cacheErr *appErrors.CacheError
	if errors.As(err, &cacheErr) {
		return adapter.ErrCacheConnectionFailed
	}

	var validationErr *appErrors.ValidationError
	if errors.As(err, &validationErr) {
		return err.Error()
	}

	return fmt.Sprintf(adapter.ErrCommandProcessingFailed, commandType)
}
