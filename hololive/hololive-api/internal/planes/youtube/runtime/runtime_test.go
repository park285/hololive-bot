package runtime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
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
	if err == nil || !strings.Contains(err.Error(), scraperDatabaseRole) {
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
				return sourceobservation.ClaimedBatch{}, errors.New("serialization_failure")
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

func newTestRuntime(claimer observationClaimer, consumer observationConsumer) *Runtime {
	cfg := settings.DefaultYouTubePlaneConfig()
	cfg.ClaimInterval = 20 * time.Millisecond
	cfg.TargetProjection.Interval = time.Hour
	return &Runtime{
		Config:    cfg,
		Logger:    slog.Default(),
		claimer:   claimer,
		consumer:  consumer,
		refresher: fakeRefresher{},
		builder:   targetprojection.PolicyBuilder{Reader: emptyRosterReader{}, Schedules: targetprojection.DefaultPolicySchedules()},
		now:       func() time.Time { return time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC) },
		dbSem:     make(chan struct{}, cfg.DBOperationConcurrency),
		workCh:    make(chan sourceobservation.Observation, cfg.ConsumerWorkers),
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
