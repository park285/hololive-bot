package formatter

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

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
		{"unknown start", nil, asOf, ""},
		{"under a minute", startedBefore(59 * time.Second), asOf, "방금 시작"},
		{"one minute", startedBefore(time.Minute), asOf, "1분째"},
		{"hours floor minutes", startedBefore(83*time.Minute + 59*time.Second), asOf, "1시간 23분째"},
		{"days", startedBefore(77*time.Hour + 5*time.Minute), asOf, "3일 5시간 5분째"},
		{"start after query time", new(asOf.Add(time.Second)), asOf, ""},
		{"missing query time", startedBefore(time.Hour), time.Time{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := f.LiveQuery(t.Context(), livequery.Result{
				Items: []livequery.Item{{
					VideoID: "live0000001", ChannelID: "UC_live", ChannelName: "츠즈리", Title: "잡담 방송", StartedAt: tc.startedAt,
				}},
				Status: livequery.Complete, AsOf: tc.asOf,
			}, "")

			if elapsed := liveElapsed(tc.startedAt, tc.asOf); elapsed != tc.want {
				t.Errorf("조회 시각 기준 경과 불일치: %q, want %q", elapsed, tc.want)
			}
			if !strings.Contains(kakaoformat.Render(out), tc.want) {
				t.Errorf("경과 표시 손실: %q in %q", tc.want, out)
			}
		})
	}
}
