package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-shared/pkg/config/settings/apiplane"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func TestShutdownJoinsRetentionAndReplayWorkers(t *testing.T) {
	var (
		retentionTicks atomic.Int64
		replayTicks    atomic.Int64
	)

	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})

	runtime.Config.Retention.Enabled = true
	runtime.Config.Replay.Enabled = true
	runtime.retainer = fakeRetainer{
		tick: func(context.Context, sourceobservation.RetentionConfig, time.Time) (sourceobservation.RetentionResult, error) {
			retentionTicks.Add(1)

			return sourceobservation.RetentionResult{}, nil
		},
	}
	runtime.replayer = fakeReplayer{
		next: func(context.Context) (bool, error) {
			replayTicks.Add(1)

			return false, nil
		},
	}

	runtime.Start(t.Context(), make(chan error, 1))

	if runtime.loopCount != 5 {
		t.Fatalf("loopCount = %d, want 5", runtime.loopCount)
	}

	waitForTicks(t, &retentionTicks)
	waitForTicks(t, &replayTicks)

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestRetentionAndReplayLoopsStayStoppedWhenDisabled(t *testing.T) {
	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})

	runtime.Config.Retention.Enabled = false
	runtime.Config.Replay.Enabled = false
	runtime.Start(t.Context(), make(chan error, 1))

	if runtime.loopCount != 3 {
		t.Fatalf("loopCount = %d, want 3 with only claim/projection/live-end", runtime.loopCount)
	}

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestRetentionTickKeepsSourceWhenProjectionFails(t *testing.T) {
	var sourceTicks atomic.Int64

	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})

	runtime.Config.Retention.Enabled = true
	runtime.Config.Retention.ProjectionRetiredAge = 24 * time.Hour
	runtime.projectionRetainer = fakeProjectionRetainer{
		retain: func(context.Context, time.Time, time.Duration, int) (targetprojection.RetentionResult, error) {
			return targetprojection.RetentionResult{}, errors.New("projection retain failed")
		},
	}
	runtime.retainer = fakeRetainer{
		tick: func(context.Context, sourceobservation.RetentionConfig, time.Time) (sourceobservation.RetentionResult, error) {
			sourceTicks.Add(1)

			return sourceobservation.RetentionResult{Table: "source_observation_queue", Deleted: 1}, nil
		},
	}

	errCh := make(chan error, 1)
	runtime.Start(t.Context(), errCh)
	waitForTicks(t, &sourceTicks)

	select {
	case err := <-errCh:
		t.Fatalf("retention must not kill the process: %v", err)
	default:
	}

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestEvidenceRetentionAgesCoversEveryObservationKind(t *testing.T) {
	age := 24 * time.Hour
	cfg := apiplane.YouTubePlaneRetentionConfig{
		CommunityPageAge:    age,
		VideoListAge:        age,
		ShortsListAge:       age,
		LiveSnapshotAge:     age,
		ViewerSampleAge:     age,
		ChannelStatsAge:     age,
		ChannelProfileAge:   age,
		ChannelPhotoAge:     age,
		ScheduleSnapshotAge: age,
	}
	ages := evidenceRetentionAges(&cfg)
	wantKinds := []contract.ObservationKind{
		contract.KindCommunityPage,
		contract.KindVideoList,
		contract.KindShortsList,
		contract.KindLiveSnapshot,
		contract.KindViewerSample,
		contract.KindChannelStats,
		contract.KindChannelProfile,
		contract.KindChannelPhoto,
		contract.KindSchedule,
	}

	for _, kind := range wantKinds {
		if ages[kind] != age {
			t.Fatalf("retention age for %s = %s, want %s", kind, ages[kind], age)
		}
	}
}

func TestPlaneRetentionConfigIncludesDependentRetention(t *testing.T) {
	cfg := apiplane.YouTubePlaneRetentionConfig{
		ApplicationAuditGrace: 60 * 24 * time.Hour,
		CheckpointHistoryAge:  7 * 24 * time.Hour,
		ViewerSampleAge:       30 * 24 * time.Hour,
		BatchSize:             1000,
	}
	got := planeRetentionConfig(&cfg)

	if got.ApplicationAuditGrace != cfg.ApplicationAuditGrace ||
		got.CheckpointHistoryAge != cfg.CheckpointHistoryAge {
		t.Fatalf("dependent retention config = %#v", got)
	}

	if got.EvidenceAgeByKind[contract.KindViewerSample] != cfg.ViewerSampleAge {
		t.Fatalf("viewer evidence age = %s", got.EvidenceAgeByKind[contract.KindViewerSample])
	}
}

func waitForTicks(t *testing.T, ticks *atomic.Int64) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ticks.Load() > 0 {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("worker tick did not run")
}

type fakeRetainer struct {
	tick func(context.Context, sourceobservation.RetentionConfig, time.Time) (sourceobservation.RetentionResult, error)
}

func (f fakeRetainer) RunRetentionTick(
	ctx context.Context,
	cfg sourceobservation.RetentionConfig,
	now time.Time,
) (sourceobservation.RetentionResult, error) {
	if f.tick == nil {
		return sourceobservation.RetentionResult{}, nil
	}

	out, err := f.tick(ctx, cfg, now)
	if err != nil {
		return out, fmt.Errorf("tick: %w", err)
	}

	return out, nil
}

type fakeProjectionRetainer struct {
	retain func(context.Context, time.Time, time.Duration, int) (targetprojection.RetentionResult, error)
}

func (f fakeProjectionRetainer) Retain(
	ctx context.Context,
	now time.Time,
	age time.Duration,
	batchSize int,
) (targetprojection.RetentionResult, error) {
	if f.retain == nil {
		return targetprojection.RetentionResult{}, nil
	}

	out, err := f.retain(ctx, now, age, batchSize)
	if err != nil {
		return out, fmt.Errorf("retain: %w", err)
	}

	return out, nil
}

type fakeReplayer struct {
	next func(context.Context) (bool, error)
}

func (f fakeReplayer) ProcessNextReplay(ctx context.Context) (bool, error) {
	if f.next == nil {
		return false, nil
	}

	out, err := f.next(ctx)
	if err != nil {
		return out, fmt.Errorf("next: %w", err)
	}

	return out, nil
}
