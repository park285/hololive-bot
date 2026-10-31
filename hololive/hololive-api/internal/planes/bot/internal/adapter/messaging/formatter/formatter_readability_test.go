package formatter

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-api/internal/templateview"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
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
			if strings.Contains(out, streams[streamListDisplayLimit].GetYouTubeURL()) {
				t.Errorf("표시 한도를 넘은 항목 노출: %q", out)
			}

			for _, stream := range streams[:streamListDisplayLimit] {
				if !strings.Contains("\n"+out+"\n", "\n"+stream.GetYouTubeURL()+"\n") {
					t.Errorf("독립 URL 손실: %q", stream.GetYouTubeURL())
				}
			}

			padding := strings.Repeat(kakaotext.ZeroWidthSpace, templateview.KakaoSeeMorePadding)
			if strings.Count(out, padding) != 1 || templateview.FoldForSeeMore(out) != out {
				t.Error("fold padding lost or duplicated")
			}

			head, _, _ := strings.Cut(rendered, padding)
			if !strings.Contains(head, fmt.Sprint(len(streams))) || !strings.Contains(head, fmt.Sprint(streamListDisplayLimit)) {
				t.Error("표시 한도 수치가 접힌 머리 문단에서 사라짐")
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
			VideoID: videoID, MemberName: "||미코|| **별명**\r\n이름", TypeLabel: "노래", Title: title,
			URL: url, HasThumbnail: true, Time: time.Unix(1, 0),
			TimeLabel: "||시각|| **원문**",
		},
		{MemberName: "다음 채널", TypeLabel: "게임", Title: strings.Repeat("긴 제목 ", 40), Time: time.Unix(1, 0)},
	}
	out := kakaoformat.Render(f.BroadcastHistory(t.Context(), BroadcastHistoryFilter{Days: 7}, entries))
	visible := strings.ReplaceAll(out, kakaotext.ZeroWidthSpace, "")

	for _, want := range []string{"||미코|| **별명** 이름", "||시각|| **원문**", title, "\n" + url + "\n", "!썸네일 " + videoID, "다음 채널"} {
		if !strings.Contains(visible, want) {
			t.Errorf("history display lost %q: %q", want, out)
		}
	}

	if strings.Contains(out, strings.Repeat("긴 제목 ", 20)) {
		t.Errorf("long or literal title formatting regressed: %q", out)
	}
}

func TestStreamTitlesNormalizeBeforeFirstTruncation(t *testing.T) {
	pool := dbtest.NewPool(t)
	f := NewResponseFormatter("!", template.NewRenderer(pool, slog.Default()),
		WithMessageStrings(messagestrings.NewStore(pool, slog.Default())))

	for name, padding := range map[string]string{
		"spaces":    strings.Repeat(" ", 100),
		"invisible": strings.Repeat(kakaotext.ZeroWidthSpace, 100),
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

	padding := strings.Repeat(kakaotext.ZeroWidthSpace, templateview.KakaoSeeMorePadding)
	withoutNotice := kakaoformat.Render(f.LiveQuery(t.Context(), livequery.Result{
		Items: items, Status: livequery.Partial, Truncated: false, AsOf: time.Unix(1, 0),
	}, ""))
	head, body, folded := strings.Cut(out, padding)
	plainHead, plainBody, plainFolded := strings.Cut(withoutNotice, padding)

	if !folded || !plainFolded || len(head) <= len(plainHead) || body != plainBody {
		t.Fatal("잘린 결과 안내가 접기 아래로 이동하거나 항목을 변경함")
	}
}
