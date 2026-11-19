package matcher

import (
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func newBoundTestMatcher() *Matcher {
	return &Matcher{
		logger:                slog.New(slog.DiscardHandler),
		matchCache:            make(map[string]*MatchCacheEntry),
		matchCacheTTL:         time.Minute,
		matchCacheLastCleanup: time.Now(),
	}
}

func TestStoreMatch_EnforcesSizeBound(t *testing.T) {
	mm := newBoundTestMatcher()

	total := matchCacheMaxEntries + 500
	for i := range total {
		mm.storeMatch(fmt.Sprintf("match:q-%d", i), &domain.Channel{ID: fmt.Sprintf("ch-%d", i)}, nil)
	}

	mm.matchCacheMu.RLock()

	size := len(mm.matchCache)
	mm.matchCacheMu.RUnlock()

	if size > matchCacheMaxEntries {
		t.Fatalf("matchCache exceeded cap: size=%d cap=%d", size, matchCacheMaxEntries)
	}
}

func TestStoreMatch_EvictsOldestWhenFull(t *testing.T) {
	mm := newBoundTestMatcher()
	base := time.Now()

	for i := range matchCacheMaxEntries {
		key := fmt.Sprintf("match:k-%d", i)

		mm.matchCache[key] = &MatchCacheEntry{
			Channel:   &domain.Channel{ID: key},
			Timestamp: base.Add(time.Duration(i) * time.Millisecond),
		}
	}

	oldestKey := "match:k-0"

	mm.storeMatch("match:overflow", &domain.Channel{ID: "overflow"}, nil)

	mm.matchCacheMu.RLock()
	defer mm.matchCacheMu.RUnlock()

	if len(mm.matchCache) > matchCacheMaxEntries {
		t.Fatalf("matchCache exceeded cap after overflow insert: size=%d cap=%d", len(mm.matchCache), matchCacheMaxEntries)
	}

	if _, ok := mm.matchCache["match:overflow"]; !ok {
		t.Fatal("expected newly stored entry to be present")
	}

	if _, ok := mm.matchCache[oldestKey]; ok {
		t.Fatalf("expected oldest entry %q to be evicted", oldestKey)
	}
}

func TestStoreMatch_PrefersExpiredEviction(t *testing.T) {
	mm := newBoundTestMatcher()
	base := time.Now()

	for i := range matchCacheMaxEntries {
		key := fmt.Sprintf("match:k-%d", i)
		ts := base.Add(time.Duration(i) * time.Millisecond)

		if i == 5 {
			ts = base.Add(-2 * mm.matchCacheTTL)
		}

		mm.matchCache[key] = &MatchCacheEntry{
			Channel:   &domain.Channel{ID: key},
			Timestamp: ts,
		}
	}

	mm.storeMatch("match:overflow", &domain.Channel{ID: "overflow"}, nil)

	mm.matchCacheMu.RLock()
	defer mm.matchCacheMu.RUnlock()

	if _, ok := mm.matchCache["match:k-5"]; ok {
		t.Fatal("expected expired entry match:k-5 to be evicted before oldest")
	}

	if _, ok := mm.matchCache["match:k-0"]; !ok {
		t.Fatal("expected non-expired oldest entry match:k-0 to survive when an expired entry exists")
	}
}
