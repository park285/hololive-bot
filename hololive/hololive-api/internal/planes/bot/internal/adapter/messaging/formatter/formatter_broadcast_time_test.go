package formatter

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func broadcastTimeTestSummary() *BroadcastTimeSummary {
	return &BroadcastTimeSummary{
		MemberName: "쿄코",
		Days:       7,
		Now:        time.Date(2026, time.November, 8, 0, 0, 0, 0, time.UTC),
		Entries: []BroadcastHistoryEntry{
			{
				TypeLabel: "게임",
				StartedAt: time.Date(2026, time.November, 7, 6, 39, 0, 0, time.UTC),
				EndedAt:   time.Date(2026, time.November, 7, 10, 38, 0, 0, time.UTC),
			},
			{
				// 미분류 방송도 합계와 목록에 넣는다.
				TypeLabel: "미분류",
				StartedAt: time.Date(2026, time.November, 7, 2, 0, 0, 0, time.UTC),
				EndedAt:   time.Date(2026, time.November, 7, 5, 34, 0, 0, time.UTC),
			},
			{
				// 자정을 넘긴 방송은 종료에 날짜를 붙인다.
				TypeLabel: "멤버십",
				StartedAt: time.Date(2026, time.November, 6, 12, 59, 0, 0, time.UTC),
				EndedAt:   time.Date(2026, time.November, 6, 15, 8, 0, 0, time.UTC),
			},
			{
				TypeLabel: "잡담",
				StartedAt: time.Date(2026, time.November, 5, 13, 13, 0, 0, time.UTC),
			},
		},
	}
}

func TestBroadcastTimeSumsWeekInHeadAndListsPerVariant(t *testing.T) {
	t.Parallel()

	f := NewResponseFormatter("!", nil)

	require.Equal(t, `📅 쿄코 방송시간 · 최근 7일
합계 9시간 42분 · 4회 (종료 미확인 1회 제외)

1 · 11/07 15:39~19:38 · 3시간 59분 · 게임
2 · 11/07 11:00~14:34 · 3시간 34분 · 미분류
3 · 11/06 21:59~11/07 00:08 · 2시간 9분 · 멤버십
4 · 11/05 22:13 시작 · 잡담`, f.BroadcastTime(t.Context(), broadcastTimeTestSummary()))

	require.Equal(t, `📅 **쿄코** 방송시간 · 최근 7일
합계 9시간 42분 · 4회 (종료 미확인 1회 제외)

11/07 15:39~19:38 · 3시간 59분 · 게임
11/07 11:00~14:34 · 3시간 34분 · 미분류
11/06 21:59~11/07 00:08 · 2시간 9분 · 멤버십
11/05 22:13 시작 · 잡담`, f.BroadcastTime(WithReplyFormat(t.Context(), domain.TemplateFormatRichText), broadcastTimeTestSummary()))
}

func TestBroadcastTimeFoldsListBehindSeeMore(t *testing.T) {
	t.Parallel()

	f := NewResponseFormatter("!", nil, WithSeeMoreFold(true))

	for _, count := range []int{1, 4} {
		summary := broadcastTimeTestSummary()

		summary.Entries = summary.Entries[:count]

		head, rest, folded := strings.Cut(f.BroadcastTime(t.Context(), summary), "\u200b")
		require.True(t, folded, count)
		require.True(t, strings.HasPrefix(head, "📅 쿄코 방송시간 · 최근 7일\n합계 "), head)
		require.NotContains(t, head, "1 · ", count)
		require.Contains(t, rest, "1 · 11/07 15:39~19:38", count)
	}
}

func TestBroadcastTimeShowsYearOnlyOutsideCurrentYear(t *testing.T) {
	t.Parallel()

	summary := broadcastTimeTestSummary()

	summary.Days = 365
	summary.Entries = []BroadcastHistoryEntry{{
		StartedAt: time.Date(2025, time.December, 31, 14, 0, 0, 0, time.UTC),
		EndedAt:   time.Date(2025, time.December, 31, 16, 0, 0, 0, time.UTC),
	}}

	got := NewResponseFormatter("!", nil).BroadcastTime(t.Context(), summary)
	require.Contains(t, got, "합계 2시간 · 1회\n")
	require.Contains(t, got, "1 · 2025/12/31 23:00~2026/01/01 01:00 · 2시간")
}

func TestBroadcastTimeTotalDoesNotSplitDays(t *testing.T) {
	t.Parallel()

	require.Equal(t, "41시간 5분", formatBroadcastTimeTotal(41*time.Hour+5*time.Minute))
	require.Equal(t, "45분", formatBroadcastTimeTotal(45*time.Minute))
}

func TestBroadcastTimeEmptyAndPartial(t *testing.T) {
	t.Parallel()

	f := NewResponseFormatter("!", nil)
	summary := broadcastTimeTestSummary()

	summary.Entries = nil

	require.Equal(t, "📅 쿄코: 최근 7일 이내 종료된 방송이 없습니다.", f.BroadcastTime(t.Context(), summary))

	summary.Partial = true
	require.Equal(t, "📅 쿄코: 최근 7일 이내 종료된 방송이 없습니다.\n"+broadcastTimePartialNotice, f.BroadcastTime(t.Context(), summary))

	summary = broadcastTimeTestSummary()
	summary.Partial = true
	require.Contains(t, f.BroadcastTime(t.Context(), summary), "(종료 미확인 1회 제외)\n"+broadcastTimePartialNotice+"\n\n")
}

func TestBroadcastTimeUsageKeepsCommandCopyable(t *testing.T) {
	t.Parallel()

	require.Equal(t, "❌ 멤버 이름을 입력해주세요.\n예) !방송시간 페코라", NewResponseFormatter("!", nil).BroadcastTimeUsage())
}
