package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func (r *Runtime) runRetentionLoop(ctx context.Context, errCh chan<- error) {
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
		if r.retentionFailed.CompareAndSwap(true, false) {
			r.publishHealth()
		}

		return false
	}

	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return true
	}

	if !retryableRetentionError(err) {
		r.retentionFailed.Store(true)
		r.publishHealth()
	}

	r.Logger.Error("youtube plane retention failed", slog.Any("error", err))

	return false
}

func (r *Runtime) retentionTick(ctx context.Context) error {
	if r == nil {
		return nil
	}

	return errors.Join(r.retainProjections(ctx), r.retainSource(ctx))
}

func retryableRetentionError(err error) bool {
	phases, ok := errors.AsType[interface {
		error
		Unwrap() []error
	}](err)
	if !ok {
		return retryableObservationError(err)
	}

	return !slices.ContainsFunc(phases.Unwrap(), func(phaseErr error) bool {
		return !retryableObservationError(phaseErr)
	})
}

func (r *Runtime) retainSource(ctx context.Context) error {
	if r.retainer == nil {
		return nil
	}

	var result sourceobservation.RetentionResult

	err := r.withBudgetDB(ctx, dbOperationRetentionSource, func(ctx context.Context) error {
		defer observeSeconds(youtubeRetentionTickSeconds, retentionPhaseSource, time.Now())

		var tickErr error

		result, tickErr = r.retainer.RunRetentionTick(ctx, planeRetentionConfig(&r.Config.Retention), r.now())
		if tickErr != nil {
			return fmt.Errorf("run retention tick: %w", tickErr)
		}

		return nil
	})
	recordRetentionTick(result, err)

	if err != nil {
		return fmt.Errorf("retain source observations: %w", err)
	}

	return nil
}

func (r *Runtime) retainProjections(ctx context.Context) error {
	if r.projectionRetainer == nil || r.Config.Retention.ProjectionRetiredAge <= 0 {
		return nil
	}

	var result targetprojection.RetentionResult

	err := r.withBudgetDB(ctx, dbOperationRetentionProjection, func(ctx context.Context) error {
		defer observeSeconds(youtubeRetentionTickSeconds, retentionPhaseProjection, time.Now())

		var retainErr error

		result, retainErr = r.retainProjectionBatches(ctx, r.now())

		return retainErr
	})
	recordProjectionRetention(result, err)

	if err != nil {
		return fmt.Errorf("retain target projections: %w", err)
	}

	return nil
}

func (r *Runtime) withBudgetDB(ctx context.Context, operation dbOperation, fn func(context.Context) error) error {
	if r.Config.TransactionTimeout <= 0 {
		return errors.New("youtube plane transaction timeout must be positive")
	}

	budgetCtx, cancel := context.WithTimeout(ctx, r.Config.TransactionTimeout)

	defer cancel()

	if operation == dbOperationConsume || operation == dbOperationRetry || operation == dbOperationDeadLetter {
		budgetCtx = pgxdb.WithControlAcquire(budgetCtx)
	}

	return r.withDB(budgetCtx, operation, fn)
}

func planeRetentionConfig(cfg *apiconfig.YouTubePlaneRetentionConfig) sourceobservation.RetentionConfig {
	return sourceobservation.RetentionConfig{
		QueueProcessedAge:     cfg.QueueProcessedAge,
		QueueDLQAge:           cfg.QueueDLQAge,
		CollisionAge:          cfg.CollisionAge,
		LiveAbsenceSlotAge:    cfg.LiveAbsenceSlotAge,
		ContentAbsenceSlotAge: cfg.ContentAbsenceSlotAge,
		CheckpointHistoryAge:  cfg.CheckpointHistoryAge,
		EvidenceAgeByKind:     evidenceRetentionAges(cfg),
		BatchSize:             cfg.BatchSize,
	}
}

func evidenceRetentionAges(cfg *apiconfig.YouTubePlaneRetentionConfig) map[contract.ObservationKind]time.Duration {
	ages := make(map[contract.ObservationKind]time.Duration, 11)
	addEvidenceRetentionAge(ages, contract.KindCommunityPage, cfg.CommunityPageAge)
	addEvidenceRetentionAge(ages, contract.KindVideoList, cfg.VideoListAge)
	addEvidenceRetentionAge(ages, contract.KindShortsList, cfg.ShortsListAge)
	addEvidenceRetentionAge(ages, contract.KindLiveSnapshot, cfg.LiveSnapshotAge)
	addEvidenceRetentionAge(ages, contract.KindChannelProfile, cfg.ChannelProfileAge)
	addEvidenceRetentionAge(ages, contract.KindChannelPhoto, cfg.ChannelPhotoAge)
	addEvidenceRetentionAge(ages, contract.KindSchedule, cfg.ScheduleSnapshotAge)
	addEvidenceRetentionAge(ages, contract.KindChannelLiveCheck, cfg.ChannelLiveCheckAge)
	addEvidenceRetentionAge(ages, contract.KindVideoLiveCheck, cfg.VideoLiveCheckAge)

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

func recordRetentionTick(result sourceobservation.RetentionResult, err error) {
	youtubeRetentionBacklogAgeSeconds.DeleteLabelValues("source_observations")

	for _, part := range retentionParts(result) {
		recordRetentionPart(part)
	}

	if err != nil {
		table := result.FailedTable
		if table == "" {
			table = retentionUnknownTable
		}

		youtubeRetentionErrorsTotal.WithLabelValues(table).Inc()
		youtubeRetentionBacklogAgeSeconds.DeleteLabelValues(table)
	}
}

func retentionParts(result sourceobservation.RetentionResult) []sourceobservation.RetentionResult {
	if len(result.ByTable) > 0 {
		return result.ByTable
	}

	return []sourceobservation.RetentionResult{result}
}

func recordRetentionPart(part sourceobservation.RetentionResult) {
	table := part.Table
	if table == "" {
		return
	}

	if part.Deleted > 0 {
		youtubeRetentionDeletedTotal.WithLabelValues(table).Add(float64(part.Deleted))
	}

	youtubeRetentionSourceScannedTotal.Add(float64(part.Scanned))
	youtubeRetentionSourceTraversalsTotal.Add(float64(part.Traversals))

	if part.BacklogKnown {
		youtubeRetentionBacklogAgeSeconds.WithLabelValues(table).Set(part.BacklogAge.Seconds())
	} else {
		youtubeRetentionBacklogAgeSeconds.DeleteLabelValues(table)
	}
}

func recordProjectionRetention(result targetprojection.RetentionResult, err error) {
	if err != nil {
		youtubeRetentionErrorsTotal.WithLabelValues(projectionRetentionGenerationsTable).Inc()
	}

	if result.LeasesDeleted > 0 {
		youtubeRetentionDeletedTotal.WithLabelValues(projectionRetentionLeasesTable).Add(float64(result.LeasesDeleted))
	}

	if result.ReasonsDeleted > 0 {
		youtubeRetentionDeletedTotal.WithLabelValues(projectionRetentionReasonsTable).Add(float64(result.ReasonsDeleted))
	}

	if result.TargetsDeleted > 0 {
		youtubeRetentionDeletedTotal.WithLabelValues(projectionRetentionTargetsTable).Add(float64(result.TargetsDeleted))
	}

	if result.GenerationsDeleted > 0 {
		youtubeRetentionDeletedTotal.WithLabelValues(projectionRetentionGenerationsTable).Add(float64(result.GenerationsDeleted))
	}
}
