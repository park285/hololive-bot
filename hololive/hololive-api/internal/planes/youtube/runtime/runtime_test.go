package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/shared-go/v2/pkg/health"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/apiplane"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func TestRuntimeClaimsLiveViewerAndScheduleKinds(t *testing.T) {
	t.Parallel()

	got := make(map[contract.ObservationKind]bool, 9)

	for _, kind := range youtubePlaneClaimKinds() {
		got[kind] = true
	}

	for _, kind := range []contract.ObservationKind{
		contract.KindCommunityPage, contract.KindVideoList, contract.KindShortsList,
		contract.KindLiveSnapshot, contract.KindViewerSample, contract.KindSchedule,
		contract.KindChannelStats, contract.KindChannelProfile, contract.KindChannelPhoto,
	} {
		if !got[kind] {
			t.Fatalf("missing claim kind %s", kind)
		}
	}
}

func TestBuildFailsClosedOnInvalidBudget(t *testing.T) {
	t.Parallel()

	cfg := apiplane.DefaultYouTubePlaneConfig()

	cfg.DBOperationConcurrency = cfg.PostgresPoolMaxConns

	_, err := Build(t.Context(), &cfg, &settings.PostgresConfig{User: "hololive_runtime"}, slog.Default())

	if err == nil || !strings.Contains(err.Error(), "leave one pool connection reserved") {
		t.Fatalf("Build() = %v", err)
	}
}

func TestBuildFailsClosedOnScraperRole(t *testing.T) {
	t.Parallel()

	cfg := apiplane.DefaultYouTubePlaneConfig()
	_, err := Build(t.Context(), &cfg, &settings.PostgresConfig{User: scraperDatabaseRole}, slog.Default())

	if err == nil || !strings.Contains(err.Error(), runtimeDatabaseRole) {
		t.Fatalf("Build() = %v", err)
	}
}

func TestBuildFailsClosedOnUnexpectedDatabaseRole(t *testing.T) {
	t.Parallel()

	cfg := apiplane.DefaultYouTubePlaneConfig()
	_, err := Build(t.Context(), &cfg, &settings.PostgresConfig{User: "postgres_admin"}, slog.Default())

	if err == nil || !strings.Contains(err.Error(), runtimeDatabaseRole) {
		t.Fatalf("Build() = %v", err)
	}
}

func TestShutdownStopsClaimAndJoinsWorkers(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})

	var claims atomic.Int64

	runtime := newTestRuntime(fakeClaimer{
		claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
			claims.Add(1)

			return sourceobservation.ClaimedBatch{Claims: []sourceobservation.ClaimWork{{
				ObservationID:   7,
				LeaseToken:      strings.Repeat("ab", 32),
				ObservationKind: contract.KindCommunityPage,
				SubjectKey:      "UC_TEST",
			}}}, nil
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			select {
			case <-entered:
			default:
				close(entered)
			}

			<-release

			return nil
		},
	})

	ctx := t.Context()
	runtime.Start(ctx, make(chan error, 1))

	awaitSignal(t, entered, "worker did not start consume")

	done := shutdownAsync(t, runtime)

	select {
	case err := <-done:
		t.Fatalf("shutdown returned before worker joined: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	held := claims.Load()

	time.Sleep(30 * time.Millisecond)

	if claims.Load() != held {
		t.Fatalf("claim continued after shutdown: before=%d after=%d", held, claims.Load())
	}

	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not join workers")
	}

	if runtime.Ready() {
		t.Fatal("runtime stayed ready after shutdown")
	}
}

