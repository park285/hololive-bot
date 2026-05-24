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

	msging "github.com/kapu/hololive-kakao-bot-go/internal/adapter/messaging"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"
	templateview "github.com/kapu/hololive-shared/pkg/templateview"
)

type memberNewsDigestTemplateData struct {
	Emoji       msging.UIEmoji
	Headline    string
	TopItems    []membernewscontracts.SummaryItem
	MoreSummary string
	TotalCount  int
}

type memberNewsSubscriptionTemplateData struct {
	Emoji        msging.UIEmoji
	Prefix       string
	IsSubscribed bool
}

func (f *ResponseFormatter) FormatMemberNewsDigest(ctx context.Context, digest *membernewscontracts.Digest) string {
	if digest == nil {
		return msging.ErrorMessage(msging.ErrDisplayMemberNewsFailed)
	}

	if f == nil || f.renderer == nil {
		return msging.ErrorMessage(msging.ErrDisplayMemberNewsFailed)
	}

	data := memberNewsDigestTemplateData{
		Emoji:       msging.DefaultEmoji,
		Headline:    digest.Headline,
		TopItems:    localizeMemberNewsItems(digest.TopItems),
		MoreSummary: digest.MoreSummary,
		TotalCount:  digest.TotalCount,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsDigest, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayMemberNewsFailed)
	}

	return rendered
}

func (f *ResponseFormatter) FormatMemberNewsNoMembers(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return msging.MsgMemberNewsNoMembers
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsNoMembers, memberNewsSubscriptionTemplateData{Emoji: msging.DefaultEmoji, Prefix: f.prefix})
	if err != nil {
		return msging.MsgMemberNewsNoMembers
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsSubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return msging.MsgMemberNewsSubscribed
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsSubscribed, memberNewsSubscriptionTemplateData{Emoji: msging.DefaultEmoji, Prefix: f.prefix})
	if err != nil {
		return msging.MsgMemberNewsSubscribed
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsAlreadySubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return msging.MsgMemberNewsAlreadySubscribed
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsAlreadySub, memberNewsSubscriptionTemplateData{Emoji: msging.DefaultEmoji, Prefix: f.prefix})
	if err != nil {
		return msging.MsgMemberNewsAlreadySubscribed
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsUnsubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return msging.MsgMemberNewsUnsubscribed
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsUnsubscribed, memberNewsSubscriptionTemplateData{Emoji: msging.DefaultEmoji, Prefix: f.prefix})
	if err != nil {
		return msging.MsgMemberNewsUnsubscribed
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsNotSubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return msging.MsgMemberNewsNotSubscribed
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsNotSub, memberNewsSubscriptionTemplateData{Emoji: msging.DefaultEmoji, Prefix: f.prefix})
	if err != nil {
		return msging.MsgMemberNewsNotSubscribed
	}

	return message
}

func (f *ResponseFormatter) FormatMemberNewsStatus(ctx context.Context, isSubscribed bool) string {
	if f == nil || f.renderer == nil {
		if isSubscribed {
			return msging.MsgMemberNewsStatusOn
		}

		return msging.MsgMemberNewsStatusOff
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsStatus, memberNewsSubscriptionTemplateData{
		Emoji:        msging.DefaultEmoji,
		Prefix:       f.prefix,
		IsSubscribed: isSubscribed,
	})
	if err != nil {
		if isSubscribed {
			return msging.MsgMemberNewsStatusOn
		}

		return msging.MsgMemberNewsStatusOff
	}

	return message
}

func localizeMemberNewsItems(items []membernewscontracts.SummaryItem) []membernewscontracts.SummaryItem {
	if len(items) == 0 {
		return items
	}

	localized := make([]membernewscontracts.SummaryItem, len(items))
	copy(localized, items)

	for i := range localized {
		localized[i].Category = memberNewsCategoryLabel(localized[i].Category)
	}

	return localized
}

func memberNewsCategoryLabel(raw string) string {
	return templateview.MemberNewsCategoryLabel(raw)
}
