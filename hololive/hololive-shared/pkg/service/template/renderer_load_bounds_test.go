package template

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	texttemplate "text/template"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/repository"
)

type (
	sourceChangeTraceKey struct{}
	sourceChangeTrace    struct {
		versions     atomic.Int64
		bodies       atomic.Int64
		deadlines    []time.Time
		afterResolve func(context.Context, int64)
	}
)

func (tr *sourceChangeTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "SELECT id, row_version") {
		n := tr.versions.Add(1)
		deadline, _ := ctx.Deadline()

		tr.deadlines = append(tr.deadlines, deadline)

		return context.WithValue(ctx, sourceChangeTraceKey{}, n)
	}

	if strings.HasPrefix(data.SQL, "SELECT body, row_version") {
		tr.bodies.Add(1)
	}

	return ctx
}

func (tr *sourceChangeTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if n, ok := ctx.Value(sourceChangeTraceKey{}).(int64); ok && tr.afterResolve != nil {
		tr.afterResolve(ctx, n)
	}
}

func TestRendererSourceReselectionIsBounded(t *testing.T) {
	t.Run("override deleted", func(t *testing.T) { checkSourceReselection(t, false) })
	t.Run("both selected sources deleted", func(t *testing.T) { checkSourceReselection(t, true) })
}

func checkSourceReselection(t *testing.T, deleteDefault bool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	admin := NewAdminService(repository.NewTemplateRepository(pool, logger), NewRenderer(pool, logger), logger)
	key := domain.TemplateKeyCmdHelp
	channel := "bounded-reselect"

	if _, err := admin.Save(t.Context(), key, nil, "DEFAULT"); err != nil {
		t.Fatal(err)
	}

	if _, err := admin.Save(t.Context(), key, &channel, "OVERRIDE"); err != nil {
		t.Fatal(err)
	}

	tr := &sourceChangeTrace{}

	tr.afterResolve = func(ctx context.Context, n int64) {
		if n == 1 {
			if err := admin.DeleteOverride(ctx, key, channel); err != nil {
				t.Error(err)
			}
		} else if deleteDefault {
			if _, err := pool.Exec(ctx, `DELETE FROM notification_templates WHERE template_key=$1 AND channel_id IS NULL`, key); err != nil {
				t.Error(err)
			}
		}
	}

	var logs bytes.Buffer

	r := NewRenderer(tracedCachePool(t, pool, tr), slog.New(slog.NewTextHandler(&logs, nil)))
	got, err := r.Render(t.Context(), key, channel, nil)

	assertSourceReselectionResult(t, got, err, deleteDefault, logs.String())

	if tr.versions.Load() != 2 || tr.bodies.Load() != 2 {
		t.Fatalf("queries: version=%d body=%d", tr.versions.Load(), tr.bodies.Load())
	}

	if len(tr.deadlines) != 2 || tr.deadlines[0].IsZero() || !tr.deadlines[0].Equal(tr.deadlines[1]) {
		t.Fatalf("reselection changed wait budget: %v", tr.deadlines)
	}

	if !strings.Contains(logs.String(), "attempt=1") {
		t.Fatal("missing reselection event")
	}
}

func assertSourceReselectionResult(t *testing.T, got string, err error, deleteDefault bool, logs string) {
	t.Helper()

	if deleteDefault {
		if !errors.Is(err, errTemplateSourceChanged) || !strings.Contains(err.Error(), "after 2 attempts") {
			t.Fatalf("expected bounded source-change error, got %v", err)
		}

		if !strings.Contains(logs, "exhausted=true") {
			t.Fatal("missing exhaustion event")
		}
	} else if err != nil || got != "DEFAULT" {
		t.Fatalf("render after delete: %q %v", got, err)
	}
}