func TestFirstClaimTickTransientErrorDoesNotKillProcess(t *testing.T) {
	var attempts atomic.Int64

	errCh := make(chan error, 1)
	runtime := newTestRuntime(fakeClaimer{
		claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
			if attempts.Add(1) == 1 {
				return sourceobservation.ClaimedBatch{}, &pgconn.PgError{Code: "40001", Message: "serialization failure"}
			}

			return sourceobservation.ClaimedBatch{}, nil
		},
	}, fakeConsumer{})
	ctx := t.Context()
	runtime.Start(ctx, errCh)
	time.Sleep(80 * time.Millisecond)

	select {
	case err := <-errCh:
		t.Fatalf("transient first claim killed the process: %v", err)
	default:
	}

	if attempts.Load() < 2 {
		t.Fatalf("claim loop stopped after transient error: attempts=%d", attempts.Load())
	}

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestUnknownClaimErrorFailsSupervisor(t *testing.T) {
	errCh := make(chan error, 1)
	runtime := newTestRuntime(fakeClaimer{
		claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
			return sourceobservation.ClaimedBatch{}, errors.New("unexpected claim failure")
		},
	}, fakeConsumer{})
	runtime.Start(t.Context(), errCh)

	select {
	case err := <-errCh:
		if !strings.Contains(err.Error(), "claim community observations") {
			t.Fatalf("supervisor error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unknown claim error did not fail the supervisor")
	}

	if runtime.Ready() {
		t.Fatal("runtime stayed ready after supervisor failure")
	}

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestShutdownReleasesUnsentClaimsAfterCanceledContext(t *testing.T) {
	var (
		claimed atomic.Bool
		retried atomic.Int64
	)

	entered := make(chan struct{})
	release := make(chan struct{})
	claimer := fakeClaimer{
		claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
			if claimed.Swap(true) {
				return sourceobservation.ClaimedBatch{}, nil
			}

			return sourceobservation.ClaimedBatch{Claims: []sourceobservation.ClaimWork{
				{ObservationID: 1, LeaseToken: strings.Repeat("ab", 32), ObservationKind: contract.KindCommunityPage, SubjectKey: "UC_A"},
				{ObservationID: 2, LeaseToken: strings.Repeat("cd", 32), ObservationKind: contract.KindCommunityPage, SubjectKey: "UC_B"},
			}}, nil
		},
		retry: func(_ context.Context, input sourceobservation.RetryInput) (contract.Status, error) {
			retried.Add(1)

			if input.ObservationID != 2 {
				t.Fatalf("retry id = %d, want unsent 2", input.ObservationID)
			}

			return contract.StatusPending, nil
		},
	}
	runtime := newTestRuntime(claimer, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			select {
			case <-entered:
			default:
				close(entered)
			}

			<-release

			return nil
		},
	})

	runtime.Config.ConsumerWorkers = 1
	runtime.workCh = make(chan sourceobservation.ClaimWork)

	ctx := t.Context()
	runtime.Start(ctx, make(chan error, 1))

	awaitSignal(t, entered, "worker did not start")

	done := shutdownAsync(t, runtime)

	time.Sleep(20 * time.Millisecond)
	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish")
	}

	if retried.Load() < 1 {
		t.Fatal("unsent claim was not retried after canceled shutdown context")
	}
}

func TestShutdownReleasesInFlightWhenWorkerDoesNotJoin(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})

	var retried atomic.Int64

	runtime := newTestRuntime(fakeClaimer{
		claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
			return sourceobservation.ClaimedBatch{Claims: []sourceobservation.ClaimWork{{
				ObservationID:   9,
				LeaseToken:      strings.Repeat("ef", 32),
				ObservationKind: contract.KindCommunityPage,
				SubjectKey:      "UC_TIMEOUT",
			}}}, nil
		},
		retry: func(context.Context, sourceobservation.RetryInput) (contract.Status, error) {
			retried.Add(1)

			return contract.StatusPending, nil
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			close(entered)
			<-release

			return nil
		},
	})

	runtime.Config.ShutdownTimeout = 30 * time.Millisecond
	runtime.Start(t.Context(), make(chan error, 1))

	awaitSignal(t, entered, "worker did not start")

	err := runtime.Shutdown(t.Context())
	if err == nil || !strings.Contains(err.Error(), "worker") {
		t.Fatalf("Shutdown() error = %v, want worker join timeout", err)
	}

	if retried.Load() != 1 {
		t.Fatalf("active observation release attempts = %d, want 1", retried.Load())
	}

	close(release)

	awaitSignal(t, runtime.workerDone, "worker did not exit after test release")
}

