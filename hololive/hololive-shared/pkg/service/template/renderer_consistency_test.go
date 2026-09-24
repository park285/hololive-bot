package template

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	texttemplate "text/template"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/repository"
)

const cacheAfterBody = "AFTER"

func TestRendererAdminSaveReachesConsumerRenderer(t *testing.T) {
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	consumer := NewRenderer(pool, logger)
	admin := NewAdminService(repository.NewTemplateRepository(pool, logger), NewRenderer(pool, logger), logger)
	key := domain.TemplateKeyCmdHelp

	if _, err := admin.Save(t.Context(), key, nil, "BEFORE"); err != nil {
		t.Fatal(err)
	}

	if got, err := consumer.Render(t.Context(), key, "", nil); err != nil || got != "BEFORE" {
		t.Fatalf("warm cache: got=%q err=%v", got, err)
	}

	saved, err := admin.Save(t.Context(), key, nil, cacheAfterBody)
	if err != nil || saved.Body != cacheAfterBody {
		t.Fatalf("save: err=%v", err)
	}

	if fresh, err := NewRenderer(pool, logger).Render(t.Context(), key, "", nil); err != nil || fresh != cacheAfterBody {
		t.Fatalf("database update missing: got=%q err=%v", fresh, err)
	}

	for range 3 {
		if got, err := consumer.Render(t.Context(), key, "", nil); err != nil || got != cacheAfterBody {
			t.Errorf("consumer uses stale saved template: got=%q err=%v", got, err)
		}
	}
}

type (
	cacheBodyQueryKey    struct{}
	cacheBodyReadBarrier struct {
		entered, release chan struct{}
		held             atomic.Bool
	}
)

func (tr *cacheBodyReadBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, cacheBodyQueryKey{}, strings.HasPrefix(strings.TrimSpace(data.SQL), "SELECT body, row_version"))
}

func (tr *cacheBodyReadBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(cacheBodyQueryKey{}) != true {
		return
	}

	if tr.held.CompareAndSwap(false, true) {
		close(tr.entered)

		select {
		case <-tr.release:
		case <-ctx.Done():
		}
	}
}

func TestRendererInFlightReadCannotUndoInvalidation(t *testing.T) {
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	tr := &cacheBodyReadBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	tracedPool := tracedCachePool(t, pool, tr)

	var releaseOnce sync.Once

	unblock := func() { releaseOnce.Do(func() { close(tr.release) }) }
	t.Cleanup(unblock)

	renderer := NewRenderer(tracedPool, logger)
	admin := NewAdminService(repository.NewTemplateRepository(pool, logger), renderer, logger)
	key := domain.TemplateKeyCmdHelp

	if _, err := admin.Save(t.Context(), key, nil, "BEFORE"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)

	defer cancel()

	type result struct {
		text string
		err  error
	}

	done := make(chan result, 1)

	go func() { msg, err := renderer.Render(ctx, key, "", nil); done <- result{msg, err} }()

	select {
	case <-tr.entered:
	case <-ctx.Done():
		t.Fatal("old template read did not reach barrier")
	}

	if _, err := admin.Save(ctx, key, nil, cacheAfterBody); err != nil {
		t.Fatal(err)
	}

	if got, err := renderer.Render(ctx, key, "", nil); err != nil || got != cacheAfterBody {
		t.Fatalf("post-save request joined old flight: got=%q err=%v", got, err)
	}

	unblock()

	select {
	case first := <-done:
		if first.err != nil || first.text != "BEFORE" {
			t.Fatalf("fixture did not read pre-update body: %+v", first)
		}
	case <-ctx.Done():
		t.Fatal("in-flight render did not finish")
	}

	for range 3 {
		if got, err := renderer.Render(ctx, key, "", nil); err != nil || got != cacheAfterBody {
			t.Errorf("in-flight load reinstalled stale cache after save: got=%q err=%v", got, err)
		}
	}
}

