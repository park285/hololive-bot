package scraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFeedSchedulerPartialRetryPreservesRemainingRecovery(t *testing.T) {
	t.Parallel()

	repository := &fakeEventRepository{failures: map[string]error{olderEventID: errors.New("save failed")}}
	service, unavailable := newPartialRetryService(t, repository)
	scheduledAt := time.Date(2026, time.October, 15, 0, 0, 0, 0, time.UTC)
	scheduler, err := NewFeedScheduler(service, FeedScheduleConfig{
		RunTimeout: time.Minute, RetryDelays: []time.Duration{time.Hour, 2 * time.Hour},
	}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	// 기존 regular 부분 성공은 새 retry를 만들지 않는다.
	unavailable.Store(true)

	scheduler.nowFn = func() time.Time { return scheduledAt.Add(time.Minute) }
	scheduler.executeCycle(t.Context(), scrapeTriggerRegular, scheduledAt)
	require.Empty(t, scheduler.retryRuns)
	require.Len(t, repository.upserted, 1)

	unavailable.Store(false)
	scheduler.executeCycle(t.Context(), scrapeTriggerRegular, scheduledAt)
	require.Equal(t, []time.Time{scheduledAt.Add(time.Hour), scheduledAt.Add(2 * time.Hour)}, scheduler.retryRuns)

	// 저장 장애가 해소돼도 같은 feed의 일시적 조회 실패는 복구 완료가 아니다.
	delete(repository.failures, olderEventID)
	unavailable.Store(true)

	retryAt, trigger := scheduler.nextRun(scheduledAt.Add(time.Hour))
	scheduler.executeCycle(t.Context(), trigger, retryAt)
	require.Equal(t, []time.Time{scheduledAt.Add(2 * time.Hour)}, scheduler.retryRuns)
	require.Len(t, repository.upserted, 1)

	unavailable.Store(false)

	retryAt, trigger = scheduler.nextRun(scheduledAt.Add(2 * time.Hour))
	scheduler.executeCycle(t.Context(), trigger, retryAt)
	require.Empty(t, scheduler.retryRuns)
	require.Equal(t, []string{newerEventID, olderEventID, olderEventID}, repository.attempts)
	require.Len(t, repository.upserted, 2)
}

func TestFeedSchedulerRetryCompletionAndExhaustion(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		unavailable bool
	}{
		{name: "complete_clears_remaining"},
		{name: "partial_exhausts_without_reseeding", unavailable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repository := &fakeEventRepository{failures: map[string]error{olderEventID: errors.New("save failed")}}
			service, unavailable := newPartialRetryService(t, repository)
			scheduledAt := time.Date(2026, time.October, 15, 0, 0, 0, 0, time.UTC)
			scheduler, err := NewFeedScheduler(service, FeedScheduleConfig{
				RunTimeout: time.Minute, RetryDelays: []time.Duration{time.Hour, 2 * time.Hour},
			}, slog.New(slog.DiscardHandler))
			require.NoError(t, err)

			scheduler.nowFn = func() time.Time { return scheduledAt.Add(time.Minute) }
			scheduler.executeCycle(t.Context(), scrapeTriggerRegular, scheduledAt)
			require.Len(t, scheduler.retryRuns, 2)

			delete(repository.failures, olderEventID)
			unavailable.Store(test.unavailable)

			retryAt, trigger := scheduler.nextRun(scheduledAt.Add(time.Hour))
			scheduler.executeCycle(t.Context(), trigger, retryAt)

			if !test.unavailable {
				require.Empty(t, scheduler.retryRuns, "complete retry must cancel the remaining scheduled attempt")
				require.Len(t, repository.upserted, 2)

				return
			}

			require.Equal(t, []time.Time{scheduledAt.Add(2 * time.Hour)}, scheduler.retryRuns)

			retryAt, trigger = scheduler.nextRun(scheduledAt.Add(2 * time.Hour))
			scheduler.executeCycle(t.Context(), trigger, retryAt)
			require.Empty(t, scheduler.retryRuns, "partial retries must exhaust the original budget without reseeding")
			require.Len(t, repository.upserted, 1)
		})
	}
}

