package runtime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func TestBuildFailsClosedOnInvalidBudget(t *testing.T) {
	t.Parallel()
	cfg := settings.DefaultYouTubePlaneConfig()
	cfg.DBOperationConcurrency = cfg.PostgresPoolMaxConns
	_, err := Build(context.Background(), cfg, settings.PostgresConfig{User: "hololive_runtime"}, slog.Default())
	if err == nil || !strings.Contains(err.Error(), "leave one pool connection reserved") {
		t.Fatalf("Build() = %v", err)
	}
}

func TestBuildFailsClosedOnScraperRole(t *testing.T) {
	t.Parallel()
	_, err := Build(context.Background(), settings.DefaultYouTubePlaneConfig(), settings.PostgresConfig{User: scraperDatabaseRole}, slog.Default())
	if err == nil || !strings.Contains(err.Error(), runtimeDatabaseRole) {
		t.Fatalf("Build() = %v", err)
	}
}

func TestBuildFailsClosedOnUnexpectedDatabaseRole(t *testing.T) {
	t.Parallel()
	_, err := Build(context.Background(), settings.DefaultYouTubePlaneConfig(), settings.PostgresConfig{User: "postgres_admin"}, slog.Default())
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
			return sourceobservation.ClaimedBatch{Observations: []sourceobservation.Observation{{
				ID:              7,
				LeaseToken:      strings.Repeat("ab", 32),
				ObservationKind: contract.KindCommunityPage,
				SubjectKey:      "UC_TEST",
			}}}, nil
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Observation, string) error {
			select {
			case <-entered:
			default:
				close(entered)
			}
			<-release
			return nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Start(ctx, make(chan error, 1))
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start consume")
	}

	done := make(chan error, 1)
	go func() {
		done <- runtime.Shutdown(context.Background())
	}()
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
	if err := runtime.Shutdown(context.Background()); err != nil {
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
	runtime.Start(context.Background(), errCh)

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
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestShutdownReleasesUnsentClaimsAfterCanceledContext(t *testing.T) {
	var claimed atomic.Bool
	var retried atomic.Int64
	entered := make(chan struct{})
	release := make(chan struct{})
	claimer := fakeClaimer{
		claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
			if claimed.Swap(true) {
				return sourceobservation.ClaimedBatch{}, nil
			}
			return sourceobservation.ClaimedBatch{Observations: []sourceobservation.Observation{
				{ID: 1, LeaseToken: strings.Repeat("ab", 32), ObservationKind: contract.KindCommunityPage, SubjectKey: "UC_A"},
				{ID: 2, LeaseToken: strings.Repeat("cd", 32), ObservationKind: contract.KindCommunityPage, SubjectKey: "UC_B"},
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
		consume: func(context.Context, sourceobservation.Observation, string) error {
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
	runtime.workCh = make(chan sourceobservation.Observation)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Start(ctx, make(chan error, 1))
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start")
	}
	done := make(chan error, 1)
	go func() {
		done <- runtime.Shutdown(context.Background())
	}()
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

func TestShutdownReturnsErrorWhenWorkerDoesNotJoin(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var retried atomic.Int64
	runtime := newTestRuntime(fakeClaimer{
		claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
			return sourceobservation.ClaimedBatch{Observations: []sourceobservation.Observation{{
				ID:              9,
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
		consume: func(context.Context, sourceobservation.Observation, string) error {
			close(entered)
			<-release
			return nil
		},
	})
	runtime.Config.ShutdownTimeout = 30 * time.Millisecond
	runtime.Start(context.Background(), make(chan error, 1))
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start")
	}

	err := runtime.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "worker") {
		t.Fatalf("Shutdown() error = %v, want worker join timeout", err)
	}
	if retried.Load() != 0 {
		t.Fatalf("ambiguous active observation retried %d time(s)", retried.Load())
	}
	close(release)
	select {
	case <-runtime.workerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit after test release")
	}
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
			return sourceobservation.ClaimedBatch{Observations: []sourceobservation.Observation{
				{ID: 10, LeaseToken: strings.Repeat("ab", 32), ObservationKind: contract.KindCommunityPage, SubjectKey: "UC_A"},
				{ID: 11, LeaseToken: strings.Repeat("cd", 32), ObservationKind: contract.KindCommunityPage, SubjectKey: "UC_B"},
			}}, nil
		},
		retry: func(_ context.Context, input sourceobservation.RetryInput) (contract.Status, error) {
			if input.ObservationID != 11 {
				t.Fatalf("retry id = %d, want unsent 11", input.ObservationID)
			}
			return "", errors.New("release failed")
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Observation, string) error {
			close(entered)
			<-release
			return nil
		},
	})
	runtime.Config.ConsumerWorkers = 1
	runtime.workCh = make(chan sourceobservation.Observation)
	runtime.Start(context.Background(), make(chan error, 1))
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start")
	}
	done := make(chan error, 1)
	go func() { done <- runtime.Shutdown(context.Background()) }()
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
	if err := runtime.refreshProjection(context.Background()); err != nil {
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
		consume: func(context.Context, sourceobservation.Observation, string) error {
			return &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}
		},
	})
	observation := sourceobservation.Observation{
		ID:              12,
		LeaseToken:      strings.Repeat("ab", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_RETRY",
	}

	if err := runtime.processObservation(context.Background(), observation); err != nil {
		t.Fatalf("processObservation() error = %v", err)
	}
	if retryInput.ObservationID != observation.ID || retryInput.Delay != runtime.Config.ClaimInterval {
		t.Fatalf("Retry input = %#v", retryInput)
	}
}

func TestUnknownConsumeErrorDoesNotRetry(t *testing.T) {
	var retries atomic.Int64
	runtime := newTestRuntime(fakeClaimer{
		retry: func(context.Context, sourceobservation.RetryInput) (contract.Status, error) {
			retries.Add(1)
			return contract.StatusPending, nil
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Observation, string) error {
			return errors.New("unexpected canonical write failure")
		},
	})
	observation := sourceobservation.Observation{
		ID:              13,
		LeaseToken:      strings.Repeat("cd", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_FATAL",
	}

	err := runtime.processObservation(context.Background(), observation)
	if err == nil || !strings.Contains(err.Error(), "unexpected canonical write failure") {
		t.Fatalf("processObservation() error = %v", err)
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
		consume: func(context.Context, sourceobservation.Observation, string) error {
			return context.Canceled
		},
	})
	observation := sourceobservation.Observation{
		ID:              15,
		LeaseToken:      strings.Repeat("ab", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_CANCELED",
	}

	if err := runtime.processObservation(context.Background(), observation); !errors.Is(err, context.Canceled) {
		t.Fatalf("processObservation() error = %v, want context.Canceled", err)
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
		consume: func(context.Context, sourceobservation.Observation, string) error {
			return &pgconn.PgError{Code: "40001", Message: "serialization failure"}
		},
	})
	observation := sourceobservation.Observation{
		ID:              16,
		LeaseToken:      strings.Repeat("cd", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_DLQ",
	}

	if err := runtime.processObservation(context.Background(), observation); err != nil {
		t.Fatalf("processObservation() error = %v", err)
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
		consume: func(context.Context, sourceobservation.Observation, string) error {
			return sourceobservation.ErrClaimLost
		},
	})
	observation := sourceobservation.Observation{
		ID:              14,
		LeaseToken:      strings.Repeat("ef", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_LOST",
	}

	if err := runtime.processObservation(context.Background(), observation); err != nil {
		t.Fatalf("processObservation() error = %v", err)
	}
	if retries.Load() != 0 {
		t.Fatalf("lost claim retried %d time(s)", retries.Load())
	}
}

func TestRuntimePublishesProcessReadiness(t *testing.T) {
	health.RemoveComponent(youtubeHealthComponent)
	t.Cleanup(func() { health.RemoveComponent(youtubeHealthComponent) })
	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})
	runtime.degraded.Store(true)
	runtime.Start(context.Background(), make(chan error, 1))

	response, ready := health.GetReadiness()
	component := response.Components[youtubeHealthComponent]
	if !ready || !component.Ready || !component.Degraded {
		t.Fatalf("started readiness = (%#v, %v)", response, ready)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	response, ready = health.GetReadiness()
	if ready || response.Components[youtubeHealthComponent].Ready {
		t.Fatalf("shutdown readiness = (%#v, %v)", response, ready)
	}
}

func newTestRuntime(claimer observationClaimer, consumer observationConsumer) *Runtime {
	cfg := settings.DefaultYouTubePlaneConfig()
	cfg.ClaimInterval = 20 * time.Millisecond
	cfg.TargetProjection.Interval = time.Hour
	return &Runtime{
		Config:     cfg,
		Logger:     slog.Default(),
		claimer:    claimer,
		consumer:   consumer,
		refresher:  fakeRefresher{},
		builder:    targetprojection.PolicyBuilder{Reader: emptyRosterReader{}, Schedules: targetprojection.DefaultPolicySchedules()},
		now:        func() time.Time { return time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC) },
		dbSem:      make(chan struct{}, cfg.DBOperationConcurrency),
		workCh:     make(chan sourceobservation.Observation, cfg.ConsumerWorkers),
		loopDone:   make(chan struct{}, 2),
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
	return f.claim(ctx, options)
}

func (fakeClaimer) ProbeClaim(context.Context, sourceobservation.ClaimOptions) error {
	return nil
}

func (fakeClaimer) EnsureClaimBudget(context.Context, sourceobservation.Claim, time.Duration) error {
	return nil
}

func (f fakeClaimer) Retry(ctx context.Context, input sourceobservation.RetryInput) (contract.Status, error) {
	if f.retry != nil {
		return f.retry(ctx, input)
	}
	return contract.StatusPending, nil
}

type fakeConsumer struct {
	consume func(context.Context, sourceobservation.Observation, string) error
}

func (f fakeConsumer) ConsumeObservation(ctx context.Context, observation sourceobservation.Observation, consumerName string) error {
	if f.consume == nil {
		return nil
	}
	return f.consume(ctx, observation, consumerName)
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