func TestShutdownReturnsReleaseFailure(t *testing.T) {
	var claimed atomic.Bool

	entered := make(chan struct{})
	release := make(chan struct{})
	runtime := newTestRuntime(fakeClaimer{
		claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
			if claimed.Swap(true) {
				return sourceobservation.ClaimedBatch{}, nil
			}

			return sourceobservation.ClaimedBatch{Claims: []sourceobservation.ClaimWork{
				{ObservationID: 10, LeaseToken: strings.Repeat("ab", 32), ObservationKind: contract.KindCommunityPage, SubjectKey: "UC_A"},
				{ObservationID: 11, LeaseToken: strings.Repeat("cd", 32), ObservationKind: contract.KindCommunityPage, SubjectKey: "UC_B"},
			}}, nil
		},
		retry: func(_ context.Context, input sourceobservation.RetryInput) (contract.Status, error) {
			if input.ObservationID != 11 {
				t.Fatalf("retry id = %d, want unsent 11", input.ObservationID)
			}

			return "", errors.New("release failed")
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			close(entered)
			<-release

			return nil
		},
	})

	runtime.Config.ConsumerWorkers = 1
	runtime.workCh = make(chan sourceobservation.ClaimWork)
	runtime.Start(t.Context(), make(chan error, 1))

	awaitSignal(t, entered, "worker did not start")

	done := shutdownAsync(t, runtime)

	time.Sleep(20 * time.Millisecond)
	close(release)

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "release failed") {
			t.Fatalf("Shutdown() error = %v, want release failure", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish")
	}
}

func TestEmptyViewerRosterDoesNotFailProjectionRefresh(t *testing.T) {
	t.Parallel()

	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})

	runtime.builder = targetprojection.PolicyBuilder{
		Reader:    emptyRosterReader{},
		Schedules: targetprojection.DefaultPolicySchedules(),
	}
	runtime.refresher = fakeRefresher{}

	if err := runtime.refreshProjection(t.Context()); err != nil {
		t.Fatalf("empty viewer roster refresh: %v", err)
	}

	if runtime.Degraded() {
		t.Fatal("empty viewer roster must not degrade the plane")
	}
}

func TestTransientConsumeErrorUsesBoundedQueueRetry(t *testing.T) {
	var retryInput sourceobservation.RetryInput

	runtime := newTestRuntime(fakeClaimer{
		retry: func(_ context.Context, input sourceobservation.RetryInput) (contract.Status, error) {
			retryInput = input
			return contract.StatusPending, nil
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			return &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}
		},
	})
	observation := sourceobservation.ClaimWork{
		ObservationID:   12,
		LeaseToken:      strings.Repeat("ab", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_RETRY",
	}

	if err := runtime.processClaim(t.Context(), observation); err != nil {
		t.Fatalf("processClaim() error = %v", err)
	}

	if retryInput.ObservationID != observation.ObservationID || retryInput.Delay != runtime.Config.ClaimInterval {
		t.Fatalf("Retry input = %#v", retryInput)
	}
}

func TestClaimLoopImmediatelyDrainsFullBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	secondClaim := make(chan struct{})

	var calls atomic.Int64

	runtime := newTestRuntime(fakeClaimer{
		claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
			if calls.Add(1) == 2 {
				close(secondClaim)
				cancel()

				return sourceobservation.ClaimedBatch{}, nil
			}

			claims := make([]sourceobservation.ClaimWork, runtimeClaimBatchSizeForTest)
			for i := range claims {
				claims[i] = sourceobservation.ClaimWork{ObservationID: int64(i + 1)}
			}

			return sourceobservation.ClaimedBatch{Claims: claims}, nil
		},
	}, fakeConsumer{})

	runtime.Config.ClaimInterval = time.Hour
	runtime.claim.Limit = runtimeClaimBatchSizeForTest
	runtime.workCh = make(chan sourceobservation.ClaimWork, runtimeClaimBatchSizeForTest)
	runtime.claiming.Store(true)

	go runtime.runClaimLoop(ctx, make(chan error, 1))

	select {
	case <-secondClaim:
	case <-time.After(time.Second):
		t.Fatal("full claim batch did not trigger an immediate follow-up claim")
	}
}

const runtimeClaimBatchSizeForTest = 4

