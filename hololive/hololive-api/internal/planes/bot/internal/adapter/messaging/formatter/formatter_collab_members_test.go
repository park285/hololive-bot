package formatter

import (
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
)

func TestStreamListsRenderCollabMembers(t *testing.T) {
	pool := dbtest.NewPool(t)
	f := NewResponseFormatter("!", template.NewRenderer(pool, slog.Default()),
		WithMessageStrings(messagestrings.NewStore(pool, slog.Default())))
	start := time.Now().Add(3 * time.Hour)
	collab := []string{"스이세이", "미코"}
	streams := []*domain.Stream{
		{ID: "collab00001", ChannelID: "UC_a", ChannelName: "소라", Title: "콜라보 방송", StartScheduled: &start, CollaboTalentNames: collab},
		{ID: "solo0000001", ChannelID: "UC_b", ChannelName: "아즈키", Title: "혼자 방송", StartScheduled: &start},
	}

	outputs := map[string]string{
		"live": f.LiveQuery(t.Context(), livequery.Result{
			Status: livequery.Complete, AsOf: time.Now(),
			Items: []livequery.Item{
				{VideoID: "collab00001", ChannelID: "UC_a", ChannelName: "소라", Title: "콜라보 방송"},
				{VideoID: "solo0000001", ChannelID: "UC_b", ChannelName: "아즈키", Title: "혼자 방송"},
			},
		}, "", map[string][]string{"collab00001": collab}),
		"upcoming": f.UpcomingStreams(t.Context(), streams, 24),
		"schedule": f.ChannelSchedule(t.Context(), &domain.Channel{ID: "UC_a", Name: "소라"}, streams, 7),
	}

	for name, out := range outputs {
		visible := kakaoformat.Render(out)
		if strings.Count(visible, "콜라보: 스이세이, 미코") != 1 || strings.Count(visible, "콜라보:") != 1 {
			t.Errorf("%s: 콜라보 줄이 한 번만 표시되지 않음: %q", name, visible)
		}
	}
}
