package template

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/repository"
)

type resolveStatementCounter struct {
	batches atomic.Int64
	singles atomic.Int64
}

func (c *resolveStatementCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	switch sql := strings.TrimSpace(data.SQL); {
	case strings.HasPrefix(sql, "SELECT DISTINCT ON (request.ordinality)"):
		c.batches.Add(1)
	case strings.HasPrefix(sql, "SELECT id, row_version"):
		c.singles.Add(1)
	}

	return ctx
}

func (*resolveStatementCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func upsertTestTemplate(t *testing.T, repo *repository.TemplateRepository, key domain.TemplateKey, channelID *string, body string) {
	t.Helper()

	if _, err := repo.Upsert(t.Context(), key, channelID, body); err != nil {
		t.Fatalf("upsert %s: %v", key, err)
	}
}

// 배치 렌더는 항목 수와 무관하게 호출당 버전 확인 한 문장만 쓰고, 항목별 override 선택·부재·실행 실패를 분리하며,
// 호출 전에 끝난 저장을 다음 호출의 모든 항목이 봅니다.
func TestRenderBatchResolvesOncePerCallWithOverridesAndFreshness(t *testing.T) {
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	repo := repository.NewTemplateRepository(pool, logger)
	key, limitKey := domain.TemplateKeyCmdHelp, domain.TemplateKeyOutboxShorts
	channel := "channel-a"

	upsertTestTemplate(t, repo, key, nil, "DEFAULT {{.}}")
	upsertTestTemplate(t, repo, key, &channel, "OVERRIDE {{.}}")
	upsertTestTemplate(t, repo, limitKey, nil, `{{range 20}}{{printf "%5000s" ""}}{{end}}`)

	counter := &resolveStatementCounter{}
	r := NewRenderer(tracedCachePool(t, pool, counter), logger)
	requests := []RenderRequest{
		{Key: key, ChannelID: channel, Data: "1"},
		{Key: key, ChannelID: "channel-b", Data: "2"},
		{Key: key, ChannelID: channel, Data: "3"},
		{Key: domain.TemplateKey("NO_SUCH_TEMPLATE")},
		{Key: limitKey},
	}

	results, err := r.RenderBatch(t.Context(), requests)
	if err != nil || len(results) != len(requests) {
		t.Fatalf("RenderBatch() = %+v, %v", results, err)
	}

	for i, want := range []string{"OVERRIDE 1", "DEFAULT 2", "OVERRIDE 3"} {
		if results[i].Err != nil || results[i].Text != want {
			t.Errorf("result %d = %+v; want %q", i, results[i], want)
		}
	}

	if !errors.Is(results[3].Err, ErrTemplateNotFound) || results[3].Text != "" {
		t.Errorf("missing template result = %+v; want ErrTemplateNotFound", results[3])
	}

	if !errors.Is(results[4].Err, ErrTemplateExecutionLimit) || results[4].Text != "" {
		t.Errorf("oversize result = %+v; want ErrTemplateExecutionLimit without output", results[4])
	}

	if batches, singles := counter.batches.Load(), counter.singles.Load(); batches != 1 || singles != 0 {
		t.Fatalf("resolve statements = batch %d single %d; want one batch statement", batches, singles)
	}

	upsertTestTemplate(t, repo, key, &channel, "UPDATED {{.}}")

	results, err = r.RenderBatch(t.Context(), requests[:2])
	if err != nil || results[0].Text != "UPDATED 1" || results[1].Text != "DEFAULT 2" {
		t.Fatalf("after save RenderBatch() = %+v, %v; want saved override", results, err)
	}

	if batches := counter.batches.Load(); batches != 2 {
		t.Fatalf("resolve statements after second call = %d; want 2", batches)
	}
}

// 실행 중 취소된 배치는 앞서 렌더된 항목을 포함한 어떤 결과도 돌려주지 않습니다.
func TestRenderBatchCancellationReturnsNoPartialResults(t *testing.T) {
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	repo := repository.NewTemplateRepository(pool, logger)
	key, cancelKey := domain.TemplateKeyCmdHelp, domain.TemplateKeyOutboxVideo

	upsertTestTemplate(t, repo, key, nil, "FIRST")
	upsertTestTemplate(t, repo, cancelKey, nil, "{{call .Cancel}}SECOND")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	results, err := NewRenderer(pool, logger).RenderBatch(ctx, []RenderRequest{
		{Key: key},

		{Key: cancelKey, Data: map[string]any{"Cancel": func() string {
			cancel()

			return ""
		}}},
	})
	if !errors.Is(err, context.Canceled) || results != nil {
		t.Fatalf("RenderBatch() = %+v, %v; want context.Canceled without results", results, err)
	}

	results, err = NewRenderer(pool, logger).RenderBatch(ctx, []RenderRequest{{Key: key}})
	if err == nil || results != nil {
		t.Fatalf("RenderBatch(canceled) = %+v, %v; want error without results", results, err)
	}
}
