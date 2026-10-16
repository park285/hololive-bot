package scraper

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type sourceFailureRepository struct {
	fakeEventRepository

	lookup func(context.Context, []string) error
}

func (r *sourceFailureRepository) GetExistingExternalIDs(ctx context.Context, eventType domain.MajorEventType, ids []string) ([]string, error) {
	if err := r.lookup(ctx, ids); err != nil {
		return nil, err
	}

	return r.fakeEventRepository.GetExistingExternalIDs(ctx, eventType, ids)
}

func newSourceFailureService(t *testing.T, repository eventRepository) *Service {
	t.Helper()

	service := newTestScrapeService(t, repository, map[string]string{
		"/a": rssTestFeed(rssTestItem(olderEventID, "Thu, 09 Jan 2025 05:00:00 +0000")),
		"/b": rssTestFeed(rssTestItem(newerEventID, "Fri, 10 Jan 2025 05:00:00 +0000")),
	}, 50)
	// 단일 worker에서 건강한 source가 먼저 끝나도록 해 panic 취소와 부분 성공을 분리한다.
	slices.SortFunc(service.config.Sources, func(a, b FeedSource) int {
		return strings.Compare(a.Name, b.Name)
	})

	return service
}

func TestServiceScrape_AllPanicsAccountForEverySource(t *testing.T) {
	t.Parallel()

	for _, stringPanic := range []bool{false, true} {
		t.Run(fmt.Sprintf("string_panic_%t", stringPanic), func(t *testing.T) {
			t.Parallel()

			firstCause := &pgconn.PgError{Code: "XX000", Message: "first lookup panic"}
			secondCause := errors.New("second lookup panic")

			var entered atomic.Int32

			ready := make(chan struct{})
			repository := &sourceFailureRepository{lookup: func(ctx context.Context, ids []string) error {
				if entered.Add(1) == 2 {
					close(ready)
				}

				select {
				case <-ready:
				case <-ctx.Done():
					if entered.Load() != 2 {
						return ctx.Err()
					}
				}

				if stringPanic {
					panic("lookup panic")
				}

				if ids[0] == olderEventID {
					panic(firstCause)
				}

				panic(secondCause)
			}}
			service := newSourceFailureService(t, repository)

			service.config.FeedConcurrency = 2

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

			defer cancel()

			result, err := service.Scrape(ctx)
			require.Error(t, err)
			require.Equal(t, ScrapeResult{FeedsAttempted: 2, FeedsFailed: 2}, result)
			require.Empty(t, repository.attempts)

			if !stringPanic {
				require.ErrorIs(t, err, firstCause)
				require.ErrorIs(t, err, secondCause)

				pgErr, ok := errors.AsType[*pgconn.PgError](err)
				require.True(t, ok)
				require.Same(t, firstCause, pgErr)
			}
		})
	}
}

func TestServiceScrape_MixedSourceFailurePreservesHealthyWrites(t *testing.T) {
	t.Parallel()

	for _, failure := range []string{"panic", "cancel", "deadline", "database"} {
		for _, failPersistence := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/persistence_failure_%t", failure, failPersistence), func(t *testing.T) {
				t.Parallel()

				cause := sourceTestCause(failure)
				repository := &sourceFailureRepository{lookup: func(_ context.Context, ids []string) error {
					if ids[0] == olderEventID {
						return nil
					}

					if failure == "panic" {
						panic(cause)
					}

					return fmt.Errorf("lookup: %w", cause)
				}}
				persistenceCause := errors.New("save failed")

				if failPersistence {
					repository.failures = map[string]error{olderEventID: persistenceCause}
				}

				service := newSourceFailureService(t, repository)

				result, err := service.Scrape(t.Context())
				require.Equal(t, 2, result.FeedsAttempted)
				require.Equal(t, 1, result.FeedsFailed)
				require.Equal(t, 1, result.ParsedEvents)
				require.Equal(t, []string{olderEventID}, repository.attempts)

				if failPersistence {
					require.ErrorIs(t, err, persistenceCause)
					require.ErrorIs(t, err, cause)
					require.Equal(t, 1, result.FailedEvents)
					require.Zero(t, result.StoredEvents)
				} else {
					require.NoError(t, err)
					require.Zero(t, result.FailedEvents)
					require.Equal(t, 1, result.StoredEvents)
				}
			})
		}
	}
}

func sourceTestCause(failure string) error {
	switch failure {
	case "cancel":
		return context.Canceled
	case "deadline":
		return context.DeadlineExceeded
	default:
		return &pgconn.PgError{Code: "08006", Message: "lookup connection lost"}
	}
}

