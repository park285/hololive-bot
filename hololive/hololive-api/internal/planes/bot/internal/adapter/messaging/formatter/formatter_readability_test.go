package formatter

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

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
		"live":     f.FormatLiveStreams(t.Context(), streams),
		"upcoming": f.UpcomingStreams(t.Context(), streams, 24),
		"schedule": f.ChannelSchedule(t.Context(), &domain.Channel{Name: "채널"}, streams, 7),
	} {
		t.Run(name, func(t *testing.T) {
			out := kakaoformat.Render(rendered)
			if !strings.Contains(out, "전체 101개 중 100개 표시") || strings.Contains(out, streams[100].GetYouTubeURL()) {
				t.Errorf("display cap notice or item bound broken: %q", out)
			}

			if strings.Count(out, "\n\n──────────\n\n") != 99 {
				t.Error("multi-stream item spacing lost after final conversion")
			}

			padding := strings.Repeat(util.KakaoZeroWidthSpace, util.KakaoSeeMorePadding)
			if strings.Count(out, padding) != 1 || util.FoldForSeeMore(out, util.KakaoSeeMoreThreshold) != out {
				t.Error("fold padding lost or duplicated")
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

	for _, want := range []string{"1 · [노래] 미코 Miko", title, "\n" + url + "\n", "썸네일: !썸네일 " + videoID, "\n\n──────────\n\n2 · [게임] 다음 채널"} {
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
				"live":     f.FormatLiveStreams(t.Context(), streams),
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
