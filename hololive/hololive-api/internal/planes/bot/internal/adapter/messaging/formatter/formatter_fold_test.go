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
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	dbtest "github.com/kapu/hololive-dbtest"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func TestFormatHelp_SeeMoreFoldToggle(t *testing.T) {
	longBody := "도움말 헤더\n" + strings.Repeat("명령 설명 행입니다\n", 40)
	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdHelp: longBody,
	})

	folded := NewResponseFormatter("!", renderer, WithSeeMoreFold(true)).FormatHelp(t.Context())
	assert.True(t, strings.HasPrefix(folded, "도움말 헤더\u200b"), "padding must follow the head line")
	assert.Contains(t, folded, "\u200b")

	plain := NewResponseFormatter("!", renderer).FormatHelp(t.Context())
	assert.NotContains(t, plain, "\u200b")
}

func TestFormatMemberInfo_SeeMoreFoldKeepsProfileHead(t *testing.T) {
	longBody := "👤 {{index .Names 0}}\n별칭: 미코치\n\n" + strings.Repeat("프로필 상세 행입니다\n", 40)
	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdProfile: longBody,
	})
	member := &domain.Member{NameKo: "사쿠라 미코"}

	folded := NewResponseFormatter("!", renderer, WithSeeMoreFold(true)).FormatMemberInfo(t.Context(), member)
	assert.True(t, strings.HasPrefix(folded, "👤 사쿠라 미코\n별칭: 미코치\u200b"), "profile head paragraph must stay above the fold")

	plain := NewResponseFormatter("!", renderer).FormatMemberInfo(t.Context(), member)
	assert.NotContains(t, plain, "\u200b")
}

func TestListFoldingUsesDisplayedCountRegardlessOfLength(t *testing.T) {
	for _, body := range []string{"제목\n\n항목", "제목\n\n" + strings.Repeat("긴 본문\n", 100)} {
		renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
			domain.TemplateKeyCmdLiveStreams: body, domain.TemplateKeyCmdUpcomingStreams: body,
			domain.TemplateKeyCmdChannelSchedule: body, domain.TemplateKeyCmdAlarmList: body,
			domain.TemplateKeyCmdMemberDirectory: body, domain.TemplateKeyCmdCalendar: body,
			domain.TemplateKeyCmdMemberNewsDigest: body,
		})

		for _, tc := range []struct {
			count int
			fold  bool
		}{{0, false}, {1, false}, {2, true}} {
			streams := make([]*domain.Stream, tc.count)
			items := make([]livequery.Item, tc.count)
			alarms := make([]AlarmListEntry, tc.count)
			members := make([]MemberDirectoryEntry, tc.count)
			calendar := make([]domain.CalendarEntry, tc.count)
			history := make([]BroadcastHistoryEntry, tc.count)

			for i := range tc.count {
				streams[i] = &domain.Stream{ID: fmt.Sprintf("video%d", i), ChannelName: "채널", Title: body}
				items[i] = livequery.Item{VideoID: streams[i].ID, ChannelName: "채널", Title: body}
				alarms[i] = AlarmListEntry{MemberName: "멤버"}
				members[i] = MemberDirectoryEntry{PrimaryName: "멤버"}
				calendar[i] = domain.CalendarEntry{Day: 1, Member: &domain.Member{Name: "멤버"}}
				history[i] = BroadcastHistoryEntry{Title: body, MemberName: "멤버", Time: time.Unix(1, 0)}
			}

			calls := map[string]func(*ResponseFormatter) string{
				"live": func(f *ResponseFormatter) string {
					return f.LiveQuery(t.Context(), livequery.Result{Items: items, Status: livequery.Complete}, "")
				},
				"member live": func(f *ResponseFormatter) string {
					return f.LiveQuery(t.Context(), livequery.Result{Items: items, Status: livequery.Complete}, "멤버")
				},
				"upcoming": func(f *ResponseFormatter) string { return f.UpcomingStreams(t.Context(), streams, 24) },
				"schedule": func(f *ResponseFormatter) string {
					return f.ChannelSchedule(t.Context(), &domain.Channel{Name: "채널"}, streams, 7)
				},
				"alarms": func(f *ResponseFormatter) string { return f.FormatAlarmList(t.Context(), alarms) },
				"directory": func(f *ResponseFormatter) string {
					return f.MemberDirectory(t.Context(), []MemberDirectoryGroup{{Members: members}}, 100)
				},
				"calendar": func(f *ResponseFormatter) string { return f.CelebrationCalendar(t.Context(), 10, 2026, calendar) },
				"history": func(f *ResponseFormatter) string {
					return f.BroadcastHistory(t.Context(), BroadcastHistoryFilter{MemberName: strings.Repeat("긴 필터", 100)}, history)
				},
				"news": func(f *ResponseFormatter) string {
					return f.FormatMemberNewsDigest(t.Context(), &membernewscontracts.Digest{TopItems: make([]membernewscontracts.SummaryItem, tc.count), TotalCount: 100})
				},
			}
			for name, call := range calls {
				t.Run(fmt.Sprintf("%s/%d/%d", name, tc.count, len(body)), func(t *testing.T) {
					on := call(NewResponseFormatter("!", renderer, WithSeeMoreFold(true)))
					off := call(NewResponseFormatter("!", renderer, WithSeeMoreFold(false)))
					assertFoldedBody(t, on, off, tc.fold)
				})
			}
		}
	}
}

