package format

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

// 여러 outbox 항목을 한 번에 포맷해도 채널별 override와 기본 template 선택이 항목마다 유지되고,
// payload를 읽지 못한 항목만 실패하며 다른 항목은 정상 본문을 받는다.
func TestFormatMessagesKeepsPerItemTemplateSelection(t *testing.T) {
	t.Parallel()

	db := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	overrideChannel := "UC_override"

	if _, _, err := repository.NewTemplateRepository(db, logger).UpsertWithRevision(t.Context(), domain.TemplateKeyOutboxShorts, &overrideChannel, "OVERRIDE {{.MemberName}} {{.VideoID}}", 0); err != nil {
		t.Fatalf("upsert override: %v", err)
	}

	formatter := NewMessageFormatter(template.NewRenderer(db, logger), staticMemberNameSource{name: "멤버"}, nil)
	items := []domain.YouTubeNotificationOutbox{
		{ID: 1, Kind: domain.OutboxKindNewShort, ChannelID: "UC_default", Payload: `{"video_id":"abc","title":"테스트 쇼츠"}`},
		{ID: 2, Kind: domain.OutboxKindNewShort, ChannelID: overrideChannel, Payload: `{"video_id":"def","title":"다른 쇼츠"}`},
		{ID: 3, Kind: domain.OutboxKindNewShort, ChannelID: "UC_default", Payload: `not json`},
	}

	results, err := formatter.FormatMessages(t.Context(), items)
	if err != nil || len(results) != len(items) {
		t.Fatalf("FormatMessages() = %+v, %v", results, err)
	}

	if results[0].Err != nil {
		t.Fatalf("default item: %v", results[0].Err)
	}

	for _, field := range []string{"멤버", "테스트 쇼츠", "https://www.youtube.com/shorts/abc"} {
		if !strings.Contains(kakaoformat.Render(results[0].Message), field) {
			t.Errorf("default item lost %q: %q", field, results[0].Message)
		}
	}

	if want := "OVERRIDE 멤버 def"; results[1].Err != nil || results[1].Message != want {
		t.Errorf("override item = %+v; want %q", results[1], want)
	}

	if results[2].Err == nil || results[2].Message != "" {
		t.Errorf("invalid payload item = %+v; want item error without message", results[2])
	}
}
