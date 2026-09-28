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

func (f *ResponseFormatter) memberNewsNotify(key messagestrings.Key) string {
	if f == nil {
		return f.renderFailureText()
	}

	return f.messageStrings.Text(key)
}

func (f *ResponseFormatter) FormatMemberNewsDigest(ctx context.Context, digest *membernewscontracts.Digest) string {
	if digest == nil {
		return f.renderFailureText()
	}

	if f == nil || f.renderer == nil {
		return f.renderFailureText()
	}

	data := memberNewsDigestTemplateData{
		Headline:    digest.Headline,
		TopItems:    f.localizeMemberNewsItems(ctx, digest.TopItems),
		MoreSummary: digest.MoreSummary,
		TotalCount:  digest.TotalCount,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsDigest, data)
	if err != nil {
		return f.renderFailureText()
	}

	return f.foldSeeMore(rendered)
}

func (f *ResponseFormatter) FormatMemberNewsNoMembers(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return f.memberNewsNotify(messagestrings.NotifyMemberNewsNoMembers)
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsNoMembers, memberNewsSubscriptionTemplateData{Prefix: f.prefix})
	if err != nil {
		return f.memberNewsNotify(messagestrings.NotifyMemberNewsNoMembers)
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsSubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return f.memberNewsNotify(messagestrings.NotifyMemberNewsSubscribed)
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsSubscribed, memberNewsSubscriptionTemplateData{Prefix: f.prefix})
	if err != nil {
		return f.memberNewsNotify(messagestrings.NotifyMemberNewsSubscribed)
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsAlreadySubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return f.memberNewsNotify(messagestrings.NotifyMemberNewsAlreadySubscribed)
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsAlreadySub, memberNewsSubscriptionTemplateData{Prefix: f.prefix})
	if err != nil {
		return f.memberNewsNotify(messagestrings.NotifyMemberNewsAlreadySubscribed)
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsUnsubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return f.memberNewsNotify(messagestrings.NotifyMemberNewsUnsubscribed)
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsUnsubscribed, memberNewsSubscriptionTemplateData{Prefix: f.prefix})
	if err != nil {
		return f.memberNewsNotify(messagestrings.NotifyMemberNewsUnsubscribed)
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsNotSubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return f.memberNewsNotify(messagestrings.NotifyMemberNewsNotSubscribed)
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsNotSub, memberNewsSubscriptionTemplateData{Prefix: f.prefix})
	if err != nil {
		return f.memberNewsNotify(messagestrings.NotifyMemberNewsNotSubscribed)
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

func (f *ResponseFormatter) memberNewsStatusFallback(_ context.Context, isSubscribed bool) string {
	if isSubscribed {
		return f.memberNewsNotify(messagestrings.NotifyMemberNewsStatusOn)
	}

	return f.memberNewsNotify(messagestrings.NotifyMemberNewsStatusOff)
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

func (f *ResponseFormatter) memberNewsCategoryLabel(_ context.Context, raw string) string {
	if label, ok := f.messageStrings.Lookup(messagestrings.NamespaceNewsCat, strings.ToLower(strings.TrimSpace(raw))); ok {
		return label
	}

	return raw
}