func TestFeedSchedulerSourceFailuresPreserveOriginalRetryBudget(t *testing.T) {
	t.Parallel()

	for _, failure := range []string{"panic", "cancel", "deadline", "database"} {
		for _, recoverAll := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/recover_%t", failure, recoverAll), func(t *testing.T) {
				t.Parallel()
				runSourceFailureRetryScenario(t, failure, recoverAll)
			})
		}
	}
}

func newPartialRetryService(t *testing.T, repository eventRepository) (*Service, *atomic.Bool) {
	t.Helper()

	unavailable := new(atomic.Bool)
	feeds := map[string]string{
		"/a": rssTestFeed(rssTestItem(olderEventID, "Thu, 09 Jan 2025 05:00:00 +0000")),
		"/b": rssTestFeed(rssTestItem(newerEventID, "Fri, 10 Jan 2025 05:00:00 +0000")),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a" && unavailable.Load() {
			http.Error(w, "temporary feed failure", http.StatusServiceUnavailable)

			return
		}

		_, err := w.Write([]byte(feeds[r.URL.Path]))
		if err != nil {
			t.Errorf("write retry feed: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	service, err := NewService(repository,
		NewFeedFetcher(server.Client(), DefaultFeedFetcherConfig()), NewRSSParser(),
		ServiceConfig{
			Sources: []FeedSource{
				{Name: "a", EventType: domain.MajorEventTypeNews, FeedURL: server.URL + "/a"},
				{Name: "b", EventType: domain.MajorEventTypeNews, FeedURL: server.URL + "/b"},
			},
			FeedConcurrency: 2, IncrementalLimit: 200,
		}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	return service, unavailable
}

func runSourceFailureRetryScenario(t *testing.T, failure string, recoverAll bool) {
	t.Helper()

	cause := sourceTestCause(failure)
	allFail, recovered := false, false
	repository := &sourceFailureRepository{lookup: func(_ context.Context, ids []string) error {
		if recovered || (!allFail && ids[0] == olderEventID) {
			return nil
		}

		if failure == "panic" {
			panic(cause)
		}

		return cause
	}}
	service := newSourceFailureService(t, repository)
	scheduledAt := time.Date(2026, time.October, 15, 0, 0, 0, 0, time.UTC)
	scheduler, err := NewFeedScheduler(service, FeedScheduleConfig{
		RunTimeout: time.Minute, RetryDelays: []time.Duration{time.Hour, 2 * time.Hour, 3 * time.Hour},
	}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	scheduler.nowFn = func() time.Time { return scheduledAt.Add(time.Minute) }

	// 복구된 panic도 regular 부분 성공에서는 새 retry를 예약하지 않는다.
	scheduler.executeCycle(t.Context(), scrapeTriggerRegular, scheduledAt)
	require.Empty(t, scheduler.retryRuns)
	require.Equal(t, []string{olderEventID}, repository.attempts)

	allFail = true

	scheduler.executeCycle(t.Context(), scrapeTriggerRegular, scheduledAt)
	require.Equal(t, []time.Time{
		scheduledAt.Add(time.Hour), scheduledAt.Add(2 * time.Hour), scheduledAt.Add(3 * time.Hour),
	}, scheduler.retryRuns)

	allFail = false

	retryAt, trigger := scheduler.nextRun(scheduledAt.Add(time.Hour))
	require.Equal(t, scrapeTriggerRetry, trigger)
	scheduler.executeCycle(t.Context(), trigger, retryAt)
	require.Equal(t, []time.Time{scheduledAt.Add(2 * time.Hour), scheduledAt.Add(3 * time.Hour)}, scheduler.retryRuns)

	recovered = recoverAll
	retryAt, trigger = scheduler.nextRun(scheduledAt.Add(2 * time.Hour))
	scheduler.executeCycle(t.Context(), trigger, retryAt)

	if recoverAll {
		require.Empty(t, scheduler.retryRuns, "full recovery cancels the last original retry")
		require.Equal(t, []string{olderEventID, newerEventID}, repository.attempts)

		return
	}

	require.Equal(t, []time.Time{scheduledAt.Add(3 * time.Hour)}, scheduler.retryRuns)

	retryAt, trigger = scheduler.nextRun(scheduledAt.Add(3 * time.Hour))
	scheduler.executeCycle(t.Context(), trigger, retryAt)
	require.Empty(t, scheduler.retryRuns, "partial recovery exhausts without reseeding")
	require.Equal(t, []string{olderEventID}, repository.attempts)
}
