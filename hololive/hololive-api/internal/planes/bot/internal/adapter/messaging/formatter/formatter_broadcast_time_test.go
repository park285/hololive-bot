package formatter

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func broadcastTimeTestEntries() []BroadcastHistoryEntry {
	return []BroadcastHistoryEntry{
		{
			TypeLabel: "게임",
			StartedAt: time.Date(2026, time.November, 6, 11, 59, 0, 0, time.UTC),
			EndedAt:   time.Date(2026, time.November, 6, 13, 29, 0, 0, time.UTC),
		},
		{
			TypeLabel: "잡담",
			StartedAt: time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC),
		},
		{
			Time:      time.Date(2026, time.November, 3, 13, 0, 0, 0, time.UTC),
			TimeLabel: "종료",
		},
	}
}

func TestBroadcastTimeListsRecentBroadcastsPerVariant(t *testing.T) {
	t.Parallel()

	f := NewResponseFormatter("!", nil)
	entries := broadcastTimeTestEntries()

	plain := f.BroadcastTime(t.Context(), "페코라", entries, false)
	require.Equal(t, `📅 페코라 방송시간 · 최근 3개
최근: 2026/11/06 20:59~22:29 · 1시간 30분

1 · 2026/11/06 20:59~22:29 · 1시간 30분 · 게임
2 · 2026/11/05 21:00 시작 · 잡담
3 · 2026/11/03 22:00 종료`, plain)

	rich := f.BroadcastTime(WithReplyFormat(t.Context(), domain.TemplateFormatRichText), "페코라", entries, false)
	require.Equal(t, `📅 **페코라** 방송시간 · 최근 3개
최근: 2026/11/06 20:59~22:29 · 1시간 30분

2026/11/06 20:59~22:29 · 1시간 30분 · 게임
2026/11/05 21:00 시작 · 잡담
2026/11/03 22:00 종료`, rich)
}

// 한 건이면 "최근:" 줄이 항목과 같으므로 두지 않고 접지도 않는다.
func TestBroadcastTimeSingleEntryStaysUnfolded(t *testing.T) {
	t.Parallel()

	f := NewResponseFormatter("!", nil, WithSeeMoreFold(true))
	got := f.BroadcastTime(t.Context(), "페코라", broadcastTimeTestEntries()[:1], false)

	require.Equal(t, "📅 페코라 방송시간 · 최근 1개\n\n1 · 2026/11/06 20:59~22:29 · 1시간 30분 · 게임", got)
}

// 2건 이상은 접히므로 가장 최근 방송 시각이 접힌 머리 문단에 남아야 한다.
func TestBroadcastTimeFoldKeepsLatestInHead(t *testing.T) {
	t.Parallel()

	f := NewResponseFormatter("!", nil, WithSeeMoreFold(true))
	got := f.BroadcastTime(t.Context(), "페코라", broadcastTimeTestEntries(), false)

	head, _, folded := strings.Cut(got, "\u200b")
	require.True(t, folded, got)
	require.Equal(t, "📅 페코라 방송시간 · 최근 3개\n최근: 2026/11/06 20:59~22:29 · 1시간 30분", strings.TrimRight(head, "\n"))
}

func TestBroadcastTimeEmptyAndTruncated(t *testing.T) {
	t.Parallel()

	f := NewResponseFormatter("!", nil)

	require.Equal(t, "📅 페코라: 최근 1년 이내 종료된 방송이 없습니다.", f.BroadcastTime(t.Context(), "페코라", nil, false))
	require.Equal(t, "📅 페코라: 최근 1년 이내 종료된 방송이 없습니다.\n"+broadcastHistoryTruncatedNotice,
		f.BroadcastTime(t.Context(), "페코라", nil, true))
	require.Contains(t, f.BroadcastTime(t.Context(), "페코라", broadcastTimeTestEntries(), true),
		"최근: 2026/11/06 20:59~22:29 · 1시간 30분\n"+broadcastHistoryTruncatedNotice+"\n\n")
}

func TestBroadcastTimeUsageKeepsCommandCopyable(t *testing.T) {
	t.Parallel()

	require.Equal(t, "❌ 멤버 이름을 입력해주세요.\n예) !방송시간 페코라", NewResponseFormatter("!", nil).BroadcastTimeUsage())
}
