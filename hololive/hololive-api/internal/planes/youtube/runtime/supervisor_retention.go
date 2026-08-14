package runtime

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func (r *Runtime) runRetentionLoop(ctx context.Context, errCh chan<- error) {
	defer func() { r.loopDone <- struct{}{} }()
	ticker := time.NewTicker(r.Config.Retention.Interval)
	defer ticker.Stop()
	if r.stopAfterRetentionError(ctx, errCh, r.retentionTick(ctx)) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.stopAfterRetentionError(ctx, errCh, r.retentionTick(ctx)) {
				return
			}
		}
	}
}

func (r *Runtime) stopAfterRetentionError(ctx context.Context, errCh chan<- error, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return true
	}
	if retryableObservationError(err) {
		r.Logger.Error("youtube plane retention failed", slog.Any("error", err))
		return false
	}
	r.reportLoopError(ctx, errCh, "retain source observations", err)
	return true
}

func (r *Runtime) retentionTick(ctx context.Context) error {
	if r == nil || r.retainer == nil || !r.Config.Retention.Enabled {
		return nil
	}
	started := time.Now()
	var result sourceobservation.RetentionResult
	err := r.withDB(ctx, func(ctx context.Context) error {
		var tickErr error
		result, tickErr = r.retainer.RunRetentionTick(ctx, planeRetentionConfig(r.Config.Retention), r.now())
		return tickErr
	})
	recordRetentionTick(result, time.Since(started), err)
	return err
}

func (r *Runtime) runReplayLoop(ctx context.Context, errCh chan<- error) {
	defer func() { r.loopDone <- struct{}{} }()
	ticker := time.NewTicker(r.Config.Replay.Interval)
	defer ticker.Stop()
	if r.stopAfterReplayError(ctx, errCh, r.replayTick(ctx)) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.stopAfterReplayError(ctx, errCh, r.replayTick(ctx)) {
				return
			}
		}
	}
}

func (r *Runtime) stopAfterReplayError(ctx context.Context, errCh chan<- error, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return true
	}
	if retryableObservationError(err) {
		r.Logger.Error("youtube plane replay failed", slog.Any("error", err))
		return false
	}
	r.reportLoopError(ctx, errCh, "replay source observations", err)
	return true
}

func (r *Runtime) replayTick(ctx context.Context) error {
	if r == nil || r.replayer == nil || !r.Config.Replay.Enabled {
		return nil
	}
	for i := 0; i < r.Config.Replay.BatchSize; i++ {
		var processed bool
		err := r.withDB(ctx, func(ctx context.Context) error {
			var tickErr error
			processed, tickErr = r.replayer.ProcessNextReplay(ctx)
			return tickErr
		})
		if err != nil || !processed {
			return err
		}
	}
	return nil
}

func planeRetentionConfig(cfg settings.YouTubePlaneRetentionConfig) sourceobservation.RetentionConfig {
	return sourceobservation.RetentionConfig{
		QueueProcessedAge: cfg.QueueProcessedAge,
		QueueDLQAge:       cfg.QueueDLQAge,
		CollisionAge:      cfg.CollisionAge,
		ReplayAuditAge:    cfg.ReplayAuditAge,
		EvidenceAgeByKind: inventoriedEvidenceAges(cfg),
		BatchSize:         cfg.BatchSize,
	}
}

func inventoriedEvidenceAges(cfg settings.YouTubePlaneRetentionConfig) map[contract.ObservationKind]time.Duration {
	ages := make(map[contract.ObservationKind]time.Duration, 3)
	if cfg.ChannelStatsAge > 0 {
		ages[contract.KindChannelStats] = cfg.ChannelStatsAge
	}
	if cfg.LiveSnapshotAge > 0 {
		ages[contract.KindLiveSnapshot] = cfg.LiveSnapshotAge
	}
	if cfg.ViewerSampleAge > 0 {
		ages[contract.KindViewerSample] = cfg.ViewerSampleAge
	}
	return ages
}

func recordRetentionTick(result sourceobservation.RetentionResult, elapsed time.Duration, err error) {
	table := result.Table
	if table == "" {
		table = "none"
	}
	youtubeRetentionTickSeconds.Observe(elapsed.Seconds())
	if err != nil {
		youtubeRetentionErrorsTotal.WithLabelValues(table).Inc()
		return
	}
	if result.Deleted > 0 {
		youtubeRetentionDeletedTotal.WithLabelValues(table).Add(float64(result.Deleted))
	}
	youtubeRetentionBacklogAgeSeconds.WithLabelValues(table).Set(result.BacklogAge.Seconds())
}