func TestRendererOverrideLifecycleAndDefaultSharing(t *testing.T) {
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	r := NewRenderer(pool, logger)
	admin := NewAdminService(repository.NewTemplateRepository(pool, logger), NewRenderer(pool, logger), logger)
	key := domain.TemplateKeyCmdHelp

	if _, err := admin.Save(t.Context(), key, nil, "DEFAULT"); err != nil {
		t.Fatal(err)
	}

	a, err := r.getTemplate(t.Context(), key, "channel-a")
	if err != nil {
		t.Fatal(err)
	}

	b, err := r.getTemplate(t.Context(), key, "channel-b")
	if err != nil {
		t.Fatal(err)
	}

	if a != b {
		t.Fatal("default parsed separately for each channel")
	}

	channel := "channel-a"
	assertRender := func(channel, want string) {
		t.Helper()

		got, err := r.Render(t.Context(), key, channel, nil)
		if err != nil || got != want {
			t.Fatalf("channel %s: got=%q want=%q err=%v", channel, got, want, err)
		}
	}

	if _, err := admin.Save(t.Context(), key, &channel, "OVERRIDE"); err != nil {
		t.Fatal(err)
	}

	assertRender(channel, "OVERRIDE")
	assertRender("channel-b", "DEFAULT")

	const updatedOverride = "UPDATED OVERRIDE"

	if _, err := admin.Save(t.Context(), key, &channel, updatedOverride); err != nil {
		t.Fatal(err)
	}

	assertRender(channel, updatedOverride)

	if _, err := admin.Save(t.Context(), key, nil, "NEW DEFAULT"); err != nil {
		t.Fatal(err)
	}

	assertRender(channel, updatedOverride)
	assertRender("channel-b", "NEW DEFAULT")

	if err := admin.DeleteOverride(t.Context(), key, channel); err != nil {
		t.Fatal(err)
	}

	assertRender(channel, "NEW DEFAULT")

	if _, err := admin.Save(t.Context(), key, &channel, "RECREATED"); err != nil {
		t.Fatal(err)
	}

	assertRender(channel, "RECREATED")
}

type (
	cacheQueryKind   struct{}
	cacheQueryTracer struct {
		versions atomic.Int64
		bodies   atomic.Int64
		resolved chan struct{}
		entered  chan struct{}
		release  chan struct{}
	}
)

func (tr *cacheQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	body := strings.HasPrefix(d.SQL, "SELECT body, row_version")
	if strings.HasPrefix(d.SQL, "SELECT id, row_version") {
		tr.versions.Add(1)

		return context.WithValue(ctx, cacheQueryKind{}, "version")
	}

	if body {
		tr.bodies.Add(1)
	}

	return context.WithValue(ctx, cacheQueryKind{}, body)
}

func (tr *cacheQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(cacheQueryKind{}) == "version" && tr.resolved != nil {
		select {
		case tr.resolved <- struct{}{}:
		case <-ctx.Done():
		}

		return
	}

	if ctx.Value(cacheQueryKind{}) != true || tr.entered == nil {
		return
	}

	select {
	case tr.entered <- struct{}{}:
	case <-ctx.Done():
		return
	}

	select {
	case <-tr.release:
	case <-ctx.Done():
	}
}

func tracedCachePool(t *testing.T, pool *pgxpool.Pool, tr pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()

	cfg := pool.Config()

	cfg.ConnConfig.Tracer = tr
	cfg.MaxConns = 12

	out, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(out.Close)

	return out
}

func TestRendererConcurrentLoadsAndCancellation(t *testing.T) {
	pool := dbtest.NewPool(t)
	tr := &cacheQueryTracer{resolved: make(chan struct{}, 16), entered: make(chan struct{}, 16), release: make(chan struct{})}
	traced := tracedCachePool(t, pool, tr)

	var once sync.Once

	unblock := func() { once.Do(func() { close(tr.release) }) }
	t.Cleanup(unblock)

	r := NewRenderer(traced, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)

	defer cancel()

	firstCtx, cancelFirst := context.WithCancel(ctx)
	first := make(chan error, 1)

	go func() { _, err := r.getTemplate(firstCtx, domain.TemplateKeyCmdHelp, ""); first <- err }()

	select {
	case <-tr.entered:
	case <-ctx.Done():
		t.Fatal("body query not reached")
	}

	const readers = 8

	done := make(chan parsedCacheResult, readers)

	for i := range readers {
		go func() {
			tmpl, err := r.getTemplate(ctx, domain.TemplateKeyCmdHelp, fmt.Sprintf("channel-%d", i))
			done <- parsedCacheResult{tmpl, err}
		}()
	}

	for range readers + 1 {
		select {
		case <-tr.resolved:
		case <-ctx.Done():
			t.Fatal("version queries did not finish before shared body release")
		}
	}

	cancelFirst()

	select {
	case err := <-first:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("canceled waiter blocked")
	}

	unblock()

	assertSharedCacheResults(ctx, t, done, readers)

	if got := tr.bodies.Load(); got != 1 {
		t.Fatalf("body queries=%d want=1", got)
	}

	if got := tr.versions.Load(); got != readers+1 {
		t.Fatalf("version queries=%d", got)
	}

	t.Logf("%d callers: %d version queries, %d body query, one shared AST; canceled leader isolated", readers+1, tr.versions.Load(), tr.bodies.Load())
}

