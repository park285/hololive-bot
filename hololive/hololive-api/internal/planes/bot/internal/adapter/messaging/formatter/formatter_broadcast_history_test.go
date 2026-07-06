package formatter

import (
	"strings"
	"testing"
	"time"
)

func TestBroadcastHistoryShowsLimitFilter(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", nil)
	got := formatter.BroadcastHistory(t.Context(), BroadcastHistoryFilter{
		TypeLabel:  "게임",
		Days:       14,
		Limit:      10,
		IncludeAll: false,
	}, []BroadcastHistoryEntry{
		{
			VideoID:    "AqxEw3kXcgU",
			MemberName: "테스트",
			TypeLabel:  "게임",
			Title:      "test",
			Time:       time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
			URL:        "https://www.youtube.com/watch?v=AqxEw3kXcgU",
		},
	})

	for _, want := range []string{"타입: 게임", "기간: 최근 14일", "개수: 최대 10건"} {
		if !strings.Contains(got, want) {
			t.Fatalf("BroadcastHistory() missing %q in:\n%s", want, got)
		}
	}
}

func TestBroadcastHistoryEmptyShowsLimitFilter(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", nil)
	got := formatter.BroadcastHistoryEmpty(t.Context(), BroadcastHistoryFilter{
		IncludeAll: true,
		Limit:      20,
	})

	for _, want := range []string{"기간: 전체", "개수: 최대 20건"} {
		if !strings.Contains(got, want) {
			t.Fatalf("BroadcastHistoryEmpty() missing %q in:\n%s", want, got)
		}
	}
}
