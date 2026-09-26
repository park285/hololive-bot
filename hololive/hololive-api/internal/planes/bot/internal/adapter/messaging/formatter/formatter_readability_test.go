package formatter

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
)

func TestDefaultStreamLayoutsReportDisplayLimitAndKeepFold(t *testing.T) {
	pool := dbtest.NewPool(t)
	f := NewResponseFormatter("!", template.NewRenderer(pool, slog.Default()),
		WithMessageStrings(messagestrings.NewStore(pool, slog.Default())), WithSeeMoreFold(true))
	streams := make([]*domain.Stream, streamListDisplayLimit+1)

	for i := range streams {
		streams[i] = &domain.Stream{
			ID: fmt.Sprintf("video%06d", i), ChannelName: "채널", Title: "1. **긴 제목** " + strings.Repeat("방송", 50),
			Status: domain.StreamStatusUpcoming,
		}
	}

	for name, rendered := range map[string]string{
		"live":     formatLiveStreams(t.Context(), f, streams),
		"upcoming": f.UpcomingStreams(t.Context(), streams, 24),
		"schedule": f.ChannelSchedule(t.Context(), &domain.Channel{Name: "채널"}, streams, 7),
	} {
		t.Run(name, func(t *testing.T) {
			out := kakaoformat.Render(rendered)
			if !strings.Contains(out, "전체 101개 중 100개 표시") || strings.Contains(out, streams[100].GetYouTubeURL()) {
				t.Errorf("display cap notice or item bound broken: %q", out)
			}

			if strings.Count(out, "\n──────────\n") != 99 || strings.Contains(out, "\n\n──────────") || strings.Contains(out, "──────────\n\n") {
				t.Error("multi-stream item separators lost or widened after final conversion")
			}

			padding := strings.Repeat(util.KakaoZeroWidthSpace, util.KakaoSeeMorePadding)
			if strings.Count(out, padding) != 1 || util.FoldForSeeMore(out, util.KakaoSeeMoreThreshold) != out {
				t.Error("fold padding lost or duplicated")
			}

			// 표시 한도 안내는 머리 문단에 있으므로 접힌 화면(패딩 앞)에 남아야 한다.
			if notice := strings.Index(out, "전체 101개 중 100개 표시"); notice < 0 || notice > strings.Index(out, padding) {
				t.Errorf("display cap notice folded away: %q", out[:min(len(out), 160)])
			}
		})
	}
}

func TestBroadcastHistoryFinalTextKeepsBoundariesAndCopyableShortcut(t *testing.T) {
	f := NewResponseFormatter("!", nil, WithSeeMoreFold(true))

	const (
		title   = "1. **노래** _今日_ ~~보이는 문자~~"
		videoID = "_abcdEF123_"
		url     = "https://youtu.be/" + videoID
	)

	entries := []BroadcastHistoryEntry{
		{
			VideoID: videoID, MemberName: "미코\r\nMiko", TypeLabel: "노래", Title: title,
			URL: url, HasThumbnail: true, Time: time.Unix(1, 0),
		},
		{MemberName: "다음 채널", TypeLabel: "게임", Title: strings.Repeat("긴 제목 ", 40), Time: time.Unix(1, 0)},
	}
	out := kakaoformat.Render(f.BroadcastHistory(t.Context(), BroadcastHistoryFilter{Days: 7}, entries))
	visible := strings.ReplaceAll(out, util.KakaoZeroWidthSpace, "")

	for _, want := range []string{"1 · [노래] 미코 Miko", title, "\n" + url + "\n", "썸네일: !썸네일 " + videoID, "_\n──────────\n2 · [게임] 다음 채널"} {
		if !strings.Contains(visible, want) {
			t.Errorf("history display lost %q: %q", want, out)
		}
	}

	if strings.Contains(out, strings.Repeat("긴 제목 ", 20)) || strings.Contains(out, "❪") {
		t.Errorf("long or literal title formatting regressed: %q", out)
	}
}

func TestStreamTitlesNormalizeBeforeFirstTruncation(t *testing.T) {
	pool := dbtest.NewPool(t)
	f := NewResponseFormatter("!", template.NewRenderer(pool, slog.Default()),
		WithMessageStrings(messagestrings.NewStore(pool, slog.Default())))

	for name, padding := range map[string]string{
		"spaces":    strings.Repeat(" ", 100),
		"invisible": strings.Repeat(util.KakaoZeroWidthSpace, 100),
	} {
		t.Run(name, func(t *testing.T) {
			const title = "노래 방송 첫 줄 다음 줄"

			original := padding + "노래 방송\r\n첫 줄\t다음 줄"
			stream := &domain.Stream{ID: "example0001", ChannelName: "채널", Title: original, Status: domain.StreamStatusLive}
			streams := []*domain.Stream{stream}

			for layout, rendered := range map[string]string{
				"live":     formatLiveStreams(t.Context(), f, streams),
				"upcoming": f.UpcomingStreams(t.Context(), streams, 24),
				"schedule": f.ChannelSchedule(t.Context(), &domain.Channel{Name: "채널"}, streams, 7),
			} {
				if output := kakaoformat.Render(rendered); !strings.Contains(output, title) {
					t.Errorf("%s lost visible title: %q", layout, output)
				}
			}

			if stream.Title != original {
				t.Fatal("display normalization changed source title")
			}
		})
	}
}

func TestLiveQueryKeepsTruncationNoticeAboveFold(t *testing.T) {
	pool := dbtest.NewPool(t)
	f := NewResponseFormatter("!", template.NewRenderer(pool, slog.Default()),
		WithMessageStrings(messagestrings.NewStore(pool, slog.Default())), WithSeeMoreFold(true))
	items := make([]livequery.Item, livequery.MaxItems)

	for i := range items {
		items[i] = livequery.Item{VideoID: fmt.Sprintf("live%07d", i), ChannelID: "UC_live", ChannelName: "채널", Title: "방송 제목"}
	}

	out := kakaoformat.Render(f.LiveQuery(t.Context(), livequery.Result{
		Items: items, Status: livequery.Partial, Truncated: true, AsOf: time.Unix(1, 0),
	}, ""))

	padding := strings.Repeat(util.KakaoZeroWidthSpace, util.KakaoSeeMorePadding)
	if notice := strings.Index(out, "표시 한도를 초과한 방송이 있습니다."); notice < 0 || notice > strings.Index(out, padding) {
		t.Errorf("truncation notice folded away: %q", out[:min(len(out), 160)])
	}

	for _, removed := range []string{"조회 미완료", "기준:"} {
		if strings.Contains(out, removed) {
			t.Errorf("coverage diagnostics must not be shown: %q", removed)
		}
	}
}

func TestAlarmListShowsOnlyRestrictedTypeLabels(t *testing.T) {
	pool := dbtest.NewPool(t)
	f := NewResponseFormatter("!", template.NewRenderer(pool, slog.Default()),
		WithMessageStrings(messagestrings.NewStore(pool, slog.Default())))

	out := f.FormatAlarmList(t.Context(), []AlarmListEntry{
		{MemberName: "미오"},
		{MemberName: "비비", AlarmTypes: domain.AlarmTypes(domain.AllAlarmTypes)},
		{MemberName: "이로하", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeShorts}},
	})

	want := "🔔 설정된 알람 · 3개\n\n1 · 미오\n2 · 비비\n3 · 이로하 (방송+쇼츠)"
	if out != want {
		t.Fatalf("alarm list labels:\ngot =%q\nwant=%q", out, want)
	}
}