func TestRendererOtherErrorsDoNotReselect(t *testing.T) {
	for _, kind := range []string{"missing", "parse", "database"} {
		t.Run(kind, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			key := domain.TemplateKeyCmdHelp
			tr := &sourceChangeTrace{}

			switch kind {
			case "missing":
				key = "RESELECT_MISSING"
			case "parse":
				if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body='{{' WHERE template_key=$1`, key); err != nil {
					t.Fatal(err)
				}
			case "database":
				tr.afterResolve = func(ctx context.Context, _ int64) {
					if _, err := pool.Exec(ctx, `ALTER TABLE notification_templates RENAME TO temporarily_unavailable_templates`); err != nil {
						t.Error(err)
					}
				}
			}

			r := NewRenderer(tracedCachePool(t, pool, tr), slog.New(slog.DiscardHandler))
			if _, err := r.Render(t.Context(), key, "", nil); err == nil {
				t.Fatal("expected error")
			}

			if got := tr.versions.Load(); got != 1 {
				t.Fatalf("retried %s error: version queries=%d", kind, got)
			}
		})
	}
}

func TestRendererWarmLookupBudgetIncludesPoolWait(t *testing.T) {
	for _, tc := range []struct {
		name          string
		callerTimeout time.Duration
	}{
		{name: "renderer budget"},
		{name: "shorter caller budget", callerTimeout: 50 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := dbtest.NewPool(t)
			cfg := base.Config()

			cfg.MaxConns = 1
			cfg.MinConns = 0

			pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(pool.Close)

			r := NewRenderer(pool, slog.New(slog.DiscardHandler))
			if _, err = r.getTemplate(t.Context(), domain.TemplateKeyCmdHelp, ""); err != nil {
				t.Fatal(err)
			}

			held, err := pool.Acquire(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer held.Release()

			ctx := t.Context()
			want := templateLoadTimeout

			if tc.callerTimeout > 0 {
				var cancel context.CancelFunc

				ctx, cancel = context.WithTimeout(ctx, tc.callerTimeout)

				defer cancel()

				want = tc.callerTimeout
			}

			start := time.Now()

			_, err = r.getTemplate(ctx, domain.TemplateKeyCmdHelp, "")

			elapsed := time.Since(start)

			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("pool wait: %v", err)
			}

			if elapsed < want/2 || elapsed > want+2*time.Second {
				t.Fatalf("wait=%s budget=%s", elapsed, want)
			}

			t.Logf("pool wait stopped after %s (budget %s)", elapsed, want)
		})
	}
}

func TestRendererBlockedParseDoesNotBlockOtherCacheKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBoundTestRenderer()
		blocked := cacheKey{templateKey: domain.TemplateKeyCmdHelp, id: 1, version: 1}
		other := cacheKey{templateKey: domain.TemplateKeyCmdProfile, id: 2, version: 1}
		expected := texttemplate.Must(texttemplate.New("shared").Parse("SHARED"))
		r.storeTemplateAt(other, expected, time.Now())

		release := make(chan struct{})

		owner := r.parses.DoChan(fmt.Sprintf("%d/%d", blocked.id, blocked.version), func() (any, error) {
			<-release

			return expected, nil
		})
		done := make(chan parsedCacheResult, 2)

		for range 2 {
			go func() {
				tmpl, err := r.parseTemplate(blocked, "{{invalid")
				done <- parsedCacheResult{tmpl, err}
			}()
		}

		synctest.Wait()

		if !r.cacheMu.TryLock() {
			t.Fatal("shared parse wait holds the common cache lock")
		}

		r.cacheMu.Unlock()

		if r.cachedTemplate(other) != expected {
			t.Fatal("unrelated cache entry unavailable")
		}

		r.InvalidateKey(other.templateKey)

		if r.cachedTemplate(other) != nil {
			t.Fatal("unrelated invalidation failed")
		}

		close(release)
		<-owner

		for range 2 {
			result := <-done
			if result.err != nil || result.tmpl != expected {
				t.Fatalf("parse did not share actual-version flight: %+v", result)
			}
		}
	})
}

func TestRendererDifferentResolvedVersionsShareActualParse(t *testing.T) {
	pool := dbtest.NewPool(t)
	key := domain.TemplateKeyCmdHelp
	old := cacheKey{templateKey: key}

	if err := pool.QueryRow(t.Context(), mustSQL("renderer_resolve.sql"), key, "").Scan(&old.id, &old.version, &old.channelID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body='NEW ACTUAL VERSION' WHERE id=$1`, old.id); err != nil {
		t.Fatal(err)
	}

	current := old
	current.version++

	tr := &cacheQueryTracer{entered: make(chan struct{}, 2), release: make(chan struct{})}
	r := NewRenderer(tracedCachePool(t, pool, tr), slog.New(slog.DiscardHandler))

	var once sync.Once

	release := func() { once.Do(func() { close(tr.release) }) }
	t.Cleanup(release)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)

	defer cancel()

	done := make(chan parsedCacheResult, 2)

	for _, ck := range []cacheKey{old, current} {
		go func() { tmpl, err := r.loadResolvedTemplate(ctx, ck); done <- parsedCacheResult{tmpl, err} }()
	}

	for range 2 {
		select {
		case <-tr.entered:
		case <-ctx.Done():
			t.Fatal("both body reads did not finish")
		}
	}

	release()
	assertSharedCacheResults(ctx, t, done, 2)

	if r.cachedTemplate(old) != nil || r.cachedTemplate(current) == nil {
		t.Fatal("parsed body stored under wrong version")
	}
}

func TestRendererResolvedCacheHonorsCancellation(t *testing.T) {
	r := newBoundTestRenderer()
	ck := cacheKey{templateKey: domain.TemplateKeyCmdHelp, id: 1, version: 1}
	r.storeTemplateAt(ck, parsedTestTemplate(t), time.Now())

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := r.loadResolvedTemplate(ctx, ck); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached result concealed caller cancellation: %v", err)
	}
}
