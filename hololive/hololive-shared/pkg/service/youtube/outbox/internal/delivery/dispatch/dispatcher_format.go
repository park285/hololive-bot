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

package dispatch

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/format"
)

type MessageFormatter struct {
	f *format.MessageFormatter
}

func newMessageFormatter(renderer *template.Renderer, cacheClient cache.Client, logger *slog.Logger, messageStrings *messagestrings.Store) *MessageFormatter {
	return &MessageFormatter{f: format.NewMessageFormatter(renderer, cacheClient, logger, messageStrings)}
}

func (mf *MessageFormatter) inner() *format.MessageFormatter {
	if mf.f != nil {
		return mf.f
	}
	return &format.MessageFormatter{}
}

func (mf *MessageFormatter) vtuberFallback(ctx context.Context) string {
	return mf.inner().MessageStrings.VTuberFallbackContext(ctx)
}

func (mf *MessageFormatter) formatMessage(ctx context.Context, item *domain.YouTubeNotificationOutbox) (string, error) {
	msg, err := mf.inner().FormatMessage(ctx, item)
	if err != nil {
		return "", fmt.Errorf("format message: %w", err)
	}
	return msg, nil
}

func (mf *MessageFormatter) buildTemplateData(memberName string, item *domain.YouTubeNotificationOutbox) (format.TemplateData, error) {
	data, err := mf.inner().BuildTemplateData(memberName, item)
	if err != nil {
		return format.TemplateData{}, fmt.Errorf("build template data: %w", err)
	}
	return data, nil
}

func (mf *MessageFormatter) getMemberName(ctx context.Context, channelID string) (string, error) {
	memberName, err := mf.inner().GetMemberName(ctx, channelID)
	if err != nil {
		return "", fmt.Errorf("get member name: %w", err)
	}
	return memberName, nil
}

func (mf *MessageFormatter) formatGroupedMessage(ctx context.Context, memberName, channelID string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) (string, error) {
	msg, err := mf.inner().FormatGroupedMessage(ctx, memberName, channelID, kind, items)
	if err != nil {
		return "", fmt.Errorf("format grouped message: %w", err)
	}
	return msg, nil
}

func (mf *MessageFormatter) FormatYouTubeOutboxPayload(ctx context.Context, payload *domain.YouTubeOutboxDispatchPayload) (string, error) {
	msg, err := mf.inner().FormatYouTubeOutboxPayload(ctx, payload)
	if err != nil {
		return "", fmt.Errorf("format youtube outbox payload: %w", err)
	}
	return msg, nil
}

func (mf *MessageFormatter) buildGroupedTemplateData(memberName string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) format.GroupedTemplateData {
	return mf.inner().BuildGroupedTemplateData(memberName, kind, items)
}

var _ format.DispatchPayloadFormatter = (*MessageFormatter)(nil)