func TestRendererErrorsDoNotReturnStaleCache(t *testing.T) {
	pool := dbtest.NewPool(t)
	r := NewRenderer(pool, slog.New(slog.DiscardHandler))
	key := domain.TemplateKeyCmdHelp

	if _, err := r.getTemplate(t.Context(), key, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body='{{' WHERE template_key=$1`, key); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if _, err := r.getTemplate(t.Context(), key, ""); err == nil {
			t.Fatal("parse error concealed")
		}
	}

	if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body='RECOVERED' WHERE template_key=$1`, key); err != nil {
		t.Fatal(err)
	}

	if got, err := r.Render(t.Context(), key, "", nil); err != nil || got != "RECOVERED" {
		t.Fatalf("recovery: %q %v", got, err)
	}

	if _, err := pool.Exec(t.Context(), `DELETE FROM notification_templates WHERE template_key=$1`, key); err != nil {
		t.Fatal(err)
	}

	if _, err := r.getTemplate(t.Context(), key, ""); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("missing template: %v", err)
	}

	pool.Close()

	if _, err := r.getTemplate(t.Context(), key, ""); err == nil {
		t.Fatal("closed pool returned cached body")
	}
}

func BenchmarkRenderVersionValidated(b *testing.B) {
	pool := dbtest.NewPool(b)
	r := NewRenderer(pool, slog.New(slog.DiscardHandler))
	key := domain.TemplateKeyCmdHelp

	if _, err := pool.Exec(b.Context(), `UPDATE notification_templates SET body='Hello {{.Name}}' WHERE template_key=$1`, key); err != nil {
		b.Fatal(err)
	}

	data := struct{ Name string }{"benchmark"}
	if _, err := r.Render(b.Context(), key, "", data); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		if _, err := r.Render(b.Context(), key, "", data); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRendererBodyReadCachesActualVersion(t *testing.T) {
	pool := dbtest.NewPool(t)
	r := NewRenderer(pool, slog.New(slog.DiscardHandler))
	ck := cacheKey{templateKey: domain.TemplateKeyCmdHelp}

	if err := pool.QueryRow(t.Context(), mustSQL("renderer_resolve.sql"), ck.templateKey, "").Scan(&ck.id, &ck.version, &ck.channelID); err != nil {
		t.Fatal(err)
	}

	// 메타데이터 조회와 본문 조회 사이에 저장이 완료된 상황입니다.
	if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body='LATEST' WHERE id=$1`, ck.id); err != nil {
		t.Fatal(err)
	}

	loaded, err := r.loadTemplate(t.Context(), ck)
	if err != nil {
		t.Fatal(err)
	}

	if r.cachedTemplate(ck) != nil {
		t.Fatal("new body cached under old version")
	}

	current := ck
	current.version++

	if r.cachedTemplate(current) != loaded {
		t.Fatal("actual body version was not cached")
	}

	if got, err := r.Render(t.Context(), ck.templateKey, "", nil); err != nil || got != "LATEST" {
		t.Fatalf("render: %q %v", got, err)
	}
}

type parsedCacheResult struct {
	tmpl *texttemplate.Template
	err  error
}

func assertSharedCacheResults(ctx context.Context, t *testing.T, done <-chan parsedCacheResult, readers int) {
	t.Helper()

	var shared *texttemplate.Template

	for range readers {
		select {
		case got := <-done:
			if got.err != nil {
				t.Fatal(got.err)
			}

			if shared != nil && shared != got.tmpl {
				t.Fatal("duplicate parsed template")
			}

			shared = got.tmpl
		case <-ctx.Done():
			t.Fatal("shared load failed to finish")
		}
	}
}

func TestRendererBodyReadRejectsChangedSource(t *testing.T) {
	pool := dbtest.NewPool(t)
	r := NewRenderer(pool, slog.New(slog.DiscardHandler))
	ck := cacheKey{templateKey: domain.TemplateKeyCmdHelp}

	if err := pool.QueryRow(t.Context(), mustSQL("renderer_resolve.sql"), ck.templateKey, "").Scan(&ck.id, &ck.version, &ck.channelID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET channel_id='another-channel',body='OVERRIDE ONLY' WHERE id=$1`, ck.id); err != nil {
		t.Fatal(err)
	}

	if _, err := r.loadTemplate(t.Context(), ck); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("changed source must not be returned as the default: %v", err)
	}
}
