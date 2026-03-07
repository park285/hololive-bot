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

package adapter

import (
	"context"
	"strings"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type memberNewsDigestTemplateData struct {
	Emoji       UIEmoji
	Headline    string
	TopItems    []membernewscontracts.SummaryItem
	MoreSummary string
	TotalCount  int
}

type memberNewsSubscriptionTemplateData struct {
	Emoji        UIEmoji
	Prefix       string
	IsSubscribed bool
}

// FormatMemberNewsDigest: 멤버 뉴스 다이제스트를 렌더링합니다.
func (f *ResponseFormatter) FormatMemberNewsDigest(ctx context.Context, digest *membernewscontracts.Digest) string {
	if digest == nil {
		return ErrorMessage(ErrDisplayMemberNewsFailed)
	}

	if f == nil || f.renderer == nil {
		return ErrorMessage(ErrDisplayMemberNewsFailed)
	}

	data := memberNewsDigestTemplateData{
		Emoji:       DefaultEmoji,
		Headline:    digest.Headline,
		TopItems:    localizeMemberNewsItems(digest.TopItems),
		MoreSummary: digest.MoreSummary,
		TotalCount:  digest.TotalCount,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsDigest, data)
	if err != nil {
		return ErrorMessage(ErrDisplayMemberNewsFailed)
	}

	return rendered
}

// FormatMemberNewsNoMembers: room 알람 멤버가 없는 경우 안내 메시지.
func (f *ResponseFormatter) FormatMemberNewsNoMembers(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return MsgMemberNewsNoMembers
	}
	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsNoMembers, memberNewsSubscriptionTemplateData{Emoji: DefaultEmoji, Prefix: f.prefix})
	if err != nil {
		return MsgMemberNewsNoMembers
	}
	return message
}

// FormatMemberNewsSubscribed: 구독 켜기 성공 메시지.
func (f *ResponseFormatter) FormatMemberNewsSubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return MsgMemberNewsSubscribed
	}
	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsSubscribed, memberNewsSubscriptionTemplateData{Emoji: DefaultEmoji, Prefix: f.prefix})
	if err != nil {
		return MsgMemberNewsSubscribed
	}
	return message
}

// FormatMemberNewsAlreadySubscribed: 이미 구독 중 안내 메시지.
func (f *ResponseFormatter) FormatMemberNewsAlreadySubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return MsgMemberNewsAlreadySubscribed
	}
	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsAlreadySub, memberNewsSubscriptionTemplateData{Emoji: DefaultEmoji, Prefix: f.prefix})
	if err != nil {
		return MsgMemberNewsAlreadySubscribed
	}
	return message
}

// FormatMemberNewsUnsubscribed: 구독 해제 성공 메시지.
func (f *ResponseFormatter) FormatMemberNewsUnsubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return MsgMemberNewsUnsubscribed
	}
	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsUnsubscribed, memberNewsSubscriptionTemplateData{Emoji: DefaultEmoji, Prefix: f.prefix})
	if err != nil {
		return MsgMemberNewsUnsubscribed
	}
	return message
}

// FormatMemberNewsNotSubscribed: 미구독 상태 안내 메시지.
func (f *ResponseFormatter) FormatMemberNewsNotSubscribed(ctx context.Context) string {
	if f == nil || f.renderer == nil {
		return MsgMemberNewsNotSubscribed
	}
	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsNotSub, memberNewsSubscriptionTemplateData{Emoji: DefaultEmoji, Prefix: f.prefix})
	if err != nil {
		return MsgMemberNewsNotSubscribed
	}
	return message
}

// FormatMemberNewsStatus: 구독 상태 메시지.
func (f *ResponseFormatter) FormatMemberNewsStatus(ctx context.Context, isSubscribed bool) string {
	if f == nil || f.renderer == nil {
		if isSubscribed {
			return MsgMemberNewsStatusOn
		}
		return MsgMemberNewsStatusOff
	}

	message, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsStatus, memberNewsSubscriptionTemplateData{
		Emoji:        DefaultEmoji,
		Prefix:       f.prefix,
		IsSubscribed: isSubscribed,
	})
	if err != nil {
		if isSubscribed {
			return MsgMemberNewsStatusOn
		}
		return MsgMemberNewsStatusOff
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
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "birthday_live":
		return "생일 라이브"
	case "solo_live":
		return "솔로 라이브"
	case "collab":
		return "콜라보"
	case "event":
		return "이벤트"
	case "goods":
		return "굿즈"
	case "other":
		return "기타"
	default:
		return raw
	}
}
