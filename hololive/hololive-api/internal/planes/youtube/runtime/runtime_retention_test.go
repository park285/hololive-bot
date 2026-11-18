package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestShutdownJoinsRetentionWorker(t *testing.T) {
	ticked := make(chan struct{})
	recordTick := sync.OnceFunc(func() { close(ticked) })

	runtime := newTestRuntime(t, fakeClaimer{}, fakeConsumer{})

	runtime.retainer = fakeRetainer{
		tick: func(context.Context, sourceobservation.RetentionConfig, time.Time) (sourceobservation.RetentionResult, error) {
			recordTick()

			return sourceobservation.RetentionResult{}, nil
		},
	}
	runtime.Start(t.Context(), make(chan error, 1))

	if runtime.loopCount != 5 {
		t.Fatalf("loopCount = %d, want 5 with claim/projection/live-end/metrics/retention", runtime.loopCount)
	}

	awaitSignal(t, ticked, "retention worker did not run")

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestRetentionTickKeepsSourceWhenProjectionFails(t *testing.T) {
	ticked := make(chan struct{})
	recordTick := sync.OnceFunc(func() { close(ticked) })

	runtime := newTestRuntime(t, fakeClaimer{}, fakeConsumer{})

	runtime.Config.Retention.ProjectionRetiredAge = 24 * time.Hour
	runtime.projectionRetainer = fakeProjectionRetainer{
		retain: func(context.Context, time.Time, time.Duration, int) (targetprojection.RetentionResult, error) {
			return targetprojection.RetentionResult{}, errors.New("projection retain failed")
		},
	}
	runtime.retainer = fakeRetainer{
		tick: func(context.Context, sourceobservation.RetentionConfig, time.Time) (sourceobservation.RetentionResult, error) {
			recordTick()

			return sourceobservation.RetentionResult{Table: "source_observation_queue", Deleted: 1}, nil
		},
	}

	errCh := make(chan error, 1)
	runtime.Start(t.Context(), errCh)
	awaitSignal(t, ticked, "retention worker did not run")

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
	day := 24 * time.Hour
	cfg := apiconfig.YouTubePlaneRetentionConfig{
		CommunityPageAge:    1 * day,
		VideoListAge:        2 * day,
		ShortsListAge:       3 * day,
		LiveSnapshotAge:     4 * day,
		ChannelProfileAge:   7 * day,
		ChannelPhotoAge:     8 * day,
		ScheduleSnapshotAge: 9 * day,
		ChannelLiveCheckAge: 10 * day,
		VideoLiveCheckAge:   11 * day,
	}
	ages := evidenceRetentionAges(&cfg)
	want := map[contract.ObservationKind]time.Duration{
		contract.KindCommunityPage:    cfg.CommunityPageAge,
		contract.KindVideoList:        cfg.VideoListAge,
		contract.KindShortsList:       cfg.ShortsListAge,
		contract.KindLiveSnapshot:     cfg.LiveSnapshotAge,
		contract.KindChannelProfile:   cfg.ChannelProfileAge,
		contract.KindChannelPhoto:     cfg.ChannelPhotoAge,
		contract.KindSchedule:         cfg.ScheduleSnapshotAge,
		contract.KindChannelLiveCheck: cfg.ChannelLiveCheckAge,
		contract.KindVideoLiveCheck:   cfg.VideoLiveCheckAge,
	}

	for kind, age := range want {
		if ages[kind] != age {
			t.Fatalf("retention age for %s = %s, want %s", kind, ages[kind], age)
		}
	}

	for _, kind := range youtubePlaneClaimKinds() {
		if _, ok := ages[kind]; !ok {
			t.Fatalf("claimed kind %s has no evidence retention age", kind)
		}
	}

	cfg.ChannelLiveCheckAge = 0
	if _, ok := evidenceRetentionAges(&cfg)[contract.KindChannelLiveCheck]; ok {
		t.Fatal("disabled channel live check retention must not delete evidence")
	}
}

func TestPlaneRetentionConfigIncludesDependentRetention(t *testing.T) {
	cfg := apiconfig.YouTubePlaneRetentionConfig{
		LiveAbsenceSlotAge:    30 * 24 * time.Hour,
		ContentAbsenceSlotAge: 14 * 24 * time.Hour,
		CheckpointHistoryAge:  7 * 24 * time.Hour,
		BatchSize:             1000,
	}
	got := planeRetentionConfig(&cfg)

	if got.LiveAbsenceSlotAge != cfg.LiveAbsenceSlotAge ||
		got.ContentAbsenceSlotAge != cfg.ContentAbsenceSlotAge ||
		got.CheckpointHistoryAge != cfg.CheckpointHistoryAge {
		t.Fatalf("dependent retention config = %#v", got)
	}
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
