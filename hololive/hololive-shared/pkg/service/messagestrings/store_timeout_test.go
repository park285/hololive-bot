package messagestrings

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type blockingQuerier struct {
	queried chan struct{}
}

func (q *blockingQuerier) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	select {
	case q.queried <- struct{}{}:
	default:
	}
	<-ctx.Done()
	err := ctx.Err()
	if err == nil {
		err = context.Canceled
	}
	return nil, err
}

func newBlockingStore(timeout time.Duration) (*Store, *blockingQuerier) {
	q := &blockingQuerier{queried: make(chan struct{}, 8)}
	return &Store{pool: q, logger: slog.Default(), loadTimeout: timeout}, q
}

func TestEnsureLoadedBoundsLazyLoadTimeout(t *testing.T) {
	store, q := newBlockingStore(50 * time.Millisecond)

	start := time.Now()
	got := store.Get(NamespaceMisc, "x")
	elapsed := time.Since(start)

	if got != "" {
		t.Fatalf("Get during failed load = %q, want empty", got)
	}
	if elapsed > time.Second {
		t.Fatalf("ensureLoaded blocked %v, want bounded near loadTimeout", elapsed)
	}
	select {
	case <-q.queried:
	default:
		t.Fatal("reload query was never attempted")
	}
}

func TestEnsureLoadedDoesNotPinReloadMuBeyondTimeout(t *testing.T) {
	store, _ := newBlockingStore(100 * time.Millisecond)

	const callers = 4
	done := make(chan time.Duration, callers)
	for range callers {
		go func() {
			start := time.Now()
			store.Get(NamespaceMisc, "x")
			done <- time.Since(start)
		}()
	}

	for range callers {
		select {
		case d := <-done:
			if d > 2*time.Second {
				t.Fatalf("caller blocked %v on reloadMu, want bounded", d)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("caller never returned — reloadMu pinned by timeout-less query (hang regression)")
		}
	}
}
