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
	"testing"

	msging "github.com/kapu/hololive-kakao-bot-go/internal/adapter/messaging"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
)

func TestFormatMemberNewsSubscriptionMessages(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMemberNewsNoMembers:    "NO_MEMBERS {{.Prefix}}",
		domain.TemplateKeyCmdMemberNewsSubscribed:   "SUBSCRIBED {{.Prefix}}",
		domain.TemplateKeyCmdMemberNewsAlreadySub:   "ALREADY_SUB {{.Prefix}}",
		domain.TemplateKeyCmdMemberNewsUnsubscribed: "UNSUBSCRIBED {{.Prefix}}",
		domain.TemplateKeyCmdMemberNewsNotSub:       "NOT_SUB {{.Prefix}}",
		domain.TemplateKeyCmdMemberNewsStatus:       "STATUS {{if .IsSubscribed}}ON{{else}}OFF{{end}} {{.Prefix}}",
	})
	formatter := NewResponseFormatter("?", renderer)

	ctx := t.Context()
	assert.Equal(t, "NO_MEMBERS ?", formatter.FormatMemberNewsNoMembers(ctx))
	assert.Equal(t, "SUBSCRIBED ?", formatter.FormatMemberNewsSubscribed(ctx))
	assert.Equal(t, "ALREADY_SUB ?", formatter.FormatMemberNewsAlreadySubscribed(ctx))
	assert.Equal(t, "UNSUBSCRIBED ?", formatter.FormatMemberNewsUnsubscribed(ctx))
	assert.Equal(t, "NOT_SUB ?", formatter.FormatMemberNewsNotSubscribed(ctx))
	assert.Equal(t, "STATUS ON ?", formatter.FormatMemberNewsStatus(ctx, true))
	assert.Equal(t, "STATUS OFF ?", formatter.FormatMemberNewsStatus(ctx, false))
}

func TestFormatMemberNewsSubscriptionMessages_Fallback(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	formatter := NewResponseFormatter("!", nil)

	assert.Equal(t, msging.MsgMemberNewsNoMembers, formatter.FormatMemberNewsNoMembers(ctx))
	assert.Equal(t, msging.MsgMemberNewsSubscribed, formatter.FormatMemberNewsSubscribed(ctx))
	assert.Equal(t, msging.MsgMemberNewsAlreadySubscribed, formatter.FormatMemberNewsAlreadySubscribed(ctx))
	assert.Equal(t, msging.MsgMemberNewsUnsubscribed, formatter.FormatMemberNewsUnsubscribed(ctx))
	assert.Equal(t, msging.MsgMemberNewsNotSubscribed, formatter.FormatMemberNewsNotSubscribed(ctx))
	assert.Equal(t, msging.MsgMemberNewsStatusOn, formatter.FormatMemberNewsStatus(ctx, true))
	assert.Equal(t, msging.MsgMemberNewsStatusOff, formatter.FormatMemberNewsStatus(ctx, false))
}

func TestMemberNewsLocalizationHelpers(t *testing.T) {
	t.Parallel()

	items := []membernewscontracts.SummaryItem{
		{Category: "birthday_live"},
		{Category: "solo_live"},
		{Category: "collab"},
		{Category: "event"},
		{Category: "goods"},
		{Category: "other"},
		{Category: "unknown"},
	}

	localized := localizeMemberNewsItems(items)
	assert.Equal(t, "생일 라이브", localized[0].Category)
	assert.Equal(t, "솔로 라이브", localized[1].Category)
	assert.Equal(t, "콜라보", localized[2].Category)
	assert.Equal(t, "이벤트", localized[3].Category)
	assert.Equal(t, "굿즈", localized[4].Category)
	assert.Equal(t, "기타", localized[5].Category)
	assert.Equal(t, "unknown", localized[6].Category)

	assert.Empty(t, memberNewsCategoryLabel(""))
	assert.Equal(t, "굿즈", memberNewsCategoryLabel(" goods "))
}
