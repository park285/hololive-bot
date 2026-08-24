package messagestrings

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type failingQuerier struct {
	err   error
	calls int
}

func (q *failingQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	q.calls++
	return nil, q.err
}

type emptyRows struct{}

func (emptyRows) Close()                                       {}
func (emptyRows) Err() error                                   { return nil }
func (emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (emptyRows) Next() bool                                   { return false }
func (emptyRows) Scan(...any) error                            { return nil }
func (emptyRows) Values() ([]any, error)                       { return nil, nil }
func (emptyRows) RawValues() [][]byte                          { return nil }
func (emptyRows) Conn() *pgx.Conn                              { return nil }

type emptyQuerier struct{}

func (emptyQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return emptyRows{}, nil
}

type fallbackCounts struct {
	loadFailures float64
	unloaded     float64
	missing      float64
}

func snapshotFallbackCounts(namespace string) fallbackCounts {
	initMetrics()

	return fallbackCounts{
		loadFailures: testutil.ToFloat64(loadFailuresTotal),
		unloaded:     testutil.ToFloat64(lookupFallbackTotal.WithLabelValues(fallbackReasonUnloaded, namespace)),
		missing:      testutil.ToFloat64(lookupFallbackTotal.WithLabelValues(fallbackReasonMissing, namespace)),
	}
}

func assertFallbackDelta(t *testing.T, before, after, want fallbackCounts) {
	t.Helper()

	got := fallbackCounts{
		loadFailures: after.loadFailures - before.loadFailures,
		unloaded:     after.unloaded - before.unloaded,
		missing:      after.missing - before.missing,
	}
	if got != want {
		t.Fatalf("metric delta = %+v, want %+v", got, want)
	}
}

func TestGetContextCountsUnloadedFallbackAndLoadFailure(t *testing.T) {
	q := &failingQuerier{err: errors.New("connection refused")}
	store := &Store{pool: q, logger: slog.Default(), loadTimeout: time.Second}

	before := snapshotFallbackCounts(NamespaceMisc)
	if got := store.Get(NamespaceMisc, "vtuber_fallback"); got != "" {
		t.Fatalf("Get on failed load = %q, want empty", got)
	}

	assertFallbackDelta(t, before, snapshotFallbackCounts(NamespaceMisc), fallbackCounts{loadFailures: 1, unloaded: 1})

	before = snapshotFallbackCounts(NamespaceMisc)
	if got := store.GetOrContext(t.Context(), NamespaceMisc, "vtuber_fallback", "code-fallback"); got != "code-fallback" {
		t.Fatalf("GetOrContext on failed load = %q, want code-fallback", got)
	}

	assertFallbackDelta(t, before, snapshotFallbackCounts(NamespaceMisc), fallbackCounts{unloaded: 1})

	if q.calls != 1 {
		t.Fatalf("reload attempts = %d, want 1 (retry suppressed within retry interval)", q.calls)
	}
}

func TestLoadCountsFailureWithoutLookupFallback(t *testing.T) {
	q := &failingQuerier{err: errors.New("connection refused")}
	store := &Store{pool: q, logger: slog.Default(), loadTimeout: time.Second}

	before := snapshotFallbackCounts(NamespaceMisc)
	err := store.Load(t.Context())

	if err == nil || !errors.Is(err, q.err) {
		t.Fatalf("Load error = %v, want wrapped %v", err, q.err)
	}

	assertFallbackDelta(t, before, snapshotFallbackCounts(NamespaceMisc), fallbackCounts{loadFailures: 1})
}

func TestLookupFallbackSeriesPreRegisteredAtZero(t *testing.T) {
	initMetrics()

	if got, want := testutil.CollectAndCount(lookupFallbackTotal), 2*len(knownNamespaces); got != want {
		t.Fatalf("lookup_fallback series = %d, want %d (unloaded+missing x every Namespace* constant)", got, want)
	}

	fresh := lookupFallbackTotal.WithLabelValues(fallbackReasonMissing, NamespaceKaring)
	if v := testutil.ToFloat64(fresh); v != 0 {
		t.Fatalf("pre-registered series value = %v, want 0", v)
	}
}

func TestGetContextCountsMissingFallbackWhenLoaded(t *testing.T) {
	store := &Store{pool: emptyQuerier{}, logger: slog.Default(), loadTimeout: time.Second}
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	before := snapshotFallbackCounts(NamespaceOrg)
	if got := store.Get(NamespaceOrg, "nonexistent"); got != "" {
		t.Fatalf("Get missing key = %q, want empty", got)
	}

	assertFallbackDelta(t, before, snapshotFallbackCounts(NamespaceOrg), fallbackCounts{missing: 1})
}

func TestGetContextDoesNotCountHit(t *testing.T) {
	store := &Store{pool: emptyQuerier{}, logger: slog.Default(), loadTimeout: time.Second}
	store.mu.Lock()

	store.cache = map[string]map[string]string{NamespaceOrg: {"Hololive": "Holo"}}
	store.loaded = true
	store.mu.Unlock()

	before := snapshotFallbackCounts(NamespaceOrg)
	if got := store.Get(NamespaceOrg, "Hololive"); got != "Holo" {
		t.Fatalf("Get hit = %q, want Holo", got)
	}

	assertFallbackDelta(t, before, snapshotFallbackCounts(NamespaceOrg), fallbackCounts{})
}
