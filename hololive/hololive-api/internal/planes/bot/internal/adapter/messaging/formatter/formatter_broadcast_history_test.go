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

func TestBroadcastHistoryShowsThumbnailShortcut(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", nil)
	got := formatter.BroadcastHistory(t.Context(), BroadcastHistoryFilter{}, []BroadcastHistoryEntry{
		{
			VideoID:      "MKjXgiJSB_o",
			MemberName:   "테스트",
			TypeLabel:    "게임",
			Time:         time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
			HasThumbnail: true,
		},
	})

	if want := "   !썸네일 MKjXgiJSB_o"; !strings.Contains(got, want) {
		t.Fatalf("BroadcastHistory() missing %q in:\n%s", want, got)
	}
	if notWant := "썸네일 다운로드:"; strings.Contains(got, notWant) {
		t.Fatalf("BroadcastHistory() still contains redundant thumbnail label %q in:\n%s", notWant, got)
	}
}

func TestBroadcastHistoryOmitsRedundantMembershipTitleTag(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", nil)
	got := formatter.BroadcastHistory(t.Context(), BroadcastHistoryFilter{}, []BroadcastHistoryEntry{
		{
			VideoID:    "wrzJxt3KyNI",
			MemberName: "미코",
			Type:       "membership",
			TypeLabel:  "멤버십",
			Title:      "【メンバー限定】ちょこっとカラオケするよ~~ん【ホロライブ/さくらみこ】",
			Time:       time.Date(2026, 7, 5, 15, 32, 44, 0, time.UTC),
		},
	})

	if want := "   ちょこっとカラオケするよ~~ん【ホロライブ/さくらみこ】"; !strings.Contains(got, want) {
		t.Fatalf("BroadcastHistory() missing cleaned membership title %q in:\n%s", want, got)
	}
	if notWant := "【メンバー限定】"; strings.Contains(got, notWant) {
		t.Fatalf("BroadcastHistory() still contains redundant membership tag %q in:\n%s", notWant, got)
	}
}

func TestBroadcastHistoryKeepsNonMembershipPartOfCompoundMembershipTitleTag(t *testing.T) {
	t.Parallel()

	got := broadcastHistoryDisplayTitle("membership", "【メン限同時視聴】映画ワイルドスピード")

	if want := "【同時視聴】映画ワイルドスピード"; got != want {
		t.Fatalf("broadcastHistoryDisplayTitle() = %q, want %q", got, want)
	}
}

func TestBroadcastHistoryUsesTypeNotDisplayLabelForMembershipTitleCleanup(t *testing.T) {
	t.Parallel()

	got := broadcastHistoryDisplayTitle("membership", "【Members Only】yuru camp")

	if want := "yuru camp"; got != want {
		t.Fatalf("broadcastHistoryDisplayTitle() = %q, want %q", got, want)
	}
}

func TestBroadcastHistoryKeepsMembershipLikeTitleForOtherTypes(t *testing.T) {
	t.Parallel()

	title := "【メンバー限定】ちょこっとカラオケするよ~~ん"
	if got := broadcastHistoryDisplayTitle("singing", title); got != title {
		t.Fatalf("broadcastHistoryDisplayTitle() = %q, want %q", got, title)
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
