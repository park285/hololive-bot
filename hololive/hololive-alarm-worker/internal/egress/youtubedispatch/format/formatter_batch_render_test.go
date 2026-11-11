package format

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

// countingMemberNameSource는 채널별 표시명 조회 횟수를 세고, err가 있으면 모든 조회에 그 오류를 돌려준다.
type countingMemberNameSource struct {
	name  string
	err   error
	calls map[string]int
}

func (s *countingMemberNameSource) GetMemberName(_ context.Context, channelID string) (string, error) {
	if s.calls == nil {
		s.calls = make(map[string]int)
	}

	s.calls[channelID]++

	return s.name, s.err
}

// 여러 outbox 항목을 한 번에 포맷해도 채널별 override와 기본 template 선택이 항목·변형마다 유지되고,
// payload를 읽지 못한 항목만 실패하며 다른 항목은 정상 본문을 받는다. 표시명은 채널마다 한 번만 조회한다.
func TestFormatMessagesKeepsPerItemTemplateSelection(t *testing.T) {
	t.Parallel()

	db := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	overrideChannel := "UC_override"

	if _, err := db.Exec(t.Context(), `INSERT INTO notification_templates (template_key, channel_id, format_variant, body)
VALUES ($1, $2, 'plain', $3), ($1, $2, 'richtext', $4)`,
		domain.TemplateKeyOutboxShorts, overrideChannel, "OVERRIDE {{.MemberName}} {{.VideoID}}", "**OVERRIDE** {{.MemberName}} {{.VideoID}}"); err != nil {
		t.Fatalf("upsert override: %v", err)
	}

	memberNames := &countingMemberNameSource{name: "멤버"}
	formatter := NewMessageFormatter(template.NewRenderer(db, logger), memberNames, nil)
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

	for _, message := range []string{results[0].Message.Plain, results[0].Message.RichText} {
		for _, field := range []string{"멤버", "테스트 쇼츠", "https://www.youtube.com/shorts/abc"} {
			if !strings.Contains(kakaoformat.Render(message), field) {
				t.Errorf("default item lost %q: %q", field, message)
			}
		}
	}

	want := delivery.MessageVariants{Plain: "OVERRIDE 멤버 def", RichText: "**OVERRIDE** 멤버 def"}
	if results[1].Err != nil || results[1].Message != want {
		t.Errorf("override item = %+v; want %+v", results[1], want)
	}

	if results[2].Err == nil || results[2].Message != (delivery.MessageVariants{}) {
		t.Errorf("invalid payload item = %+v; want item error without message", results[2])
	}

	if want := map[string]int{"UC_default": 1, overrideChannel: 1}; !maps.Equal(memberNames.calls, want) {
		t.Errorf("member name lookups = %v; want %v", memberNames.calls, want)
	}
}

// 표시명 조회 실패도 채널마다 한 번만 조회하고, 그 채널의 모든 항목이 대체 문구 없이 같은 실패를 받는다.
func TestFormatMessagesSharesMemberNameLookupFailurePerChannel(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("postgres unavailable")
	memberNames := &countingMemberNameSource{err: lookupErr}
	items := []domain.YouTubeNotificationOutbox{
		{ID: 1, Kind: domain.OutboxKindNewVideo, ChannelID: "a", Payload: `{"video_id":"v1","title":"t1"}`},
		{ID: 2, Kind: domain.OutboxKindNewVideo, ChannelID: "a", Payload: `{"video_id":"v2","title":"t2"}`},
		{ID: 3, Kind: domain.OutboxKindNewVideo, ChannelID: "b", Payload: `{"video_id":"v3","title":"t3"}`},
	}

	results, err := NewMessageFormatter(nil, memberNames, nil).FormatMessages(t.Context(), items)
	if err != nil || len(results) != len(items) {
		t.Fatalf("FormatMessages() = %+v, %v; want item results", results, err)
	}

	for i := range results {
		if !errors.Is(results[i].Err, lookupErr) || results[i].Message != (delivery.MessageVariants{}) {
			t.Errorf("result %d = %+v; want lookup failure without message", i, results[i])
		}
	}

	if want := map[string]int{"a": 1, "b": 1}; !maps.Equal(memberNames.calls, want) {
		t.Errorf("member name lookups = %v; want %v", memberNames.calls, want)
	}
}

type renderStatementCounter struct {
	statements atomic.Int64
}

func (c *renderStatementCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.statements.Add(1)

	return ctx
}

func (*renderStatementCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// 묶음 메시지의 두 본문 변형은 template 버전 확인을 한 문장으로 끝낸다. 파싱 cache가 찬 뒤에는 그 한 문장만 실행한다.
func TestFormatGroupedMessageResolvesBothVariantsInOneStatement(t *testing.T) {
	t.Parallel()

	counter := &renderStatementCounter{}
	cfg := dbtest.NewPool(t).Config()

	cfg.ConnConfig.Tracer = counter

	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatalf("traced pool: %v", err)
	}

	t.Cleanup(pool.Close)

	formatter := NewMessageFormatter(template.NewRenderer(pool, slog.New(slog.DiscardHandler)), nil, nil)
	items := []domain.YouTubeNotificationOutbox{
		{ID: 1, Kind: domain.OutboxKindNewShort, ChannelID: "UC_group", Payload: `{"video_id":"abc","title":"첫 쇼츠"}`},
		{ID: 2, Kind: domain.OutboxKindNewShort, ChannelID: "UC_group", Payload: `{"video_id":"def","title":"둘째 쇼츠"}`},
	}

	_, err = formatter.FormatGroupedMessage(t.Context(), "멤버", "UC_group", domain.OutboxKindNewShort, items)
	if err != nil {
		t.Fatalf("warm FormatGroupedMessage() error = %v", err)
	}

	counter.statements.Store(0)

	message, err := formatter.FormatGroupedMessage(t.Context(), "멤버", "UC_group", domain.OutboxKindNewShort, items)
	if err != nil {
		t.Fatalf("FormatGroupedMessage() error = %v", err)
	}

	for _, text := range []string{message.Plain, message.RichText} {
		if !strings.Contains(kakaoformat.Render(text), "둘째 쇼츠") {
			t.Errorf("grouped message lost item title: %q", text)
		}
	}

	if statements := counter.statements.Load(); statements != 1 {
		t.Fatalf("statements = %d; want one template version statement", statements)
	}
}
