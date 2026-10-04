package collectorruntime

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func TestRetrySchedulePreservesExplicitCooldown(t *testing.T) {
	t.Parallel()

	cfg := collectorconfig.DefaultConfig()
	executor := &collectionExecutor{retryBounds: collection.RetryBounds{Minimum: cfg.RetryMin, Maximum: cfg.RetryMax}}
	at := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)

	for _, test := range []struct {
		name string
		err  error
		at   time.Time
	}{
		{name: "429 delta seconds", err: collecterr.FromStatus("holodex", http.StatusTooManyRequests, "600", time.Now())},
		{name: "429 HTTP date beyond one hour", err: collecterr.FromStatus("holodex", http.StatusTooManyRequests, at.Format(http.TimeFormat), time.Now()), at: at},
		{name: "503 HTTP date", err: collecterr.FromStatus("official", http.StatusServiceUnavailable, at.Format(http.TimeFormat), time.Now()), at: at},
		{name: "CooldownUntil", err: collecterr.CooldownUntil("helper cooldown", at), at: at},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			before := time.Now().UTC()
			schedule, err := executor.retrySchedule(test.err)
			after := time.Now().UTC()

			if err != nil {
				t.Fatal(err)
			}

			if !schedule.IsNotBefore() || !schedule.At().After(after.Add(cfg.RetryMax)) {
				t.Fatalf("cooldown shortened to bounded retry: %s", schedule.At())
			}

			if !test.at.IsZero() {
				if !schedule.At().Equal(test.at) {
					t.Fatalf("deadline = %s, want %s", schedule.At(), test.at)
				}
			} else if schedule.At().Before(before.Add(10*time.Minute)) || schedule.At().After(after.Add(10*time.Minute+time.Microsecond)) {
				t.Fatalf("delta deadline = %s, want now + 10m", schedule.At())
			}
		})
	}
}

func TestFailedRunPersistsProviderRetryAfterBeyondMaximum(t *testing.T) {
	for _, dateHeader := range []bool{false, true} {
		name := "delta seconds"

		if dateHeader {
			name = "HTTP date"
		}

		t.Run(name, func(t *testing.T) {
			var fatal []error

			runner := stubJob(contract.ProviderYouTubeJS, testCommunityJobKind, contract.KindCommunityPage)

			var requested time.Time

			runner.collect = func(context.Context, *collection.RunInput) (collection.CollectResult, error) {
				requested = time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second)

				header := "600"

				if dateHeader {
					header = requested.Format(http.TimeFormat)
				}

				return collection.CollectResult{}, collecterr.FromStatus("holodex", http.StatusTooManyRequests, header, time.Now())
			}

			executor, spec := newExecutorFixture(t, runner, &fatal)
			cfg := collectorconfig.DefaultConfig()

			executor.retryBounds = collection.RetryBounds{Minimum: cfg.RetryMin, Maximum: cfg.RetryMax}
			executor.runSpec(t.Context(), spec)

			var stored, databaseNow time.Time

			if err := executor.publisher.contracts.pool.QueryRow(t.Context(), `
				SELECT retry_not_before, clock_timestamp() FROM youtube_collection_job_leases WHERE job_key = $1
			`, spec.JobKey).Scan(&stored, &databaseNow); err != nil {
				t.Fatal(err)
			}

			if stored.Before(requested) || !stored.After(databaseNow.Add(cfg.RetryMax)) || (dateHeader && !stored.Equal(requested)) {
				t.Fatalf("stored deadline = %s, requested %s, DB now %s", stored, requested, databaseNow)
			}

			if _, err := executor.repository.Acquire(t.Context(), spec, "other-collector"); !errors.Is(err, joblease.ErrNotAcquired) {
				t.Fatalf("provider cooldown reacquired early: %v", err)
			}

			if len(fatal) != 0 {
				t.Fatalf("cooldown promoted to fatal: %v", fatal)
			}
		})
	}
}

func TestRetryScheduleKeepsOrdinaryAndInvalidHintsBounded(t *testing.T) {
	t.Parallel()

	cfg := collectorconfig.DefaultConfig()
	executor := &collectionExecutor{retryBounds: collection.RetryBounds{Minimum: cfg.RetryMin, Maximum: cfg.RetryMax}}

	for _, header := range []string{"", " ", "0", "-1", "NaN", "Inf", "1.5", "2147483648", "999999999999999999999"} {
		err := collecterr.FromStatus("holodex", http.StatusTooManyRequests, header, time.Now())
		assertOrdinaryRetryDelay(t, executor, err, cfg.RetryMin+(cfg.RetryMax-cfg.RetryMin)/2)
	}

	hint, err := collecterr.NewRetryAfterHint(2 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	for _, code := range []collecterr.ErrorCode{collecterr.Failed, collecterr.HelperBusy} {
		cause := collecterr.WithRetry(collecterr.New(code, collecterr.ClassTransient, "ordinary failure"), hint)
		assertOrdinaryRetryDelay(t, executor, cause, cfg.RetryMax)
	}

	assertOrdinaryRetryDelay(t, executor, collecterr.New(collecterr.Failed, collecterr.ClassTransient, "ordinary failure"), cfg.RetryMin+(cfg.RetryMax-cfg.RetryMin)/2)
}

func assertOrdinaryRetryDelay(t *testing.T, executor *collectionExecutor, cause error, delay time.Duration) {
	t.Helper()

	before := time.Now().UTC()
	schedule, err := executor.retrySchedule(cause)
	after := time.Now().UTC()

	if err != nil {
		t.Fatal(err)
	}

	if schedule.IsNotBefore() || schedule.At().Before(before.Add(delay)) || schedule.At().After(after.Add(delay)) {
		t.Fatalf("ordinary retry = %s, want bounded now + %s", schedule.At(), delay)
	}
}

func TestRetryScheduleCooldownPastUsesMinimumAndInvalidBoundsFail(t *testing.T) {
	t.Parallel()

	executor := &collectionExecutor{retryBounds: collection.RetryBounds{Minimum: time.Second, Maximum: time.Minute}}
	cause := collecterr.CooldownUntil("past cooldown", time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC))
	before := time.Now().UTC()
	schedule, err := executor.retrySchedule(cause)
	after := time.Now().UTC()

	if err != nil {
		t.Fatal(err)
	}

	if !schedule.IsNotBefore() || schedule.At().Before(before.Add(time.Second)) || schedule.At().After(after.Add(time.Second+time.Microsecond)) {
		t.Fatalf("past cooldown = %s, want now + minimum", schedule.At())
	}

	executor.retryBounds.Maximum = 2 * time.Hour
	if _, err := executor.retrySchedule(cause); err == nil {
		t.Fatal("explicit cooldown bypassed invalid retry bounds")
	}
}
