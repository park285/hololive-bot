package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConcurrentSessionReadsPreserveFamilyIdentity(t *testing.T) {
	store, _ := newTestStore(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	families := make([]Session, 16)
	for index := range families {
		created, err := store.Create(ctx)
		require.NoError(t, err)

		families[index] = created
	}

	const workers = 32

	start := make(chan struct{})
	results := make(chan error, workers)

	var work sync.WaitGroup

	for index := range workers {
		work.Go(func() {
			<-start

			results <- readFamilyRepeatedly(ctx, store, families[index%len(families)])
		})
	}

	close(start)
	work.Wait()
	close(results)

	for err := range results {
		require.NoError(t, err)
	}
}

func readFamilyRepeatedly(ctx context.Context, store *Store, family Session) error {
	for range 8 {
		actual, ok, err := store.Get(ctx, family.ID)
		if err != nil {
			return fmt.Errorf("read concurrent session: %w", err)
		}

		if !ok || actual.ID != family.ID || actual.FamilyID != family.FamilyID {
			return errors.New("concurrent session response mixed or lost its family")
		}

		active, err := store.FamilyActive(ctx, family.FamilyID)
		if err != nil {
			return fmt.Errorf("read concurrent family lease: %w", err)
		}

		if !active {
			return errors.New("concurrent family lease disappeared")
		}
	}

	return nil
}

func TestConcurrentLoginFailuresRetainEveryIncrement(t *testing.T) {
	limiter := newTestDistributedLoginLimiter(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	const workers = 32

	type result struct {
		count int
		err   error
	}

	start := make(chan struct{})
	results := make(chan result, workers)

	var work sync.WaitGroup

	for range workers {
		work.Go(func() {
			<-start

			count, err := limiter.RecordFailure(ctx, "203.0.113.20", "admin")
			results <- result{count: count, err: err}
		})
	}

	close(start)
	work.Wait()
	close(results)

	seen := make(map[int]bool, workers)

	for actual := range results {
		require.NoError(t, actual.err)
		require.GreaterOrEqual(t, actual.count, 1)
		require.LessOrEqual(t, actual.count, workers)
		require.False(t, seen[actual.count], "each atomic increment must have a distinct count")

		seen[actual.count] = true
	}

	retry, err := limiter.Check(ctx, "203.0.113.20", "admin")
	require.NoError(t, err)
	require.Positive(t, retry)
}