func TestUnknownConsumeErrorDoesNotRetry(t *testing.T) {
	var retries atomic.Int64

	runtime := newTestRuntime(fakeClaimer{
		retry: func(context.Context, sourceobservation.RetryInput) (contract.Status, error) {
			retries.Add(1)

			return contract.StatusPending, nil
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			return errors.New("unexpected canonical write failure")
		},
	})
	observation := sourceobservation.ClaimWork{
		ObservationID:   13,
		LeaseToken:      strings.Repeat("cd", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_FATAL",
	}

	err := runtime.processClaim(t.Context(), observation)
	if err == nil || !strings.Contains(err.Error(), "unexpected canonical write failure") {
		t.Fatalf("processClaim() error = %v", err)
	}

	if retries.Load() != 0 {
		t.Fatalf("unknown error retried %d time(s)", retries.Load())
	}
}

func TestCanceledConsumeWithoutLifecycleCancellationFailsClosed(t *testing.T) {
	var retries atomic.Int64

	runtime := newTestRuntime(fakeClaimer{
		retry: func(context.Context, sourceobservation.RetryInput) (contract.Status, error) {
			retries.Add(1)

			return contract.StatusPending, nil
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			return context.Canceled
		},
	})
	observation := sourceobservation.ClaimWork{
		ObservationID:   15,
		LeaseToken:      strings.Repeat("ab", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_CANCELED",
	}

	if err := runtime.processClaim(t.Context(), observation); !errors.Is(err, context.Canceled) {
		t.Fatalf("processClaim() error = %v, want context.Canceled", err)
	}

	if retries.Load() != 0 {
		t.Fatalf("unowned cancellation retried %d time(s)", retries.Load())
	}
}

func TestRetryDeadLetterDegradesPlane(t *testing.T) {
	runtime := newTestRuntime(fakeClaimer{
		retry: func(context.Context, sourceobservation.RetryInput) (contract.Status, error) {
			return contract.StatusDeadLetter, nil
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			return &pgconn.PgError{Code: "40001", Message: "serialization failure"}
		},
	})
	observation := sourceobservation.ClaimWork{
		ObservationID:   16,
		LeaseToken:      strings.Repeat("cd", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_DLQ",
	}

	if err := runtime.processClaim(t.Context(), observation); err != nil {
		t.Fatalf("processClaim() error = %v", err)
	}

	if !runtime.Degraded() {
		t.Fatal("DEAD_LETTER retry outcome did not degrade the plane")
	}
}

func TestClaimLostDoesNotRetry(t *testing.T) {
	var retries atomic.Int64

	runtime := newTestRuntime(fakeClaimer{
		retry: func(context.Context, sourceobservation.RetryInput) (contract.Status, error) {
			retries.Add(1)

			return contract.StatusPending, nil
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			return sourceobservation.ErrClaimLost
		},
	})
	observation := sourceobservation.ClaimWork{
		ObservationID:   14,
		LeaseToken:      strings.Repeat("ef", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_LOST",
	}

	if err := runtime.processClaim(t.Context(), observation); err != nil {
		t.Fatalf("processClaim() error = %v", err)
	}

	if retries.Load() != 0 {
		t.Fatalf("lost claim retried %d time(s)", retries.Load())
	}
}

func TestForgetStaleClaimDoesNotDeleteNewToken(t *testing.T) {
	t.Parallel()

	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})
	oldWork := sourceobservation.ClaimWork{ObservationID: 21, LeaseToken: strings.Repeat("ab", 32)}
	newWork := sourceobservation.ClaimWork{ObservationID: 21, LeaseToken: strings.Repeat("cd", 32)}

	runtime.remember(oldWork)
	runtime.remember(newWork)

	runtime.forget(oldWork)

	if _, ok := runtime.inFlight.Load(oldWork.Key()); ok {
		t.Fatal("stale claim remained in flight after exact forget")
	}

	if got, ok := runtime.inFlight.Load(newWork.Key()); !ok || got != newWork {
		t.Fatalf("new claim = %#v, present=%t", got, ok)
	}
}

func TestRuntimePublishesProcessReadiness(t *testing.T) {
	health.RemoveComponent(youtubeHealthComponent)
	t.Cleanup(func() { health.RemoveComponent(youtubeHealthComponent) })

	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})
	runtime.degraded.Store(true)
	runtime.Start(t.Context(), make(chan error, 1))

	response, ready := health.GetReadiness()
	component := response.Components[youtubeHealthComponent]

	if !ready || !component.Ready || !component.Degraded {
		t.Fatalf("started readiness = (%#v, %v)", response, ready)
	}

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	response, ready = health.GetReadiness()
	if ready || response.Components[youtubeHealthComponent].Ready {
		t.Fatalf("shutdown readiness = (%#v, %v)", response, ready)
	}
}