func assertFoldedBody(t *testing.T, got, plain string, wantFold bool) {
	t.Helper()

	padding := strings.Repeat("\u200b", 500)
	wantCount := 0

	if wantFold {
		wantCount = 1
	}

	assert.Equal(t, wantCount, strings.Count(got, padding))
	assert.Equal(t, plain, strings.Replace(got, padding, "", 1))
	assert.NotContains(t, plain, padding)
}

func TestFilteredEmptyAndUnknownResultsStayUnfolded(t *testing.T) {
	body := "안내\n\n" + strings.Repeat("긴 안내\n", 100)
	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMemberDirectory: body, domain.TemplateKeyCmdCalendar: body, domain.TemplateKeyCmdChannelSchedule: body,
	})
	f := NewResponseFormatter("!", renderer, WithSeeMoreFold(true))
	assert.Equal(t, strings.TrimRight(body, "\n"), f.MemberDirectory(t.Context(), []MemberDirectoryGroup{{Members: []MemberDirectoryEntry{{PrimaryName: " "}, {SecondaryName: " "}}}}, 100))
	assert.Equal(t, strings.TrimRight(body, "\n"), f.CelebrationCalendar(t.Context(), 10, 2026, []domain.CalendarEntry{{}, {}}))
	assert.Equal(t, strings.TrimRight(body, "\n"), f.ChannelSchedule(t.Context(), nil, []*domain.Stream{{Title: "a"}, {Title: "b"}}, 7))
	assert.Equal(t, "현재 방송 상태를 확인할 수 없습니다.", f.LiveQuery(t.Context(), livequery.Result{Status: livequery.Partial}, ""))
}

func TestDetailAndNewsSummaryFolding(t *testing.T) {
	for _, body := range []string{"머리\n\n본문", "머리\n\n" + strings.Repeat("본문", 300)} {
		renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{domain.TemplateKeyCmdHelp: body, domain.TemplateKeyCmdProfile: body, domain.TemplateKeyCmdMemberNewsDigest: body})
		f := NewResponseFormatter("!", renderer, WithSeeMoreFold(true))
		assertFoldedBody(t, f.FormatHelp(t.Context()), body, true)
		assertFoldedBody(t, f.FormatMemberInfo(t.Context(), &domain.Member{Name: "멤버"}), body, true)
		assertFoldedBody(t, f.FormatMemberNewsDigest(t.Context(), &membernewscontracts.Digest{TopItems: []membernewscontracts.SummaryItem{{Title: "항목"}}, MoreSummary: "추가 소식"}), body, true)
	}
}

func TestRenderFailureStaysUnfolded(t *testing.T) {
	f := NewResponseFormatter("!", nil, WithSeeMoreFold(true), WithMessageStrings(setupFormatterTestStore(t)))
	assert.Equal(t, renderFailureMessage, f.FormatAlarmList(t.Context(), []AlarmListEntry{{}, {}}))
}

func TestNormalEmptyNewsUsesUnfoldedTemplateNotice(t *testing.T) {
	f := NewResponseFormatter("!", template.NewRenderer(dbtest.NewPool(t), slog.New(slog.DiscardHandler)), WithSeeMoreFold(true))
	got := f.FormatMemberNewsDigest(t.Context(), &membernewscontracts.Digest{Headline: "뉴스"})
	assert.Contains(t, got, "표시할 뉴스가 없습니다.")
	assert.NotContains(t, got, strings.Repeat("\u200b", 500))
}