func TestServiceScrape_AllLookupFailuresPreserveCauses(t *testing.T) {
	t.Parallel()

	for _, failure := range []string{"cancel", "deadline", "database"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()

			cause := sourceTestCause(failure)
			repository := &fakeEventRepository{lookupErr: fmt.Errorf("query existing IDs: %w", cause)}
			service := newSourceFailureService(t, repository)
			result, err := service.Scrape(t.Context())
			require.ErrorIs(t, err, cause)
			require.Equal(t, ScrapeResult{FeedsAttempted: 2, FeedsFailed: 2}, result)
			require.Empty(t, repository.attempts)

			if failure == "database" {
				pgErr, ok := errors.AsType[*pgconn.PgError](err)
				require.True(t, ok)
				require.Same(t, cause, pgErr)
			}
		})
	}
}

func TestServiceScrape_CanceledContextsAccountForQueuedSources(t *testing.T) {
	t.Parallel()

	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprintf("deadline_%t", deadline), func(t *testing.T) {
			t.Parallel()

			repository := &fakeEventRepository{}
			service := newSourceFailureService(t, repository)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			cause := context.Canceled

			if deadline {
				ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				defer cancel()

				cause = context.DeadlineExceeded
			}

			result, err := service.Scrape(ctx)
			require.ErrorIs(t, err, cause)
			require.Equal(t, ScrapeResult{FeedsAttempted: 2, FeedsFailed: 2}, result)
			require.Empty(t, repository.attempts)
		})
	}
}

func TestServiceScrape_PanicStillCancelsSiblingAndAccountsForQueuedSource(t *testing.T) {
	t.Parallel()

	cause := errors.New("lookup panic")
	siblingEntered := make(chan struct{})
	repository := &sourceFailureRepository{lookup: func(ctx context.Context, ids []string) error {
		if ids[0] == olderEventID {
			select {
			case <-siblingEntered:
				panic(cause)
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		close(siblingEntered)
		<-ctx.Done()

		return ctx.Err()
	}}
	service := newSourceFailureService(t, repository)

	service.config.FeedConcurrency = 2
	service.config.Sources = append(service.config.Sources, FeedSource{
		Name: "/queued", EventType: domain.MajorEventTypeNews, FeedURL: service.config.Sources[0].FeedURL,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

	defer cancel()

	result, err := service.Scrape(ctx)
	require.NoError(t, ctx.Err(), "only the errgroup context should be canceled")
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, ScrapeResult{FeedsAttempted: 3, FeedsFailed: 3}, result)
	require.Empty(t, repository.attempts)
}

func TestServiceScrape_LaterLookupFailureDoesNotSalvagePrefix(t *testing.T) {
	t.Parallel()

	cause := errors.New("second chunk lookup failed")
	repository := &sourceFailureRepository{lookup: func(_ context.Context, ids []string) error {
		if slices.Contains(ids, newerEventID) {
			return cause
		}

		return nil
	}}
	service := newTestScrapeService(t, repository, map[string]string{newsFeedPath: rssTestFeed(
		rssTestItem(olderEventID, "Thu, 09 Jan 2025 05:00:00 +0000"),
		rssTestItem(newerEventID, "Fri, 10 Jan 2025 05:00:00 +0000"),
	)}, 1)

	result, err := service.Scrape(t.Context())
	require.ErrorIs(t, err, cause)
	require.Equal(t, ScrapeResult{FeedsAttempted: 1, FeedsFailed: 1}, result)
	require.Empty(t, repository.attempts)
}

func TestServiceScrape_SourceOrderingAndDeduplicationSurviveMixedFailure(t *testing.T) {
	t.Parallel()

	repository := &fakeEventRepository{}
	service := newSourceFailureService(t, repository)
	duplicate := service.config.Sources[1]

	duplicate.Name = "/c"

	broken := FeedSource{Name: "/failure", EventType: domain.MajorEventTypeNews, FeedURL: ":invalid"}

	service.config.Sources = []FeedSource{duplicate, service.config.Sources[1], broken, service.config.Sources[0]}
	service.config.FeedConcurrency = 3

	result, err := service.Scrape(t.Context())
	require.NoError(t, err)
	require.Equal(t, ScrapeResult{FeedsAttempted: 4, FeedsFailed: 1, ParsedEvents: 3, StoredEvents: 2}, result)
	require.Equal(t, []string{olderEventID, newerEventID}, repository.attempts)
}
