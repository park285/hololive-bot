package runtime

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
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
	for waitTicker(ctx, ticker) {
		if r.stopAfterRetentionError(ctx, errCh, r.retentionTick(ctx)) {
			return
		}
	}
}

func (r *Runtime) stopAfterRetentionError(ctx context.Context, _ chan<- error, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return true
	}
	if !retryableObservationError(err) {
		r.ready.Store(false)
		r.publishHealth()
	}
	r.Logger.Error("youtube plane retention failed", slog.Any("error", err))
	return false
}

func (r *Runtime) retentionTick(ctx context.Context) error {
	if r == nil || !r.Config.Retention.Enabled {
		return nil
	}
	return errors.Join(r.retainProjections(ctx), r.retainSource(ctx))
}

func (r *Runtime) retainSource(ctx context.Context) error {
	if r.retainer == nil {
		return nil
	}
	started := time.Now()
	var result sourceobservation.RetentionResult
	err := r.withRetainDB(ctx, func(ctx context.Context) error {
		var tickErr error
		result, tickErr = r.retainer.RunRetentionTick(ctx, planeRetentionConfig(&r.Config.Retention), r.now())
		return tickErr
	})
	recordRetentionTick(result, time.Since(started), err)
	return err
}

func (r *Runtime) retainProjections(ctx context.Context) error {
	if r.projectionRetainer == nil || r.Config.Retention.ProjectionRetiredAge <= 0 {
		return nil
	}
	started := time.Now()
	var result targetprojection.RetentionResult
	err := r.withRetainDB(ctx, func(ctx context.Context) error {
		var retainErr error
		result, retainErr = r.projectionRetainer.Retain(
			ctx,
			r.now(),
			r.Config.Retention.ProjectionRetiredAge,
			r.Config.Retention.BatchSize,
		)
		return retainErr
	})
	recordProjectionRetention(result, time.Since(started), err)
	return err
}

func (r *Runtime) withRetainDB(ctx context.Context, fn func(context.Context) error) error {
	if r.Config.TransactionTimeout <= 0 {
		return errors.New("youtube plane transaction timeout must be positive")
	}
	retainCtx, cancel := context.WithTimeout(ctx, r.Config.TransactionTimeout)
	defer cancel()
	return r.withDB(retainCtx, fn)
}

func (r *Runtime) runReplayLoop(ctx context.Context, errCh chan<- error) {
	defer func() { r.loopDone <- struct{}{} }()
	ticker := time.NewTicker(r.Config.Replay.Interval)
	defer ticker.Stop()
	if r.stopAfterReplayError(ctx, errCh, r.replayTick(ctx)) {
		return
	}
	for waitTicker(ctx, ticker) {
		if r.stopAfterReplayError(ctx, errCh, r.replayTick(ctx)) {
			return
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

func planeRetentionConfig(cfg *settings.YouTubePlaneRetentionConfig) sourceobservation.RetentionConfig {
	return sourceobservation.RetentionConfig{
		QueueProcessedAge: cfg.QueueProcessedAge,
		QueueDLQAge:       cfg.QueueDLQAge,
		CollisionAge:      cfg.CollisionAge,
		ReplayAuditAge:    cfg.ReplayAuditAge,
		EvidenceAgeByKind: evidenceRetentionAges(cfg),
		BatchSize:         cfg.BatchSize,
	}
}

func evidenceRetentionAges(cfg *settings.YouTubePlaneRetentionConfig) map[contract.ObservationKind]time.Duration {
	ages := make(map[contract.ObservationKind]time.Duration, 9)
	addEvidenceRetentionAge(ages, contract.KindCommunityPage, cfg.CommunityPageAge)
	addEvidenceRetentionAge(ages, contract.KindVideoList, cfg.VideoListAge)
	addEvidenceRetentionAge(ages, contract.KindShortsList, cfg.ShortsListAge)
	addEvidenceRetentionAge(ages, contract.KindLiveSnapshot, cfg.LiveSnapshotAge)
	addEvidenceRetentionAge(ages, contract.KindViewerSample, cfg.ViewerSampleAge)
	addEvidenceRetentionAge(ages, contract.KindChannelStats, cfg.ChannelStatsAge)
	addEvidenceRetentionAge(ages, contract.KindChannelProfile, cfg.ChannelProfileAge)
	addEvidenceRetentionAge(ages, contract.KindChannelPhoto, cfg.ChannelPhotoAge)
	addEvidenceRetentionAge(ages, contract.KindSchedule, cfg.ScheduleSnapshotAge)
	return ages
}

func addEvidenceRetentionAge(
	ages map[contract.ObservationKind]time.Duration,
	kind contract.ObservationKind,
	age time.Duration,
) {
	if age > 0 {
		ages[kind] = age
	}
}

func recordRetentionTick(result sourceobservation.RetentionResult, elapsed time.Duration, err error) {
	youtubeRetentionTickSeconds.Observe(elapsed.Seconds())
	for _, part := range retentionParts(result) {
		recordRetentionPart(part, err)
	}
}

func retentionParts(result sourceobservation.RetentionResult) []sourceobservation.RetentionResult {
	if len(result.ByTable) > 0 {
		return result.ByTable
	}
	return []sourceobservation.RetentionResult{{
		Table: result.Table, Deleted: result.Deleted, BacklogAge: result.BacklogAge,
	}}
}

func recordRetentionPart(part sourceobservation.RetentionResult, err error) {
	table := part.Table
	if table == "" {
		table = "none"
	}
	if err != nil {
		youtubeRetentionErrorsTotal.WithLabelValues(table).Inc()
		return
	}
	if part.Deleted > 0 {
		youtubeRetentionDeletedTotal.WithLabelValues(table).Add(float64(part.Deleted))
	}
	if part.BacklogAge > 0 {
		youtubeRetentionBacklogAgeSeconds.WithLabelValues(table).Set(part.BacklogAge.Seconds())
	}
}

func recordProjectionRetention(result targetprojection.RetentionResult, elapsed time.Duration, err error) {
	youtubeRetentionTickSeconds.Observe(elapsed.Seconds())
	if err != nil {
		youtubeRetentionErrorsTotal.WithLabelValues("youtube_collection_projection_generations").Inc()
		return
	}
	if result.LeasesDeleted > 0 {
		youtubeRetentionDeletedTotal.WithLabelValues("youtube_collection_job_leases").Add(float64(result.LeasesDeleted))
	}
	if result.GenerationsDeleted > 0 {
		youtubeRetentionDeletedTotal.WithLabelValues("youtube_collection_projection_generations").Add(float64(result.GenerationsDeleted))
	}
}