func shutdownAsync(t *testing.T, runtime *Runtime) <-chan error {
	t.Helper()

	ctx := t.Context()
	done := make(chan error, 1)

	go func() { done <- runtime.Shutdown(ctx) }()

	return done
}

func awaitSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

func newTestRuntime(claimer observationClaimer, consumer observationConsumer) *Runtime {
	cfg := apiplane.DefaultYouTubePlaneConfig()

	cfg.ClaimInterval = 20 * time.Millisecond
	cfg.TargetProjection.Interval = time.Hour

	return &Runtime{
		Config:     cfg,
		Logger:     slog.Default(),
		claimer:    claimer,
		consumer:   consumer,
		refresher:  fakeRefresher{},
		builder:    targetprojection.PolicyBuilder{Reader: emptyRosterReader{}, Schedules: targetprojection.DefaultPolicySchedules()},
		now:        func() time.Time { return time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC) },
		dbSem:      make(chan struct{}, cfg.DBOperationConcurrency),
		workCh:     make(chan sourceobservation.ClaimWork, cfg.ConsumerWorkers),
		loopDone:   make(chan struct{}, youtubeSupervisorLoopCapacity),
		workerDone: make(chan struct{}, cfg.ConsumerWorkers),
		claim: sourceobservation.ClaimOptions{
			ConsumerName:  communityConsumerName,
			LeaseOwner:    communityLeaseOwner,
			Kinds:         []contract.ObservationKind{contract.KindCommunityPage},
			Limit:         cfg.ClaimBatchSize,
			LeaseDuration: cfg.ClaimLease,
		},
	}
}

type fakeClaimer struct {
	claim func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error)
	retry func(context.Context, sourceobservation.RetryInput) (contract.Status, error)
}

func (f fakeClaimer) ClaimBatch(ctx context.Context, options sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
	if f.claim == nil {
		return sourceobservation.ClaimedBatch{}, nil
	}

	out, err := f.claim(ctx, options)
	if err != nil {
		return out, fmt.Errorf("claim: %w", err)
	}

	return out, nil
}

func (fakeClaimer) ProbeClaim(context.Context, sourceobservation.ClaimOptions) error {
	return nil
}

func (fakeClaimer) EnsureClaimBudget(context.Context, sourceobservation.Claim, time.Duration) error {
	return nil
}

func (f fakeClaimer) Retry(ctx context.Context, input sourceobservation.RetryInput) (contract.Status, error) {
	if f.retry != nil {
		out, err := f.retry(ctx, input)
		if err != nil {
			return out, fmt.Errorf("retry: %w", err)
		}

		return out, nil
	}

	return contract.StatusPending, nil
}

type fakeConsumer struct {
	consume func(context.Context, sourceobservation.Claim) error
}

func (f fakeConsumer) ConsumeClaim(ctx context.Context, claim sourceobservation.Claim) error {
	if f.consume == nil {
		return nil
	}

	if err := f.consume(ctx, claim); err != nil {
		return fmt.Errorf("consume: %w", err)
	}

	return nil
}

type fakeRefresher struct{}

func (fakeRefresher) Refresh(context.Context, targetprojection.Builder, time.Time) (targetprojection.Result, error) {
	return targetprojection.Result{}, nil
}

type emptyRosterReader struct{}

func (emptyRosterReader) NotificationChannelIDs(context.Context, dbx.Tx) ([]string, error) {
	return nil, nil
}

func (emptyRosterReader) OperationalChannelIDs(context.Context, dbx.Tx) ([]string, error) {
	return nil, nil
}

func (emptyRosterReader) ViewerVideoIDs(context.Context, dbx.Tx) ([]string, error) {
	return nil, nil
}
