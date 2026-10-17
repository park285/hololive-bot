package formatter

import (
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-shared/pkg/kakaotext"
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
			MemberName: testDisplayName,
			TypeLabel:  "게임",
			Title:      "test",
			Time:       time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC),
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
			MemberName:   testDisplayName,
			TypeLabel:    "게임",
			Time:         time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC),
			HasThumbnail: true,
		},
	})

	if want := "썸네일: !썸네일 MKjXgiJSB_o"; !strings.Contains(kakaoformat.Render(got), want) {
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
			Time:       time.Date(2026, time.July, 5, 15, 32, 44, 0, time.UTC),
		},
	})

	if want := kakaotext.ZeroWidthSpace + kakaotext.MarkdownNeutralize("ちょこっとカラオケするよ~~ん【ホロライブ/さくらみこ】"); !strings.Contains(got, want) {
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

func TestBroadcastHistoryReportsTruncatedQueryBudget(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", nil)
	got := formatter.BroadcastHistory(t.Context(), BroadcastHistoryFilter{
		Days:      365,
		Limit:     20,
		Truncated: true,
	}, []BroadcastHistoryEntry{{
		VideoID:    "AqxEw3kXcgU",
		MemberName: testDisplayName,
		TypeLabel:  "게임",
		Time:       time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC),
	}})

	if !strings.Contains(got, "조회 예산") {
		t.Fatalf("BroadcastHistory() missing truncation notice in:\n%s", got)
	}
}

func TestBroadcastHistoryEmptyReportsTruncatedQueryBudget(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", nil)
	got := formatter.BroadcastHistoryEmpty(t.Context(), BroadcastHistoryFilter{Truncated: true})

	if !strings.Contains(got, "조회 예산") {
		t.Fatalf("BroadcastHistoryEmpty() missing truncation notice in:\n%s", got)
	}
}

func TestBroadcastHistoryTimeSpan(t *testing.T) {
	t.Parallel()

	kst := time.FixedZone("KST", 9*60*60)
	startedAt := time.Date(2026, time.October, 17, 20, 59, 30, 0, kst)

	for name, tc := range map[string]struct {
		entry BroadcastHistoryEntry
		want  string
	}{
		"same day": {
			entry: BroadcastHistoryEntry{StartedAt: startedAt, EndedAt: time.Date(2026, time.October, 17, 22, 29, 50, 0, kst)},
			want:  "⏰ 2026/10/17 20:59~22:29 · 1시간 30분",
		},
		"crosses midnight": {
			entry: BroadcastHistoryEntry{StartedAt: startedAt, EndedAt: time.Date(2026, time.October, 18, 0, 3, 0, 0, kst)},
			want:  "⏰ 2026/10/17 20:59~10/18 00:03 · 3시간 3분",
		},
		"crosses year": {
			entry: BroadcastHistoryEntry{
				StartedAt: time.Date(2026, time.December, 31, 23, 0, 0, 0, kst),
				EndedAt:   time.Date(2027, time.January, 1, 0, 30, 0, 0, kst),
			},
			want: "⏰ 2026/12/31 23:00~2027/01/01 00:30 · 1시간 30분",
		},
		// 종료 오차를 확인하지 못한 세션은 종료 시각과 길이를 쓰지 않는다.
		"unverified end": {
			entry: BroadcastHistoryEntry{StartedAt: startedAt, Time: time.Date(2026, time.October, 20, 12, 0, 0, 0, kst), TimeLabel: "종료"},
			want:  "⏰ 2026/10/17 20:59 시작",
		},
		"no start uses labeled fallback": {
			entry: BroadcastHistoryEntry{Time: time.Date(2026, time.October, 17, 22, 29, 0, 0, kst), TimeLabel: "종료"},
			want:  "⏰ 2026/10/17 22:29 종료",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			entry := tc.entry

			entry.VideoID, entry.MemberName, entry.TypeLabel = "AqxEw3kXcgU", testDisplayName, "게임"

			got := kakaoformat.Render(NewResponseFormatter("!", nil).BroadcastHistory(t.Context(), BroadcastHistoryFilter{}, []BroadcastHistoryEntry{entry}))
			if !hasBroadcastHistoryLine(got, tc.want) {
				t.Fatalf("BroadcastHistory() missing line %q in:\n%s", tc.want, got)
			}
		})
	}
}

func hasBroadcastHistoryLine(text, want string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		if line == want {
			return true
		}
	}

	return false
}
