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
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/format"
)

type MessageFormatter struct {
	f *format.MessageFormatter
}

func newMessageFormatter(renderer *template.Renderer, cacheClient cache.Client, logger *slog.Logger) *MessageFormatter {
	return &MessageFormatter{f: format.NewMessageFormatter(renderer, cacheClient, logger)}
}

func (mf *MessageFormatter) inner() *format.MessageFormatter {
	if mf.f != nil {
		return mf.f
	}
	return &format.MessageFormatter{}
}

func (mf *MessageFormatter) formatMessage(ctx context.Context, item *domain.YouTubeNotificationOutbox) (string, error) {
	return mf.inner().FormatMessage(ctx, item)
}

func (mf *MessageFormatter) buildTemplateData(memberName string, item *domain.YouTubeNotificationOutbox) (format.TemplateData, error) {
	return mf.inner().BuildTemplateData(memberName, item)
}

func (mf *MessageFormatter) formatMessageFallback(memberName string, item *domain.YouTubeNotificationOutbox) (string, error) {
	return mf.inner().FormatMessageFallback(memberName, item)
}

func (mf *MessageFormatter) formatVideoMessage(memberName, payload string, kind domain.OutboxKind) (string, error) {
	return mf.inner().FormatVideoMessage(memberName, payload, kind)
}

func (mf *MessageFormatter) getMemberName(ctx context.Context, channelID string) (string, error) {
	return mf.inner().GetMemberName(ctx, channelID)
}

func (mf *MessageFormatter) formatGroupedMessage(ctx context.Context, memberName, channelID string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) (string, error) {
	return mf.inner().FormatGroupedMessage(ctx, memberName, channelID, kind, items)
}

func (mf *MessageFormatter) getGroupedTemplateKeyAndHeader(memberName string, kind domain.OutboxKind, count int) (templateKey domain.TemplateKey, header string) {
	return mf.inner().GetGroupedTemplateKeyAndHeader(memberName, kind, count)
}

func (mf *MessageFormatter) FormatYouTubeOutboxPayload(ctx context.Context, payload *domain.YouTubeOutboxDispatchPayload) (string, error) {
	return mf.inner().FormatYouTubeOutboxPayload(ctx, payload)
}

func (mf *MessageFormatter) buildGroupedTemplateData(memberName string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) format.GroupedTemplateData {
	return mf.inner().BuildGroupedTemplateData(memberName, kind, items)
}

var _ format.DispatchPayloadFormatter = (*MessageFormatter)(nil)
