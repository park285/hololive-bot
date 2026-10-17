package formatter

import (
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func TestLiveQueryShowsElapsedSinceQueryTime(t *testing.T) {
	pool := dbtest.NewPool(t)
	f := NewResponseFormatter("!", template.NewRenderer(pool, slog.Default()),
		WithMessageStrings(messagestrings.NewStore(pool, slog.Default())))
	asOf := time.Date(2026, time.October, 17, 21, 0, 0, 0, time.UTC)
	startedBefore := func(d time.Duration) *time.Time { return new(asOf.Add(-d)) }

	for _, tc := range []struct {
		name      string
		startedAt *time.Time
		asOf      time.Time
		want      string
	}{
		{"unknown start", nil, asOf, "1 · 츠즈리"},
		{"under a minute", startedBefore(59 * time.Second), asOf, "1 · 츠즈리 · ⏱ 방금 시작"},
		{"one minute", startedBefore(time.Minute), asOf, "1 · 츠즈리 · ⏱ 1분째"},
		{"hours floor minutes", startedBefore(83*time.Minute + 59*time.Second), asOf, "1 · 츠즈리 · ⏱ 1시간 23분째"},
		{"days", startedBefore(77*time.Hour + 5*time.Minute), asOf, "1 · 츠즈리 · ⏱ 3일 5시간 5분째"},
		{"start after query time", new(asOf.Add(time.Second)), asOf, "1 · 츠즈리"},
		{"missing query time", startedBefore(time.Hour), time.Time{}, "1 · 츠즈리"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := f.LiveQuery(t.Context(), livequery.Result{
				Items: []livequery.Item{{
					VideoID: "live0000001", ChannelID: "UC_live", ChannelName: "츠즈리", Title: "잡담 방송", StartedAt: tc.startedAt,
				}},
				Status: livequery.Complete, AsOf: tc.asOf,
			}, "")

			// 경과를 표시하지 않는 경우에는 구분자와 아이콘까지 빠져 방송명 줄이 그대로여야 한다.
			if !slices.Contains(strings.Split(out, "\n"), tc.want) || (!strings.Contains(tc.want, "⏱") && strings.Contains(out, "⏱")) {
				t.Errorf("channel line %q missing or elapsed shown unexpectedly: %q", tc.want, out)
			}
		})
	}
}
