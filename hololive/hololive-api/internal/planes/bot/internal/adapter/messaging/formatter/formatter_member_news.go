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

package formatter

import (
	"context"
	"strings"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type memberNewsDigestTemplateData struct {
	Headline    string
	TopItems    []membernewscontracts.SummaryItem
	MoreSummary string
	TotalCount  int
}

type memberNewsSubscriptionTemplateData struct {
	Prefix       string
	IsSubscribed bool
}

func (f *ResponseFormatter) memberNewsNotify(ctx context.Context, key string) string {
	if f == nil {
		return messagestrings.FallbackSentinel
	}
	return f.messageStrings.GetContext(ctx, messagestrings.NamespaceNotify, key)
}

func (f *ResponseFormatter) FormatMemberNewsDigest(ctx context.Context, digest *membernewscontracts.Digest) string {
	if digest == nil {
		return messagestrings.FallbackSentinel
	}

	if f == nil || f.renderer == nil {
		return messagestrings.FallbackSentinel
	}

	data := memberNewsDigestTemplateData{
		Headline:    digest.Headline,
		TopItems:    f.localizeMemberNewsItems(ctx, digest.TopItems),
		MoreSummary: digest.MoreSummary,
		TotalCount:  digest.TotalCount,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsDigest, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func (f *ResponseFormatter) FormatMemberNewsNoMembers(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return f.memberNewsNotify(ctx, "member_news_no_members")
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsNoMembers, memberNewsSubscriptionTemplateData{Prefix: f.prefix})
	if err != nil {
		return f.memberNewsNotify(ctx, "member_news_no_members")
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsSubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return f.memberNewsNotify(ctx, "member_news_subscribed")
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsSubscribed, memberNewsSubscriptionTemplateData{Prefix: f.prefix})
	if err != nil {
		return f.memberNewsNotify(ctx, "member_news_subscribed")
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsAlreadySubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return f.memberNewsNotify(ctx, "member_news_already_subscribed")
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsAlreadySub, memberNewsSubscriptionTemplateData{Prefix: f.prefix})
	if err != nil {
		return f.memberNewsNotify(ctx, "member_news_already_subscribed")
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsUnsubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return f.memberNewsNotify(ctx, "member_news_unsubscribed")
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsUnsubscribed, memberNewsSubscriptionTemplateData{Prefix: f.prefix})
	if err != nil {
		return f.memberNewsNotify(ctx, "member_news_unsubscribed")
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsNotSubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return f.memberNewsNotify(ctx, "member_news_not_subscribed")
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsNotSub, memberNewsSubscriptionTemplateData{Prefix: f.prefix})
	if err != nil {
		return f.memberNewsNotify(ctx, "member_news_not_subscribed")
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsStatus(ctx context.Context, isSubscribed bool) string {
	if f == nil || f.renderer == nil {
		return f.memberNewsStatusFallback(ctx, isSubscribed)
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsStatus, memberNewsSubscriptionTemplateData{
		Prefix:       f.prefix,
		IsSubscribed: isSubscribed,
	})
	if err != nil {
		return f.memberNewsStatusFallback(ctx, isSubscribed)
	}

	return message
}

func (f *ResponseFormatter) memberNewsStatusFallback(ctx context.Context, isSubscribed bool) string {
	if isSubscribed {
		return f.memberNewsNotify(ctx, "member_news_status_on")
	}

	return f.memberNewsNotify(ctx, "member_news_status_off")
}

func (f *ResponseFormatter) localizeMemberNewsItems(ctx context.Context, items []membernewscontracts.SummaryItem) []membernewscontracts.SummaryItem {
	if len(items) == 0 {
		return items
	}

	localized := make([]membernewscontracts.SummaryItem, len(items))
	copy(localized, items)

	for i := range localized {
		localized[i].Category = f.memberNewsCategoryLabel(ctx, localized[i].Category)
	}

	return localized
}

func (f *ResponseFormatter) memberNewsCategoryLabel(ctx context.Context, raw string) string {
	if label := f.messageStrings.GetContext(ctx, messagestrings.NamespaceNewsCat, strings.ToLower(strings.TrimSpace(raw))); label != "" {
		return label
	}
	return raw
}
